package config

import "github.com/spf13/viper"

// SectionLoader is the interface for a configuration section deep module.
// Each section loader sets its own viper defaults and applies its own
// environment variable overrides, keeping configuration local to the
// section it owns.
type SectionLoader interface {
	// SetDefaults registers the section's defaults on the given viper.
	SetDefaults(v *viper.Viper)
	// ApplyEnvOverrides applies the section's environment variable
	// overrides to the top-level Config, reading from os.Getenv.
	ApplyEnvOverrides(cfg *Config)
}
