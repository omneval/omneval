package handlers

import (
	"net/http"
	"strings"
)

// ExtractAPIKey extracts the raw API key from an HTTP request.
// It checks X-API-Key first (precedence), then falls back to
// Authorization: Bearer <key>. Returns empty string for missing
// or malformed headers.
func ExtractAPIKey(r *http.Request) string {
	if key := r.Header.Get("X-API-Key"); key != "" {
		return key
	}

	auth := r.Header.Get("Authorization")
	if auth == "" {
		return ""
	}

	if !strings.HasPrefix(strings.ToLower(auth), "bearer ") {
		return ""
	}

	key := strings.TrimSpace(auth[7:])
	if key == "" {
		return ""
	}
	return key
}
