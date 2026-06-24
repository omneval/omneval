package metadata

import (
	"github.com/omneval/omneval/internal/metadata/postgres"
	"github.com/omneval/omneval/internal/metadata/sqlite"
)

// PostgresFocusedStore wraps *postgres.Store so that callers can depend on
// the narrower focused interfaces (PromptStore, EvalRuleStore, ...) instead
// of the full god Store. Embedding the concrete store promotes all domain
// methods; the focused Store methods below override the embedded store's
// package-local methods so they return this package's focused interface types
// instead.
type PostgresFocusedStore struct {
	*postgres.Store
}

// SessionStore returns the focused SessionStore interface.
func (s *PostgresFocusedStore) SessionStore() SessionStore {
	return s.Store
}

// BookmarkStore returns the focused BookmarkStore interface.
func (s *PostgresFocusedStore) BookmarkStore() BookmarkStore {
	return s.Store
}

// PromptStore returns the focused PromptStore interface.
func (s *PostgresFocusedStore) PromptStore() PromptStore {
	return s.Store
}

// EvalRuleStore returns the focused EvalRuleStore interface.
func (s *PostgresFocusedStore) EvalRuleStore() EvalRuleStore {
	return s.Store
}

// Compile-time check: PostgresFocusedStore satisfies metadata.Store.
var _ Store = (*PostgresFocusedStore)(nil)

// SQLiteFocusedStore wraps *sqlite.Store so that callers can depend on the
// narrower focused interfaces instead of the full god Store.
type SQLiteFocusedStore struct {
	*sqlite.Store
}

// SessionStore returns the focused SessionStore interface.
func (s *SQLiteFocusedStore) SessionStore() SessionStore {
	return s.Store
}

// PromptStore returns the focused PromptStore interface.
func (s *SQLiteFocusedStore) PromptStore() PromptStore {
	return s.Store
}

// EvalRuleStore returns the focused EvalRuleStore interface.
func (s *SQLiteFocusedStore) EvalRuleStore() EvalRuleStore {
	return s.Store
}

// Compile-time check: SQLiteFocusedStore satisfies metadata.Store.
var _ Store = (*SQLiteFocusedStore)(nil)
