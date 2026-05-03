package domain

import "time"

type Organization struct {
	OrgID     string
	Name      string
	CreatedAt time.Time
}

type Project struct {
	ProjectID string
	OrgID     string
	Name      string
	CreatedAt time.Time
}

type User struct {
	UserID    string
	OrgID     string
	Email     string
	CreatedAt time.Time
}

// APIKeyKind distinguishes project-scoped and service-scoped keys.
type APIKeyKind string

const (
	APIKeyKindProject APIKeyKind = "project" // ltn_proj_ prefix
	APIKeyKindService APIKeyKind = "service"  // ltn_svc_ prefix
)

type APIKey struct {
	KeyID       string
	ProjectID   string
	Kind        APIKeyKind
	ServiceName string // non-empty for service-scoped keys
	HashedKey   string
	CreatedAt   time.Time
	RevokedAt   *time.Time
}
