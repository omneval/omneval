package server

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

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

// TestProbeLakeStaysResponsiveWhenQueryPoolIsSaturated reproduces the
// production incident this fix addresses: ordinary UI load (e.g. the
// Dashboard's ten concurrent analytics queries) can legitimately saturate
// deps.Lake's connection pool — that's not a wedge. Before ProbeLake
// existed, the readiness/liveness Catalog check pinged through deps.Lake
// itself, so it queued behind that saturation exactly like a real wedge
// would and failed healthz/readyz (which gates liveness, see WireDeps)
// under nothing worse than normal Dashboard traffic, restarting healthy
// pods into a crash loop. ProbeLake's own connection must stay isolated
// from deps.Lake's pool regardless of how saturated the latter gets.
func TestProbeLakeStaysResponsiveWhenQueryPoolIsSaturated(t *testing.T) {
	cfg := wireTestConfig(t)

	deps, err := WireDeps(cfg)
	if err != nil {
		t.Fatalf("WireDeps: %v", err)
	}
	defer deps.Close()

	if deps.ProbeLake == nil {
		t.Fatal("expected ProbeLake to be wired")
	}

	ctx := context.Background()
	if err := deps.ProbeLake.Ping(ctx); err != nil { // warm up
		t.Fatalf("warmup ping: %v", err)
	}
	baselineStart := time.Now()
	if err := deps.ProbeLake.Ping(ctx); err != nil {
		t.Fatalf("baseline ping: %v", err)
	}
	baseline := time.Since(baselineStart)

	// Saturate every connection in deps.Lake's pool — as many held
	// connections as MaxOpenConns allows, so any further caller on
	// deps.Lake would have to queue behind them.
	poolSize := deps.Lake.DB().Stats().MaxOpenConnections
	if poolSize <= 0 {
		poolSize = 2
	}
	holdDuration := baseline * 8
	if holdDuration < 300*time.Millisecond {
		holdDuration = 300 * time.Millisecond
	}

	var wg sync.WaitGroup
	holdStarted := make(chan struct{}, poolSize)
	for i := 0; i < poolSize; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			conn, err := deps.Lake.DB().Conn(ctx)
			if err != nil {
				holdStarted <- struct{}{}
				return
			}
			defer conn.Close()
			holdStarted <- struct{}{}
			time.Sleep(holdDuration)
		}()
	}
	for i := 0; i < poolSize; i++ {
		<-holdStarted
	}
	time.Sleep(20 * time.Millisecond) // let the goroutines settle into their hold

	start := time.Now()
	if err := deps.ProbeLake.Ping(ctx); err != nil {
		t.Fatalf("ping while query pool saturated: %v", err)
	}
	elapsed := time.Since(start)
	if threshold := baseline*5 + 100*time.Millisecond; elapsed > threshold {
		t.Fatalf("ProbeLake.Ping took %v (baseline %v) while deps.Lake's pool was fully saturated for %v — expected it to stay isolated instead of queuing behind deps.Lake", elapsed, baseline, holdDuration)
	}

	wg.Wait()
}
