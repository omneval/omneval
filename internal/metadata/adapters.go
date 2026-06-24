package metadata

import (
	"github.com/omneval/omneval/internal/metadata/postgres"
	"github.com/omneval/omneval/internal/metadata/sqlite"
)

// PostgresPromptStore wraps *postgres.Store so that callers can depend on
// the narrower metadata.Store interface instead of the full god Store.
// Embedding the concrete store promotes all domain methods; the PromptStore()
// method returns the embedded type as the focused interface.
type PostgresPromptStore struct {
	*postgres.Store
}

// PromptStore returns the focused PromptStore interface.
func (s *PostgresPromptStore) PromptStore() PromptStore {
	return s
}

// Compile-time check: PostgresPromptStore satisfies metadata.PromptStore.
var _ PromptStore = (*PostgresPromptStore)(nil)

// SQLitePromptStore wraps *sqlite.Store so that callers can depend on the
// narrower metadata.Store interface instead of the full god Store.
type SQLitePromptStore struct {
	*sqlite.Store
}

// PromptStore returns the focused PromptStore interface.
func (s *SQLitePromptStore) PromptStore() PromptStore {
	return s
}

// Compile-time check: SQLitePromptStore satisfies metadata.PromptStore.
var _ PromptStore = (*SQLitePromptStore)(nil)

// --- Compile-time interface compliance checks for Store ---

var (
	_ Store = (*PostgresPromptStore)(nil)
	_ Store = (*SQLitePromptStore)(nil)
)