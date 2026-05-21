package handler

import (
	"context"
	"database/sql"
)

// DBHandle is the minimal database interface used by query handlers.
// It is satisfied by *sql.DB, *sql.Tx, and SwappableDB.
type DBHandle interface {
	// Query executes a query that returns rows.
	Query(query string, args ...any) (*sql.Rows, error)

	// QueryRow executes a query expected to return at most one row.
	QueryRow(query string, args ...any) *sql.Row

	// Exec executes a query without returning rows.
	Exec(query string, args ...any) (sql.Result, error)

	// QueryContext executes a query that returns rows, with context.
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)

	// ExecContext executes a query without returning rows, with context.
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}
