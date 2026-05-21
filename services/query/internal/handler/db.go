package handler

import (
	"context"
	"database/sql"
)

// DBHandle is the minimal interface required by all handler types that
// execute DuckDB queries. Both *sql.DB and *server.SwappableDB satisfy
// this interface.
type DBHandle interface {
	Query(query string, args ...any) (*sql.Rows, error)
	QueryRow(query string, args ...any) *sql.Row
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	Exec(query string, args ...any) (sql.Result, error)
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}
