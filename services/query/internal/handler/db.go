package handler

import (
	"context"
	"database/sql"
)

// DBHandle is the database interface used by all handlers in this package.
// It abstracts *sql.DB so the server can substitute a SwappableDB that
// atomically reopens the connection each time a new DuckDB snapshot arrives
// from S3.
type DBHandle interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryRow(query string, args ...any) *sql.Row
	Exec(query string, args ...any) (sql.Result, error)
	Query(query string, args ...any) (*sql.Rows, error)
}
