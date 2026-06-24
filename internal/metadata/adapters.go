package metadata

import (
	"github.com/omneval/omneval/internal/metadata/postgres"
	"github.com/omneval/omneval/internal/metadata/sqlite"
)

// PostgresEvalRuleStore wraps *postgres.Store so that callers can depend on
// the narrower metadata.Store interface instead of the full god Store.
// Embedding the concrete store promotes all domain methods; the EvalRuleStore()
// method returns the embedded type as the focused interface.
type PostgresEvalRuleStore struct {
	*postgres.Store
}

// EvalRuleStore returns the focused EvalRuleStore interface.
func (s *PostgresEvalRuleStore) EvalRuleStore() EvalRuleStore {
	return s.Store
}

// SQLiteEvalRuleStore wraps *sqlite.Store so that callers can depend on the
// narrower metadata.Store interface instead of the full god Store.
type SQLiteEvalRuleStore struct {
	*sqlite.Store
}

// EvalRuleStore returns the focused EvalRuleStore interface.
func (s *SQLiteEvalRuleStore) EvalRuleStore() EvalRuleStore {
	return s.Store
}

// --- Compile-time interface compliance checks ---

var (
	_ Store = (*PostgresEvalRuleStore)(nil)
	_ Store = (*SQLiteEvalRuleStore)(nil)
)