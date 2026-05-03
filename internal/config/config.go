package config

// Config is the top-level configuration structure populated by Viper from
// lantern.yaml and environment variable overrides.
type Config struct {
	Database  DatabaseConfig  `mapstructure:"database"`
	Redis     RedisConfig     `mapstructure:"redis"`
	Storage   StorageConfig   `mapstructure:"storage"`
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

type IngestConfig struct {
	Addr string `mapstructure:"addr"`
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
	Addr       string `mapstructure:"addr"`
	Concurrency int   `mapstructure:"concurrency"`
}

type PricingConfig struct {
	// ModelOverrides maps model name to USD price per million tokens [input, output].
	ModelOverrides map[string][2]float64 `mapstructure:"model_overrides"`
}

type MetricsConfig struct {
	// DisableProjectLabels suppresses per-project label cardinality on all metrics.
	DisableProjectLabels bool `mapstructure:"disable_project_labels"`
}

// Load reads lantern.yaml and applies environment variable overrides.
// Environment variables use the LANTERN_ prefix with _ as the key separator.
func Load(path string) (*Config, error) {
	// TODO: implement using github.com/spf13/viper
	panic("not implemented")
}
