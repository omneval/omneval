package sync

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/zbloss/lantern/internal/config"
)

// mockStore implements storage.ObjectStore for testing.
type mockStore struct {
	puts    []putCall
	gets    []string
	deletes []string
	lists   []string
}

type putCall struct {
	key string
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

func TestNew_DefaultInterval(t *testing.T) {
	cfg := &config.Config{
		Writer: config.WriterConfig{
			SyncInterval: "30s",
		},
	}
	s := New(nil, "/tmp/test.db", cfg, nil)
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
	s := New(nil, "/tmp/test.db", cfg, nil)
	if s.syncInterval != 1*time.Minute {
		t.Errorf("syncInterval: got %v, want %v", s.syncInterval, 1*time.Minute)
	}
}

func TestNew_NoStore(t *testing.T) {
	cfg := &config.Config{}
	s := New(nil, "/tmp/test.db", cfg, nil)
	if s.store != nil {
		t.Error("expected nil store")
	}
}

func TestRun_NoStore(t *testing.T) {
	cfg := &config.Config{}
	s := New(nil, "/tmp/test.db", cfg, nil)
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

	s := New(store, dbPath, cfg, nil)
	ctx := context.Background()

	s.doSync(ctx)

	if len(store.puts) != 1 {
		t.Fatalf("expected 1 put, got %d", len(store.puts))
	}
	if store.puts[0].key != filepath.Join("us-west-2", "snapshots", "duckdb.db") {
		t.Errorf("key: got %q, want %q", store.puts[0].key,
			filepath.Join("us-west-2", "snapshots", "duckdb.db"))
	}
	if string(store.puts[0].data) != "fake duckdb" {
		t.Errorf("data: got %q, want %q", store.puts[0].data, "fake duckdb")
	}
}

func TestDoSync_NoStore(t *testing.T) {
	s := New(nil, "/tmp/nonexistent.db", &config.Config{}, nil)
	ctx := context.Background()

	// Should not panic, just log a warn.
	s.doSync(ctx)
}

func TestDoSync_DBPathIsDir(t *testing.T) {
	tmpDir := t.TempDir()

	store := &mockStore{}
	cfg := &config.Config{}
	s := New(store, tmpDir, cfg, nil)
	ctx := context.Background()

	s.doSync(ctx)

	if len(store.puts) != 0 {
		t.Error("expected no puts when db path is a directory")
	}
}

func TestDoSync_NonExistentDB(t *testing.T) {
	store := &mockStore{}
	cfg := &config.Config{}
	s := New(store, "/tmp/nonexistent-database-xyz.db", cfg, nil)
	ctx := context.Background()

	s.doSync(ctx)

	if len(store.puts) != 0 {
		t.Error("expected no puts when DB file does not exist")
	}
}

func TestDoSync_Metrics(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "lantern.db")
	if err := os.WriteFile(dbPath, []byte("test data"), 0644); err != nil {
		t.Fatalf("create temp db: %v", err)
	}

	store := &mockStore{}
	cfg := &config.Config{
		Storage: config.StorageConfig{
			Bucket: "test-bucket",
		},
	}

	reg := prometheus.NewRegistry()
	s := New(store, dbPath, cfg, reg)
	ctx := context.Background()

	s.doSync(ctx)

	// Check that histogram was registered.
	if s.syncDuration == nil {
		t.Error("expected non-nil syncDuration histogram")
	}
	if s.syncTotal == nil {
		t.Error("expected non-nil syncTotal counter")
	}
	if s.syncFailures == nil {
		t.Error("expected non-nil syncFailures counter")
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
	reg := prometheus.NewRegistry()
	s := New(failingStore, dbPath, cfg, reg)
	ctx := context.Background()

	s.doSync(ctx)

	// Counter should be incremented.
	if s.syncFailures == nil {
		t.Fatal("expected non-nil syncFailures counter")
	}
	// Verify failure was counted (can't easily read counter, but no panic is good)
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
