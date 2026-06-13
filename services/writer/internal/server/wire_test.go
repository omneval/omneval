package server

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/omneval/omneval/internal/config"
	"github.com/omneval/omneval/internal/leader"
)

// wireTestConfig returns a config whose local pieces (DuckDB path, SQLite
// metadata store) live in a temp dir. Redis points at a closed port so tests
// exercising earlier failures never touch a real server.
func wireTestConfig(t *testing.T) *config.Config {
	t.Helper()
	tmp := t.TempDir()
	cfg := &config.Config{}
	cfg.Writer.DuckDBPath = filepath.Join(tmp, "writer.db")
	cfg.Database.Driver = "sqlite"
	cfg.Database.DSN = filepath.Join(tmp, "meta.db")
	cfg.Redis.Addr = "127.0.0.1:1" // reserved port: connect always fails fast
	return cfg
}

func TestWireDeps_DuckDBOpenFailure(t *testing.T) {
	cfg := wireTestConfig(t)
	// A directory is not a valid DuckDB file path.
	cfg.Writer.DuckDBPath = t.TempDir()

	_, err := WireDeps(cfg)
	if err == nil {
		t.Fatal("expected error for unopenable DuckDB path, got nil")
	}
	if !strings.Contains(err.Error(), "duckdb") {
		t.Errorf("error %q should mention duckdb", err)
	}
}

func TestWireDeps_MetadataStoreFailure(t *testing.T) {
	cfg := wireTestConfig(t)
	cfg.Database.Driver = "nosuchdriver"

	_, err := WireDeps(cfg)
	if err == nil {
		t.Fatal("expected error for unknown metadata driver, got nil")
	}
	if !strings.Contains(err.Error(), "open metadata") {
		t.Errorf("error %q should mention open metadata", err)
	}
}

func TestWireDeps_RedisPingFailure(t *testing.T) {
	cfg := wireTestConfig(t)

	_, err := WireDeps(cfg)
	if err == nil {
		t.Fatal("expected error for unreachable redis, got nil")
	}
	if !strings.Contains(err.Error(), "redis ping") {
		t.Errorf("error %q should mention redis ping", err)
	}
}

// fakeLeaderOps returns leader.Ops backed by an in-memory single-key lock.
func fakeLeaderOps(acquireOK bool, acquireErr error) leader.Ops {
	var held string
	return leader.Ops{
		SetNX: func(ctx context.Context, key, value string, ttl time.Duration) (bool, error) {
			if acquireOK && acquireErr == nil {
				held = value
			}
			return acquireOK, acquireErr
		},
		Get: func(ctx context.Context, key string) (string, error) {
			if held != "" {
				return held, nil
			}
			return "someone-else", nil
		},
		Set: func(ctx context.Context, key, value string, ttl time.Duration) (bool, error) {
			return true, nil
		},
		Del: func(ctx context.Context, keys ...string) (int64, error) {
			return 1, nil
		},
		DelIfMatch: func(ctx context.Context, key, expected string) (bool, error) {
			return true, nil
		},
	}
}

func TestSetupLeaderElection_AcquiresLock(t *testing.T) {
	cfg := wireTestConfig(t)
	cfg.Writer.LeaderElection.Enabled = true
	cfg.Writer.LeaderElection.LockTTL = 15

	election, err := setupLeaderElection(cfg, fakeLeaderOps(true, nil))
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if !election.IsLeader() {
		t.Error("expected instance to be leader after successful acquire")
	}
}

func TestSetupLeaderElection_NotLeader(t *testing.T) {
	cfg := wireTestConfig(t)
	cfg.Writer.LeaderElection.Enabled = true

	election, err := setupLeaderElection(cfg, fakeLeaderOps(false, nil))
	if err != nil {
		t.Fatalf("expected no error when lock is held elsewhere, got: %v", err)
	}
	if election.IsLeader() {
		t.Error("expected instance to not be leader when lock is held elsewhere")
	}
}

func TestSetupLeaderElection_AcquireError(t *testing.T) {
	cfg := wireTestConfig(t)
	cfg.Writer.LeaderElection.Enabled = true

	_, err := setupLeaderElection(cfg, fakeLeaderOps(false, fmt.Errorf("redis exploded")))
	if err == nil {
		t.Fatal("expected error when acquire fails, got nil")
	}
	if !strings.Contains(err.Error(), "acquire leader lock") {
		t.Errorf("error %q should mention acquire leader lock", err)
	}
}

func TestWireDeps_FencingDefaultsOnWhenElectionEnabled(t *testing.T) {
	cfg := wireTestConfig(t)
	cfg.Writer.LeaderElection.Enabled = true

	// Redis is unreachable, so WireDeps errors later — the fencing default
	// is applied up front and must stick regardless.
	_, _ = WireDeps(cfg)
	if !cfg.Writer.LeaderElection.FencingEnabled {
		t.Error("expected fencing_enabled to default to true when leader election is enabled")
	}
}
