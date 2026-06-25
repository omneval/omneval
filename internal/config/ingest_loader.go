package config

import (
	"os"
	"strings"

	"github.com/spf13/viper"
)

type ingestLoader struct{}

func NewIngestLoader() SectionLoader { return &ingestLoader{} }

func (l *ingestLoader) SetDefaults(v *viper.Viper) {
	v.SetDefault("ingest.addr", ":8000")
	v.SetDefault("ingest.buffer.enabled", false)
	v.SetDefault("ingest.log_system_prompt", true)
	v.SetDefault("ingest.cors_allowed_origins", []string{"*"})
}

func (l *ingestLoader) ApplyEnvOverrides(cfg *Config) {
	envString(&cfg.Ingest.Addr, "OMNEVAL_INGEST_ADDR")
	envBool(&cfg.Ingest.Buffer.Enabled, "OMNEVAL_INGEST_BUFFER_ENABLED")
	envBool(&cfg.Ingest.LogSystemPrompt, "OMNEVAL_INGEST_LOG_SYSTEM_PROMPT")
	if v := os.Getenv("OMNEVAL_INGEST_CORS_ALLOWED_ORIGINS"); v != "" {
		cfg.Ingest.CORSAllowedOrigins = strings.Split(v, ",")
	}
}
