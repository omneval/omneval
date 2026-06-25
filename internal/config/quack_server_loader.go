package config

import "github.com/spf13/viper"

type quackServerLoader struct{}

func NewQuackServerLoader() SectionLoader { return &quackServerLoader{} }

func (l *quackServerLoader) SetDefaults(v *viper.Viper) {
	v.SetDefault("quack.server.listen_addr", ":9494")
	v.SetDefault("quack.server.token", "")
	v.SetDefault("quack.server.catalog_driver", "")
	v.SetDefault("quack.server.catalog_dsn", "")
	v.SetDefault("quack.server.data_path", "")
	v.SetDefault("quack.server.memory_limit", "")
	v.SetDefault("quack.server.threads", 0)
	v.SetDefault("quack.server.retention.enabled", false)
	v.SetDefault("quack.server.retention.max_age_days", 0)
}

func (l *quackServerLoader) ApplyEnvOverrides(cfg *Config) {
	envString(&cfg.Quack.Server.ListenAddr, "OMNEVAL_QUACK_SERVER_LISTEN_ADDR")
	envString(&cfg.Quack.Server.Token, "OMNEVAL_QUACK_SERVER_TOKEN")
	envString(&cfg.Quack.Server.CatalogDriver, "OMNEVAL_QUACK_SERVER_CATALOG_DRIVER")
	envString(&cfg.Quack.Server.CatalogDSN, "OMNEVAL_QUACK_SERVER_CATALOG_DSN")
	envString(&cfg.Quack.Server.DataPath, "OMNEVAL_QUACK_SERVER_DATA_PATH")
	envString(&cfg.Quack.Server.MemoryLimit, "OMNEVAL_QUACK_SERVER_MEMORY_LIMIT")
	envInt(&cfg.Quack.Server.Threads, "OMNEVAL_QUACK_SERVER_THREADS")
	envBool(&cfg.Quack.Server.Retention.Enabled, "OMNEVAL_QUACK_SERVER_RETENTION_ENABLED")
	envInt(&cfg.Quack.Server.Retention.MaxAgeDays, "OMNEVAL_QUACK_SERVER_RETENTION_MAX_AGE_DAYS")
}
