package domain

import "time"

type Organization struct {
	OrgID     string    `json:"org_id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
}

type Project struct {
	ProjectID string    `json:"project_id"`
	OrgID     string    `json:"org_id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
}

type User struct {
	UserID             string
	OrgID              string
	Email              string
	PasswordHash       string
	CreatedAt          time.Time
	PasswordResetToken string
	ResetTokenExpiry   time.Time
}

type Session struct {
	SessionID string
	UserID    string
	ExpiresAt time.Time
	CreatedAt time.Time
}

// APIKeyKind distinguishes project-scoped and service-scoped keys.
type APIKeyKind string

const (
	APIKeyKindProject APIKeyKind = "project" // oev_proj_ prefix
	APIKeyKindService APIKeyKind = "service" // oev_svc_ prefix
)

type APIKey struct {
	KeyID       string
	ProjectID   string
	Kind        APIKeyKind
	ServiceName string // non-empty for service-scoped keys
	Name        string // optional user-supplied display name (any key kind)
	HashedKey   string
	CreatedAt   time.Time
	RevokedAt   *time.Time
}
