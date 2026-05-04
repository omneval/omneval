package cors

import (
	"net/http"
	"strings"
)

// Middleware wraps an http.Handler with CORS headers based on allowed origins.
type Middleware struct {
	allowedOrigins []string
	allowedMethods []string
	allowedHeaders []string
}

// New creates a new CORS middleware with the given allowed origins.
// allowedMethods defaults to ["POST", "OPTIONS"].
// allowedHeaders defaults to ["Content-Type", "Authorization"].
func New(allowedOrigins []string) *Middleware {
	return &Middleware{
		allowedOrigins: allowedOrigins,
		allowedMethods: []string{"POST", "OPTIONS"},
		allowedHeaders: []string{"Content-Type", "Authorization"},
	}
}

// Handler wraps the given handler with CORS headers.
func (m *Middleware) Handler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		allowOrigin := m.ResolveOrigin(origin)

		if allowOrigin != "" {
			w.Header().Set("Access-Control-Allow-Origin", allowOrigin)
			w.Header().Set("Access-Control-Allow-Credentials", "true")
		}

		// Preflight handling
		if r.Method == http.MethodOptions {
			// Set preflight headers
			w.Header().Set("Access-Control-Allow-Methods", strings.Join(m.allowedMethods, ", "))
			w.Header().Set("Access-Control-Allow-Headers", strings.Join(m.allowedHeaders, ", "))
			w.Header().Set("Access-Control-Max-Age", "86400") // 24 hours
			w.WriteHeader(http.StatusNoContent)
			return
		}

		// Regular request: pass through
		next.ServeHTTP(w, r)
	})
}

// ResolveOrigin determines which origin to allow for the response.
// Returns empty string if the origin is not allowed.
func (m *Middleware) ResolveOrigin(origin string) string {
	if origin == "" {
		return ""
	}

	// Wildcard: allow any origin
	for _, allowed := range m.allowedOrigins {
		if allowed == "*" {
			return "*"
		}
	}

	// Exact match
	for _, allowed := range m.allowedOrigins {
		if origin == allowed {
			return allowed
		}
	}

	return ""
}
