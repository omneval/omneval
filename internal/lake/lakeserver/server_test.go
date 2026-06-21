package lakeserver_test

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/omneval/omneval/internal/config"
	"github.com/omneval/omneval/internal/lake/lakeserver"
	"github.com/omneval/omneval/internal/lake/lakeservertest"
)

func TestConfigFromApp_CatalogDriver(t *testing.T) {
	cases := []struct {
		name            string
		catalogDriver   string
		databaseDriver  string
		wantCatalog     string
		wantCatalogDSN  string
	}{
		{
			name:           "no explicit catalogDriver + postgres database → duckdb",
			databaseDriver: "postgres",
			wantCatalog:    lakeserver.CatalogDriverLocal,
			wantCatalogDSN: "lake/catalog.ducklake",
		},
		{
			name:           "no explicit catalogDriver + sqlite database → duckdb",
			databaseDriver: "sqlite",
			wantCatalog:    lakeserver.CatalogDriverLocal,
			wantCatalogDSN: "lake/catalog.ducklake",
		},
		{
			name:           "no explicit catalogDriver + empty database → duckdb",
			databaseDriver: "",
			wantCatalog:    lakeserver.CatalogDriverLocal,
			wantCatalogDSN: "lake/catalog.ducklake",
		},
		{
			name:           "explicit catalogDriver postgres + postgres database → postgres",
			catalogDriver:  lakeserver.CatalogDriverPostgres,
			databaseDriver: "postgres",
			wantCatalog:    lakeserver.CatalogDriverPostgres,
			wantCatalogDSN: "host=localhost dbname=omneval",
		},
		{
			name:           "explicit catalogDriver postgres + sqlite database → postgres (override)",
			catalogDriver:  lakeserver.CatalogDriverPostgres,
			databaseDriver: "sqlite",
			wantCatalog:    lakeserver.CatalogDriverPostgres,
			wantCatalogDSN: "host=localhost dbname=omneval",
		},
		{
			name:          "explicit catalogDriver duckdb → duckdb",
			catalogDriver: lakeserver.CatalogDriverLocal,
			wantCatalog:   lakeserver.CatalogDriverLocal,
			wantCatalogDSN: "lake/catalog.ducklake",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			app := &config.Config{
				Database: config.DatabaseConfig{
					Driver: tc.databaseDriver,
					DSN:    "host=localhost dbname=omneval",
				},
				Quack: config.QuackConfig{
					Server: config.QuackServerConfig{
						CatalogDriver: tc.catalogDriver,
					},
				},
			}
			cfg := lakeserver.ConfigFromApp(app)
			if cfg.CatalogDriver != tc.wantCatalog {
				t.Errorf("CatalogDriver: got %q, want %q", cfg.CatalogDriver, tc.wantCatalog)
			}
			if cfg.CatalogDSN != tc.wantCatalogDSN {
				t.Errorf("CatalogDSN: got %q, want %q", cfg.CatalogDSN, tc.wantCatalogDSN)
			}
		})
	}
}

// TestConfigFromApp_MemoryLimit proves ConfigFromApp passes through
// quack.server.memory_limit unchanged (no derivation/defaulting — the right
// value depends entirely on the deployment's container memory limit, which
// this package cannot know).
func TestConfigFromApp_MemoryLimit(t *testing.T) {
	app := &config.Config{
		Quack: config.QuackConfig{
			Server: config.QuackServerConfig{
				MemoryLimit: "3GB",
			},
		},
	}
	cfg := lakeserver.ConfigFromApp(app)
	if cfg.MemoryLimit != "3GB" {
		t.Errorf("MemoryLimit: got %q, want %q", cfg.MemoryLimit, "3GB")
	}
}

// TestConfigFromApp_Threads proves ConfigFromApp passes through
// quack.server.threads unchanged.
func TestConfigFromApp_Threads(t *testing.T) {
	app := &config.Config{
		Quack: config.QuackConfig{
			Server: config.QuackServerConfig{
				Threads: 2,
			},
		},
	}
	cfg := lakeserver.ConfigFromApp(app)
	if cfg.Threads != 2 {
		t.Errorf("Threads: got %d, want %d", cfg.Threads, 2)
	}
}

// TestServe_AppliesMemoryLimit proves that a configured MemoryLimit is
// actually applied to the Quack Server's DuckDB session via `SET
// memory_limit`, so DuckDB's buffer manager backs off under memory pressure
// instead of growing until the kernel OOM-kills the process (the root cause
// of a production incident: quack-server has no memory_limit configured by
// default, so it sizes its buffer pool against the host's total RAM rather
// than the container's cgroup limit).
func TestServe_AppliesMemoryLimit(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	port := lakeservertest.FreePort(t)

	srv, err := lakeserver.Serve(ctx, lakeserver.Config{
		ListenAddr:    fmt.Sprintf(":%d", port),
		CatalogDriver: lakeserver.CatalogDriverLocal,
		CatalogDSN:    filepath.Join(dir, "catalog", "lake.ducklake"),
		MemoryLimit:   "512MiB",
	})
	if err != nil {
		t.Fatalf("serve: %v", err)
	}
	defer srv.Close()

	var limit string
	if err := srv.DB().QueryRowContext(ctx, "SELECT current_setting('memory_limit')").Scan(&limit); err != nil {
		t.Fatalf("query current_setting: %v", err)
	}
	if !strings.Contains(limit, "512") {
		t.Errorf("memory_limit: got %q, want it to reflect the configured 512MiB", limit)
	}
}

// TestServe_NoMemoryLimitConfigured proves that an empty MemoryLimit leaves
// DuckDB's default behavior untouched (no SET statement issued) — this is a
// regression guard for deployments that haven't set quack.server.memoryLimit.
func TestServe_NoMemoryLimitConfigured(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	port := lakeservertest.FreePort(t)

	srv, err := lakeserver.Serve(ctx, lakeserver.Config{
		ListenAddr:    fmt.Sprintf(":%d", port),
		CatalogDriver: lakeserver.CatalogDriverLocal,
		CatalogDSN:    filepath.Join(dir, "catalog", "lake.ducklake"),
	})
	if err != nil {
		t.Fatalf("serve: %v", err)
	}
	defer srv.Close()

	var limit string
	if err := srv.DB().QueryRowContext(ctx, "SELECT current_setting('memory_limit')").Scan(&limit); err != nil {
		t.Fatalf("query current_setting: %v", err)
	}
	if strings.Contains(limit, "512") {
		t.Errorf("memory_limit: got %q, expected DuckDB's untouched default, not the 512MB value from another test", limit)
	}
}

// TestServe_AppliesThreads proves that a configured Threads is actually
// applied to the Quack Server's DuckDB session via `SET threads`, so DuckDB
// caps its intra-query parallelism at the container's CPU limit instead of
// fanning out across the host's full core count and blowing through a CFS
// period's quota in one burst (the root cause of a production incident:
// query pod's /healthz and /readyz timed out under normal load even after
// the container's CPU limit was raised, because DuckDB kept detecting and
// using the host's full core count regardless of the cgroup limit).
func TestServe_AppliesThreads(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	port := lakeservertest.FreePort(t)

	srv, err := lakeserver.Serve(ctx, lakeserver.Config{
		ListenAddr:    fmt.Sprintf(":%d", port),
		CatalogDriver: lakeserver.CatalogDriverLocal,
		CatalogDSN:    filepath.Join(dir, "catalog", "lake.ducklake"),
		Threads:       2,
	})
	if err != nil {
		t.Fatalf("serve: %v", err)
	}
	defer srv.Close()

	var threads string
	if err := srv.DB().QueryRowContext(ctx, "SELECT current_setting('threads')").Scan(&threads); err != nil {
		t.Fatalf("query current_setting: %v", err)
	}
	if threads != "2" {
		t.Errorf("threads: got %q, want %q", threads, "2")
	}
}

// TestServe_NoThreadsConfigured proves that a zero Threads leaves DuckDB's
// default thread-count behavior untouched (no SET statement issued) — a
// regression guard for deployments that haven't set quack.server.threads.
func TestServe_NoThreadsConfigured(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	port := lakeservertest.FreePort(t)

	srv, err := lakeserver.Serve(ctx, lakeserver.Config{
		ListenAddr:    fmt.Sprintf(":%d", port),
		CatalogDriver: lakeserver.CatalogDriverLocal,
		CatalogDSN:    filepath.Join(dir, "catalog", "lake.ducklake"),
	})
	if err != nil {
		t.Fatalf("serve: %v", err)
	}
	defer srv.Close()

	var threads string
	if err := srv.DB().QueryRowContext(ctx, "SELECT current_setting('threads')").Scan(&threads); err != nil {
		t.Fatalf("query current_setting: %v", err)
	}
	if threads == "2" {
		t.Errorf("threads: got %q, expected DuckDB's untouched default, not the 2 value from another test", threads)
	}
}