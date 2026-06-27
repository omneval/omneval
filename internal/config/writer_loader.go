package config

import "github.com/spf13/viper"

type writerLoader struct{}

func NewWriterLoader() SectionLoader { return &writerLoader{} }

func (l *writerLoader) SetDefaults(v *viper.Viper) {
	v.SetDefault("writer.addr", ":8001")
	v.SetDefault("writer.lake.enabled", true)
	v.SetDefault("writer.reconciliation.enabled", false)
	v.SetDefault("writer.reconciliation.interval_minutes", 5)
	v.SetDefault("writer.reconciliation.grace_period_minutes", 10)
	v.SetDefault("writer.reconciliation.retention_hours", 168)
}

func (l *writerLoader) ApplyEnvOverrides(cfg *Config) {
	envString(&cfg.Writer.Addr, "OMNEVAL_WRITER_ADDR")
	envBool(&cfg.Writer.Lake.Enabled, "OMNEVAL_WRITER_LAKE_ENABLED")
	envBool(&cfg.Writer.Reconciliation.Enabled, "OMNEVAL_WRITER_RECONCILIATION_ENABLED")
	envInt(&cfg.Writer.Reconciliation.IntervalMinutes, "OMNEVAL_WRITER_RECONCILIATION_INTERVAL_MINUTES")
	envInt(&cfg.Writer.Reconciliation.GracePeriodMinutes, "OMNEVAL_WRITER_RECONCILIATION_GRACE_PERIOD_MINUTES")
	envInt(&cfg.Writer.Reconciliation.RetentionHours, "OMNEVAL_WRITER_RECONCILIATION_RETENTION_HOURS")
}
