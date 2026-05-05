package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/zbloss/lantern/internal/metadata"
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

// SessionMiddleware wraps an http.Handler with session validation from a cookie.
type SessionMiddleware struct {
	store      metadata.Store
	secure     bool
	sessionTTL time.Duration
}

// NewSessionMiddleware creates a new session middleware.
func NewSessionMiddleware(store metadata.Store, secure bool, sessionTTL time.Duration) *SessionMiddleware {
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
		cookie, err := r.Cookie("lantern_session")
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
func RequireAuth(store metadata.Store, secure bool, sessionTTL time.Duration) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return NewSessionMiddleware(store, secure, sessionTTL).Handler(next)
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
		Name:     "lantern_session",
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
		Name:     "lantern_session",
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


