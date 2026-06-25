package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestAllSectionLoadersHaveLoaders verifies that all sections in the Config
// have corresponding loaders registered in AllLoaders.
func TestAllSectionLoadersHaveLoaders(t *testing.T) {
	loaders := AllLoaders()

	// We expect 13 loaders: Database, LogLevel, Redis, Auth, Ingest, Writer,
	// Query, Eval, QuackServer, QuackClient, Pricing, Metrics, Storage.
	if len(loaders) != 13 {
		t.Errorf("AllLoaders() returned %d loaders, want 13", len(loaders))
	}
}

// TestLoadWithLoadersIntegration verifies that the refactored Load() function
// produces identical results to the original behaviour by testing a full
// config load from a YAML file with env overrides.
func TestLoadWithLoadersIntegration(t *testing.T) {
	yaml := `
database:
  driver: postgres
  dsn: "host=localhost dbname=omneval"
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
	f := filepath.Join(t.TempDir(), "omneval.yaml")
	if err := os.WriteFile(f, []byte(yaml), 0600); err != nil {
		t.Fatal(err)
	}

	t.Setenv("OMNEVAL_AUTH_ADMIN_EMAIL", "env@example.com")
	t.Setenv("OMNEVAL_AUTH_ADMIN_PASSWORD", "envpass")
	t.Setenv("OMNEVAL_REDIS_ADDR", "redis-env:6379")
	t.Setenv("OMNEVAL_EVAL_LLM_BASE_URL", "http://env-llm/v1")
	t.Setenv("OMNEVAL_EVAL_LLM_API_KEY", "env-key")

	cfg, err := Load(f)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	// Env overrides should win over file values
	if cfg.Auth.AdminEmail != "env@example.com" {
		t.Errorf("Auth.AdminEmail: got %q, want %q (env override)", cfg.Auth.AdminEmail, "env@example.com")
	}
	if cfg.Auth.AdminPassword != "envpass" {
		t.Errorf("Auth.AdminPassword: got %q, want %q (env override)", cfg.Auth.AdminPassword, "envpass")
	}
	if cfg.Redis.Addr != "redis-env:6379" {
		t.Errorf("Redis.Addr: got %q, want %q (env override)", cfg.Redis.Addr, "redis-env:6379")
	}
	if cfg.Eval.LLMBaseURL != "http://env-llm/v1" {
		t.Errorf("Eval.LLMBaseURL: got %q, want %q (env override)", cfg.Eval.LLMBaseURL, "http://env-llm/v1")
	}

	// Non-overridden file values should be preserved
	if cfg.Database.Driver != "postgres" {
		t.Errorf("Database.Driver: got %q, want %q", cfg.Database.Driver, "postgres")
	}
	if cfg.Redis.Password != "secret" {
		t.Errorf("Redis.Password: got %q, want %q", cfg.Redis.Password, "secret")
	}
	if cfg.Auth.SessionTTL != "24h" {
		t.Errorf("Auth.SessionTTL: got %q, want %q", cfg.Auth.SessionTTL, "24h")
	}
	// OMNEVAL_EVAL_LLM_API_KEY is set above, so env wins over file value
	if cfg.Eval.LLMAPIKey != "env-key" {
		t.Errorf("Eval.LLMAPIKey: got %q, want %q (env override)", cfg.Eval.LLMAPIKey, "env-key")
	}
}

// TestLoadFromYAML_NoEnv verifies that a YAML file produces the expected config.
func TestLoadFromYAML_NoEnv(t *testing.T) {
	yaml := `
database:
  driver: postgres
  dsn: "host=localhost dbname=test"
redis:
  addr: "redis.internal:6380"
  password: "secret"
  db: 3
storage:
  endpoint: "s3.us-west-2.amazonaws.com"
  bucket: "omneval-data"
  region: "us-west-2"
ingest:
  addr: ":8888"
  log_system_prompt: false
writer:
  addr: ":8889"
  lake:
    enabled: false
query:
  addr: ":8890"
  lake:
    enabled: false
eval:
  addr: ":8891"
  concurrency: 2
  llm_base_url: "http://llm.internal:8080"
  llm_model: "claude-sonnet-4-20250514"
  llm_api_key: "test-key"
metrics:
  addr: ":9100"
  disable_project_labels: true
quack:
  server:
    listen_addr: ":9495"
    token: "server-token"
    catalog_driver: "postgres"
    catalog_dsn: "host=catdb user=cat"
    data_path: "s3://omneval-lake/data"
    retention:
      enabled: true
      max_age_days: 30
  client:
    url: "quack-server:9495"
    token: "client-token"
    data_path: "s3://omneval-lake/data"
    max_open_conns: 4
    memory_limit: "1024MiB"
auth:
  session_ttl: "24h"
  secure_cookie: true
  admin_email: "admin@example.com"
  admin_password: "adminpass"
`
	f := filepath.Join(t.TempDir(), "omneval.yaml")
	if err := os.WriteFile(f, []byte(yaml), 0600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(f)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	if cfg.LogLevel != "info" {
		t.Errorf("LogLevel: got %q, want %q", cfg.LogLevel, "info")
	}
	if cfg.Database.Driver != "postgres" {
		t.Errorf("Database.Driver: got %q, want %q", cfg.Database.Driver, "postgres")
	}
	if cfg.Database.DSN != "host=localhost dbname=test" {
		t.Errorf("Database.DSN: got %q, want %q", cfg.Database.DSN, "host=localhost dbname=test")
	}
	if cfg.Redis.Addr != "redis.internal:6380" {
		t.Errorf("Redis.Addr: got %q, want %q", cfg.Redis.Addr, "redis.internal:6380")
	}
	if cfg.Redis.Password != "secret" {
		t.Errorf("Redis.Password: got %q, want %q", cfg.Redis.Password, "secret")
	}
	if cfg.Redis.DB != 3 {
		t.Errorf("Redis.DB: got %d, want %d", cfg.Redis.DB, 3)
	}
	if cfg.Storage.Endpoint != "s3.us-west-2.amazonaws.com" {
		t.Errorf("Storage.Endpoint: got %q, want %q", cfg.Storage.Endpoint, "s3.us-west-2.amazonaws.com")
	}
	if cfg.Storage.Bucket != "omneval-data" {
		t.Errorf("Storage.Bucket: got %q, want %q", cfg.Storage.Bucket, "omneval-data")
	}
	if cfg.Storage.Region != "us-west-2" {
		t.Errorf("Storage.Region: got %q, want %q", cfg.Storage.Region, "us-west-2")
	}
	if cfg.Auth.SessionTTL != "24h" {
		t.Errorf("Auth.SessionTTL: got %q, want %q", cfg.Auth.SessionTTL, "24h")
	}
	if !cfg.Auth.SecureCookie {
		t.Error("Auth.SecureCookie: got false, want true")
	}
	if cfg.Auth.AdminEmail != "admin@example.com" {
		t.Errorf("Auth.AdminEmail: got %q, want %q", cfg.Auth.AdminEmail, "admin@example.com")
	}
	if cfg.Auth.AdminPassword != "adminpass" {
		t.Errorf("Auth.AdminPassword: got %q, want %q", cfg.Auth.AdminPassword, "adminpass")
	}
	if cfg.Ingest.Addr != ":8888" {
		t.Errorf("Ingest.Addr: got %q, want %q", cfg.Ingest.Addr, ":8888")
	}
	if cfg.Ingest.LogSystemPrompt != false {
		t.Errorf("Ingest.LogSystemPrompt: got %v, want false", cfg.Ingest.LogSystemPrompt)
	}
	if cfg.Writer.Addr != ":8889" {
		t.Errorf("Writer.Addr: got %q, want %q", cfg.Writer.Addr, ":8889")
	}
	if cfg.Writer.Lake.Enabled != false {
		t.Errorf("Writer.Lake.Enabled: got %v, want false", cfg.Writer.Lake.Enabled)
	}
	if cfg.Query.Addr != ":8890" {
		t.Errorf("Query.Addr: got %q, want %q", cfg.Query.Addr, ":8890")
	}
	if cfg.Query.Lake.Enabled != false {
		t.Errorf("Query.Lake.Enabled: got %v, want false", cfg.Query.Lake.Enabled)
	}
	if cfg.Eval.Addr != ":8891" {
		t.Errorf("Eval.Addr: got %q, want %q", cfg.Eval.Addr, ":8891")
	}
	if cfg.Eval.Concurrency != 2 {
		t.Errorf("Eval.Concurrency: got %d, want %d", cfg.Eval.Concurrency, 2)
	}
	if cfg.Eval.LLMBaseURL != "http://llm.internal:8080" {
		t.Errorf("Eval.LLMBaseURL: got %q, want %q", cfg.Eval.LLMBaseURL, "http://llm.internal:8080")
	}
	if cfg.Eval.LLMModel != "claude-sonnet-4-20250514" {
		t.Errorf("Eval.LLMModel: got %q, want %q", cfg.Eval.LLMModel, "claude-sonnet-4-20250514")
	}
	if cfg.Eval.LLMAPIKey != "test-key" {
		t.Errorf("Eval.LLMAPIKey: got %q, want %q", cfg.Eval.LLMAPIKey, "test-key")
	}
	if cfg.Metrics.Addr != ":9100" {
		t.Errorf("Metrics.Addr: got %q, want %q", cfg.Metrics.Addr, ":9100")
	}
	if !cfg.Metrics.DisableProjectLabels {
		t.Error("Metrics.DisableProjectLabels: got false, want true")
	}
	if cfg.Quack.Server.ListenAddr != ":9495" {
		t.Errorf("Quack.Server.ListenAddr: got %q, want %q", cfg.Quack.Server.ListenAddr, ":9495")
	}
	if cfg.Quack.Server.Token != "server-token" {
		t.Errorf("Quack.Server.Token: got %q, want %q", cfg.Quack.Server.Token, "server-token")
	}
	if cfg.Quack.Server.CatalogDriver != "postgres" {
		t.Errorf("Quack.Server.CatalogDriver: got %q, want %q", cfg.Quack.Server.CatalogDriver, "postgres")
	}
	if cfg.Quack.Server.CatalogDSN != "host=catdb user=cat" {
		t.Errorf("Quack.Server.CatalogDSN: got %q, want %q", cfg.Quack.Server.CatalogDSN, "host=catdb user=cat")
	}
	if cfg.Quack.Server.DataPath != "s3://omneval-lake/data" {
		t.Errorf("Quack.Server.DataPath: got %q, want %q", cfg.Quack.Server.DataPath, "s3://omneval-lake/data")
	}
	if !cfg.Quack.Server.Retention.Enabled {
		t.Error("Quack.Server.Retention.Enabled: got false, want true")
	}
	if cfg.Quack.Server.Retention.MaxAgeDays != 30 {
		t.Errorf("Quack.Server.Retention.MaxAgeDays: got %d, want %d", cfg.Quack.Server.Retention.MaxAgeDays, 30)
	}
	if cfg.Quack.Client.URL != "quack-server:9495" {
		t.Errorf("Quack.Client.URL: got %q, want %q", cfg.Quack.Client.URL, "quack-server:9495")
	}
	if cfg.Quack.Client.Token != "client-token" {
		t.Errorf("Quack.Client.Token: got %q, want %q", cfg.Quack.Client.Token, "client-token")
	}
	if cfg.Quack.Client.DataPath != "s3://omneval-lake/data" {
		t.Errorf("Quack.Client.DataPath: got %q, want %q", cfg.Quack.Client.DataPath, "s3://omneval-lake/data")
	}
	if cfg.Quack.Client.MaxOpenConns != 4 {
		t.Errorf("Quack.Client.MaxOpenConns: got %d, want %d", cfg.Quack.Client.MaxOpenConns, 4)
	}
	if cfg.Quack.Client.MemoryLimit != "1024MiB" {
		t.Errorf("Quack.Client.MemoryLimit: got %q, want %q", cfg.Quack.Client.MemoryLimit, "1024MiB")
	}
}

// TestEnvOverridesYAMLFile verifies that env vars override YAML file values.
func TestEnvOverridesYAMLFile(t *testing.T) {
	yaml := `
database:
  driver: postgres
  dsn: "host=localhost dbname=test"
redis:
  addr: "redis.local:6380"
ingest:
  addr: ":8888"
  log_system_prompt: false
writer:
  addr: ":8889"
  lake:
    enabled: false
query:
  addr: ":8890"
  lake:
    enabled: false
eval:
  addr: ":8891"
  concurrency: 2
  llm_base_url: "http://internal:8080"
  llm_api_key: "internal-key"
metrics:
  addr: ":9100"
  disable_project_labels: true
quack:
  server:
    listen_addr: ":9495"
    token: "server-token"
  client:
    url: "quack-internal:9495"
    token: "client-token"
`
	f := filepath.Join(t.TempDir(), "omneval.yaml")
	if err := os.WriteFile(f, []byte(yaml), 0600); err != nil {
		t.Fatal(err)
	}

	t.Setenv("OMNEVAL_DATABASE_DRIVER", "sqlite")
	t.Setenv("OMNEVAL_DATABASE_DSN", "file:testdb")
	t.Setenv("OMNEVAL_REDIS_ADDR", "redis-env:6379")
	t.Setenv("OMNEVAL_INGEST_ADDR", ":8887")
	t.Setenv("OMNEVAL_INGEST_LOG_SYSTEM_PROMPT", "true")
	t.Setenv("OMNEVAL_WRITER_ADDR", ":8886")
	t.Setenv("OMNEVAL_WRITER_LAKE_ENABLED", "true")
	t.Setenv("OMNEVAL_QUERY_ADDR", ":8885")
	t.Setenv("OMNEVAL_QUERY_LAKE_ENABLED", "true")
	t.Setenv("OMNEVAL_EVAL_ADDR", ":8884")
	t.Setenv("OMNEVAL_EVAL_CONCURRENCY", "8")
	t.Setenv("OMNEVAL_EVAL_LLM_BASE_URL", "http://env-llm:4000")
	t.Setenv("OMNEVAL_EVAL_LLM_API_KEY", "env-key")
	t.Setenv("OMNEVAL_METRICS_ADDR", ":9091")
	t.Setenv("OMNEVAL_METRICS_DISABLE_PROJECT_LABELS", "false")
	t.Setenv("OMNEVAL_QUACK_SERVER_LISTEN_ADDR", ":9496")
	t.Setenv("OMNEVAL_QUACK_SERVER_TOKEN", "env-server-token")
	t.Setenv("OMNEVAL_QUACK_CLIENT_URL", "quack-env:9496")
	t.Setenv("OMNEVAL_QUACK_CLIENT_TOKEN", "env-client-token")
	t.Setenv("OMNEVAL_LOG_LEVEL", "warn")

	cfg, err := Load(f)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	if cfg.Database.Driver != "sqlite" {
		t.Errorf("Database.Driver: got %q, want %q", cfg.Database.Driver, "sqlite")
	}
	if cfg.Database.DSN != "file:testdb" {
		t.Errorf("Database.DSN: got %q, want %q", cfg.Database.DSN, "file:testdb")
	}
	if cfg.Redis.Addr != "redis-env:6379" {
		t.Errorf("Redis.Addr: got %q, want %q", cfg.Redis.Addr, "redis-env:6379")
	}
	if cfg.LogLevel != "warn" {
		t.Errorf("LogLevel: got %q, want %q", cfg.LogLevel, "warn")
	}
	if cfg.Ingest.Addr != ":8887" {
		t.Errorf("Ingest.Addr: got %q, want %q", cfg.Ingest.Addr, ":8887")
	}
	if cfg.Ingest.LogSystemPrompt != true {
		t.Errorf("Ingest.LogSystemPrompt: got %v, want true", cfg.Ingest.LogSystemPrompt)
	}
	if cfg.Writer.Addr != ":8886" {
		t.Errorf("Writer.Addr: got %q, want %q", cfg.Writer.Addr, ":8886")
	}
	if cfg.Writer.Lake.Enabled != true {
		t.Errorf("Writer.Lake.Enabled: got %v, want true", cfg.Writer.Lake.Enabled)
	}
	if cfg.Query.Addr != ":8885" {
		t.Errorf("Query.Addr: got %q, want %q", cfg.Query.Addr, ":8885")
	}
	if cfg.Query.Lake.Enabled != true {
		t.Errorf("Query.Lake.Enabled: got %v, want true", cfg.Query.Lake.Enabled)
	}
	if cfg.Eval.Addr != ":8884" {
		t.Errorf("Eval.Addr: got %q, want %q", cfg.Eval.Addr, ":8884")
	}
	if cfg.Eval.Concurrency != 8 {
		t.Errorf("Eval.Concurrency: got %d, want %d", cfg.Eval.Concurrency, 8)
	}
	if cfg.Eval.LLMBaseURL != "http://env-llm:4000" {
		t.Errorf("Eval.LLMBaseURL: got %q, want %q", cfg.Eval.LLMBaseURL, "http://env-llm:4000")
	}
	if cfg.Eval.LLMAPIKey != "env-key" {
		t.Errorf("Eval.LLMAPIKey: got %q, want %q", cfg.Eval.LLMAPIKey, "env-key")
	}
	if cfg.Metrics.Addr != ":9091" {
		t.Errorf("Metrics.Addr: got %q, want %q", cfg.Metrics.Addr, ":9091")
	}
	if cfg.Metrics.DisableProjectLabels != false {
		t.Errorf("Metrics.DisableProjectLabels: got %v, want false", cfg.Metrics.DisableProjectLabels)
	}
	if cfg.Quack.Server.ListenAddr != ":9496" {
		t.Errorf("Quack.Server.ListenAddr: got %q, want %q", cfg.Quack.Server.ListenAddr, ":9496")
	}
	if cfg.Quack.Server.Token != "env-server-token" {
		t.Errorf("Quack.Server.Token: got %q, want %q", cfg.Quack.Server.Token, "env-server-token")
	}
	if cfg.Quack.Client.URL != "quack-env:9496" {
		t.Errorf("Quack.Client.URL: got %q, want %q", cfg.Quack.Client.URL, "quack-env:9496")
	}
	if cfg.Quack.Client.Token != "env-client-token" {
		t.Errorf("Quack.Client.Token: got %q, want %q", cfg.Quack.Client.Token, "env-client-token")
	}
}

// TestLoad_NoEnvFile verifies Load works with no env vars and no config file.
func TestLoad_NoEnvFile(t *testing.T) {
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	if cfg.LogLevel != "info" {
		t.Errorf("LogLevel: got %q, want %q", cfg.LogLevel, "info")
	}
	if cfg.Database.Driver != "" {
		t.Errorf("Database.Driver: got %q, want empty", cfg.Database.Driver)
	}
	if cfg.Redis.Addr != "localhost:6379" {
		t.Errorf("Redis.Addr: got %q, want %q", cfg.Redis.Addr, "localhost:6379")
	}
	if cfg.Ingest.Addr != ":8000" {
		t.Errorf("Ingest.Addr: got %q, want %q", cfg.Ingest.Addr, ":8000")
	}
	if cfg.Ingest.LogSystemPrompt != true {
		t.Errorf("Ingest.LogSystemPrompt: got %v, want true", cfg.Ingest.LogSystemPrompt)
	}
	if cfg.Writer.Addr != ":8001" {
		t.Errorf("Writer.Addr: got %q, want %q", cfg.Writer.Addr, ":8001")
	}
	if cfg.Writer.Lake.Enabled != true {
		t.Errorf("Writer.Lake.Enabled: got %v, want true", cfg.Writer.Lake.Enabled)
	}
	if cfg.Query.Addr != ":8002" {
		t.Errorf("Query.Addr: got %q, want %q", cfg.Query.Addr, ":8002")
	}
	if cfg.Query.Lake.Enabled != true {
		t.Errorf("Query.Lake.Enabled: got %v, want true", cfg.Query.Lake.Enabled)
	}
	if cfg.Eval.Addr != ":8003" {
		t.Errorf("Eval.Addr: got %q, want %q", cfg.Eval.Addr, ":8003")
	}
	if cfg.Eval.Concurrency != 4 {
		t.Errorf("Eval.Concurrency: got %d, want %d", cfg.Eval.Concurrency, 4)
	}
	if cfg.Eval.LLMModel != "gpt-4" {
		t.Errorf("Eval.LLMModel: got %q, want %q", cfg.Eval.LLMModel, "gpt-4")
	}
	if cfg.Eval.RetryCount != 3 {
		t.Errorf("Eval.RetryCount: got %d, want %d", cfg.Eval.RetryCount, 3)
	}
	if cfg.Metrics.Addr != ":9090" {
		t.Errorf("Metrics.Addr: got %q, want %q", cfg.Metrics.Addr, ":9090")
	}
	if cfg.Quack.Server.ListenAddr != ":9494" {
		t.Errorf("Quack.Server.ListenAddr: got %q, want %q", cfg.Quack.Server.ListenAddr, ":9494")
	}
	if cfg.Quack.Client.URL != "localhost:9494" {
		t.Errorf("Quack.Client.URL: got %q, want %q", cfg.Quack.Client.URL, "localhost:9494")
	}
	if cfg.Auth.SessionTTL != "168h" {
		t.Errorf("Auth.SessionTTL: got %q, want %q", cfg.Auth.SessionTTL, "168h")
	}
}

// TestLoad_FailsOnBadYAML verifies Load returns an error when given invalid YAML.
func TestLoad_FailsOnBadYAML(t *testing.T) {
	yaml := `this: is: not: valid: yaml: [[[[[`
	f := filepath.Join(t.TempDir(), "bad.yaml")
	if err := os.WriteFile(f, []byte(yaml), 0600); err != nil {
		t.Fatal(err)
	}

	_, err := Load(f)
	if err == nil {
		t.Fatal("expected error for invalid YAML, got nil")
	}

	if !strings.Contains(err.Error(), "reading config") {
		t.Errorf("error should contain 'reading config': got %q", err.Error())
	}
}

// TestSectionLoaderOrder verifies that loaders are called in a stable order.
func TestSectionLoaderOrder(t *testing.T) {
	names := []string{}
	for _, l := range AllLoaders() {
		switch l.(type) {
		case *databaseLoader:
			names = append(names, "database")
		case *logLevelLoader:
			names = append(names, "log_level")
		case *redisLoader:
			names = append(names, "redis")
		case *authLoader:
			names = append(names, "auth")
		case *ingestLoader:
			names = append(names, "ingest")
		case *writerLoader:
			names = append(names, "writer")
		case *queryLoader:
			names = append(names, "query")
		case *evalLoader:
			names = append(names, "eval")
		case *pricingLoader:
			names = append(names, "pricing")
		case *metricsLoader:
			names = append(names, "metrics")
		case *quackServerLoader:
			names = append(names, "quack_server")
		case *quackClientLoader:
			names = append(names, "quack_client")
		case *storageLoader:
			names = append(names, "storage")
		}
	}

	expected := []string{
		"database", "log_level", "redis", "auth", "ingest",
		"writer", "query", "eval", "pricing", "metrics",
		"quack_server", "quack_client", "storage",
	}
	if len(names) != len(expected) {
		t.Fatalf("loader count: got %d, want %d", len(names), len(expected))
	}
	for i, want := range expected {
		if names[i] != want {
			t.Errorf("loader[%d]: got %q, want %q", i, names[i], want)
		}
	}
}
