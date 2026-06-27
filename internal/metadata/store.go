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
	DatasetStore
	EvalRuleStore

	// ListUsers is not part of a focused interface because the auth flow
	// only uses it internally (user listing for admin pages).
	ListUsers(ctx context.Context, orgID string) ([]*domain.User, error)

	// Migrations and lifecycle are backend-specific concerns.
	Migrate(ctx context.Context) error
	Close() error

	// SessionStore returns a focused SessionStore interface that exposes
	// only the session management methods, enabling callers to depend on the
	// narrower type rather than the god interface.
	SessionStore() SessionStore

	// BookmarkStore returns a focused BookmarkStore interface that exposes
	// only the bookmark operations, enabling callers to depend on the
	// narrower type rather than the god interface.
	BookmarkStore() BookmarkStore

	// ProjectStore returns a focused ProjectStore interface that exposes
	// only the project CRUD methods, enabling callers to depend on the
	// narrower type rather than the god interface.
	ProjectStore() ProjectStore

	// PromptStore returns a focused PromptStore interface that exposes
	// only the prompt registry methods, enabling callers to depend on the
	// narrower type rather than the god interface.
	PromptStore() PromptStore

	// EvalRuleStore returns a focused EvalRuleStore interface that exposes
	// only the evaluation-rule methods, enabling callers to depend on the
	// narrower type rather than the god interface.
	EvalRuleStore() EvalRuleStore

	// APIKeyStore returns a focused APIKeyStore interface that exposes
	// only the API key methods, enabling callers to depend on the
	// narrower type rather than the god interface.
	APIKeyStore() APIKeyStore

	// BatchLedgerStore returns a focused BatchLedgerStore interface that
	// exposes only the batch commit dedupe methods, enabling callers to
	// depend on the narrower type rather than the god interface.
	BatchLedgerStore() BatchLedgerStore

	// AuthStore returns a focused AuthStore interface that exposes
	// only the user and organization methods, enabling callers to depend on
	// the narrower type rather than the god interface.
	AuthStore() AuthStore
}
