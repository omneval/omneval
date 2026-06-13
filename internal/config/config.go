package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/viper"
)

// Config is the top-level configuration structure populated by Viper from
// omneval.yaml and environment variable overrides.
type Config struct {
	LogLevel string         `mapstructure:"log_level"`
	Database DatabaseConfig `mapstructure:"database"`
	Redis    RedisConfig    `mapstructure:"redis"`
	Storage  StorageConfig  `mapstructure:"storage"`
	Auth     AuthConfig     `mapstructure:"auth"`
	Ingest   IngestConfig   `mapstructure:"ingest"`
	Writer   WriterConfig   `mapstructure:"writer"`
	Query    QueryConfig    `mapstructure:"query"`
	Eval     EvalConfig     `mapstructure:"eval"`
	Pricing  PricingConfig  `mapstructure:"pricing"`
	Metrics  MetricsConfig  `mapstructure:"metrics"`
	Quack    QuackConfig    `mapstructure:"quack"`
}

// QuackConfig holds the top-level `quack` configuration section (ADR-0005).
// The Quack Server (services/quack) is the sole holder of a direct DuckLake
// Catalog/data-path connection; every other service (Writer, Query API,
// backfill tool) attaches to the Lake as a thin Quack client.
type QuackConfig struct {
	// Server configures the Quack Server (services/quack only).
	Server QuackServerConfig `mapstructure:"server"`
	// Client configures every other service's attachment to the Quack
	// Server (Writer, Query API, backfill tool, demo profile).
	Client QuackClientConfig `mapstructure:"client"`
}

// CatalogDriverPostgres uses the shared Postgres instance as the Catalog —
// the serialization point that makes multiple concurrent writers safe.
const CatalogDriverPostgres = "postgres"

// CatalogDriverLocal uses a local DuckDB-file Catalog (single-writer,
// demo profile only).
const CatalogDriverLocal = "duckdb"

// QuackServerConfig configures services/quack: the sole process holding a
// direct DuckLake Catalog and data-path connection (ADR-0005). Empty values
// are derived at wiring time: the Catalog follows the metadata-store
// database (Postgres in prod, a local single-writer catalog file otherwise)
// and the data path follows the storage bucket (s3://<bucket>/lake).
type QuackServerConfig struct {
	// ListenAddr is the address quack_serve() binds to (default ":9494").
	ListenAddr string `mapstructure:"listen_addr"`
	// Token authenticates Quack clients via `CREATE SECRET ... TYPE quack`.
	// If unset, a random token is generated at startup and logged (demo/dev
	// convenience).
	Token string `mapstructure:"token"`
	// CatalogDriver is "postgres" or "duckdb" (local single-writer catalog).
	CatalogDriver string `mapstructure:"catalog_driver"`
	// CatalogDSN is the Postgres DSN for the Catalog, or the local catalog
	// file path when CatalogDriver is "duckdb".
	CatalogDSN string `mapstructure:"catalog_dsn"`
	// DataPath is where the Lake's Parquet files live: an s3://bucket/prefix
	// URL or a local directory.
	DataPath string `mapstructure:"data_path"`
	// MaintenanceInterval is how often the Table Maintenance scheduler runs
	// (ducklake_flush_inlined_data, ducklake_merge_adjacent_files,
	// ducklake_expire_snapshots, orphan/old-file cleanup). Default "5m".
	MaintenanceInterval string `mapstructure:"maintenance_interval"`
	// Retention controls Lake-native retention (#92): DELETEs aged spans and
	// scores through the DuckLake Catalog and reclaims physical Parquet files
	// in the same maintenance pass. This replaces the legacy S3-prefix-based
	// file-deletion retention worker (Writer.Retention), which is wrong under
	// DuckLake.
	Retention QuackRetentionConfig `mapstructure:"retention"`
}

// QuackRetentionConfig controls the Lake-native retention step in Table
// Maintenance (#92). Only Enabled and MaxAgeDays are used; the legacy
// RetentionConfig fields (Action, Destination, etc.) are irrelevant for
// the Catalog-based approach.
type QuackRetentionConfig struct {
	// Enabled turns on the retention DELETE step. Default false: Table
	// Maintenance runs its compaction steps only, with no data loss.
	Enabled bool `mapstructure:"enabled"`
	// MaxAgeDays is how old a span (by start_time) or score (by
	// span_start_time) must be to be deleted. Zero means no age limit.
	MaxAgeDays int `mapstructure:"max_age_days"`
}

// QuackClientConfig configures a Quack client attachment (Writer, Query
// API, backfill tool, demo profile). Every Lake access in the platform
// other than services/quack goes through this.
type QuackClientConfig struct {
	// URL is the Quack Server address, e.g. "quack://quack-server.omneval:9494"
	// or "localhost:9494" (host:port; the "quack://" scheme is optional and
	// stripped if present).
	URL string `mapstructure:"url"`
	// Token authenticates this client via `CREATE SECRET ... TYPE quack`.
	// Must match the Quack Server's configured token.
	Token string `mapstructure:"token"`
	// DataPath is where the Lake's Parquet files live: an s3://bucket/prefix
	// URL or a local directory. Read directly by the client for DATA_PATH in
	// the ATTACH statement (Quack carries catalog metadata only).
	DataPath string `mapstructure:"data_path"`
}

// WriterLakeConfig controls the Writer's Lake participation.
type WriterLakeConfig struct {
	// Enabled turns on writing every batch to the Lake. Default true
	// (#90 cutover) — the Lake is the only storage tier.
	Enabled bool `mapstructure:"enabled"`
}

type DatabaseConfig struct {
	Driver string `mapstructure:"driver"` // "postgres" or "sqlite"
	DSN    string `mapstructure:"dsn"`
}

type RedisConfig struct {
	Addr     string `mapstructure:"addr"`
	Password string `mapstructure:"password"`
	DB       int    `mapstructure:"db"`
}

type StorageConfig struct {
	Endpoint  string `mapstructure:"endpoint"`
	Bucket    string `mapstructure:"bucket"`
	Region    string `mapstructure:"region"`
	AccessKey string `mapstructure:"access_key"`
	SecretKey string `mapstructure:"secret_key"`
}

type AuthConfig struct {
	// SessionTTL is the session cookie lifetime (default "168h" / 7 days).
	SessionTTL string `mapstructure:"session_ttl"`
	// SecureCookie sets the Secure flag on the session cookie.
	// Disable for local HTTP development only.
	SecureCookie bool `mapstructure:"secure_cookie"`
	// Bootstrap admin credentials. If set and no users exist, the Query API
	// creates this admin user on startup. Typically set via environment
	// variables: OMNEVAL_AUTH_ADMIN_EMAIL / OMNEVAL_AUTH_ADMIN_PASSWORD.
	AdminEmail    string `mapstructure:"admin_email"`
	AdminPassword string `mapstructure:"admin_password"`
}

// IngestBufferConfig controls S3-first ingestion (ADR-0004).
type IngestBufferConfig struct {
	// Enabled stages every translated span batch in the Ingest Buffer (an
	// S3 prefix keyed by Batch ID) and enqueues only the Batch ID reference.
	// Requires storage (S3) to be configured. Default false: batches are
	// enqueued as full payloads in Redis (legacy flow).
	Enabled bool `mapstructure:"enabled"`
}

type IngestConfig struct {
	Addr string `mapstructure:"addr"`
	// Buffer controls S3-first ingestion via the Ingest Buffer (ADR-0004).
	Buffer IngestBufferConfig `mapstructure:"buffer"`
	// LogSystemPrompt controls whether the system prompt is included as the
	// first element of a span's Input array. Defaults to true.
	// Override via OMNEVAL_INGEST_LOG_SYSTEM_PROMPT=false.
	LogSystemPrompt bool `mapstructure:"log_system_prompt"`
	// CORSAllowedOrigins lists allowed origins for CORS requests. Use ["*"] for all origins.
	// Override via OMNEVAL_INGEST_CORS_ALLOWED_ORIGINS (comma-separated).
	CORSAllowedOrigins []string `mapstructure:"cors_allowed_origins"`
}

type WriterConfig struct {
	Addr          string `mapstructure:"addr"`
	DuckDBPath    string `mapstructure:"duckdb_path"`
	SyncInterval  string `mapstructure:"sync_interval"`  // default "30s"
	FlushInterval string `mapstructure:"flush_interval"` // default "30m"
	FlushAgeDays  int    `mapstructure:"flush_age_days"` // default 2
	// LeaderElection enables Redis-based leader election for multi-replica HA.
	// When enabled, only the leader processes the ingest queue and writes to DuckDB.
	LeaderElection LeaderElectionConfig `mapstructure:"leader_election"`
	// Lake controls writing to the Lake (ADR-0004).
	Lake WriterLakeConfig `mapstructure:"lake"`
	// Reconciliation controls the Ingest Buffer reconciliation sweep
	// (ADR-0004, #88).
	Reconciliation ReconciliationConfig `mapstructure:"reconciliation"`
}

// ReconciliationConfig holds settings for the Ingest Buffer reconciliation
// sweep: a leader-elected job that recovers buffered batches whose queue
// reference was lost and garbage-collects committed buffer objects past
// their retention window.
type ReconciliationConfig struct {
	// Enabled starts the reconciliation ticker when true. Requires S3
	// (storage) to be configured.
	Enabled bool `mapstructure:"enabled"`
	// IntervalMinutes is how often the sweep runs. Default 5.
	IntervalMinutes int `mapstructure:"interval_minutes"`
	// GracePeriodMinutes is the minimum age of a buffer object before the
	// sweep will act on it, giving an in-flight writer time to commit and
	// ack normally. Default 10.
	GracePeriodMinutes int `mapstructure:"grace_period_minutes"`
	// RetentionHours is how long a *committed* buffer object is kept before
	// the sweep deletes it. Uncommitted objects are never deleted. Default
	// 168 (7 days).
	RetentionHours int `mapstructure:"retention_hours"`
}

// Validate checks that the reconciliation config is consistent. Disabled
// reconciliation is always valid (no-op).
func (rc ReconciliationConfig) Validate() error {
	if !rc.Enabled {
		return nil
	}
	if rc.IntervalMinutes <= 0 {
		return fmt.Errorf("reconciliation.interval_minutes must be > 0, got %d", rc.IntervalMinutes)
	}
	if rc.GracePeriodMinutes <= 0 {
		return fmt.Errorf("reconciliation.grace_period_minutes must be > 0, got %d", rc.GracePeriodMinutes)
	}
	if rc.RetentionHours <= 0 {
		return fmt.Errorf("reconciliation.retention_hours must be > 0, got %d", rc.RetentionHours)
	}
	return nil
}

type LeaderElectionConfig struct {
	// Enabled enables leader election (default false for single-replica deployments).
	Enabled bool `mapstructure:"enabled"`
	// LockTTL is the leader election lock TTL in seconds (default 15).
	LockTTL int `mapstructure:"lock_ttl"`
	// FencingEnabled prevents dual-writer data corruption on failover.
	// When true, a newly-elected leader reconciles the S3 snapshot before
	// accepting writes, and a deposed leader closes DuckDB immediately.
	// Defaults to true when leader election is enabled.
	FencingEnabled bool `mapstructure:"fencing_enabled"`
}

// QueryLakeConfig controls the Query API's Lake participation.
type QueryLakeConfig struct {
	// Enabled gates the Lake read path (ADR-0004). When true, the Query
	// API attaches read-only to the Lake (DuckLake via the Catalog) instead
	// of downloading the S3 snapshot. All span reads — span list, trace
	// detail waterfall, conversations, and the Analytics DSL — compile
	// against the single Lake table set. Default true (#90 cutover) — the
	// Lake is the only read path.
	Enabled bool `mapstructure:"enabled"`
}

type QueryConfig struct {
	Addr         string `mapstructure:"addr"`
	DuckDBPath   string `mapstructure:"duckdb_path"`
	SyncInterval string `mapstructure:"sync_interval"` // default "30s"
	// Lake controls the Lake read path (ADR-0004).
	Lake QueryLakeConfig `mapstructure:"lake"`
	// PlaygroundLLMBaseURL is an OpenAI-compatible base URL for the playground LLM.
	// Works with OpenAI, LiteLLM proxy, Ollama, or any compatible endpoint.
	PlaygroundLLMBaseURL string `mapstructure:"playground_llm_base_url"`
	// PlaygroundLLMAPIKey is the API key for the playground LLM.
	PlaygroundLLMAPIKey string `mapstructure:"playground_llm_api_key"`
	// JudgeLLMBaseURL is an OpenAI-compatible base URL for dataset run judge LLM calls.
	JudgeLLMBaseURL string `mapstructure:"judge_llm_base_url"`
	// JudgeLLMAPIKey is the API key for the dataset run judge LLM.
	JudgeLLMAPIKey string `mapstructure:"judge_llm_api_key"`
}

type EvalConfig struct {
	Addr        string `mapstructure:"addr"`
	Concurrency int    `mapstructure:"concurrency"`
	// LLMBaseURL is an OpenAI-compatible base URL for judge LLM calls.
	// Works with OpenAI, LiteLLM proxy, Ollama, or any compatible endpoint.
	LLMBaseURL string `mapstructure:"llm_base_url"`
	// LLMModel is the model name for judge LLM calls.
	LLMModel  string `mapstructure:"llm_model"`
	LLMAPIKey string `mapstructure:"llm_api_key"`
	// JudgeTimeout is the maximum duration for a judge LLM call.
	JudgeTimeout time.Duration `mapstructure:"judge_timeout"`
	// RetryCount is the number of retries for failed judge calls.
	RetryCount int `mapstructure:"retry_count"`
}

// PricingModelOverride holds per-million-token prices for a single model.
// Values are in USD per million tokens (human-readable convention).
// Converted to per-token internally at startup.
type PricingModelOverride struct {
	InputPerMillion  float64 `mapstructure:"input_per_million"`
	OutputPerMillion float64 `mapstructure:"output_per_million"`
}

type PricingConfig struct {
	// ModelOverrides maps model name to per-million-token price overrides.
	ModelOverrides map[string]PricingModelOverride `mapstructure:"model_overrides"`
}

type MetricsConfig struct {
	// Addr is the listen address for the Prometheus /metrics endpoint (default ":9090").
	Addr string `mapstructure:"addr"`
	// DisableProjectLabels suppresses per-project label cardinality on all metrics.
	DisableProjectLabels bool `mapstructure:"disable_project_labels"`
}

// Load reads omneval.yaml and applies environment variable overrides.
// Environment variables use the OMNEVAL_ prefix with _ as the key separator.
func Load(path string) (*Config, error) {
	v := viper.New()

	// database
	v.SetDefault("database.driver", "")
	v.SetDefault("database.dsn", "")
	// redis
	v.SetDefault("redis.addr", "localhost:6379")
	v.SetDefault("redis.password", "")
	v.SetDefault("redis.db", 0)
	// storage
	v.SetDefault("storage.endpoint", "")
	v.SetDefault("storage.bucket", "")
	v.SetDefault("storage.region", "")
	v.SetDefault("storage.access_key", "")
	v.SetDefault("storage.secret_key", "")
	// auth
	v.SetDefault("auth.session_ttl", "168h")
	v.SetDefault("auth.secure_cookie", false)
	v.SetDefault("auth.admin_email", "")
	v.SetDefault("auth.admin_password", "")
	v.SetDefault("log_level", "info")
	// ingest
	v.SetDefault("ingest.addr", ":8000")
	v.SetDefault("ingest.log_system_prompt", true)
	v.SetDefault("ingest.cors_allowed_origins", []string{"*"})
	v.SetDefault("ingest.buffer.enabled", false)
	// writer
	v.SetDefault("writer.addr", ":8001")
	v.SetDefault("writer.duckdb_path", "")
	v.SetDefault("writer.sync_interval", "30s")
	v.SetDefault("writer.flush_interval", "30m")
	v.SetDefault("writer.flush_age_days", 2)
	v.SetDefault("writer.leader_election.enabled", false)
	v.SetDefault("writer.leader_election.lock_ttl", 15)
	v.SetDefault("writer.leader_election.fencing_enabled", true)
	// writer reconciliation (ingest buffer sweep, #88)
	v.SetDefault("writer.reconciliation.enabled", false)
	v.SetDefault("writer.reconciliation.interval_minutes", 5)
	v.SetDefault("writer.reconciliation.grace_period_minutes", 10)
	v.SetDefault("writer.reconciliation.retention_hours", 168)
	// query
	v.SetDefault("query.addr", ":8002")
	v.SetDefault("query.duckdb_path", "")
	v.SetDefault("query.sync_interval", "30s")
	v.SetDefault("query.lake.enabled", true)
	v.SetDefault("query.playground_llm_base_url", "")
	v.SetDefault("query.playground_llm_api_key", "")
	// eval
	v.SetDefault("eval.addr", ":8003")
	v.SetDefault("eval.concurrency", 4)
	v.SetDefault("eval.llm_base_url", "")
	v.SetDefault("eval.llm_model", "gpt-4")
	v.SetDefault("eval.llm_api_key", "")
	v.SetDefault("eval.judge_timeout", 90*time.Second)
	v.SetDefault("eval.retry_count", 3)
	// metrics
	v.SetDefault("metrics.addr", ":9090")
	v.SetDefault("metrics.disable_project_labels", false)
	// quack server (services/quack only)
	v.SetDefault("quack.server.listen_addr", ":9494")
	v.SetDefault("quack.server.token", "")
	v.SetDefault("quack.server.catalog_driver", "")
	v.SetDefault("quack.server.catalog_dsn", "")
	v.SetDefault("quack.server.data_path", "")
	v.SetDefault("quack.server.maintenance_interval", "5m")
	// quack client (Writer, Query API, backfill tool, demo profile)
	v.SetDefault("quack.client.url", "localhost:9494")
	v.SetDefault("quack.client.token", "")
	v.SetDefault("quack.client.data_path", "")
	v.SetDefault("writer.lake.enabled", true)

	if path != "" {
		v.SetConfigFile(path)
		if err := v.ReadInConfig(); err != nil {
			return nil, fmt.Errorf("reading config %q: %w", path, err)
		}
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("unmarshalling config: %w", err)
	}

	// Apply OMNEVAL_* environment variable overrides directly.
	// Viper's AutomaticEnv does not reliably propagate env vars into nested
	// struct fields via Unmarshal — os.Getenv is the only guaranteed path.
	envString(&cfg.LogLevel, "OMNEVAL_LOG_LEVEL")
	envString(&cfg.Database.Driver, "OMNEVAL_DATABASE_DRIVER")
	envString(&cfg.Database.DSN, "OMNEVAL_DATABASE_DSN")
	envString(&cfg.Redis.Addr, "OMNEVAL_REDIS_ADDR")
	envString(&cfg.Redis.Password, "OMNEVAL_REDIS_PASSWORD")
	envInt(&cfg.Redis.DB, "OMNEVAL_REDIS_DB")
	envString(&cfg.Storage.Endpoint, "OMNEVAL_STORAGE_ENDPOINT")
	envString(&cfg.Storage.Bucket, "OMNEVAL_STORAGE_BUCKET")
	envString(&cfg.Storage.Region, "OMNEVAL_STORAGE_REGION")
	envString(&cfg.Storage.AccessKey, "OMNEVAL_STORAGE_ACCESS_KEY")
	envString(&cfg.Storage.SecretKey, "OMNEVAL_STORAGE_SECRET_KEY")
	envString(&cfg.Auth.SessionTTL, "OMNEVAL_AUTH_SESSION_TTL")
	envBool(&cfg.Auth.SecureCookie, "OMNEVAL_AUTH_SECURE_COOKIE")
	envString(&cfg.Auth.AdminEmail, "OMNEVAL_AUTH_ADMIN_EMAIL")
	envString(&cfg.Auth.AdminPassword, "OMNEVAL_AUTH_ADMIN_PASSWORD")
	envString(&cfg.Ingest.Addr, "OMNEVAL_INGEST_ADDR")
	envBool(&cfg.Ingest.LogSystemPrompt, "OMNEVAL_INGEST_LOG_SYSTEM_PROMPT")
	envBool(&cfg.Ingest.Buffer.Enabled, "OMNEVAL_INGEST_BUFFER_ENABLED")
	if v := os.Getenv("OMNEVAL_INGEST_CORS_ALLOWED_ORIGINS"); v != "" {
		cfg.Ingest.CORSAllowedOrigins = strings.Split(v, ",")
	}
	envString(&cfg.Writer.Addr, "OMNEVAL_WRITER_ADDR")
	envString(&cfg.Writer.DuckDBPath, "OMNEVAL_WRITER_DUCKDB_PATH")
	envString(&cfg.Writer.SyncInterval, "OMNEVAL_WRITER_SYNC_INTERVAL")
	envString(&cfg.Writer.FlushInterval, "OMNEVAL_WRITER_FLUSH_INTERVAL")
	envInt(&cfg.Writer.FlushAgeDays, "OMNEVAL_WRITER_FLUSH_AGE_DAYS")
	envBool(&cfg.Writer.LeaderElection.Enabled, "OMNEVAL_WRITER_LEADER_ELECTION_ENABLED")
	envInt(&cfg.Writer.LeaderElection.LockTTL, "OMNEVAL_WRITER_LEADER_ELECTION_LOCK_TTL")
	envBool(&cfg.Writer.LeaderElection.FencingEnabled, "OMNEVAL_WRITER_LEADER_ELECTION_FENCING_ENABLED")
	envBool(&cfg.Writer.Reconciliation.Enabled, "OMNEVAL_WRITER_RECONCILIATION_ENABLED")
	envInt(&cfg.Writer.Reconciliation.IntervalMinutes, "OMNEVAL_WRITER_RECONCILIATION_INTERVAL_MINUTES")
	envInt(&cfg.Writer.Reconciliation.GracePeriodMinutes, "OMNEVAL_WRITER_RECONCILIATION_GRACE_PERIOD_MINUTES")
	envInt(&cfg.Writer.Reconciliation.RetentionHours, "OMNEVAL_WRITER_RECONCILIATION_RETENTION_HOURS")
	envString(&cfg.Query.Addr, "OMNEVAL_QUERY_ADDR")
	envString(&cfg.Query.DuckDBPath, "OMNEVAL_QUERY_DUCKDB_PATH")
	envString(&cfg.Query.SyncInterval, "OMNEVAL_QUERY_SYNC_INTERVAL")
	envBool(&cfg.Query.Lake.Enabled, "OMNEVAL_QUERY_LAKE_ENABLED")
	envString(&cfg.Query.PlaygroundLLMBaseURL, "OMNEVAL_QUERY_PLAYGROUND_LLM_BASE_URL")
	envString(&cfg.Query.PlaygroundLLMAPIKey, "OMNEVAL_QUERY_PLAYGROUND_LLM_API_KEY")
	envString(&cfg.Query.JudgeLLMBaseURL, "OMNEVAL_QUERY_JUDGE_LLM_BASE_URL")
	envString(&cfg.Query.JudgeLLMAPIKey, "OMNEVAL_QUERY_JUDGE_LLM_API_KEY")
	envString(&cfg.Eval.Addr, "OMNEVAL_EVAL_ADDR")
	envInt(&cfg.Eval.Concurrency, "OMNEVAL_EVAL_CONCURRENCY")
	envString(&cfg.Eval.LLMBaseURL, "OMNEVAL_EVAL_LLM_BASE_URL")
	envString(&cfg.Eval.LLMModel, "OMNEVAL_EVAL_LLM_MODEL")
	envString(&cfg.Eval.LLMAPIKey, "OMNEVAL_EVAL_LLM_API_KEY")
	envInt(&cfg.Eval.RetryCount, "OMNEVAL_EVAL_RETRY_COUNT")
	envString(&cfg.Metrics.Addr, "OMNEVAL_METRICS_ADDR")
	envBool(&cfg.Metrics.DisableProjectLabels, "OMNEVAL_METRICS_DISABLE_PROJECT_LABELS")
	envString(&cfg.Quack.Server.ListenAddr, "OMNEVAL_QUACK_SERVER_LISTEN_ADDR")
	envString(&cfg.Quack.Server.Token, "OMNEVAL_QUACK_SERVER_TOKEN")
	envString(&cfg.Quack.Server.CatalogDriver, "OMNEVAL_QUACK_SERVER_CATALOG_DRIVER")
	envString(&cfg.Quack.Server.CatalogDSN, "OMNEVAL_QUACK_SERVER_CATALOG_DSN")
	envString(&cfg.Quack.Server.DataPath, "OMNEVAL_QUACK_SERVER_DATA_PATH")
	envString(&cfg.Quack.Server.MaintenanceInterval, "OMNEVAL_QUACK_SERVER_MAINTENANCE_INTERVAL")
	envString(&cfg.Quack.Client.URL, "OMNEVAL_QUACK_CLIENT_URL")
	envString(&cfg.Quack.Client.Token, "OMNEVAL_QUACK_CLIENT_TOKEN")
	envString(&cfg.Quack.Client.DataPath, "OMNEVAL_QUACK_CLIENT_DATA_PATH")
	envBool(&cfg.Writer.Lake.Enabled, "OMNEVAL_WRITER_LAKE_ENABLED")

	return &cfg, nil
}

func envString(dst *string, key string) {
	if v := os.Getenv(key); v != "" {
		*dst = v
	}
}

func envBool(dst *bool, key string) {
	if v := os.Getenv(key); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			*dst = b
		}
	}
}

func envInt(dst *int, key string) {
	if v := os.Getenv(key); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			*dst = i
		}
	}
}
