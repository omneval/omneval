package server

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/omneval/omneval/internal/config"
	"github.com/omneval/omneval/internal/metadata"
)

// TestOpenMetadataStore_DefaultDSN verifies the writer applies its own
// default SQLite path (omneval_meta.db) before delegating to metadata.Open.
func TestOpenMetadataStore_DefaultDSN(t *testing.T) {
	t.Chdir(t.TempDir())

	cfg := &config.Config{
		Database: config.DatabaseConfig{Driver: "sqlite", DSN: ""},
	}

	store, err := openMetadataStore(cfg)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	defer store.Close()

	if _, err := os.Stat("omneval_meta.db"); err != nil {
		t.Errorf("expected default SQLite file omneval_meta.db to exist: %v", err)
	}
}

// TestOpenMetadataStore_ExplicitDSN verifies the configured DSN is passed
// through to the shared factory and a usable Store comes back.
func TestOpenMetadataStore_ExplicitDSN(t *testing.T) {
	path := filepath.Join(t.TempDir(), "explicit.db")
	cfg := &config.Config{
		Database: config.DatabaseConfig{Driver: "sqlite", DSN: path},
	}

	var store metadata.Store
	store, err := openMetadataStore(cfg)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	defer store.Close()

	if _, err := os.Stat(path); err != nil {
		t.Errorf("expected SQLite file at %s: %v", path, err)
	}
}

// TestOpenMetadataStore_PostgresRequiresDSN verifies the factory error for a
// postgres driver with no DSN surfaces through the wrapper (no sqlite
// fallback is applied for postgres).
func TestOpenMetadataStore_PostgresRequiresDSN(t *testing.T) {
	cfg := &config.Config{
		Database: config.DatabaseConfig{Driver: "postgres", DSN: ""},
	}

	if _, err := openMetadataStore(cfg); err == nil {
		t.Fatal("expected error for postgres without DSN, got nil")
	}
}
