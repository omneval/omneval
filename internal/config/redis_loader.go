package config

import "github.com/spf13/viper"

type redisLoader struct{}

func NewRedisLoader() SectionLoader { return &redisLoader{} }

func (l *redisLoader) SetDefaults(v *viper.Viper) {
	v.SetDefault("redis.addr", "localhost:6379")
	v.SetDefault("redis.password", "")
	v.SetDefault("redis.db", 0)
}

func (l *redisLoader) ApplyEnvOverrides(cfg *Config) {
	envString(&cfg.Redis.Addr, "OMNEVAL_REDIS_ADDR")
	envString(&cfg.Redis.Password, "OMNEVAL_REDIS_PASSWORD")
	envInt(&cfg.Redis.DB, "OMNEVAL_REDIS_DB")
}
