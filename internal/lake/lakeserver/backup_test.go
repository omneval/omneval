package lakeserver_test

import (
	"bytes"
	"context"
	"database/sql"
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/omneval/omneval/internal/lake/lakeserver"
	"github.com/omneval/omneval/internal/storage"
)

// fakeObjectStore is an in-memory ObjectStore for testing.
type fakeObjectStore struct {
	objects map[string][]byte
	deleted map[string]bool
}

func newFakeObjectStore() *fakeObjectStore {
	return &fakeObjectStore{
		objects: make(map[string][]byte),
		deleted: make(map[string]bool),
	}
}

func (f *fakeObjectStore) Put(_ context.Context, key string, r io.Reader) error {
	data, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	f.objects[key] = data
	return nil
}

func (f *fakeObjectStore) PutSized(_ context.Context, key string, r io.Reader, _ int64) error {
	return f.Put(context.Background(), key, r)
}

func (f *fakeObjectStore) Get(_ context.Context, key string) (io.ReadCloser, error) {
	data, ok := f.objects[key]
	if !ok {
		return nil, nil
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

func (f *fakeObjectStore) Delete(_ context.Context, key string) error {
	delete(f.objects, key)
	f.deleted[key] = true
	return nil
}

func (f *fakeObjectStore) ListPrefix(_ context.Context, prefix string) ([]string, error) {
	var keys []string
	for k := range f.objects {
		if strings.HasPrefix(k, prefix) && !f.deleted[k] {
			keys = append(keys, k)
		}
	}
	return keys, nil
}

func (f *fakeObjectStore) Stat(_ context.Context, key string) (*storage.ObjectStat, error) {
	if _, ok := f.objects[key]; !ok {
		return nil, nil
	}
	return &storage.ObjectStat{}, nil
}

// activeKeys returns all keys that still exist (uploaded minus deleted).
func (f *fakeObjectStore) activeKeys() []string {
	var keys []string
	for k := range f.objects {
		if !f.deleted[k] {
			keys = append(keys, k)
		}
	}
	return keys
}

// activeMap returns a map of active key → data.
func (f *fakeObjectStore) activeMap() map[string][]byte {
	m := make(map[string][]byte)
	for k, v := range f.objects {
		if !f.deleted[k] {
			m[k] = v
		}
	}
	return m
}

// testDuckDB opens a file-based DuckDB database in a temp dir for testing.
// A file-based catalog is required so RunBackup can read back the catalog
// file content and upload it to S3.
func testDuckDB(t *testing.T) *sql.DB {
	t.Helper()
	tmp := t.TempDir()
	path := filepath.Join(tmp, "catalog.duckdb")
	db, err := sql.Open("duckdb", path)
	if err != nil {
		t.Fatalf("open duckdb: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// TestRunBackup_PostgresNoOp verifies that backup is a no-op when the catalog
// driver is postgres — no checkpoint or upload happens.
func TestRunBackup_PostgresNoOp(t *testing.T) {
	ctx := context.Background()
	store := newFakeObjectStore()
	db := testDuckDB(t)

	result, err := lakeserver.RunBackup(ctx, db, "postgres", store, "test-catalog", lakeserver.BackupConfig{KeepCount: 5})
	if err != nil {
		t.Fatalf("RunBackup: %v", err)
	}

	if result.Uploaded != 0 {
		t.Errorf("expected uploaded=0 for postgres, got %d", result.Uploaded)
	}
	if result.Deleted != 0 {
		t.Errorf("expected deleted=0 for postgres, got %d", result.Deleted)
	}
	if !result.Skipped {
		t.Error("expected skipped=true for postgres")
	}
}

// TestRunBackup_DuckDB uploads a checkpoint file and verifies the key format
// and that the uploaded payload is non-empty (actual catalog file content).
func TestRunBackup_DuckDB(t *testing.T) {
	ctx := context.Background()
	store := newFakeObjectStore()
	db := testDuckDB(t)

	// Create a table so CHECKPOINT has data to flush.
	if _, err := db.ExecContext(ctx, "CREATE TABLE test (id INT)"); err != nil {
		t.Fatalf("create table: %v", err)
	}

	result, err := lakeserver.RunBackup(ctx, db, "duckdb", store, "test-catalog", lakeserver.BackupConfig{KeepCount: 5})
	if err != nil {
		t.Fatalf("RunBackup: %v", err)
	}

	if result.Uploaded == 0 {
		t.Error("expected uploaded=1, got 0")
	}
	if result.Deleted != 0 {
		t.Errorf("expected deleted=0, got %d", result.Deleted)
	}

	keys := store.activeKeys()
	if len(keys) != 1 {
		t.Fatalf("expected 1 uploaded object, got %d", len(keys))
	}
	// Key format: lake/catalog-backups/<catalog-name>/<timestamp>.duckdb
	if !strings.HasPrefix(keys[0], "lake/catalog-backups/") {
		t.Errorf("unexpected key format: %q", keys[0])
	}

	// Verify the uploaded content is non-empty (actual catalog file bytes).
	data := store.objects[keys[0]]
	if len(data) == 0 {
		t.Error("expected non-empty catalog file content, got empty payload")
	}
}

// TestRunBackup_DuckDBUploadsOneObject verifies that a single RunBackup call
// produces exactly one upload object — no more, no less.
func TestRunBackup_DuckDBUploadsOneObject(t *testing.T) {
	ctx := context.Background()
	store := newFakeObjectStore()
	db := testDuckDB(t)

	if _, err := db.ExecContext(ctx, "CREATE TABLE test (id INT)"); err != nil {
		t.Fatalf("create table: %v", err)
	}

	_, err := lakeserver.RunBackup(ctx, db, "duckdb", store, "test-catalog", lakeserver.BackupConfig{KeepCount: 5})
	if err != nil {
		t.Fatalf("RunBackup: %v", err)
	}

	if len(store.objects) != 1 {
		t.Fatalf("expected exactly 1 uploaded object, got %d: %v", len(store.objects), store.activeKeys())
	}
}

// TestRunBackup_PruneOldest verifies that when there are more than keepCount
// backups, the oldest ones beyond keepCount are deleted.
func TestRunBackup_PruneOldest(t *testing.T) {
	ctx := context.Background()
	store := newFakeObjectStore()
	db := testDuckDB(t)
	keepCount := 3

	// Pre-populate with 4 old backups (oldest first).
	baseTime := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 4; i++ {
		ts := baseTime.Add(time.Duration(i) * time.Hour)
		key := "lake/catalog-backups/test-catalog/" + ts.Format(time.RFC3339) + ".duckdb"
		store.objects[key] = []byte("old-backup")
	}

	result, err := lakeserver.RunBackup(ctx, db, "duckdb", store, "test-catalog", lakeserver.BackupConfig{KeepCount: int(keepCount)})
	if err != nil {
		t.Fatalf("RunBackup: %v", err)
	}

	// After upload: 5 objects. keepCount=3, so delete 5-3=2 oldest.
	if result.Deleted != 2 {
		t.Errorf("expected deleted=2 (5 total, keep 3), got %d", result.Deleted)
	}
	if result.Uploaded != 1 {
		t.Errorf("expected uploaded=1, got %d", result.Uploaded)
	}

	// Verify only 3 objects remain (2 oldest deleted, 3 newest kept).
	keys := store.activeKeys()
	if len(keys) != 3 {
		t.Fatalf("expected 3 active keys, got %d: %v", len(keys), keys)
	}
}

// TestRunBackup_PruneNeverExceedKeepCount verifies that the number of deletions
// never exceeds (totalAfterUpload - keepCount), i.e. we never delete more than
// necessary.
func TestRunBackup_PruneNeverExceedKeepCount(t *testing.T) {
	ctx := context.Background()
	store := newFakeObjectStore()
	db := testDuckDB(t)
	keepCount := 5

	// Pre-populate with exactly keepCount objects.
	baseTime := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < int(keepCount); i++ {
		ts := baseTime.Add(time.Duration(i) * time.Hour)
		key := "lake/catalog-backups/test-catalog/" + ts.Format(time.RFC3339) + ".duckdb"
		store.objects[key] = []byte("backup")
	}

	result, err := lakeserver.RunBackup(ctx, db, "duckdb", store, "test-catalog", lakeserver.BackupConfig{KeepCount: int(keepCount)})
	if err != nil {
		t.Fatalf("RunBackup: %v", err)
	}

	// After upload: keepCount+1 objects. keepCount remain, so 1 deleted.
	if result.Deleted != 1 {
		t.Errorf("expected deleted=1, got %d", result.Deleted)
	}
	if result.Uploaded != 1 {
		t.Errorf("expected uploaded=1, got %d", result.Uploaded)
	}

	keys := store.activeKeys()
	if len(keys) != int(keepCount) {
		t.Errorf("expected %d active keys, got %d", keepCount, len(keys))
	}
}

// TestRunBackup_PruneNoDeleteWhenUnderKeepCount verifies that when there are
// fewer objects than keepCount, nothing is deleted.
func TestRunBackup_PruneNoDeleteWhenUnderKeepCount(t *testing.T) {
	ctx := context.Background()
	store := newFakeObjectStore()
	db := testDuckDB(t)
	keepCount := 10

	// Pre-populate with 2 old backups.
	baseTime := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 2; i++ {
		ts := baseTime.Add(time.Duration(i) * time.Hour)
		key := "lake/catalog-backups/test-catalog/" + ts.Format(time.RFC3339) + ".duckdb"
		store.objects[key] = []byte("old")
	}

	result, err := lakeserver.RunBackup(ctx, db, "duckdb", store, "test-catalog", lakeserver.BackupConfig{KeepCount: int(keepCount)})
	if err != nil {
		t.Fatalf("RunBackup: %v", err)
	}

	if result.Deleted != 0 {
		t.Errorf("expected deleted=0 (3 total < keepCount=10), got %d", result.Deleted)
	}
	if result.Uploaded != 1 {
		t.Errorf("expected uploaded=1, got %d", result.Uploaded)
	}

	keys := store.activeKeys()
	if len(keys) != 3 {
		t.Errorf("expected 3 active keys, got %d", len(keys))
	}
}

// TestRunBackup_PruneAlwaysDeletesOldest verifies that the objects deleted are
// always the oldest ones (by timestamp in the key name).
func TestRunBackup_PruneAlwaysDeletesOldest(t *testing.T) {
	ctx := context.Background()
	store := newFakeObjectStore()
	db := testDuckDB(t)
	keepCount := 2

	// Pre-populate with 3 old backups at distinct hours.
	baseTime := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	oldestKeys := make([]string, 3)
	for i := 0; i < 3; i++ {
		ts := baseTime.Add(time.Duration(i) * time.Hour)
		key := "lake/catalog-backups/test-catalog/" + ts.Format(time.RFC3339) + ".duckdb"
		oldestKeys[i] = key
		store.objects[key] = []byte("old")
	}

	_, err := lakeserver.RunBackup(ctx, db, "duckdb", store, "test-catalog", lakeserver.BackupConfig{KeepCount: int(keepCount)})
	if err != nil {
		t.Fatalf("RunBackup: %v", err)
	}

	// 3 existing + 1 new = 4. keepCount=2, so delete 2 oldest.
	// The two oldest keys (index 0 and 1) should be deleted.
	active := store.activeMap()
	for _, k := range oldestKeys[:2] {
		if _, ok := active[k]; ok {
			t.Errorf("expected oldest key %q to be deleted, but it still exists", k)
		}
	}
	// The newest old backup + the new backup should remain.
	if len(active) != 2 {
		t.Errorf("expected 2 active keys, got %d", len(active))
	}
}