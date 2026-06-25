package config

import "github.com/spf13/viper"

type queryLoader struct{}

func NewQueryLoader() SectionLoader { return &queryLoader{} }

func (l *queryLoader) SetDefaults(v *viper.Viper) {
	v.SetDefault("query.addr", ":8002")
	v.SetDefault("query.lake.enabled", true)
	v.SetDefault("query.playground_llm_base_url", "")
	v.SetDefault("query.playground_llm_api_key", "")
	v.SetDefault("query.judge_llm_base_url", "")
	v.SetDefault("query.judge_llm_api_key", "")
}

func (l *queryLoader) ApplyEnvOverrides(cfg *Config) {
	envString(&cfg.Query.Addr, "OMNEVAL_QUERY_ADDR")
	envBool(&cfg.Query.Lake.Enabled, "OMNEVAL_QUERY_LAKE_ENABLED")
	envString(&cfg.Query.PlaygroundLLMBaseURL, "OMNEVAL_QUERY_PLAYGROUND_LLM_BASE_URL")
	envString(&cfg.Query.PlaygroundLLMAPIKey, "OMNEVAL_QUERY_PLAYGROUND_LLM_API_KEY")
	envString(&cfg.Query.JudgeLLMBaseURL, "OMNEVAL_QUERY_JUDGE_LLM_BASE_URL")
	envString(&cfg.Query.JudgeLLMAPIKey, "OMNEVAL_QUERY_JUDGE_LLM_API_KEY")
}
