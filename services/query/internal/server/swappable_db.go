package server

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// SwappableDB is a thread-safe wrapper around *sql.DB that supports atomic
// connection replacement using A/B file path rotation. The Query API uses it
// so that the S3 snapshot poller can swap in a fresh DuckDB connection without
// restarting the server.
//
// Background (issue #20): the go-duckdb/v2 driver maintains an internal
// connection cache keyed by file path. If Swap() re-opens the same path after
// a new snapshot has been written there, the driver returns the cached
// in-memory state of the old file — causing the Query API to always see 0
// spans. A/B rotation avoids this by alternating between two distinct file
// paths so the driver never sees the same path twice in a row.
//
// SwappableDB satisfies handler.DBHandle and can be passed directly to
// SpanHandler, BookmarkHandler, AdminHandler, etc.
type SwappableDB struct {
	mu sync.RWMutex
	db *sql.DB

	// A/B rotation: two fixed paths that we alternate between on each Swap.
	// pathA and pathB are determined from the directory of the initial path.
	pathA string
	pathB string
	useA  bool // true when pathA is currently active
}

// NewSwappableDB opens a DuckDB database at path and returns a SwappableDB
// wrapping it. The A/B sibling paths are placed in the same directory.
// Returns an error if the database cannot be opened.
func NewSwappableDB(path string) (*SwappableDB, error) {
	dir := filepath.Dir(path)
	pathA := filepath.Join(dir, "snap_a.duckdb")
	pathB := filepath.Join(dir, "snap_b.duckdb")

	// Copy the initial snapshot into pathA so the A/B invariant holds from
	// the start: the active path is always snap_a or snap_b.
	if err := copyFile(path, pathA); err != nil {
		return nil, fmt.Errorf("swappable db: init copy to %s: %w", pathA, err)
	}

	db, err := openSnapshotDBRW(pathA)
	if err != nil {
		return nil, fmt.Errorf("swappable db: open %s: %w", pathA, err)
	}
	return &SwappableDB{
		db:    db,
		pathA: pathA,
		pathB: pathB,
		useA:  true,
	}, nil
}

// NewSwappableDBFromDB wraps an existing *sql.DB in a SwappableDB.
// This is used for the Lake connection, which is already an in-memory
// DuckDB with DuckLake attached and does not require A/B rotation.
// Swap is a no-op on such a wrapper.
func NewSwappableDBFromDB(db *sql.DB) *SwappableDB {
	return &SwappableDB{db: db}
}

// Swap atomically replaces the underlying *sql.DB by installing newSnapshotPath
// into the currently-inactive A/B slot and opening a fresh connection there.
// The old connection is closed after the pointer swap to avoid file descriptor
// leaks. Called by the S3 snapshot poller after a new snapshot has been
// downloaded.
func (s *SwappableDB) Swap(newSnapshotPath string) error {
	// Determine the inactive slot: the one we are NOT currently using.
	s.mu.RLock()
	targetPath := s.pathB
	if !s.useA {
		targetPath = s.pathA
	}
	s.mu.RUnlock()

	// Copy/move the new snapshot into the inactive slot. On Windows, a file
	// that is open cannot be renamed onto another path, so we copy instead of
	// rename. The inactive slot's connection is already closed from the previous
	// swap, so the file there is safe to overwrite.
	if err := copyFile(newSnapshotPath, targetPath); err != nil {
		return fmt.Errorf("swappable db: swap: copy to %s: %w", targetPath, err)
	}

	// Open a fresh connection to the newly-populated inactive slot.
	// This must happen BEFORE the pointer swap so that any error does not leave
	// the SwappableDB in a broken state.
	newDB, err := openSnapshotDBRW(targetPath)
	if err != nil {
		return fmt.Errorf("swappable db: swap: open %s: %w", targetPath, err)
	}

	// Atomically swap the active pointer and flip the A/B flag.
	s.mu.Lock()
	old := s.db
	s.db = newDB
	s.useA = !s.useA
	s.mu.Unlock()

	// Close the old connection after releasing the write lock so that readers
	// that held the read lock during the swap can finish their queries cleanly.
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

// DB returns the underlying *sql.DB for use with PingContext and similar
// operations that are not part of the DBHandle interface.
func (s *SwappableDB) DB() *sql.DB {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.db
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

// copyFile copies src to dst, creating or truncating dst.
// It does NOT use os.Rename because on Windows renaming onto an open file fails.
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("copy: open src %s: %w", src, err)
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return fmt.Errorf("copy: create dst %s: %w", dst, err)
	}

	if _, err := out.ReadFrom(in); err != nil {
		out.Close()
		os.Remove(dst)
		return fmt.Errorf("copy: write dst %s: %w", dst, err)
	}
	if err := out.Close(); err != nil {
		os.Remove(dst)
		return fmt.Errorf("copy: close dst %s: %w", dst, err)
	}
	return nil
}
