package auth

import (
	"context"

	"github.com/zbloss/lantern/internal/domain"
)

// ValidatedKey is the result of successful API key authentication.
type ValidatedKey struct {
	ProjectID   string
	Kind        domain.APIKeyKind
	ServiceName string
}

// Validator authenticates raw API key strings against the metadata store.
type Validator interface {
	Validate(ctx context.Context, rawKey string) (*ValidatedKey, error)
}
