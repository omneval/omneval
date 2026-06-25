package config

import "github.com/spf13/viper"

type storageLoader struct{}

func NewStorageLoader() SectionLoader { return &storageLoader{} }

func (l *storageLoader) SetDefaults(v *viper.Viper) {
	v.SetDefault("storage.endpoint", "")
	v.SetDefault("storage.bucket", "")
	v.SetDefault("storage.region", "")
	v.SetDefault("storage.access_key", "")
	v.SetDefault("storage.secret_key", "")
}

func (l *storageLoader) ApplyEnvOverrides(cfg *Config) {
	envString(&cfg.Storage.Endpoint, "OMNEVAL_STORAGE_ENDPOINT")
	envString(&cfg.Storage.Bucket, "OMNEVAL_STORAGE_BUCKET")
	envString(&cfg.Storage.Region, "OMNEVAL_STORAGE_REGION")
	envString(&cfg.Storage.AccessKey, "OMNEVAL_STORAGE_ACCESS_KEY")
	envString(&cfg.Storage.SecretKey, "OMNEVAL_STORAGE_SECRET_KEY")
}
