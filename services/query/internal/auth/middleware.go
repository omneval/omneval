package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	internalauth "github.com/omneval/omneval/internal/auth"
	"github.com/omneval/omneval/internal/domain"
)

// ContextKey is the type used for context keys to avoid collisions.
// Deprecated: use internalauth project/user context keys instead.
type ContextKey string

const (
	// CurrentUserKey is the context key for the current authenticated user.
	// Deprecated: use internalauth.UserIDContextKey.
	CurrentUserKey ContextKey = "current_user"
)

// Re-export context keys from internalauth for backwards compatibility.
var (
	// ProjectIDContextKey is the context key for the project ID.
	ProjectIDContextKey = internalauth.ProjectIDContextKey
	// UserIDContextKey is the context key for the authenticated user's ID.
	UserIDContextKey = internalauth.UserIDContextKey
	// EmailContextKey is the context key for the authenticated user's email.
	EmailContextKey = internalauth.EmailContextKey
	// AdminEmailContextKey is the context key for the admin email.
	AdminEmailContextKey = internalauth.AdminEmailContextKey
	// APIKeyProjectIDContextKey is the context key for the project ID from an API key.
	APIKeyProjectIDContextKey = internalauth.APIKeyProjectIDContextKey
)

// CurrentUser is the authenticated user stored in the request context.
// Re-exports [internalauth.CurrentUser] for backwards compatibility.
type CurrentUser = internalauth.CurrentUser

// authSessioner is the minimal interface the auth middleware needs from metadata.
// It is satisfied by the metadata.Store interface (which embeds SessionStore and AuthStore).
type authSessioner interface {
	GetSession(ctx context.Context, sessionID string) (*domain.Session, error)
	DeleteSession(ctx context.Context, sessionID string) error
	GetUserByID(ctx context.Context, userID string) (*domain.User, error)
}

// RequireAuth returns middleware that requires a valid session cookie or a
// valid API key.  On API key success the project ID is written to the request
// context under [internalauth.APIKeyProjectIDContextKey].
func RequireAuth(store authSessioner, secure bool, sessionTTL time.Duration) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return internalauth.RequireAuth(store, nil, secure, sessionTTL)(next)
	}
}

// RequireSessionOrAPIKey wraps a handler that accepts either a valid session
// cookie or an X-API-Key header. When an API key is present and valid, the
// associated project ID is stored in the request context under apiKeyCtxKey
// (callers supply the same key that handler.extractProjectID reads from).
// If neither credential is valid, returns 401.
func RequireSessionOrAPIKey(
	store authSessioner,
	validator internalauth.Validator,
	secure bool,
	sessionTTL time.Duration,
	apiKeyCtxKey any,
) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return internalauth.RequireSessionOrAPIKey(store, validator, secure, sessionTTL, apiKeyCtxKey)(next)
	}
}

// writeJSON writes a JSON response with the given status code.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// SetSessionCookie creates a Set-Cookie header for the session cookie.
// Re-exports [internalauth.SetSessionCookie] for backwards compatibility.
func SetSessionCookie(w http.ResponseWriter, sessionID string, secure bool, sessionTTL time.Duration) {
	internalauth.SetSessionCookie(w, sessionID, secure, sessionTTL)
}

// ClearSessionCookie clears the session cookie.
// Re-exports [internalauth.ClearSessionCookie] for backwards compatibility.
func ClearSessionCookie(w http.ResponseWriter, secure bool) {
	internalauth.ClearSessionCookie(w, secure)
}

// CurrentUserFromContext returns the current user from the request context,
// or nil if no user is authenticated.
// Re-exports [internalauth.CurrentUserFromContext] for backwards compatibility.
func CurrentUserFromContext(r *http.Request) *internalauth.CurrentUser {
	return internalauth.CurrentUserFromContext(r)
}

// IsAdmin checks if the current user's email matches the admin email
// (case-insensitive). Re-exports [internalauth.IsAdmin] for backwards compatibility.
func IsAdmin(r *http.Request, adminEmail string) bool {
	return internalauth.IsAdmin(r, adminEmail)
}

// AdminContextKey is the context key for the admin email.
// Deprecated: use [internalauth.AdminEmailContextKey] directly.
var AdminContextKey ContextKey = "admin_email"

// AdminEmailFromContext returns the admin email from the request context.
func AdminEmailFromContext(r *http.Request) string {
	email, _ := r.Context().Value(AdminContextKey).(string)
	return email
}

// IsAdminUser checks if the current user is an admin (email matches admin email
// stored in context). Returns false if no admin email is configured or no user
// is authenticated.
// Re-exports [internalauth.IsAdminUser] for backwards compatibility.
func IsAdminUser(r *http.Request) bool {
	return internalauth.IsAdminUser(r)
}

// RequireAdmin returns middleware that requires a valid session cookie AND a
// user whose email matches the admin email (case-insensitive).
func RequireAdmin(store authSessioner, secure bool, sessionTTL time.Duration, adminEmail string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return internalauth.RequireAdmin(store, secure, sessionTTL, adminEmail)(next)
	}
}
