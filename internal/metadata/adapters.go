package metadata

import (
	"github.com/omneval/omneval/internal/metadata/postgres"
	"github.com/omneval/omneval/internal/metadata/sqlite"
)

// Compile-time assertions: the concrete sub-package stores must satisfy
// the focused interfaces defined in this package.
var (
	_ AuthStore = (*postgres.Store)(nil)
	_ AuthStore = (*sqlite.Store)(nil)
)

// PostgresFocusedStore wraps *postgres.Store so that callers can depend on
// the narrower focused interfaces (ProjectStore, PromptStore, EvalRuleStore, ...)
// instead of the full god Store. Embedding the concrete store promotes all domain
// methods; the ProjectStore()/PromptStore()/EvalRuleStore() methods below
// override the embedded store's package-local methods so they return this
// package's focused interface types instead.
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

// ProjectStore returns the focused PostgresProjectStore receiver that exposes
// only the ProjectStore interface methods.
func (s *PostgresFocusedStore) ProjectStore() ProjectStore {
	return s.Store.ProjectStore()
}

// PromptStore returns the focused PromptStore interface.
func (s *PostgresFocusedStore) PromptStore() PromptStore {
	return s.Store
}

// EvalRuleStore returns the focused EvalRuleStore interface.
func (s *PostgresFocusedStore) EvalRuleStore() EvalRuleStore {
	return s.Store
}

// APIKeyStore returns the focused APIKeyStore interface.
func (s *PostgresFocusedStore) APIKeyStore() APIKeyStore {
	return s.Store
}

// BatchLedgerStore returns the focused BatchLedgerStore interface.
func (s *PostgresFocusedStore) BatchLedgerStore() BatchLedgerStore {
	return s.Store
}

// AuthStore returns the focused AuthStore interface.
func (s *PostgresFocusedStore) AuthStore() AuthStore {
	return s.Store
}

// DatasetStore returns the focused DatasetStore interface.
func (s *PostgresFocusedStore) DatasetStore() DatasetStore {
	return s.Store.DatasetStore()
}

// Compile-time check: PostgresFocusedStore satisfies metadata.Store.
var _ Store = (*PostgresFocusedStore)(nil)

// SQLiteFocusedStore wraps *sqlite.Store so that callers can depend on the
// narrower focused interfaces (ProjectStore, PromptStore, EvalRuleStore, ...)
// instead of the full god Store.
type SQLiteFocusedStore struct {
	*sqlite.Store
}

// SessionStore returns the focused SessionStore interface.
func (s *SQLiteFocusedStore) SessionStore() SessionStore {
	return s.Store
}

// BookmarkStore returns the focused BookmarkStore interface.
func (s *SQLiteFocusedStore) BookmarkStore() BookmarkStore {
	return s.Store
}

// ProjectStore returns the focused SQLiteProjectStore receiver that exposes
// only the ProjectStore interface methods.
func (s *SQLiteFocusedStore) ProjectStore() ProjectStore {
	return s.Store.ProjectStore()
}

// PromptStore returns the focused PromptStore interface.
func (s *SQLiteFocusedStore) PromptStore() PromptStore {
	return s.Store
}

// EvalRuleStore returns the focused EvalRuleStore interface.
func (s *SQLiteFocusedStore) EvalRuleStore() EvalRuleStore {
	return s.Store
}

// APIKeyStore returns the focused APIKeyStore interface.
func (s *SQLiteFocusedStore) APIKeyStore() APIKeyStore {
	return s.Store
}

// BatchLedgerStore returns the focused BatchLedgerStore interface.
func (s *SQLiteFocusedStore) BatchLedgerStore() BatchLedgerStore {
	return s.Store
}

// AuthStore returns the focused AuthStore interface.
func (s *SQLiteFocusedStore) AuthStore() AuthStore {
	return s.Store
}

// DatasetStore returns the focused DatasetStore interface.
func (s *SQLiteFocusedStore) DatasetStore() DatasetStore {
	return s.Store.DatasetStore()
}

// Compile-time check: SQLiteFocusedStore satisfies metadata.Store.
var _ Store = (*SQLiteFocusedStore)(nil)
