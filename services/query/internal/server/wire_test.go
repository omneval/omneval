package server

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	"github.com/omneval/omneval/internal/config"
	"github.com/omneval/omneval/internal/lake/lakeservertest"
	"github.com/omneval/omneval/internal/metadata/sqlite"
	_ "modernc.org/sqlite"
)

// wireTestConfig returns a config whose local pieces (SQLite metadata store)
// live in a temp dir and a local Quack Server backs the Lake attachment.
func wireTestConfig(t *testing.T) *config.Config {
	t.Helper()
	tmp := t.TempDir()
	cfg := &config.Config{}
	cfg.Database.Driver = "sqlite"
	cfg.Database.DSN = filepath.Join(tmp, "meta.db")

	lc, _ := lakeservertest.NewLocal(t)
	cfg.Quack.Client.URL = "quack://" + lc.QuackAddr
	cfg.Quack.Client.Token = lc.QuackToken
	cfg.Quack.Client.DataPath = lc.DataPath

	return cfg
}

func TestWireDeps_Success_NoS3(t *testing.T) {
	cfg := wireTestConfig(t)

	deps, err := WireDeps(cfg)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	defer deps.Close()

	if deps.Store == nil || deps.Lake == nil || deps.Auth == nil {
		t.Error("expected store, lake, and auth handler to be wired")
	}
	if deps.Span == nil || deps.Admin == nil || deps.Prompt == nil ||
		deps.Dataset == nil || deps.DatasetRun == nil || deps.Playground == nil {
		t.Error("expected all handlers to be constructed")
	}
	if deps.S3 != nil {
		t.Error("expected no S3 store when storage is not configured")
	}
	if deps.Prober == nil {
		t.Error("expected probes to be wired")
	}

	// The router must be buildable from wired deps.
	if router := buildRouter(deps); router == nil {
		t.Error("expected buildRouter to return a handler")
	}
}

func TestWireDeps_MetadataStoreFailure(t *testing.T) {
	cfg := wireTestConfig(t)
	cfg.Database.Driver = "nosuchdriver"

	_, err := WireDeps(cfg)
	if err == nil {
		t.Fatal("expected error for unknown metadata driver, got nil")
	}
	if !strings.Contains(err.Error(), "open metadata store") {
		t.Errorf("error %q should mention open metadata store", err)
	}
}

func TestWireDeps_AdminBootstrapFailure(t *testing.T) {
	cfg := wireTestConfig(t)
	cfg.Auth.AdminEmail = "admin@example.com"
	cfg.Auth.AdminPassword = "hunter2hunter2"

	// Migrate the metadata store, then drop the users table. The migration
	// is recorded as applied, so WireDeps won't recreate the table and the
	// admin bootstrap's user lookup fails with a real error (not NotFound).
	store, err := sqlite.New(cfg.Database.DSN)
	if err != nil {
		t.Fatalf("pre-create sqlite store: %v", err)
	}
	if err := store.Migrate(context.Background()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	store.Close()

	raw, err := sql.Open("sqlite", cfg.Database.DSN)
	if err != nil {
		t.Fatalf("open raw sqlite: %v", err)
	}
	if _, err := raw.Exec("DROP TABLE users"); err != nil {
		t.Fatalf("drop users table: %v", err)
	}
	raw.Close()

	_, err = WireDeps(cfg)
	if err == nil {
		t.Fatal("expected error from admin bootstrap, got nil")
	}
	if !strings.Contains(err.Error(), "bootstrap admin") {
		t.Errorf("error %q should mention bootstrap admin", err)
	}
}
