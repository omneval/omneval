package config

import "github.com/spf13/viper"

type logLevelLoader struct{}

func NewLogLevelLoader() SectionLoader { return &logLevelLoader{} }

func (l *logLevelLoader) SetDefaults(v *viper.Viper) {
	v.SetDefault("log_level", "info")
}

func (l *logLevelLoader) ApplyEnvOverrides(cfg *Config) {
	envString(&cfg.LogLevel, "OMNEVAL_LOG_LEVEL")
}
