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
// lantern.yaml and environment variable overrides.
type Config struct {
	Database  DatabaseConfig  `mapstructure:"database"`
	Redis     RedisConfig     `mapstructure:"redis"`
	Storage   StorageConfig   `mapstructure:"storage"`
	Auth      AuthConfig      `mapstructure:"auth"`
	Ingest    IngestConfig    `mapstructure:"ingest"`
	Writer    WriterConfig    `mapstructure:"writer"`
	Query     QueryConfig     `mapstructure:"query"`
	Eval      EvalConfig      `mapstructure:"eval"`
	Pricing   PricingConfig   `mapstructure:"pricing"`
	Metrics   MetricsConfig   `mapstructure:"metrics"`
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
	// variables: LANTERN_AUTH_ADMIN_EMAIL / LANTERN_AUTH_ADMIN_PASSWORD.
	AdminEmail    string `mapstructure:"admin_email"`
	AdminPassword string `mapstructure:"admin_password"`
}

type IngestConfig struct {
	Addr             string   `mapstructure:"addr"`
	// LogSystemPrompt controls whether the system prompt is included as the
	// first element of a span's Input array. Defaults to true.
	// Override via LANTERN_INGEST_LOG_SYSTEM_PROMPT=false.
	LogSystemPrompt bool `mapstructure:"log_system_prompt"`
	// CORSAllowedOrigins lists allowed origins for CORS requests. Use ["*"] for all origins.
	// Override via LANTERN_INGEST_CORS_ALLOWED_ORIGINS (comma-separated).
	CORSAllowedOrigins []string `mapstructure:"cors_allowed_origins"`
}

type WriterConfig struct {
	Addr          string `mapstructure:"addr"`
	DuckDBPath    string `mapstructure:"duckdb_path"`
	SyncInterval  string `mapstructure:"sync_interval"`  // default "30s"
	FlushInterval string `mapstructure:"flush_interval"` // default "30m"
	FlushAgeDays  int    `mapstructure:"flush_age_days"` // default 2
}

type QueryConfig struct {
	Addr          string `mapstructure:"addr"`
	DuckDBPath    string `mapstructure:"duckdb_path"`
	SyncInterval  string `mapstructure:"sync_interval"` // default "30s"
}

type EvalConfig struct {
	Addr        string `mapstructure:"addr"`
	Concurrency int    `mapstructure:"concurrency"`
	// LLMBaseURL is an OpenAI-compatible base URL for judge LLM calls.
	// Works with OpenAI, LiteLLM proxy, Ollama, or any compatible endpoint.
	LLMBaseURL string `mapstructure:"llm_base_url"`
	// LLMModel is the model name for judge LLM calls.
	LLMModel string `mapstructure:"llm_model"`
	LLMAPIKey  string `mapstructure:"llm_api_key"`
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

// Load reads lantern.yaml and applies environment variable overrides.
// Environment variables use the LANTERN_ prefix with _ as the key separator.
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
	// ingest
	v.SetDefault("ingest.addr", ":8000")
	v.SetDefault("ingest.log_system_prompt", true)
	v.SetDefault("ingest.cors_allowed_origins", []string{"*"})
	// writer
	v.SetDefault("writer.addr", ":8001")
	v.SetDefault("writer.duckdb_path", "")
	v.SetDefault("writer.sync_interval", "30s")
	v.SetDefault("writer.flush_interval", "30m")
	v.SetDefault("writer.flush_age_days", 2)
	// query
	v.SetDefault("query.addr", ":8002")
	v.SetDefault("query.duckdb_path", "")
	v.SetDefault("query.sync_interval", "30s")
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

	// Apply LANTERN_* environment variable overrides directly.
	// Viper's AutomaticEnv does not reliably propagate env vars into nested
	// struct fields via Unmarshal — os.Getenv is the only guaranteed path.
	envString(&cfg.Database.Driver,  "LANTERN_DATABASE_DRIVER")
	envString(&cfg.Database.DSN,     "LANTERN_DATABASE_DSN")
	envString(&cfg.Redis.Addr,       "LANTERN_REDIS_ADDR")
	envString(&cfg.Redis.Password,   "LANTERN_REDIS_PASSWORD")
	envInt(&cfg.Redis.DB,            "LANTERN_REDIS_DB")
	envString(&cfg.Storage.Endpoint, "LANTERN_STORAGE_ENDPOINT")
	envString(&cfg.Storage.Bucket,   "LANTERN_STORAGE_BUCKET")
	envString(&cfg.Storage.Region,   "LANTERN_STORAGE_REGION")
	envString(&cfg.Storage.AccessKey,"LANTERN_STORAGE_ACCESS_KEY")
	envString(&cfg.Storage.SecretKey,"LANTERN_STORAGE_SECRET_KEY")
	envString(&cfg.Auth.SessionTTL,  "LANTERN_AUTH_SESSION_TTL")
	envBool(&cfg.Auth.SecureCookie,  "LANTERN_AUTH_SECURE_COOKIE")
	envString(&cfg.Auth.AdminEmail,  "LANTERN_AUTH_ADMIN_EMAIL")
	envString(&cfg.Auth.AdminPassword,"LANTERN_AUTH_ADMIN_PASSWORD")
	envString(&cfg.Ingest.Addr,      "LANTERN_INGEST_ADDR")
	envBool(&cfg.Ingest.LogSystemPrompt, "LANTERN_INGEST_LOG_SYSTEM_PROMPT")
	if v := os.Getenv("LANTERN_INGEST_CORS_ALLOWED_ORIGINS"); v != "" {
		cfg.Ingest.CORSAllowedOrigins = strings.Split(v, ",")
	}
	envString(&cfg.Writer.Addr,         "LANTERN_WRITER_ADDR")
	envString(&cfg.Writer.DuckDBPath,   "LANTERN_WRITER_DUCKDB_PATH")
	envString(&cfg.Writer.SyncInterval, "LANTERN_WRITER_SYNC_INTERVAL")
	envString(&cfg.Writer.FlushInterval,"LANTERN_WRITER_FLUSH_INTERVAL")
	envInt(&cfg.Writer.FlushAgeDays,    "LANTERN_WRITER_FLUSH_AGE_DAYS")
	envString(&cfg.Query.Addr,          "LANTERN_QUERY_ADDR")
	envString(&cfg.Query.DuckDBPath,    "LANTERN_QUERY_DUCKDB_PATH")
	envString(&cfg.Query.SyncInterval,  "LANTERN_QUERY_SYNC_INTERVAL")
	envString(&cfg.Eval.Addr,           "LANTERN_EVAL_ADDR")
	envInt(&cfg.Eval.Concurrency,       "LANTERN_EVAL_CONCURRENCY")
	envString(&cfg.Eval.LLMBaseURL,     "LANTERN_EVAL_LLM_BASE_URL")
	envString(&cfg.Eval.LLMModel,       "LANTERN_EVAL_LLM_MODEL")
	envString(&cfg.Eval.LLMAPIKey,      "LANTERN_EVAL_LLM_API_KEY")
	envInt(&cfg.Eval.RetryCount,        "LANTERN_EVAL_RETRY_COUNT")
	envString(&cfg.Metrics.Addr,        "LANTERN_METRICS_ADDR")
	envBool(&cfg.Metrics.DisableProjectLabels, "LANTERN_METRICS_DISABLE_PROJECT_LABELS")

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
