package config

import (
	"os"
	"time"

	"github.com/spf13/viper"
)

type evalLoader struct{}

func NewEvalLoader() SectionLoader { return &evalLoader{} }

func (l *evalLoader) SetDefaults(v *viper.Viper) {
	v.SetDefault("eval.addr", ":8003")
	v.SetDefault("eval.concurrency", 4)
	v.SetDefault("eval.llm_base_url", "")
	v.SetDefault("eval.llm_model", "gpt-4")
	v.SetDefault("eval.llm_api_key", "")
	v.SetDefault("eval.judge_timeout", 90*time.Second)
	v.SetDefault("eval.retry_count", 3)
}

func (l *evalLoader) ApplyEnvOverrides(cfg *Config) {
	envString(&cfg.Eval.Addr, "OMNEVAL_EVAL_ADDR")
	envInt(&cfg.Eval.Concurrency, "OMNEVAL_EVAL_CONCURRENCY")
	envString(&cfg.Eval.LLMBaseURL, "OMNEVAL_EVAL_LLM_BASE_URL")
	envString(&cfg.Eval.LLMModel, "OMNEVAL_EVAL_LLM_MODEL")
	envString(&cfg.Eval.LLMAPIKey, "OMNEVAL_EVAL_LLM_API_KEY")
	if v := os.Getenv("OMNEVAL_EVAL_JUDGE_TIMEOUT"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			cfg.Eval.JudgeTimeout = d
		}
	}
	envInt(&cfg.Eval.RetryCount, "OMNEVAL_EVAL_RETRY_COUNT")
}
