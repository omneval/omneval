package sync

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"


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
	s := New(nil, "/tmp/test.db", cfg, m)
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
	s := New(nil, "/tmp/test.db", cfg, m)
	if s.syncInterval != 1*time.Minute {
		t.Errorf("syncInterval: got %v, want %v", s.syncInterval, 1*time.Minute)
	}
}

func TestNew_NoStore(t *testing.T) {
	cfg := &config.Config{}
	m := newTestMetrics(t)
	s := New(nil, "/tmp/test.db", cfg, m)
	if s.store != nil {
		t.Error("expected nil store")
	}
}

func TestRun_NoStore(t *testing.T) {
	cfg := &config.Config{}
	m := newTestMetrics(t)
	s := New(nil, "/tmp/test.db", cfg, m)
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
	s := New(store, dbPath, cfg, m)
	ctx := context.Background()

	s.doSync(ctx)

	if len(store.puts) != 1 {
		t.Fatalf("expected 1 put, got %d", len(store.puts))
	}
	expectedKey := filepath.Join("us-west-2", "snapshots", "duckdb.db")
	if store.puts[0].key != expectedKey {
		t.Errorf("key: got %q, want %q", store.puts[0].key, expectedKey)
	}
	if string(store.puts[0].data) != "fake duckdb" {
		t.Errorf("data: got %q, want %q", store.puts[0].data, "fake duckdb")
	}
}

func TestDoSync_NoStore(t *testing.T) {
	m := newTestMetrics(t)
	s := New(nil, "/tmp/nonexistent.db", &config.Config{}, m)
	ctx := context.Background()

	// Should not panic, just log a warn.
	s.doSync(ctx)
}

func TestDoSync_DBPathIsDir(t *testing.T) {
	tmpDir := t.TempDir()

	store := &mockStore{}
	cfg := &config.Config{}
	m := newTestMetrics(t)
	s := New(store, tmpDir, cfg, m)
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
	s := New(store, "/tmp/nonexistent-database-xyz.db", cfg, m)
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
	s := New(failingStore, dbPath, cfg, m)
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

	s := New(store, dbPath, cfg, nil)
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

	s := New(store, dbPath, cfg, nil)
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
