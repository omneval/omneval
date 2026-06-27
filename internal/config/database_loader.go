package config

import "github.com/spf13/viper"

type databaseLoader struct{}

func NewDatabaseLoader() SectionLoader { return &databaseLoader{} }

func (l *databaseLoader) SetDefaults(v *viper.Viper) {
	v.SetDefault("database.driver", "")
	v.SetDefault("database.dsn", "")
}

func (l *databaseLoader) ApplyEnvOverrides(cfg *Config) {
	envString(&cfg.Database.Driver, "OMNEVAL_DATABASE_DRIVER")
	envString(&cfg.Database.DSN, "OMNEVAL_DATABASE_DSN")
}
