package metadata

import (
	"context"
	"time"

	"github.com/omneval/omneval/internal/domain"
)

// AuthStore is the domain interface for user and organization lookups by API key.
type AuthStore interface {
	GetUserByID(ctx context.Context, userID string) (*domain.User, error)
	GetUserByEmail(ctx context.Context, email string) (*domain.User, error)
	GetUserByResetToken(ctx context.Context, token string) (*domain.User, error)
	CountUsers(ctx context.Context) (int, error)
	CheckPassword(hashed, plaintext string) error
	CreateUser(ctx context.Context, user *domain.User) error
	UpdateUserPassword(ctx context.Context, userID, passwordHash string) error
	UpdateUserResetToken(ctx context.Context, userID, token string, expiry time.Time) error

	CreateOrganization(ctx context.Context, org *domain.Organization) error
	GetOrganization(ctx context.Context, orgID string) (*domain.Organization, error)
}