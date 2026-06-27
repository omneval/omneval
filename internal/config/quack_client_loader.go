package config

import "github.com/spf13/viper"

type quackClientLoader struct{}

func NewQuackClientLoader() SectionLoader { return &quackClientLoader{} }

func (l *quackClientLoader) SetDefaults(v *viper.Viper) {
	v.SetDefault("quack.client.url", "localhost:9494")
	v.SetDefault("quack.client.token", "")
	v.SetDefault("quack.client.data_path", "")
	v.SetDefault("quack.client.max_open_conns", 0)
	v.SetDefault("quack.client.memory_limit", "")
	v.SetDefault("quack.client.threads", 0)
}

func (l *quackClientLoader) ApplyEnvOverrides(cfg *Config) {
	envString(&cfg.Quack.Client.URL, "OMNEVAL_QUACK_CLIENT_URL")
	envString(&cfg.Quack.Client.Token, "OMNEVAL_QUACK_CLIENT_TOKEN")
	envString(&cfg.Quack.Client.DataPath, "OMNEVAL_QUACK_CLIENT_DATA_PATH")
	envInt(&cfg.Quack.Client.MaxOpenConns, "OMNEVAL_QUACK_CLIENT_MAX_OPEN_CONNS")
	envString(&cfg.Quack.Client.MemoryLimit, "OMNEVAL_QUACK_CLIENT_MEMORY_LIMIT")
	envInt(&cfg.Quack.Client.Threads, "OMNEVAL_QUACK_CLIENT_THREADS")
}
