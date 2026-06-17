package metadata

import (
	"context"

	"github.com/omneval/omneval/internal/domain"
)

// APIKeyStore is the domain interface for API key management.
type APIKeyStore interface {
	CreateAPIKey(ctx context.Context, key *domain.APIKey) error
	GetAPIKeyByHash(ctx context.Context, hashedKey string) (*domain.APIKey, error)
	ListAPIKeys(ctx context.Context, projectID string) ([]*domain.APIKey, error)
	RevokeAPIKey(ctx context.Context, keyID string) error
}