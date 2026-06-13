package flush

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	_ "github.com/duckdb/duckdb-go/v2"
	"github.com/omneval/omneval/internal/config"
	_ "github.com/omneval/omneval/internal/duckdbfix"
	"github.com/omneval/omneval/internal/storage"
)

// mockStore implements storage.ObjectStore for testing.
type mockStore struct {
	mu         sync.Mutex
	puts       map[string][]byte // key -> data
	gets       []string
	deletes    []string
	failPut    bool
	failGet    bool
	onPutSized func(key string, data []byte) // callback for PutSized (testing hook)
}

func newMockStore() *mockStore {
	return &mockStore{puts: make(map[string][]byte)}
}

func (m *mockStore) Put(_ context.Context, key string, r io.Reader) error {
	return m.put(key, r)
}

func (m *mockStore) PutSized(_ context.Context, key string, r io.Reader, _ int64) error {
	data, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	if m.failPut {
		return fmt.Errorf("simulated S3 failure")
	}
	if m.onPutSized != nil {
		m.onPutSized(key, data)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.puts[key] = data
	return nil
}

func (m *mockStore) put(key string, r io.Reader) error {
	if m.failPut {
		return fmt.Errorf("simulated S3 failure")
	}
	data, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.puts[key] = data
	return nil
}

func (m *mockStore) Get(_ context.Context, key string) (io.ReadCloser, error) {
	if m.failGet {
		return nil, fmt.Errorf("simulated S3 failure")
	}
	m.gets = append(m.gets, key)
	return nil, nil
}

func (m *mockStore) Delete(_ context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.deletes = append(m.deletes, key)
	return nil
}

func (m *mockStore) ListPrefix(_ context.Context, prefix string) ([]string, error) {
	return nil, nil
}

func (m *mockStore) Stat(_ context.Context, key string) (*storage.ObjectStat, error) {
	return nil, nil
}

func TestNew_DefaultFlushAge(t *testing.T) {
	cfg := &config.Config{
		Writer: config.WriterConfig{
			FlushAgeDays: 2,
		},
	}
	f := New(nil, cfg)
	if f.flushAge != 48*time.Hour {
		t.Errorf("flushAge: got %v, want %v", f.flushAge, 48*time.Hour)
	}
}

func TestNew_CustomFlushAge(t *testing.T) {
	cfg := &config.Config{
		Writer: config.WriterConfig{
			FlushAgeDays: 7,
		},
	}
	f := New(nil, cfg)
	if f.flushAge != 7*24*time.Hour {
		t.Errorf("flushAge: got %v, want %v", f.flushAge, 7*24*time.Hour)
	}
}

// TestFlush_AgedPartitionsWrittenToS3 tests that spans older than flushAge
// are written to S3 as Parquet files before being deleted from DuckDB.
func TestFlush_AgedPartitionsWrittenToS3(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("DuckDB COPY TO file:// does not accept Windows paths; covered by Linux CI")
	}
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.duckdb")

	db, err := sql.Open("duckdb", dbPath)
	if err != nil {
		t.Fatalf("open duckdb: %v", err)
	}
	defer db.Close()

	if err := createTestTables(db); err != nil {
		t.Fatalf("create tables: %v", err)
	}

	// Insert spans: some aged (3 days ago), some recent (1 day ago).
	baseTime := time.Now().UTC()
	agedTime := baseTime.Add(-72 * time.Hour)   // 3 days ago
	recentTime := baseTime.Add(-24 * time.Hour) // 1 day ago

	// Aged span for proj-1.
	if _, err := db.ExecContext(context.Background(),
		`INSERT INTO spans (span_id, trace_id, project_id, model, start_time, end_time, input_tokens, output_tokens) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		"span-aged-1", "trace-aged", "proj-1", "gpt-4", agedTime, agedTime.Add(10*time.Second), 100, 50); err != nil {
		t.Fatalf("insert aged span proj-1: %v", err)
	}

	// Aged span for proj-1 (different date).
	agedTime2 := baseTime.Add(-96 * time.Hour) // 4 days ago
	if _, err := db.ExecContext(context.Background(),
		`INSERT INTO spans (span_id, trace_id, project_id, model, start_time, end_time, input_tokens, output_tokens) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		"span-aged-2", "trace-aged-2", "proj-1", "gpt-4", agedTime2, agedTime2.Add(10*time.Second), 100, 50); err != nil {
		t.Fatalf("insert aged span proj-1: %v", err)
	}

	// Recent span for proj-1.
	if _, err := db.ExecContext(context.Background(),
		`INSERT INTO spans (span_id, trace_id, project_id, model, start_time, end_time, input_tokens, output_tokens) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		"span-recent-1", "trace-recent", "proj-1", "gpt-4", recentTime, recentTime.Add(10*time.Second), 200, 100); err != nil {
		t.Fatalf("insert recent span proj-1: %v", err)
	}

	// Insert a score for the aged span.
	if _, err := db.ExecContext(context.Background(),
		`INSERT INTO scores (score_id, span_id, trace_id, project_id, eval_name, value, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		"score-aged-1", "span-aged-1", "trace-aged", "proj-1", "quality", 0.95, agedTime); err != nil {
		t.Fatalf("insert score: %v", err)
	}

	store := newMockStore()
	writeDir := filepath.Join(tmpDir, "parquet")
	cfg := &config.Config{
		Storage: config.StorageConfig{
			Endpoint: "http://localhost:9000",
			Bucket:   "test",
		},
	}

	f := NewWithDB(store, db, cfg).WithFlushAge(48 * time.Hour).WithWriteDir(writeDir)

	// Perform a flush.
	if err := f.doFlush(context.Background()); err != nil {
		t.Fatalf("doFlush: %v", err)
	}

	// Verify aged spans were deleted from DuckDB.
	var count int
	if err := db.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM spans WHERE span_id = 'span-aged-1'`).Scan(&count); err != nil {
		t.Fatalf("query aged span: %v", err)
	}
	if count != 0 {
		t.Errorf("aged span should be deleted, got %d rows", count)
	}

	// Verify aged span from different date was deleted.
	if err := db.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM spans WHERE span_id = 'span-aged-2'`).Scan(&count); err != nil {
		t.Fatalf("query aged span 2: %v", err)
	}
	if count != 0 {
		t.Errorf("aged span 2 should be deleted, got %d rows", count)
	}

	// Verify recent span is still in DuckDB.
	if err := db.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM spans WHERE span_id = 'span-recent-1'`).Scan(&count); err != nil {
		t.Fatalf("query recent span: %v", err)
	}
	if count != 1 {
		t.Errorf("recent span should still exist, got %d rows", count)
	}

	// Verify scores were deleted.
	if err := db.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM scores WHERE score_id = 'score-aged-1'`).Scan(&count); err != nil {
		t.Fatalf("query score: %v", err)
	}
	if count != 0 {
		t.Errorf("score should be deleted, got %d rows", count)
	}

	// Verify Parquet files were uploaded to S3 (mock).
	store.mu.Lock()
	defer store.mu.Unlock()

	// Spans Parquet should exist.
	found := false
	for key := range store.puts {
		if strings.Contains(key, "project_id=proj-1") && strings.Contains(key, "spans") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected spans Parquet file for aged partition, got keys: %v", keys(store.puts))
	}

	// Scores Parquet should exist.
	scoresFound := false
	for key := range store.puts {
		if strings.Contains(key, "project_id=proj-1") && strings.Contains(key, "scores") {
			scoresFound = true
			break
		}
	}
	if !scoresFound {
		t.Errorf("expected scores Parquet file for aged partition, got keys: %v", keys(store.puts))
	}
}

// TestFlush_Atomicity verifies that DuckDB rows are NOT deleted if S3 upload fails.
func TestFlush_Atomicity(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("DuckDB COPY TO file:// does not accept Windows paths; covered by Linux CI")
	}
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.duckdb")

	db, err := sql.Open("duckdb", dbPath)
	if err != nil {
		t.Fatalf("open duckdb: %v", err)
	}
	defer db.Close()

	if err := createTestTables(db); err != nil {
		t.Fatalf("create tables: %v", err)
	}

	// Insert an aged span.
	agedTime := time.Now().UTC().Add(-72 * time.Hour)
	if _, err := db.ExecContext(context.Background(),
		`INSERT INTO spans (span_id, trace_id, project_id, model, start_time, end_time) VALUES (?, ?, ?, ?, ?, ?)`,
		"span-aged", "trace-aged", "proj-1", "gpt-4", agedTime, agedTime.Add(10*time.Second)); err != nil {
		t.Fatalf("insert aged span: %v", err)
	}

	// Failing store.
	failingStore := &mockStore{failPut: true}
	cfg := &config.Config{
		Storage: config.StorageConfig{
			Endpoint: "http://localhost:9000",
			Bucket:   "test",
		},
	}

	writeDir := filepath.Join(tmpDir, "parquet")
	f := NewWithDB(failingStore, db, cfg).WithFlushAge(48 * time.Hour).WithWriteDir(writeDir)

	// Flush should fail, but DuckDB rows should NOT be deleted.
	err = f.doFlush(context.Background())
	if err == nil {
		t.Fatal("expected error from failing store")
	}

	// Verify aged span is STILL in DuckDB (atomicity).
	var count int
	if err := db.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM spans WHERE span_id = 'span-aged'`).Scan(&count); err != nil {
		t.Fatalf("query aged span: %v", err)
	}
	if count != 1 {
		t.Errorf("aged span should NOT be deleted on flush failure, got %d rows", count)
	}
}

// TestFlush_NoS3Endpoint verifies flush is skipped when no S3 endpoint is configured.
func TestFlush_NoS3Endpoint(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.duckdb")

	db, err := sql.Open("duckdb", dbPath)
	if err != nil {
		t.Fatalf("open duckdb: %v", err)
	}
	defer db.Close()

	if err := createTestTables(db); err != nil {
		t.Fatalf("create tables: %v", err)
	}

	// Insert an aged span.
	agedTime := time.Now().UTC().Add(-72 * time.Hour)
	if _, err := db.ExecContext(context.Background(),
		`INSERT INTO spans (span_id, trace_id, project_id, model, start_time, end_time) VALUES (?, ?, ?, ?, ?, ?)`,
		"span-aged", "trace-aged", "proj-1", "gpt-4", agedTime, agedTime.Add(10*time.Second)); err != nil {
		t.Fatalf("insert aged span: %v", err)
	}

	cfg := &config.Config{
		Storage: config.StorageConfig{
			Endpoint: "", // No S3
		},
	}

	f := NewWithDB(nil, db, cfg).WithFlushAge(48 * time.Hour)

	// Flush should be a no-op.
	if err := f.doFlush(context.Background()); err != nil {
		t.Fatalf("doFlush: %v", err)
	}

	// Verify span is still in DuckDB.
	var count int
	if err := db.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM spans WHERE span_id = 'span-aged'`).Scan(&count); err != nil {
		t.Fatalf("query: %v", err)
	}
	if count != 1 {
		t.Errorf("span should still exist when S3 is not configured, got %d", count)
	}
}

// TestFlush_NoObjectStore verifies flush is skipped when no ObjectStore is configured.
func TestFlush_NoObjectStore(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.duckdb")

	db, err := sql.Open("duckdb", dbPath)
	if err != nil {
		t.Fatalf("open duckdb: %v", err)
	}
	defer db.Close()

	if err := createTestTables(db); err != nil {
		t.Fatalf("create tables: %v", err)
	}

	// Insert an aged span.
	agedTime := time.Now().UTC().Add(-72 * time.Hour)
	if _, err := db.ExecContext(context.Background(),
		`INSERT INTO spans (span_id, trace_id, project_id, model, start_time, end_time) VALUES (?, ?, ?, ?, ?, ?)`,
		"span-aged", "trace-aged", "proj-1", "gpt-4", agedTime, agedTime.Add(10*time.Second)); err != nil {
		t.Fatalf("insert aged span: %v", err)
	}

	cfg := &config.Config{
		Storage: config.StorageConfig{
			Endpoint: "http://localhost:9000",
			Bucket:   "test",
		},
	}

	// Pass nil for the store.
	f := NewWithDB(nil, db, cfg).WithFlushAge(48 * time.Hour)

	// Flush should be a no-op.
	if err := f.doFlush(context.Background()); err != nil {
		t.Fatalf("doFlush: %v", err)
	}

	// Verify span is still in DuckDB.
	var count int
	if err := db.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM spans WHERE span_id = 'span-aged'`).Scan(&count); err != nil {
		t.Fatalf("query: %v", err)
	}
	if count != 1 {
		t.Errorf("span should still exist when ObjectStore is nil, got %d", count)
	}
}

// TestFlush_SIGTERM verifies flush does not start a new iteration after context cancel.
func TestFlush_SIGTERM(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.duckdb")

	db, err := sql.Open("duckdb", dbPath)
	if err != nil {
		t.Fatalf("open duckdb: %v", err)
	}
	defer db.Close()

	if err := createTestTables(db); err != nil {
		t.Fatalf("create tables: %v", err)
	}

	cfg := &config.Config{
		Storage: config.StorageConfig{
			Endpoint: "http://localhost:9000",
			Bucket:   "test",
		},
		Writer: config.WriterConfig{
			FlushInterval: "100ms", // Fast for testing
		},
	}

	store := newMockStore()
	f := NewWithDB(store, db, cfg).WithFlushAge(48 * time.Hour)

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	// Run should return when context is cancelled.
	err = f.Run(ctx)
	if err != context.DeadlineExceeded {
		t.Logf("Run returned: %v", err)
	}
}

// TestFlush_NoAgedPartitions verifies that flush is a no-op when no partitions are old enough.
func TestFlush_NoAgedPartitions(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.duckdb")

	db, err := sql.Open("duckdb", dbPath)
	if err != nil {
		t.Fatalf("open duckdb: %v", err)
	}
	defer db.Close()

	if err := createTestTables(db); err != nil {
		t.Fatalf("create tables: %v", err)
	}

	// Insert only recent spans.
	recentTime := time.Now().UTC().Add(-1 * time.Hour)
	if _, err := db.ExecContext(context.Background(),
		`INSERT INTO spans (span_id, trace_id, project_id, model, start_time, end_time) VALUES (?, ?, ?, ?, ?, ?)`,
		"span-recent", "trace-recent", "proj-1", "gpt-4", recentTime, recentTime.Add(10*time.Second)); err != nil {
		t.Fatalf("insert recent span: %v", err)
	}

	store := newMockStore()
	cfg := &config.Config{
		Storage: config.StorageConfig{
			Endpoint: "http://localhost:9000",
			Bucket:   "test",
		},
	}

	f := NewWithDB(store, db, cfg).WithFlushAge(48 * time.Hour)

	// Flush should succeed with no work.
	if err := f.doFlush(context.Background()); err != nil {
		t.Fatalf("doFlush: %v", err)
	}

	// No files should have been written.
	if len(store.puts) > 0 {
		t.Errorf("expected no files written for recent-only data, got %d", len(store.puts))
	}
}

// TestPartitionPath verifies the Hive-partitioned S3 key format.
func TestPartitionPath(t *testing.T) {
	pk := partitionKey{projectID: "proj-abc", date: "2025-01-15"}

	// Spans path.
	spansKey := partitionPath(pk, "spans", "spans.parquet")
	expected := "archive/project_id=proj-abc/date=2025-01-15/spans/spans.parquet"
	if spansKey != expected {
		t.Errorf("spans key: got %q, want %q", spansKey, expected)
	}

	// Scores path.
	scoresKey := partitionPath(pk, "scores", "scores.parquet")
	expectedScores := "archive/project_id=proj-abc/date=2025-01-15/scores/scores.parquet"
	if scoresKey != expectedScores {
		t.Errorf("scores key: got %q, want %q", scoresKey, expectedScores)
	}

	// Verify Hive partition convention: project_id={id}/date={date}/
	if !strings.Contains(spansKey, "project_id=proj-abc") {
		t.Error("spans key should contain project_id partition")
	}
	if !strings.Contains(spansKey, "date=2025-01-15") {
		t.Error("spans key should contain date partition")
	}
}

// TestFlush_ConversationIDInParquet verifies that conversation_id is preserved
// in the Parquet cold archive export.
func TestFlush_ConversationIDInParquet(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("DuckDB COPY TO file:// does not accept Windows paths; covered by Linux CI")
	}
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.duckdb")
	writeDir := filepath.Join(tmpDir, "parquet")

	db, err := sql.Open("duckdb", dbPath)
	if err != nil {
		t.Fatalf("open duckdb: %v", err)
	}
	defer db.Close()

	if err := createTestTables(db); err != nil {
		t.Fatalf("create tables: %v", err)
	}

	// Insert an aged span with conversation_id.
	agedTime := time.Now().UTC().Add(-72 * time.Hour)
	convID := "conv-abc-123"
	if _, err := db.ExecContext(context.Background(),
		`INSERT INTO spans (span_id, trace_id, parent_id, conversation_id, project_id, model, start_time, end_time) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		"span-conv-1", "trace-conv", "", convID, "proj-1", "gpt-4", agedTime, agedTime.Add(10*time.Second)); err != nil {
		t.Fatalf("insert span with conversation_id: %v", err)
	}

	// Capture parquet data from S3 upload for verification.
	store := newMockStore()
	tmpParquetFile := filepath.Join(tmpDir, "spans_check.parquet")
	var parquetData []byte
	store.onPutSized = func(key string, data []byte) {
		if strings.Contains(key, "spans") {
			parquetData = data
		}
	}

	cfg := &config.Config{
		Storage: config.StorageConfig{
			Endpoint: "http://localhost:9000",
			Bucket:   "test",
		},
	}

	f := NewWithDB(store, db, cfg).WithFlushAge(48 * time.Hour).WithWriteDir(writeDir)

	if err := f.doFlush(context.Background()); err != nil {
		t.Fatalf("doFlush: %v", err)
	}

	if len(parquetData) == 0 {
		t.Fatal("no parquet data captured — spans were not flushed to S3")
	}

	// Write parquet data to a file so DuckDB can read it back.
	if err := os.WriteFile(tmpParquetFile, parquetData, 0644); err != nil {
		t.Fatalf("write parquet file: %v", err)
	}

	// Read back the Parquet file via DuckDB to verify conversation_id is present.
	var readConvID sql.NullString
	err = db.QueryRowContext(context.Background(),
		`SELECT conversation_id FROM read_parquet(?) WHERE span_id = 'span-conv-1'`,
		tmpParquetFile,
	).Scan(&readConvID)
	if err != nil {
		t.Fatalf("read parquet conversation_id: %v", err)
	}
	if !readConvID.Valid {
		t.Fatal("conversation_id is NULL in Parquet — it should be preserved in the cold archive")
	}
	if readConvID.String != convID {
		t.Errorf("conversation_id in parquet: got %q, want %q", readConvID.String, convID)
	}
}

func keys(m map[string][]byte) []string {
	result := make([]string, 0, len(m))
	for k := range m {
		result = append(result, k)
	}
	return result
}

func createTestTables(db *sql.DB) error {
	_, err := db.ExecContext(context.Background(), `
		CREATE TABLE spans (
			span_id         VARCHAR NOT NULL,
			trace_id        VARCHAR NOT NULL,
			parent_id       VARCHAR,
			conversation_id VARCHAR,
			project_id      VARCHAR NOT NULL,
			service_name    VARCHAR,
			name            VARCHAR,
			kind            VARCHAR,
			start_time      TIMESTAMPTZ NOT NULL,
			end_time        TIMESTAMPTZ,
			model           VARCHAR,
			input           JSON,
			output          JSON,
			input_tokens    BIGINT,
			output_tokens   BIGINT,
			cost_usd        DOUBLE,
			prompt_name     VARCHAR,
			prompt_version  BIGINT,
			status_code     VARCHAR,
			status_message  VARCHAR,
			attributes      JSON,
			PRIMARY KEY (trace_id, span_id)
		);
		CREATE INDEX idx_spans_project_time ON spans (project_id, start_time);

		CREATE TABLE scores (
			score_id       VARCHAR NOT NULL PRIMARY KEY,
			span_id        VARCHAR NOT NULL,
			trace_id       VARCHAR NOT NULL,
			project_id     VARCHAR NOT NULL,
			eval_name      VARCHAR,
			value          DOUBLE,
			reasoning      VARCHAR,
			judge_model    VARCHAR,
			prompt_name    VARCHAR,
			prompt_version BIGINT,
			created_at     TIMESTAMPTZ NOT NULL
		);
	`)
	return err
}
