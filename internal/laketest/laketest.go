// Package laketest provides shared Quack Lake test fixtures for integration
// tests that need a real Lake. Per ADR-0005, every Quack client attaches
// via a running Quack Server (internal/lake/lakeserver). This package
// starts a server, opens a Lake, and provides helpers for verifying Lake
// contents in tests.
package laketest

import (
	"context"
	"database/sql"
	"fmt"
	"testing"

	"github.com/omneval/omneval/internal/lake"
	"github.com/omneval/omneval/internal/lake/lakeservertest"
)

// NewLocal starts a Quack Server and returns a connected *lake.Lake ready
// for integration tests. The server and lake are closed automatically via
// t.Cleanup.
func NewLocal(t *testing.T) *lake.Lake {
	t.Helper()
	cfg, _ := lakeservertest.NewLocal(t)
	return newLake(t, cfg)
}

// NewLocalConfig starts a Quack Server and returns the lake.Config that
// attaches to it. The server is closed automatically via t.Cleanup. This is
// useful when tests need config values for wiring but do not need a full
// *lake.Lake.
func NewLocalConfig(t *testing.T) lake.Config {
	t.Helper()
	cfg, _ := lakeservertest.NewLocal(t)
	return cfg
}

// newLake opens a lake from the given config, skipping the test if the
// ducklake extension is unavailable.
func newLake(t *testing.T, cfg lake.Config) *lake.Lake {
	t.Helper()
	lk, err := lake.Open(context.Background(), cfg)
	if err != nil {
		t.Skipf("lake.Open: %v (ducklake extension unavailable)", err)
	}
	t.Cleanup(func() { lk.Close() })
	return lk
}

// NewPostgres starts a Quack Server backed by the given Postgres catalog DSN
// and returns a connected *lake.Lake ready for integration tests. The server
// is closed automatically via t.Cleanup.
func NewPostgres(t *testing.T, catalogDSN string) *lake.Lake {
	t.Helper()
	cfg := lakeservertest.NewPostgres(t, catalogDSN)
	return newLake(t, cfg)
}

// NewPostgresWithConfig starts a Quack Server backed by the given Postgres
// catalog DSN and returns a connected *lake.Lake ready for integration tests.
// The caller may modify the lake.Config before the server starts by providing
// a non-nil modifyFn. The server is closed automatically via t.Cleanup.
func NewPostgresWithConfig(t *testing.T, catalogDSN string, modifyFn func(*lake.Config)) *lake.Lake {
	t.Helper()
	cfg := lakeservertest.NewPostgres(t, catalogDSN)
	if modifyFn != nil {
		modifyFn(&cfg)
	}
	return newLake(t, cfg)
}

// NewLocalWithStorage is like NewLocal but allows customising the DataPath and
// Storage config before opening the lake.
func NewLocalWithStorage(t *testing.T, modifyFn func(cfg *lake.Config, dataDir string)) *lake.Lake {
	t.Helper()
	cfg, dataDir := lakeservertest.NewLocal(t)
	if modifyFn != nil {
		modifyFn(&cfg, dataDir)
	}
	return newLake(t, cfg)
}

// SpanCount queries the Lake for the number of spans matching the given SQL
// WHERE clause (without the word "WHERE").
func SpanCount(t *testing.T, lk *lake.Lake, where string) int {
	t.Helper()
	query := "SELECT count(*) FROM lake.spans"
	if where != "" {
		query += " WHERE " + where
	}
	var n int
	if err := lk.DB().QueryRowContext(context.Background(), query).Scan(&n); err != nil {
		t.Fatalf("count lake spans: %v", err)
	}
	return n
}

// ScoreCount queries the Lake for the number of scores matching the given SQL
// WHERE clause (without the word "WHERE").
func ScoreCount(t *testing.T, lk *lake.Lake, where string) int {
	t.Helper()
	query := "SELECT count(*) FROM lake.scores"
	if where != "" {
		query += " WHERE " + where
	}
	var n int
	if err := lk.DB().QueryRowContext(context.Background(), query).Scan(&n); err != nil {
		t.Fatalf("count lake scores: %v", err)
	}
	return n
}

// MustQueryRow executes a query and scans into dest, failing the test on any
// error. It is a convenience wrapper around lk.DB().QueryRowContext.
func MustQueryRow(t *testing.T, lk *lake.Lake, query string, dest ...any) {
	t.Helper()
	if err := lk.DB().QueryRowContext(context.Background(), query).Scan(dest...); err != nil {
		t.Fatalf("query row: %v (sql=%s)", err, query)
	}
}

// MustExec executes a statement and fails the test on any error.
func MustExec(t *testing.T, lk *lake.Lake, query string, args ...any) {
	t.Helper()
	if _, err := lk.DB().ExecContext(context.Background(), query, args...); err != nil {
		t.Fatalf("exec: %v (sql=%s)", err, query)
	}
}

// MustExecContext is like MustExec but accepts a context.
func MustExecContext(t *testing.T, ctx context.Context, lk *lake.Lake, query string, args ...any) {
	t.Helper()
	if _, err := lk.DB().ExecContext(ctx, query, args...); err != nil {
		t.Fatalf("exec context: %v (sql=%s)", err, query)
	}
}

// QueryRow executes a query and scans into dest, failing the test on any
// error.
func QueryRow(t *testing.T, lk *lake.Lake, query string, dest ...any) {
	t.Helper()
	if err := lk.DB().QueryRowContext(context.Background(), query).Scan(dest...); err != nil {
		t.Fatalf("query row: %v (sql=%s)", err, query)
	}
}

// Exec executes a statement and fails the test on any error.
func Exec(t *testing.T, lk *lake.Lake, query string, args ...any) {
	t.Helper()
	if _, err := lk.DB().ExecContext(context.Background(), query, args...); err != nil {
		t.Fatalf("exec: %v (sql=%s)", err, query)
	}
}

// DB returns the underlying *sql.DB for the lake, allowing tests that need
// to run custom SQL (views, DDL) to do so without importing *lake directly.
// Most callers should prefer SpanCount / ScoreCount over raw SQL.
func DB(lk *lake.Lake) *sql.DB {
	return lk.DB()
}

// NewLocalWithView starts a Quack Server and opens a Lake, then creates the
// given view SQL (e.g. "CREATE OR REPLACE VIEW spans AS SELECT * FROM
// lake.spans"). Returns the lake, which the caller should close via t.Cleanup.
func NewLocalWithView(t *testing.T, viewSQL string) *lake.Lake {
	t.Helper()
	lk := NewLocal(t)
	if viewSQL != "" {
		if _, err := lk.DB().ExecContext(context.Background(), viewSQL); err != nil {
			t.Fatalf("create view: %v", err)
		}
	}
	return lk
}

// BuildSpanCountQuery returns a SELECT count(*) SQL for spans with an optional
// WHERE clause.
func BuildSpanCountQuery(where string) string {
	q := "SELECT count(*) FROM lake.spans"
	if where != "" {
		q += " WHERE " + where
	}
	return q
}

// BuildScoreCountQuery returns a SELECT count(*) SQL for scores with an optional
// WHERE clause.
func BuildScoreCountQuery(where string) string {
	q := "SELECT count(*) FROM lake.scores"
	if where != "" {
		q += " WHERE " + where
	}
	return q
}

// QueryRowWithCtx executes a query on the lake using the provided context.
func QueryRowWithCtx(t *testing.T, ctx context.Context, lk *lake.Lake, query string, dest ...any) {
	t.Helper()
	if err := lk.DB().QueryRowContext(ctx, query).Scan(dest...); err != nil {
		t.Fatalf("query row with context: %v (sql=%s)", err, query)
	}
}

// ExecContext executes a statement on the lake using the provided context.
func ExecContext(t *testing.T, ctx context.Context, lk *lake.Lake, query string, args ...any) {
	t.Helper()
	if _, err := lk.DB().ExecContext(ctx, query, args...); err != nil {
		t.Fatalf("exec context: %v (sql=%s)", err, query)
	}
}

// MustQueryRowWithCtx is like MustQueryRow but accepts a context.
func MustQueryRowWithCtx(t *testing.T, ctx context.Context, lk *lake.Lake, query string, dest ...any) {
	t.Helper()
	if err := lk.DB().QueryRowContext(ctx, query).Scan(dest...); err != nil {
		t.Fatalf("query row with context: %v (sql=%s)", err, query)
	}
}

// FormatSpanCountQuery returns a readable span count query for a given project.
func FormatSpanCountQuery(projectID string) string {
	return fmt.Sprintf("SELECT count(*) FROM lake.spans WHERE project_id = %q", projectID)
}

// FormatScoreCountQuery returns a readable score count query for a given project.
func FormatScoreCountQuery(projectID string) string {
	return fmt.Sprintf("SELECT count(*) FROM lake.scores WHERE project_id = %q", projectID)
}

// QueryRowWithCtxNoError executes a query without failing on error, for tests that
// want to handle errors themselves.
func QueryRowWithCtxNoError(ctx context.Context, lk *lake.Lake, query string, dest ...any) error {
	return lk.DB().QueryRowContext(ctx, query).Scan(dest...)
}

// ExecContextNoError executes a statement without failing on error, for tests
// that want to handle errors themselves.
func ExecContextNoError(ctx context.Context, lk *lake.Lake, query string, args ...any) error {
	_, err := lk.DB().ExecContext(ctx, query, args...)
	return err
}