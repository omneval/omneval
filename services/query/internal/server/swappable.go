package server

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
)

// SwappableDB wraps a *sql.DB and allows atomically replacing it with a new
// connection when the on-disk snapshot is updated. This lets the poller call
// Swap() after downloading a fresh snapshot without interrupting in-flight
// queries on the old handle.
type SwappableDB struct {
	mu   sync.RWMutex
	db   *sql.DB
	path string
}

// NewSwappableDB opens a DuckDB database at path in read-only mode and
// returns a SwappableDB wrapping it.
func NewSwappableDB(path string) (*SwappableDB, error) {
	db, err := openSnapshotDB(path)
	if err != nil {
		return nil, err
	}
	return &SwappableDB{db: db, path: path}, nil
}

// Swap closes the current DuckDB connection and reopens the file at the same
// path. This is safe to call concurrently with Query / Exec operations because
// it holds a write lock only during the swap itself.
func (s *SwappableDB) Swap() error {
	newDB, err := openSnapshotDB(s.path)
	if err != nil {
		return fmt.Errorf("swappable: reopen %s: %w", s.path, err)
	}

	s.mu.Lock()
	old := s.db
	s.db = newDB
	s.mu.Unlock()

	// Close the old connection outside the lock so we don't block readers.
	return old.Close()
}

// Close closes the underlying database connection.
func (s *SwappableDB) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.db.Close()
}

// Query executes a query that returns rows.
func (s *SwappableDB) Query(query string, args ...any) (*sql.Rows, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.db.Query(query, args...)
}

// QueryRow executes a query expected to return at most one row.
func (s *SwappableDB) QueryRow(query string, args ...any) *sql.Row {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.db.QueryRow(query, args...)
}

// QueryContext executes a query with a context that returns rows.
func (s *SwappableDB) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.db.QueryContext(ctx, query, args...)
}

// Exec executes a query without returning rows.
func (s *SwappableDB) Exec(query string, args ...any) (sql.Result, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.db.Exec(query, args...)
}

// ExecContext executes a query with a context without returning rows.
func (s *SwappableDB) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.db.ExecContext(ctx, query, args...)
}

// openSnapshotDB opens the DuckDB snapshot file in read-write mode.
// DuckDB in read-write mode is acceptable here because the Query service
// writes scores back via the score handler.
func openSnapshotDB(path string) (*sql.DB, error) {
	return openDuckDB(path + "?access_mode=read_write")
}
