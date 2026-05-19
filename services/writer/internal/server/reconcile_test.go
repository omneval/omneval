package server

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/omneval/omneval/internal/storage"
)

// fakeS3Store implements storage.ObjectStore for testing.
type fakeS3Store struct {
	objects map[string][]byte
	// lastModified maps keys to modification times.
	lastModified map[string]time.Time
}

func (f *fakeS3Store) Put(_ context.Context, key string, r io.Reader) error {
	if f == nil {
		return io.ErrUnexpectedEOF
	}
	data, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	f.objects[key] = data
	return nil
}

func (f *fakeS3Store) Get(_ context.Context, key string) (io.ReadCloser, error) {
	if f == nil {
		return nil, io.ErrUnexpectedEOF
	}
	data, ok := f.objects[key]
	if !ok {
		return nil, os.ErrNotExist
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

func (f *fakeS3Store) Delete(_ context.Context, key string) error {
	if f == nil {
		return nil
	}
	delete(f.objects, key)
	return nil
}

func (f *fakeS3Store) ListPrefix(_ context.Context, prefix string) ([]string, error) {
	if f == nil {
		return nil, nil
	}
	var keys []string
	for k := range f.objects {
		if len(k) >= len(prefix) && k[:len(prefix)] == prefix {
			keys = append(keys, k)
		}
	}
	return keys, nil
}

func (f *fakeS3Store) Stat(_ context.Context, key string) (*storage.ObjectStat, error) {
	if f == nil {
		return nil, io.ErrUnexpectedEOF
	}
	if _, ok := f.objects[key]; !ok {
		return nil, os.ErrNotExist
	}
	return &storage.ObjectStat{
		ETag:         "fake-etag",
		LastModified: f.lastModified[key],
	}, nil
}

func TestReconciler_NoStore(t *testing.T) {
	r := NewReconciler(nil, "/tmp/test.db", "snapshots/duckdb.db")
	ctx := context.Background()

	err := r.Reconcile(ctx)
	if err != nil {
		t.Fatalf("expected nil error with no store, got: %v", err)
	}
}

func TestReconciler_LocalFileMissing(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "omneval.db")
	snapshotKey := "snapshots/duckdb.db"

	store := &fakeS3Store{
		objects: map[string][]byte{
			snapshotKey: []byte("s3 snapshot data"),
		},
		lastModified: map[string]time.Time{
			snapshotKey: time.Now(),
		},
	}

	r := NewReconciler(store, dbPath, snapshotKey)
	ctx := context.Background()

	err := r.Reconcile(ctx)
	if err != nil {
		t.Fatalf("expected nil error, got: %v", err)
	}

	// Verify the local file was created.
	fileData, err := os.ReadFile(dbPath)
	if err != nil {
		t.Fatalf("local file not created: %v", err)
	}
	if string(fileData) != "s3 snapshot data" {
		t.Errorf("file content: got %q, want %q", string(fileData), "s3 snapshot data")
	}
}

func TestReconciler_S3Newer(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "omneval.db")
	snapshotKey := "snapshots/duckdb.db"

	// Create local file with old mtime.
	oldData := []byte("old local data")
	oldMtime := time.Now().Add(-2 * time.Hour)
	if err := os.WriteFile(dbPath, oldData, 0644); err != nil {
		t.Fatalf("create local file: %v", err)
	}
	_ = os.Chtimes(dbPath, oldMtime, oldMtime)

	// S3 has newer snapshot.
	s3Data := []byte("newer s3 snapshot")
	s3Mtime := time.Now().Add(-30 * time.Minute)

	store := &fakeS3Store{
		objects: map[string][]byte{
			snapshotKey: s3Data,
		},
		lastModified: map[string]time.Time{
			snapshotKey: s3Mtime,
		},
	}

	r := NewReconciler(store, dbPath, snapshotKey)
	ctx := context.Background()

	err := r.Reconcile(ctx)
	if err != nil {
		t.Fatalf("expected nil error, got: %v", err)
	}

	// Verify the file was updated.
	data, err := os.ReadFile(dbPath)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if string(data) != "newer s3 snapshot" {
		t.Errorf("file content: got %q, want %q", string(data), "newer s3 snapshot")
	}
}

func TestReconciler_LocalNewer(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "omneval.db")
	snapshotKey := "snapshots/duckdb.db"

	// Create local file with recent mtime.
	localData := []byte("fresh local data")
	localMtime := time.Now().Add(-1 * time.Hour)
	if err := os.WriteFile(dbPath, localData, 0644); err != nil {
		t.Fatalf("create local file: %v", err)
	}
	_ = os.Chtimes(dbPath, localMtime, localMtime)

	// S3 has older snapshot.
	s3Data := []byte("old s3 data")
	s3Mtime := time.Now().Add(-3 * time.Hour)

	store := &fakeS3Store{
		objects: map[string][]byte{
			snapshotKey: s3Data,
		},
		lastModified: map[string]time.Time{
			snapshotKey: s3Mtime,
		},
	}

	r := NewReconciler(store, dbPath, snapshotKey)
	ctx := context.Background()

	err := r.Reconcile(ctx)
	if err != nil {
		t.Fatalf("expected nil error, got: %v", err)
	}

	// Verify the file was NOT overwritten.
	data, err := os.ReadFile(dbPath)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if string(data) != "fresh local data" {
		t.Errorf("file content: got %q, want %q", string(data), "fresh local data")
	}
}

func TestReconciler_SameMtime(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "omneval.db")
	snapshotKey := "snapshots/duckdb.db"

	sameMtime := time.Now().Add(-1 * time.Hour)

	// Create local file.
	localData := []byte("same mtime data")
	if err := os.WriteFile(dbPath, localData, 0644); err != nil {
		t.Fatalf("create local file: %v", err)
	}
	_ = os.Chtimes(dbPath, sameMtime, sameMtime)

	// S3 has same mtime.
	store := &fakeS3Store{
		objects: map[string][]byte{
			snapshotKey: []byte("s3 data"),
		},
		lastModified: map[string]time.Time{
			snapshotKey: sameMtime,
		},
	}

	r := NewReconciler(store, dbPath, snapshotKey)
	ctx := context.Background()

	err := r.Reconcile(ctx)
	if err != nil {
		t.Fatalf("expected nil error, got: %v", err)
	}

	// Verify the file was NOT overwritten.
	data, err := os.ReadFile(dbPath)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if string(data) != "same mtime data" {
		t.Errorf("file content: got %q, want %q", string(data), "same mtime data")
	}
}

func TestReconciler_AtomicallyOverwrites(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "omneval.db")
	snapshotKey := "snapshots/duckdb.db"

	// S3 has a snapshot.
	s3Data := []byte("s3 snapshot content")
	store := &fakeS3Store{
		objects: map[string][]byte{
			snapshotKey: s3Data,
		},
		lastModified: map[string]time.Time{
			snapshotKey: time.Now(),
		},
	}

	r := NewReconciler(store, dbPath, snapshotKey)
	ctx := context.Background()

	err := r.Reconcile(ctx)
	if err != nil {
		t.Fatalf("expected nil error, got: %v", err)
	}

	// Verify the tmp file was cleaned up.
	tmpPath := dbPath + ".tmp"
	if _, err := os.Stat(tmpPath); !os.IsNotExist(err) {
		t.Error("tmp file should not exist after reconciliation")
	}

	// Verify the actual file was created.
	data, err := os.ReadFile(dbPath)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if string(data) != "s3 snapshot content" {
		t.Errorf("file content: got %q, want %q", string(data), "s3 snapshot content")
	}
}

func TestReconciler_LocalFileStatFails(t *testing.T) {
	// Test with a non-existent path in a nonexistent directory.
	store := &fakeS3Store{
		objects: map[string][]byte{
			"snapshot": []byte("s3 data"),
		},
		lastModified: map[string]time.Time{
			"snapshot": time.Now(),
		},
	}

	r := NewReconciler(store, "/nonexistent-dir-xyz/nonexistent-db.db", "snapshot")
	ctx := context.Background()

	// Should attempt download since local file is missing.
	// The download will fail because the parent dir doesn't exist.
	err := r.Reconcile(ctx)
	if err == nil {
		t.Error("expected error when local file missing and parent dir doesn't exist")
	}
}
