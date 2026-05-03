package config

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
}

type IngestConfig struct {
	Addr             string `mapstructure:"addr"`
	// LogSystemPrompt controls whether the system prompt is included as the
	// first element of a span's Input array. Defaults to true.
	// Override via LANTERN_INGEST_LOG_SYSTEM_PROMPT=false.
	LogSystemPrompt bool `mapstructure:"log_system_prompt"`
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
	LLMAPIKey  string `mapstructure:"llm_api_key"`
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
	// TODO: implement using github.com/spf13/viper
	panic("not implemented")
}
