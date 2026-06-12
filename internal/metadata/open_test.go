package metadata_test

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	"github.com/omneval/omneval/internal/metadata"
	_ "modernc.org/sqlite"
)

func TestOpen(t *testing.T) {
	tmpDir := t.TempDir()

	tests := []struct {
		name        string
		driver      string
		dsn         string
		wantErr     bool
		errContains string
	}{
		{
			name:   "sqlite with explicit DSN",
			driver: "sqlite",
			dsn:    filepath.Join(tmpDir, "test_explicit.db"),
		},
		{
			name:   "empty driver defaults to sqlite",
			driver: "",
			dsn:    filepath.Join(tmpDir, "test_default_driver.db"),
		},
		{
			name:        "postgres requires DSN",
			driver:      "postgres",
			dsn:         "",
			wantErr:     true,
			errContains: "postgres driver requires",
		},
		{
			name:        "postgres with invalid DSN fails to connect",
			driver:      "postgres",
			dsn:         "postgresql://nonexistent:5432/omneval?sslmode=disable",
			wantErr:     true,
			errContains: "postgres",
		},
		{
			name:        "unknown driver returns error",
			driver:      "mysql",
			dsn:         "localhost:3306",
			wantErr:     true,
			errContains: "unknown database driver",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			store, err := metadata.Open(tc.driver, tc.dsn)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if tc.errContains != "" && !strings.Contains(err.Error(), tc.errContains) {
					t.Errorf("error %q should contain %q", err, tc.errContains)
				}
				return
			}
			if err != nil {
				t.Fatalf("expected no error, got: %v", err)
			}
			store.Close()
		})
	}
}

// TestOpen_MigrationFailure pre-creates a table that the first migration also
// creates (without IF NOT EXISTS), so Migrate fails and Open must surface it.
func TestOpen_MigrationFailure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "conflict.db")

	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open raw sqlite: %v", err)
	}
	if _, err := db.Exec("CREATE TABLE organizations (bogus TEXT)"); err != nil {
		t.Fatalf("create conflicting table: %v", err)
	}
	db.Close()

	if _, err := metadata.Open("sqlite", path); err == nil {
		t.Fatal("expected migration error, got nil")
	} else if !strings.Contains(err.Error(), "migrate") {
		t.Errorf("error %q should contain %q", err, "migrate")
	}
}

// TestOpen_DefaultsApplied verifies the returned value implements the Store
// interface and migrations were applied (a query against a migrated table
// succeeds).
func TestOpen_DefaultsApplied(t *testing.T) {
	path := filepath.Join(t.TempDir(), "migrated.db")

	var store metadata.Store
	store, err := metadata.Open("sqlite", path)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	defer store.Close()

	if _, err := store.CountUsers(t.Context()); err != nil {
		t.Errorf("expected migrated schema, CountUsers failed: %v", err)
	}
}
