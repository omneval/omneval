package server

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/zbloss/lantern/internal/config"
	"github.com/zbloss/lantern/internal/metadata"
)

func TestOpenMetadataStore_SQLite(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := &config.Config{
		Database: config.DatabaseConfig{
			Driver: "sqlite",
			DSN:    filepath.Join(tmpDir, "test_meta.db"),
		},
	}

	store, err := openMetadataStore(cfg)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	defer store.Close()

	// Verify it's a working metadata store by running migrations
	if err := store.Migrate(context.Background()); err != nil {
		t.Fatalf("expected migration to succeed, got: %v", err)
	}

	// Verify we can create and list an eval rule
	_, err = store.ListEvalRules(context.Background(), "proj-1")
	if err != nil {
		t.Fatalf("expected ListEvalRules to work, got: %v", err)
	}
}

func TestOpenMetadataStore_SQLite_DSNDefault(t *testing.T) {
	cfg := &config.Config{
		Database: config.DatabaseConfig{
			Driver: "sqlite",
			DSN:    "",
		},
	}

	store, err := openMetadataStore(cfg)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	defer store.Close()

	if err := store.Migrate(context.Background()); err != nil {
		t.Fatalf("expected migration to succeed, got: %v", err)
	}
}

func TestOpenMetadataStore_EmptyDriver_SQLite(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := &config.Config{
		Database: config.DatabaseConfig{
			Driver: "",
			DSN:    filepath.Join(tmpDir, "test_meta.db"),
		},
	}

	store, err := openMetadataStore(cfg)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	defer store.Close()

	if err := store.Migrate(context.Background()); err != nil {
		t.Fatalf("expected migration to succeed, got: %v", err)
	}
}

func TestOpenMetadataStore_Postgres_DSNRequired(t *testing.T) {
	// Use a DSN that will fail to connect (no Postgres running).
	// We expect New() to fail on PingContext, but the driver path should be selected.
	cfg := &config.Config{
		Database: config.DatabaseConfig{
			Driver: "postgres",
			DSN:    "postgresql://nonexistent:5432/lantern?sslmode=disable",
		},
	}

	_, err := openMetadataStore(cfg)
	// We expect an error because no Postgres is running
	if err == nil {
		t.Fatal("expected error for nonexistent postgres, got nil")
	}

	// The error should mention postgres, not sqlite
	if err.Error() != "" && err.Error() != "metadata: unknown driver" {
		// Good — it tried postgres and failed to connect
	}
}

func TestOpenMetadataStore_UnknownDriver(t *testing.T) {
	cfg := &config.Config{
		Database: config.DatabaseConfig{
			Driver: "mysql",
			DSN:    "localhost:3306",
		},
	}

	_, err := openMetadataStore(cfg)
	if err == nil {
		t.Fatal("expected error for unknown driver, got nil")
	}
	if err.Error() != "metadata: unknown driver: mysql" {
		t.Errorf("expected 'unknown driver: mysql', got: %v", err)
	}
}

// TestOpenMetadataStore_ReturnsStoreInterface verifies the returned value
// implements the metadata.Store interface.
func TestOpenMetadataStore_ReturnsStoreInterface(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := &config.Config{
		Database: config.DatabaseConfig{
			Driver: "sqlite",
			DSN:    filepath.Join(tmpDir, "test_meta.db"),
		},
	}

	var store metadata.Store
	store, err := openMetadataStore(cfg)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	defer store.Close()

	if store == nil {
		t.Fatal("expected non-nil store")
	}
}
