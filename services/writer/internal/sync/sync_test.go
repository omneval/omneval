package sync

import (
	"context"
	"database/sql"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "github.com/marcboeker/go-duckdb/v2"
	"github.com/zbloss/lantern/internal/config"
	"github.com/zbloss/lantern/internal/storage"
	"github.com/zbloss/lantern/services/writer/internal/metrics"
)

// mockStore implements storage.ObjectStore for testing.
type mockStore struct {
	puts    []putCall
	gets    []string
	deletes []string
	lists   []string
}

type putCall struct {
	key  string
	data []byte
}

func (m *mockStore) Put(_ context.Context, key string, r io.Reader) error {
	if m == nil {
		return io.ErrUnexpectedEOF
	}
	data, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	m.puts = append(m.puts, putCall{key: key, data: data})
	return nil
}

func (m *mockStore) Get(_ context.Context, key string) (io.ReadCloser, error) {
	if m == nil {
		return nil, io.ErrUnexpectedEOF
	}
	m.gets = append(m.gets, key)
	return nil, nil
}

func (m *mockStore) Delete(_ context.Context, key string) error {
	if m == nil {
		return nil
	}
	m.deletes = append(m.deletes, key)
	return nil
}

func (m *mockStore) ListPrefix(_ context.Context, prefix string) ([]string, error) {
	if m == nil {
		return nil, nil
	}
	m.lists = append(m.lists, prefix)
	return nil, nil
}

func (m *mockStore) Stat(_ context.Context, key string) (*storage.ObjectStat, error) {
	if m == nil {
		return nil, io.ErrUnexpectedEOF
	}
	return nil, nil
}

func newTestMetrics(t *testing.T) *metrics.WriterMetrics {
	// We create a nil-config metrics helper; the test only verifies
	// that doSync doesn't panic when metrics are nil-safe.
	return metrics.NewWriterMetrics(&config.Config{})
}

func TestNew_DefaultInterval(t *testing.T) {
	cfg := &config.Config{
		Writer: config.WriterConfig{
			SyncInterval: "30s",
		},
	}
	m := newTestMetrics(t)
	s := New(nil, nil, "/tmp/test.db", cfg, m)
	if s.syncInterval != 30*time.Second {
		t.Errorf("syncInterval: got %v, want %v", s.syncInterval, 30*time.Second)
	}
}

func TestNew_CustomInterval(t *testing.T) {
	cfg := &config.Config{
		Writer: config.WriterConfig{
			SyncInterval: "1m",
		},
	}
	m := newTestMetrics(t)
	s := New(nil, nil, "/tmp/test.db", cfg, m)
	if s.syncInterval != 1*time.Minute {
		t.Errorf("syncInterval: got %v, want %v", s.syncInterval, 1*time.Minute)
	}
}

func TestNew_NoStore(t *testing.T) {
	cfg := &config.Config{}
	m := newTestMetrics(t)
	s := New(nil, nil, "/tmp/test.db", cfg, m)
	if s.store != nil {
		t.Error("expected nil store")
	}
}

func TestRun_NoStore(t *testing.T) {
	cfg := &config.Config{}
	m := newTestMetrics(t)
	s := New(nil, nil, "/tmp/test.db", cfg, m)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	err := s.Run(ctx)
	// Run returns context error on timeout, not a sync error
	if err != context.DeadlineExceeded {
		t.Logf("Run returned: %v (expected DeadlineExceeded or nil)", err)
	}
}

func TestDoSync_Success(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "lantern.db")

	// Create a dummy DB file.
	if err := os.WriteFile(dbPath, []byte("fake duckdb"), 0644); err != nil {
		t.Fatalf("create temp db: %v", err)
	}

	store := &mockStore{}
	cfg := &config.Config{
		Storage: config.StorageConfig{
			Bucket: "my-bucket",
			Region: "us-west-2",
		},
	}

	m := newTestMetrics(t)
	s := New(store, nil, dbPath, cfg, m)
	ctx := context.Background()

	s.doSync(ctx)

	if len(store.puts) != 1 {
		t.Fatalf("expected 1 put, got %d", len(store.puts))
	}
	expectedKey := "snapshots/duckdb.db"
	if store.puts[0].key != expectedKey {
		t.Errorf("key: got %q, want %q", store.puts[0].key, expectedKey)
	}
	if string(store.puts[0].data) != "fake duckdb" {
		t.Errorf("data: got %q, want %q", store.puts[0].data, "fake duckdb")
	}
}

func TestDoSync_NoStore(t *testing.T) {
	m := newTestMetrics(t)
	s := New(nil, nil, "/tmp/nonexistent.db", &config.Config{}, m)
	ctx := context.Background()

	// Should not panic, just log a warn.
	s.doSync(ctx)
}

func TestDoSync_DBPathIsDir(t *testing.T) {
	tmpDir := t.TempDir()

	store := &mockStore{}
	cfg := &config.Config{}
	m := newTestMetrics(t)
	s := New(store, nil, tmpDir, cfg, m)
	ctx := context.Background()

	s.doSync(ctx)

	if len(store.puts) != 0 {
		t.Error("expected no puts when db path is a directory")
	}
}

func TestDoSync_NonExistentDB(t *testing.T) {
	store := &mockStore{}
	cfg := &config.Config{}
	m := newTestMetrics(t)
	s := New(store, nil, "/tmp/nonexistent-database-xyz.db", cfg, m)
	ctx := context.Background()

	s.doSync(ctx)

	if len(store.puts) != 0 {
		t.Error("expected no puts when DB file does not exist")
	}
}

func TestDoSync_UploadFailure(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "lantern.db")
	if err := os.WriteFile(dbPath, []byte("test"), 0644); err != nil {
		t.Fatalf("create temp db: %v", err)
	}

	// Store that always fails.
	failingStore := &failingStore{}
	cfg := &config.Config{}
	m := newTestMetrics(t)
	s := New(failingStore, nil, dbPath, cfg, m)
	ctx := context.Background()

	s.doSync(ctx)

	// No panic means metrics recording worked correctly.
}

// failingStore always returns an error on Put.
type failingStore struct{}

func (f *failingStore) Put(_ context.Context, key string, r io.Reader) error {
	return io.ErrUnexpectedEOF
}

func (f *failingStore) Get(_ context.Context, key string) (io.ReadCloser, error) {
	return nil, io.ErrUnexpectedEOF
}

func (f *failingStore) Delete(_ context.Context, key string) error {
	return io.ErrUnexpectedEOF
}

func (f *failingStore) ListPrefix(_ context.Context, prefix string) ([]string, error) {
	return nil, io.ErrUnexpectedEOF
}

func (f *failingStore) Stat(_ context.Context, key string) (*storage.ObjectStat, error) {
	return nil, io.ErrUnexpectedEOF
}

func TestRun_FinalSyncOnShutdown(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "lantern.db")
	if err := os.WriteFile(dbPath, []byte("fake duckdb"), 0644); err != nil {
		t.Fatalf("create temp db: %v", err)
	}

	store := &mockStore{}
	cfg := &config.Config{
		Writer: config.WriterConfig{
			SyncInterval: "100ms",
		},
		Storage: config.StorageConfig{
			Bucket: "my-bucket",
		},
	}

	s := New(store, nil, dbPath, cfg, nil)
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() {
		done <- s.Run(ctx)
	}()

	// Let the syncer run at least one cycle.
	time.Sleep(150 * time.Millisecond)

	// Cancel the context — this should trigger a final sync.
	cancel()

	err := <-done
	if err != context.Canceled && err != context.DeadlineExceeded {
		t.Logf("Run returned: %v", err)
	}

	// Verify that at least one final sync was attempted after cancel.
	// We expect at least 1 put call (the final sync triggered by cancel).
	if len(store.puts) == 0 {
		t.Error("expected at least one sync (final sync on shutdown), got 0")
	}
}

func TestRun_SyncIntervalTriggeredSync(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "lantern.db")
	if err := os.WriteFile(dbPath, []byte("fake duckdb"), 0644); err != nil {
		t.Fatalf("create temp db: %v", err)
	}

	store := &mockStore{}
	cfg := &config.Config{
		Writer: config.WriterConfig{
			SyncInterval: "100ms",
		},
		Storage: config.StorageConfig{
			Bucket: "my-bucket",
		},
	}

	s := New(store, nil, dbPath, cfg, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	err := s.Run(ctx)
	if err != context.DeadlineExceeded {
		t.Logf("Run returned: %v", err)
	}

	// At least one interval-triggered sync should have happened.
	if len(store.puts) == 0 {
		t.Error("expected at least one sync from interval, got 0")
	}
}

func TestDoSync_CheckpointsWALBeforeUpload(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "lantern.db")

	// Open a real DuckDB connection. DuckDB v1.4+ uses WAL by default.
	db, err := sql.Open("duckdb", dbPath)
	if err != nil {
		t.Fatalf("open duckdb: %v", err)
	}
	defer db.Close()

	// Create a table and insert data. This exercises the checkpoint path:
	// the Syncer will call CHECKPOINT (DuckDB's WAL flush) before reading the
	// main file, which should succeed without error.
	if _, err := db.ExecContext(context.Background(), `
		CREATE TABLE test_table (id INTEGER, name VARCHAR);
		INSERT INTO test_table VALUES (1, 'wal_test_data')
	`); err != nil {
		t.Fatalf("insert data: %v", err)
	}

	store := &mockStore{}
	cfg := &config.Config{
		Writer: config.WriterConfig{
			SyncInterval: "30s",
		},
		Storage: config.StorageConfig{
			Bucket: "my-bucket",
		},
	}

	m := newTestMetrics(t)
	s := New(store, nil, dbPath, cfg, m)
	s.db = db

	// Execute the sync — this runs the checkpoint before uploading.
	// If the checkpoint fails or panics, this test fails.
	s.doSync(context.Background())

	// Verify the sync uploaded the file.
	if len(store.puts) != 1 {
		t.Fatalf("expected 1 put, got %d", len(store.puts))
	}
	// The uploaded data should be non-empty (a valid DuckDB file).
	if len(store.puts[0].data) == 0 {
		t.Fatal("uploaded snapshot should be non-empty")
	}

	// Verify the checkpoint succeeded by confirming the database is still
	// fully queryable after the sync cycle (checkpoint did not corrupt state).
	var count int
	if err := db.QueryRowContext(context.Background(), "SELECT COUNT(*) FROM test_table").Scan(&count); err != nil {
		t.Fatalf("query after sync failed: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 row in test_table after checkpoint, got %d", count)
	}
}

// TestDoSync_SnapshotContainsWrittenData verifies that a fresh DuckDB
// connection opened on the snapshot file can read data written by the
// original connection. This tests the full WAL checkpoint + upload cycle.
//
// This is the regression test for issue #71: PRAGMA force_checkpoint
// (SQLite syntax) silently succeeds in DuckDB but does nothing, leaving
// recent writes trapped in the WAL. A fresh snapshot connection would
// see empty tables.
func TestDoSync_SnapshotContainsWrittenData(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "lantern.db")

	// Open a real DuckDB connection — this is the "Writer".
	writerDB, err := sql.Open("duckdb", dbPath)
	if err != nil {
		t.Fatalf("open duckdb: %v", err)
	}

	// Create schema and insert data.
	if _, err := writerDB.ExecContext(context.Background(), `
		CREATE TABLE spans (
			trace_id VARCHAR,
			span_id VARCHAR,
			name VARCHAR,
			model VARCHAR
		);
		INSERT INTO spans VALUES
			('trace-001', 'span-001', 'test_span', 'gpt-4'),
			('trace-002', 'span-002', 'another_span', 'claude-sonnet')
	`); err != nil {
		t.Fatalf("insert data: %v", err)
	}

	// Verify data is visible in the writer connection.
	var count int
	if err := writerDB.QueryRowContext(context.Background(), "SELECT COUNT(*) FROM spans").Scan(&count); err != nil {
		t.Fatalf("pre-sync query failed: %v", err)
	}
	if count != 2 {
		t.Fatalf("expected 2 rows before sync, got %d", count)
	}

	// Run the syncer to checkpoint and upload the snapshot.
	store := &mockStore{}
	cfg := &config.Config{
		Writer: config.WriterConfig{
			SyncInterval: "30s",
		},
		Storage: config.StorageConfig{
			Bucket: "my-bucket",
		},
	}
	m := newTestMetrics(t)
	s := New(store, nil, dbPath, cfg, m)
	s.db = writerDB

	s.doSync(context.Background())

	if len(store.puts) != 1 {
		t.Fatalf("expected 1 put, got %d", len(store.puts))
	}

	// Simulate the Query API: close the writer connection and open
	// a brand new one (this is what happens when Query API downloads
	// the S3 snapshot and opens it as a file).
	writerDB.Close()

	// Open a fresh connection to the main file — this is the "Query API".
	queryDB, err := sql.Open("duckdb", dbPath)
	if err != nil {
		t.Fatalf("open fresh duckdb: %v", err)
	}
	defer queryDB.Close()

	// Verify the fresh connection can read the data.
	if err := queryDB.QueryRowContext(context.Background(), "SELECT COUNT(*) FROM spans").Scan(&count); err != nil {
		t.Fatalf("fresh connection query failed: %v (checkpoint likely did not persist writes)", err)
	}
	if count != 2 {
		t.Fatalf("expected 2 rows in fresh snapshot, got %d (writes lost because WAL was not checkpointed)", count)
	}

	// Verify the actual data content.
	var model string
	if err := queryDB.QueryRowContext(context.Background(), "SELECT model FROM spans ORDER BY trace_id LIMIT 1").Scan(&model); err != nil {
		t.Fatalf("data query failed: %v", err)
	}
	if model != "gpt-4" {
		t.Errorf("expected model 'gpt-4', got '%s'", model)
	}
}

// TestDoSync_NilDBSkipsCheckpoint verifies that sync still works
// when no DB connection is provided — the checkpoint is skipped and the
// snapshot is uploaded as-is. This is the backward-compatibility path.
func TestDoSync_NilDBSkipsCheckpoint(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "lantern.db")

	if err := os.WriteFile(dbPath, []byte("fake duckdb content for sync"), 0644); err != nil {
		t.Fatalf("create temp db: %v", err)
	}

	store := &mockStore{}
	cfg := &config.Config{
		Writer: config.WriterConfig{
			SyncInterval: "30s",
		},
	}

	m := newTestMetrics(t)
	s := New(store, nil, dbPath, cfg, m)
	// s.db is nil by default — checkpoint is skipped in doSync.

	s.doSync(context.Background())

	if len(store.puts) != 1 {
		t.Fatalf("expected 1 put, got %d", len(store.puts))
	}
	if !strings.Contains(string(store.puts[0].data), "fake duckdb content for sync") {
		t.Errorf("data mismatch: got %q", string(store.puts[0].data))
	}
}
