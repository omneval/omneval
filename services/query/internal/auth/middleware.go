package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	internalauth "github.com/omneval/omneval/internal/auth"
	"github.com/omneval/omneval/internal/domain"
)

// ContextKey is the type used for context keys to avoid collisions.
type ContextKey string

const (
	// CurrentUserKey is the context key for the current authenticated user.
	CurrentUserKey ContextKey = "current_user"
)

// CurrentUser holds the authenticated user info, stored in the request context
// by the session middleware.
type CurrentUser struct {
	UserID string
	Email  string
}

// authSessioner is the minimal interface the auth middleware needs from metadata.
// It is satisfied by the metadata.Store interface (which embeds SessionStore and AuthStore).
type authSessioner interface {
	GetSession(ctx context.Context, sessionID string) (*domain.Session, error)
	DeleteSession(ctx context.Context, sessionID string) error
	GetUserByID(ctx context.Context, userID string) (*domain.User, error)
}

// SessionMiddleware wraps an http.Handler with session validation from a cookie.
type SessionMiddleware struct {
	store      authSessioner
	secure     bool
	sessionTTL time.Duration
}

// NewSessionMiddleware creates a new session middleware.
func NewSessionMiddleware(store authSessioner, secure bool, sessionTTL time.Duration) *SessionMiddleware {
	return &SessionMiddleware{
		store:      store,
		secure:     secure,
		sessionTTL: sessionTTL,
	}
}

// Handler returns an http.Handler that wraps the next handler with session
// validation. Returns 401 JSON if the session is missing or expired.
func (m *SessionMiddleware) Handler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie("omneval_session")
		if err != nil {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}

		session, err := m.store.GetSession(r.Context(), cookie.Value)
		if err != nil {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}

		// Check if session is expired
		if time.Now().After(session.ExpiresAt) {
			_ = m.store.DeleteSession(r.Context(), session.SessionID)
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}

		// Fetch user
		user, err := m.store.GetUserByID(r.Context(), session.UserID)
		if err != nil {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}

		ctx := context.WithValue(r.Context(), CurrentUserKey, &CurrentUser{
			UserID: user.UserID,
			Email:  user.Email,
		})
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// RequireAuth is a convenience function that returns middleware wrapping a
// handler with session validation.
func RequireAuth(store authSessioner, secure bool, sessionTTL time.Duration) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return NewSessionMiddleware(store, secure, sessionTTL).Handler(next)
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
	sessionMw := NewSessionMiddleware(store, secure, sessionTTL)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Try API key first.
			if rawKey := r.Header.Get("X-API-Key"); rawKey != "" && validator != nil {
				vk, err := validator.Validate(r.Context(), rawKey)
				if err == nil && vk.ProjectID != "" {
					ctx := context.WithValue(r.Context(), apiKeyCtxKey, vk.ProjectID)
					next.ServeHTTP(w, r.WithContext(ctx))
					return
				}
				// Invalid API key — 401 immediately (don't fall back to session).
				writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid API key"})
				return
			}
			// No API key header — require session cookie.
			sessionMw.Handler(next).ServeHTTP(w, r)
		})
	}
}

// writeJSON writes a JSON response with the given status code.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// SetSessionCookie creates a Set-Cookie header for the session cookie.
func SetSessionCookie(w http.ResponseWriter, sessionID string, secure bool, sessionTTL time.Duration) {
	maxAge := int(sessionTTL.Seconds())
	http.SetCookie(w, &http.Cookie{
		Name:     "omneval_session",
		Value:    sessionID,
		Path:     "/",
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   maxAge,
	})
}

// ClearSessionCookie clears the session cookie.
func ClearSessionCookie(w http.ResponseWriter, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     "omneval_session",
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
}

// CurrentUserFromContext returns the current user from the request context,
// or nil if no user is authenticated.
func CurrentUserFromContext(r *http.Request) *CurrentUser {
	user, _ := r.Context().Value(CurrentUserKey).(*CurrentUser)
	return user
}

// IsAdmin checks if the current user's email matches the admin email (case-insensitive).
func IsAdmin(r *http.Request, adminEmail string) bool {
	user := CurrentUserFromContext(r)
	if user == nil || adminEmail == "" {
		return false
	}
	return user.Email != "" && strings.EqualFold(user.Email, adminEmail)
}

// AdminContextKey is the context key for the admin email.
var AdminContextKey ContextKey = "admin_email"

// AdminEmailFromContext returns the admin email from the request context.
func AdminEmailFromContext(r *http.Request) string {
	email, _ := r.Context().Value(AdminContextKey).(string)
	return email
}

// IsAdminUser checks if the current user is an admin (email matches admin email
// stored in context). Returns false if no admin email is configured or no user
// is authenticated.
func IsAdminUser(r *http.Request) bool {
	adminEmail := AdminEmailFromContext(r)
	return IsAdmin(r, adminEmail)
}

// RequireAdmin wraps an http.Handler with session validation and admin check.
func RequireAdmin(store authSessioner, secure bool, sessionTTL time.Duration, adminEmail string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// First validate session
			cookie, err := r.Cookie("omneval_session")
			if err != nil {
				writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
				return
			}

			session, err := store.GetSession(r.Context(), cookie.Value)
			if err != nil {
				writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
				return
			}

			if time.Now().After(session.ExpiresAt) {
				_ = store.DeleteSession(r.Context(), session.SessionID)
				writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
				return
			}

			user, err := store.GetUserByID(r.Context(), session.UserID)
			if err != nil {
				writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
				return
			}

			if !strings.EqualFold(user.Email, adminEmail) {
				writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden: admin access required"})
				return
			}

			ctx := context.WithValue(r.Context(), CurrentUserKey, &CurrentUser{
				UserID: user.UserID,
				Email:  user.Email,
			})
			ctx = context.WithValue(ctx, AdminContextKey, adminEmail)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
