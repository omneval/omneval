package server

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/omneval/omneval/internal/config"
	"github.com/omneval/omneval/internal/metadata"
)

func TestOpenMetadataStore(t *testing.T) {
	tmpDir := t.TempDir()

	tests := []struct {
		name        string
		driver      string
		dsn         string
		wantErr     bool
		errContains string
	}{
		{
			name:        "sqlite with explicit DSN",
			driver:      "sqlite",
			dsn:         filepath.Join(tmpDir, "test_explicit.db"),
			wantErr:     false,
			errContains: "",
		},
		{
			name:        "sqlite with default DSN",
			driver:      "sqlite",
			dsn:         "",
			wantErr:     false,
			errContains: "",
		},
		{
			name:        "empty driver defaults to sqlite",
			driver:      "",
			dsn:         filepath.Join(tmpDir, "test_default_driver.db"),
			wantErr:     false,
			errContains: "",
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
			cfg := &config.Config{
				Database: config.DatabaseConfig{
					Driver: tc.driver,
					DSN:    tc.dsn,
				},
			}

			store, err := openMetadataStore(cfg)
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
			defer store.Close()

			// NOTE: openSQLiteStore already calls Migrate() internally, so
			// no second Migrate() call here to avoid "table already exists" errors.
		})
	}
}

// TestOpenMetadataStore_ReturnsStoreInterface verifies the returned value
// implements the metadata.Store interface.
func TestOpenMetadataStore_ReturnsStoreInterface(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := &config.Config{
		Database: config.DatabaseConfig{
			Driver: "sqlite",
			DSN:    filepath.Join(tmpDir, "test_interface.db"),
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
