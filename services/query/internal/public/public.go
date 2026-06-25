// Package public provides canonical public-path detection and UI serving
// for the query API. Public paths bypass authentication entirely.
package public

import (
	"net/http"
	"strings"
)

// publicPaths is the single canonical source of truth for public (unauthenticated)
// API paths. It is referenced by both the router middleware dispatcher and the
// server package's health-check probe logic.
var publicPaths = map[string]struct{}{
	"/login":         {},
	"/logout":        {},
	"/healthz":       {},
	"/readyz":        {},
	"/metrics":       {},
	"/api/v1/scores": {},
}

// IsPublicPath reports whether the given path is a public API route
// that does not require authentication.
func IsPublicPath(path string) bool {
	if _, ok := publicPaths[path]; ok {
		return true
	}
	if strings.HasPrefix(path, "/healthz") || strings.HasPrefix(path, "/readyz") {
		return true
	}
	return false
}

// Deprecated: use IsPublicPath instead.
var IsPublic = IsPublicPath

// serveHandler serves static files from the embedded UI dist directory.
// It handles MIME type detection and falls back to index.html for SPA routing.
// The default implementation returns 404 — real serving is injected via the
// //go:build embedui tag (see public_embed.go).
var serveHandler = func(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "not found", http.StatusNotFound)
}

// ServeHandler returns the HTTP handler that serves the embedded UI.
// Callers use this to replace the default no-op UI handler in the router.
func ServeHandler() http.Handler {
	return http.HandlerFunc(serveHandler)
}