// Package routes provides shared types for the query service HTTP layer:
// authentication policies, route descriptors, database handle, and
// session store. Both the router and domain-specific segments import
// this package so they reference a single definition of each type.
package routes

import (
	"context"
	"database/sql"
	"net/http"

	"github.com/omneval/omneval/internal/domain"
)

// AuthPolicy defines the authentication requirement for a route.
type AuthPolicy int

const (
	// AuthPolicyPublic routes bypass authentication entirely.
	AuthPolicyPublic AuthPolicy = iota
	// AuthPolicySession routes require a valid session cookie.
	AuthPolicySession
	// AuthPolicyAPIKeyOrSession routes accept a valid session cookie or X-API-Key header.
	AuthPolicyAPIKeyOrSession
	// AuthPolicyAdmin routes require a valid session cookie AND an admin user.
	AuthPolicyAdmin
)

// String returns the canonical string representation of the auth policy.
func (a AuthPolicy) String() string {
	switch a {
	case AuthPolicyPublic:
		return "public"
	case AuthPolicySession:
		return "session"
	case AuthPolicyAPIKeyOrSession:
		return "session_or_api_key"
	case AuthPolicyAdmin:
		return "admin"
	default:
		return "unknown"
	}
}

// AuthRoute pairs an HTTP method/path pattern with its handler and auth policy.
type AuthRoute struct {
	Method  string
	Path    string
	Handler func(http.ResponseWriter, *http.Request)
	Policy  AuthPolicy
}

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

	// QueryRowContext executes a query expected to return at most one row, with context.
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// SessionStore abstracts session lookup for project ID extraction.
type SessionStore interface {
	ProjectID(r *http.Request) (string, bool)
	ListProjects(r *http.Request) ([]*domain.Project, error)
}