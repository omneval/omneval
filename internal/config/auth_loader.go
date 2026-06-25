package config

import "github.com/spf13/viper"

type authLoader struct{}

func NewAuthLoader() SectionLoader { return &authLoader{} }

func (l *authLoader) SetDefaults(v *viper.Viper) {
	v.SetDefault("auth.session_ttl", "168h")
	v.SetDefault("auth.secure_cookie", false)
	v.SetDefault("auth.admin_email", "")
	v.SetDefault("auth.admin_password", "")
}

func (l *authLoader) ApplyEnvOverrides(cfg *Config) {
	envString(&cfg.Auth.SessionTTL, "OMNEVAL_AUTH_SESSION_TTL")
	envBool(&cfg.Auth.SecureCookie, "OMNEVAL_AUTH_SECURE_COOKIE")
	envString(&cfg.Auth.AdminEmail, "OMNEVAL_AUTH_ADMIN_EMAIL")
	envString(&cfg.Auth.AdminPassword, "OMNEVAL_AUTH_ADMIN_PASSWORD")
}
