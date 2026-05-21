package server

import (
	"context"
	"database/sql"
	"fmt"
	"sync"

	"github.com/omneval/omneval/internal/duckdb"
)

// SwappableDB wraps a *sql.DB and forwards all database calls to it under a
// read-write mutex. Calling Swap() closes the current connection and opens a
// fresh one from the same file path, allowing the query service to pick up a
// newly-downloaded DuckDB snapshot atomically without restarting.
//
// SwappableDB implements handler.DBHandle.
type SwappableDB struct {
	mu   sync.RWMutex
	db   *sql.DB
	path string
}

// NewSwappableDB opens a DuckDB database at path and returns a SwappableDB.
func NewSwappableDB(path string) (*SwappableDB, error) {
	db, err := duckdb.Open(path)
	if err != nil {
		return nil, fmt.Errorf("swappabledb: open %s: %w", path, err)
	}
	return &SwappableDB{db: db, path: path}, nil
}

// Swap closes the current DuckDB connection and reopens the file at the same
// path. This is called after pollAndDownload writes a new snapshot to disk.
func (s *SwappableDB) Swap() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	old := s.db
	db, err := duckdb.Open(s.path)
	if err != nil {
		return fmt.Errorf("swappabledb: reopen %s: %w", s.path, err)
	}
	s.db = db
	if old != nil {
		old.Close()
	}
	return nil
}

// Close closes the underlying database connection.
func (s *SwappableDB) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db != nil {
		return s.db.Close()
	}
	return nil
}

// QueryContext implements handler.DBHandle.
func (s *SwappableDB) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.db.QueryContext(ctx, query, args...)
}

// ExecContext implements handler.DBHandle.
func (s *SwappableDB) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.db.ExecContext(ctx, query, args...)
}

// QueryRow implements handler.DBHandle.
func (s *SwappableDB) QueryRow(query string, args ...any) *sql.Row {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.db.QueryRow(query, args...)
}

// Exec implements handler.DBHandle.
func (s *SwappableDB) Exec(query string, args ...any) (sql.Result, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.db.Exec(query, args...)
}

// Query implements handler.DBHandle.
func (s *SwappableDB) Query(query string, args ...any) (*sql.Rows, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.db.Query(query, args...)
}
