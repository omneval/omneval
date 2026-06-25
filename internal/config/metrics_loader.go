package config

import "github.com/spf13/viper"

type metricsLoader struct{}

func NewMetricsLoader() SectionLoader { return &metricsLoader{} }

func (l *metricsLoader) SetDefaults(v *viper.Viper) {
	v.SetDefault("metrics.addr", ":9090")
	v.SetDefault("metrics.disable_project_labels", false)
}

func (l *metricsLoader) ApplyEnvOverrides(cfg *Config) {
	envString(&cfg.Metrics.Addr, "OMNEVAL_METRICS_ADDR")
	envBool(&cfg.Metrics.DisableProjectLabels, "OMNEVAL_METRICS_DISABLE_PROJECT_LABELS")
}
