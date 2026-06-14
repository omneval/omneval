package server

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/omneval/omneval/internal/config"
)

// wireTestConfig returns a config whose local pieces (SQLite metadata
// store) live in a temp dir, the Lake disabled (no DuckLake/Quack server
// available in unit tests), and Redis pointed at a closed port so tests
// exercising earlier failures never touch a real server.
func wireTestConfig(t *testing.T) *config.Config {
	t.Helper()
	tmp := t.TempDir()
	cfg := &config.Config{}
	cfg.Database.Driver = "sqlite"
	cfg.Database.DSN = filepath.Join(tmp, "meta.db")
	cfg.Redis.Addr = "127.0.0.1:1" // reserved port: connect always fails fast
	cfg.Writer.Lake.Enabled = false
	return cfg
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
