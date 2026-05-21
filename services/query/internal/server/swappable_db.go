package server

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
)

// SwappableDB is a thread-safe wrapper around *sql.DB that supports atomic
// connection replacement. The Query API uses it so that the S3 snapshot poller
// can swap in a fresh DuckDB connection without restarting the server.
//
// SwappableDB satisfies handler.DBHandle and can be passed directly to
// SpanHandler, BookmarkHandler, AdminHandler, etc.
type SwappableDB struct {
	mu   sync.RWMutex
	db   *sql.DB
	path string
}

// NewSwappableDB opens a DuckDB database at path and returns a SwappableDB
// wrapping it. Returns an error if the database cannot be opened.
func NewSwappableDB(path string) (*SwappableDB, error) {
	db, err := openSnapshotDBRW(path)
	if err != nil {
		return nil, fmt.Errorf("swappable db: open %s: %w", path, err)
	}
	return &SwappableDB{db: db, path: path}, nil
}

// Swap atomically replaces the underlying *sql.DB by reopening the file at
// the same path. The old connection is closed after the swap.
// Called by the S3 snapshot poller after a new snapshot has been downloaded.
func (s *SwappableDB) Swap() error {
	newDB, err := openSnapshotDBRW(s.path)
	if err != nil {
		return fmt.Errorf("swappable db: swap: open %s: %w", s.path, err)
	}

	s.mu.Lock()
	old := s.db
	s.db = newDB
	s.mu.Unlock()

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

// Query executes a query that returns rows.
func (s *SwappableDB) Query(query string, args ...any) (*sql.Rows, error) {
	s.mu.RLock()
	db := s.db
	s.mu.RUnlock()
	return db.Query(query, args...)
}

// QueryRow executes a query expected to return at most one row.
func (s *SwappableDB) QueryRow(query string, args ...any) *sql.Row {
	s.mu.RLock()
	db := s.db
	s.mu.RUnlock()
	return db.QueryRow(query, args...)
}

// Exec executes a query without returning rows.
func (s *SwappableDB) Exec(query string, args ...any) (sql.Result, error) {
	s.mu.RLock()
	db := s.db
	s.mu.RUnlock()
	return db.Exec(query, args...)
}

// QueryContext executes a query that returns rows, with context.
func (s *SwappableDB) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	s.mu.RLock()
	db := s.db
	s.mu.RUnlock()
	return db.QueryContext(ctx, query, args...)
}

// ExecContext executes a query without returning rows, with context.
func (s *SwappableDB) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	s.mu.RLock()
	db := s.db
	s.mu.RUnlock()
	return db.ExecContext(ctx, query, args...)
}
