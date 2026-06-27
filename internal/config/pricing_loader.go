package config

import "github.com/spf13/viper"

type pricingLoader struct{}

func NewPricingLoader() SectionLoader { return &pricingLoader{} }

func (l *pricingLoader) SetDefaults(v *viper.Viper) {
	v.SetDefault("pricing.model_overrides", map[string]PricingModelOverride{})
}

func (l *pricingLoader) ApplyEnvOverrides(cfg *Config) {
	// Pricing model overrides are configured through the YAML file only.
	// Environment variable override is not supported for map fields.
}
