package metadata

import (
	"context"

	"github.com/omneval/omneval/internal/domain"
)

// SessionStore is the domain interface for session CRUD operations.
type SessionStore interface {
	CreateSession(ctx context.Context, session *domain.Session) error
	GetSession(ctx context.Context, sessionID string) (*domain.Session, error)
	DeleteSession(ctx context.Context, sessionID string) error
}