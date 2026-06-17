package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/omneval/omneval/internal/domain"
)

// authSessioner is the minimal interface auth middleware needs from a store.
type authSessioner interface {
	// GetUserByID returns the user for the given ID.
	GetUserByID(ctx context.Context, userID string) (*domain.User, error)
	// GetSession returns the session for the given ID.
	GetSession(ctx context.Context, sessionID string) (*domain.Session, error)
	// DeleteSession removes the session.
	DeleteSession(ctx context.Context, sessionID string) error
}

// currentUserIDKey is the context key for the current user.
var currentUserIDKey any = "omneval_internal_current_user"

// CurrentUser holds authenticated user info stored in the request context.
type CurrentUser struct {
	UserID string
	Email  string
}

// sessionCookieName is the name of the session cookie.
const sessionCookieName = "omneval_session"

// currentSessionTTL is a default session TTL for testing.
var currentSessionTTL = 24 * time.Hour

// RequireAuth returns middleware that requires a valid session cookie or a
// valid API key in the X-API-Key header.  On API key success the project ID
// is written to the request context under [APIKeyProjectIDContextKey].
func RequireAuth(store authSessioner, validator Validator, secure bool, sessionTTL time.Duration) func(http.Handler) http.Handler {
	sessionMw := newSessionMiddleware(store, secure, sessionTTL)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// 1. Try API key.
			if rawKey := r.Header.Get("X-API-Key"); rawKey != "" && validator != nil {
				vk, err := validator.Validate(r.Context(), rawKey)
				if err == nil && vk.ProjectID != "" {
					ctx := context.WithValue(r.Context(), APIKeyProjectIDContextKey, vk.ProjectID)
					next.ServeHTTP(w, r.WithContext(ctx))
					return
				}
				writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid API key"})
				return
			}
			// 2. Try session.
			sessionMw.Handler(next).ServeHTTP(w, r)
		})
	}
}

// RequireSessionOrAPIKey returns middleware that accepts either a valid session
// cookie or an X-API-Key header.  When an API key is present and valid, the
// associated project ID is stored in the request context under the provided
// apiKeyCtxKey.  If neither credential is valid, returns 401.
func RequireSessionOrAPIKey(
	store authSessioner,
	validator Validator,
	secure bool,
	sessionTTL time.Duration,
	apiKeyCtxKey any,
) func(http.Handler) http.Handler {
	sessionMw := newSessionMiddleware(store, secure, sessionTTL)
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
				// Invalid API key — 401 immediately.
				writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid API key"})
				return
			}
			// No API key header — require session cookie.
			sessionMw.Handler(next).ServeHTTP(w, r)
		})
	}
}

// RequireAdmin returns middleware that requires a valid session cookie AND a
// user whose email matches the admin email (case-insensitive).
func RequireAdmin(store authSessioner, secure bool, sessionTTL time.Duration, adminEmail string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			cookie, err := r.Cookie(sessionCookieName)
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

			ctx := context.WithValue(r.Context(), currentUserIDKey, &CurrentUser{
				UserID: user.UserID,
				Email:  user.Email,
			})
			ctx = context.WithValue(ctx, AdminEmailContextKey, adminEmail)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// CurrentUserFromContext returns the current user from the request context,
// or nil if no user is authenticated.
func CurrentUserFromContext(r *http.Request) *CurrentUser {
	user, _ := r.Context().Value(currentUserIDKey).(*CurrentUser)
	return user
}

// IsAdmin checks if the current user's email matches the admin email
// (case-insensitive).
func IsAdmin(r *http.Request, adminEmail string) bool {
	user := CurrentUserFromContext(r)
	if user == nil || adminEmail == "" {
		return false
	}
	return user.Email != "" && strings.EqualFold(user.Email, adminEmail)
}

// IsAdminUser checks if the current user is an admin (email matches admin email
// stored in context).  Returns false if no admin email is configured or no user
// is authenticated.
func IsAdminUser(r *http.Request) bool {
	adminEmail := r.Context().Value(AdminEmailContextKey)
	if adminEmail == nil {
		return false
	}
	email, ok := adminEmail.(string)
	if !ok {
		return false
	}
	return IsAdmin(r, email)
}

// --- Session middleware internals ---

type sessionMiddleware struct {
	store      authSessioner
	secure     bool
	sessionTTL time.Duration
}

func newSessionMiddleware(store authSessioner, secure bool, sessionTTL time.Duration) *sessionMiddleware {
	return &sessionMiddleware{
		store:      store,
		secure:     secure,
		sessionTTL: sessionTTL,
	}
}

func (m *sessionMiddleware) Handler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(sessionCookieName)
		if err != nil {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}

		session, err := m.store.GetSession(r.Context(), cookie.Value)
		if err != nil {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}

		if time.Now().After(session.ExpiresAt) {
			_ = m.store.DeleteSession(r.Context(), session.SessionID)
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}

		user, err := m.store.GetUserByID(r.Context(), session.UserID)
		if err != nil {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}

		ctx := context.WithValue(r.Context(), currentUserIDKey, &CurrentUser{
			UserID: user.UserID,
			Email:  user.Email,
		})
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}