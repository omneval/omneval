package metadata

import (
	"context"

	"github.com/omneval/omneval/internal/domain"
)

// ErrNotFound is returned when a requested entity does not exist.
// The sentinel value lives in the domain package so store implementations can
// reference it without importing this package (which imports them via Open).
var ErrNotFound = domain.ErrNotFound

// Store is the unified interface all metadata backends must satisfy.
// It embeds the focused domain interfaces defined below so that existing
// code continues to compile while callers can depend on the narrower types.
//
// Deprecated: prefer the focused interfaces (ProjectStore, PromptStore, etc.)
// at call sites. Store is kept for backward compatibility and for adapters
// that need access to the full backend.
type Store interface {
	ProjectStore
	BookmarkStore
	SessionStore
	AuthStore
	APIKeyStore
	PromptStore
	BatchLedgerStore
	EvalRuleStore
	DatasetStore

	// ListUsers is not part of a focused interface because the auth flow
	// only uses it internally (user listing for admin pages).
	ListUsers(ctx context.Context, orgID string) ([]*domain.User, error)

	// Migrations and lifecycle are backend-specific concerns.
	Migrate(ctx context.Context) error
	Close() error
}
