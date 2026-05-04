package config_test

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/zbloss/lantern/internal/config"
)

func TestLoad_Defaults(t *testing.T) {
	cfg, err := config.Load("")
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	cases := []struct {
		name string
		got  any
		want any
	}{
		{"redis.addr", cfg.Redis.Addr, "localhost:6379"},
		{"redis.db", cfg.Redis.DB, 0},
		{"auth.session_ttl", cfg.Auth.SessionTTL, "168h"},
		{"auth.secure_cookie", cfg.Auth.SecureCookie, false},
		{"ingest.addr", cfg.Ingest.Addr, ":8000"},
		{"ingest.log_system_prompt", cfg.Ingest.LogSystemPrompt, true},
		{"writer.addr", cfg.Writer.Addr, ":8001"},
		{"writer.sync_interval", cfg.Writer.SyncInterval, "30s"},
		{"writer.flush_interval", cfg.Writer.FlushInterval, "30m"},
		{"writer.flush_age_days", cfg.Writer.FlushAgeDays, 2},
		{"query.addr", cfg.Query.Addr, ":8002"},
		{"query.sync_interval", cfg.Query.SyncInterval, "30s"},
		{"eval.addr", cfg.Eval.Addr, ":8003"},
		{"eval.concurrency", cfg.Eval.Concurrency, 4},
		{"metrics.addr", cfg.Metrics.Addr, ":9090"},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, c.got, c.want)
		}
	}

	// CORS allowed origins defaults to ["*"]
	if len(cfg.Ingest.CORSAllowedOrigins) != 1 {
		t.Errorf("CORSAllowedOrigins length: got %d, want 1", len(cfg.Ingest.CORSAllowedOrigins))
	}
	if !slices.Contains(cfg.Ingest.CORSAllowedOrigins, "*") {
		t.Errorf("CORSAllowedOrigins: got %v, want [\"*\"]", cfg.Ingest.CORSAllowedOrigins)
	}
}

func TestLoad_FromFile(t *testing.T) {
	yaml := `
database:
  driver: postgres
  dsn: "host=localhost dbname=lantern"
redis:
  addr: "redis:6380"
  password: "secret"
  db: 1
auth:
  session_ttl: "24h"
  secure_cookie: true
  admin_email: "admin@example.com"
  admin_password: "hunter2"
eval:
  llm_base_url: "http://litellm:4000/v1"
  llm_api_key: "sk-test"
`
	f := filepath.Join(t.TempDir(), "lantern.yaml")
	if err := os.WriteFile(f, []byte(yaml), 0600); err != nil {
		t.Fatal(err)
	}

	cfg, err := config.Load(f)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	if cfg.Database.Driver != "postgres" {
		t.Errorf("database.driver: got %q, want %q", cfg.Database.Driver, "postgres")
	}
	if cfg.Redis.Addr != "redis:6380" {
		t.Errorf("redis.addr: got %q, want %q", cfg.Redis.Addr, "redis:6380")
	}
	if cfg.Redis.Password != "secret" {
		t.Errorf("redis.password: got %q, want %q", cfg.Redis.Password, "secret")
	}
	if cfg.Redis.DB != 1 {
		t.Errorf("redis.db: got %d, want 1", cfg.Redis.DB)
	}
	if cfg.Auth.SessionTTL != "24h" {
		t.Errorf("auth.session_ttl: got %q, want %q", cfg.Auth.SessionTTL, "24h")
	}
	if !cfg.Auth.SecureCookie {
		t.Error("auth.secure_cookie: got false, want true")
	}
	if cfg.Auth.AdminEmail != "admin@example.com" {
		t.Errorf("auth.admin_email: got %q", cfg.Auth.AdminEmail)
	}
	if cfg.Auth.AdminPassword != "hunter2" {
		t.Errorf("auth.admin_password: got %q", cfg.Auth.AdminPassword)
	}
	if cfg.Eval.LLMBaseURL != "http://litellm:4000/v1" {
		t.Errorf("eval.llm_base_url: got %q", cfg.Eval.LLMBaseURL)
	}
	if cfg.Eval.LLMAPIKey != "sk-test" {
		t.Errorf("eval.llm_api_key: got %q", cfg.Eval.LLMAPIKey)
	}
}

func TestLoad_EnvOverrides(t *testing.T) {
	t.Setenv("LANTERN_AUTH_ADMIN_EMAIL", "env@example.com")
	t.Setenv("LANTERN_AUTH_ADMIN_PASSWORD", "envpass")
	t.Setenv("LANTERN_REDIS_ADDR", "redis-env:6379")
	t.Setenv("LANTERN_EVAL_LLM_BASE_URL", "http://env-llm/v1")
	t.Setenv("LANTERN_EVAL_LLM_API_KEY", "env-key")

	cfg, err := config.Load("")
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	if cfg.Auth.AdminEmail != "env@example.com" {
		t.Errorf("LANTERN_AUTH_ADMIN_EMAIL: got %q", cfg.Auth.AdminEmail)
	}
	if cfg.Auth.AdminPassword != "envpass" {
		t.Errorf("LANTERN_AUTH_ADMIN_PASSWORD: got %q", cfg.Auth.AdminPassword)
	}
	if cfg.Redis.Addr != "redis-env:6379" {
		t.Errorf("LANTERN_REDIS_ADDR: got %q", cfg.Redis.Addr)
	}
	if cfg.Eval.LLMBaseURL != "http://env-llm/v1" {
		t.Errorf("LANTERN_EVAL_LLM_BASE_URL: got %q", cfg.Eval.LLMBaseURL)
	}
	if cfg.Eval.LLMAPIKey != "env-key" {
		t.Errorf("LANTERN_EVAL_LLM_API_KEY: got %q", cfg.Eval.LLMAPIKey)
	}
}

func TestLoad_CORSFromFile(t *testing.T) {
	yaml := `
ingest:
  cors_allowed_origins:
    - http://localhost:3000
    - https://app.example.com
`
	f := filepath.Join(t.TempDir(), "lantern.yaml")
	if err := os.WriteFile(f, []byte(yaml), 0600); err != nil {
		t.Fatal(err)
	}

	cfg, err := config.Load(f)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	if len(cfg.Ingest.CORSAllowedOrigins) != 2 {
		t.Fatalf("CORSAllowedOrigins length: got %d, want 2", len(cfg.Ingest.CORSAllowedOrigins))
	}
	if cfg.Ingest.CORSAllowedOrigins[0] != "http://localhost:3000" {
		t.Errorf("CORSAllowedOrigins[0]: got %q, want %q", cfg.Ingest.CORSAllowedOrigins[0], "http://localhost:3000")
	}
	if cfg.Ingest.CORSAllowedOrigins[1] != "https://app.example.com" {
		t.Errorf("CORSAllowedOrigins[1]: got %q, want %q", cfg.Ingest.CORSAllowedOrigins[1], "https://app.example.com")
	}
}

func TestLoad_CORS_EnvOverride(t *testing.T) {
	t.Setenv("LANTERN_INGEST_CORS_ALLOWED_ORIGINS", "http://override.com")

	cfg, err := config.Load("")
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	if len(cfg.Ingest.CORSAllowedOrigins) != 1 {
		t.Fatalf("CORSAllowedOrigins length: got %d, want 1", len(cfg.Ingest.CORSAllowedOrigins))
	}
	if cfg.Ingest.CORSAllowedOrigins[0] != "http://override.com" {
		t.Errorf("CORSAllowedOrigins[0]: got %q, want %q", cfg.Ingest.CORSAllowedOrigins[0], "http://override.com")
	}
}
