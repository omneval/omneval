// Package public provides canonical public-path detection and UI serving
// for the query API. Public paths bypass authentication entirely.
package public

import (
	"embed"
	"log/slog"
	"mime"
	"net/http"
	"path/filepath"
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

//go:embed ui/dist
var uiFS embed.FS

// serveHandler serves static files from the embedded UI dist directory.
// It handles MIME type detection and falls back to index.html for SPA routing.
var serveHandler = func(w http.ResponseWriter, r *http.Request) {
	// Clean the path to prevent directory traversal.
	path := filepath.Clean(r.URL.Path)
	if path == "/" {
		path = "/index.html"
	}

	// Try to serve the exact file.
	data, err := uiFS.ReadFile("ui/dist" + path)
	if err == nil {
		// Determine content type from file extension.
		ct := mime.TypeByExtension(filepath.Ext(path))
		if ct == "" {
			// Fallback: sniff from content.
			ct = http.DetectContentType(data)
		}
		w.Header().Set("Content-Type", ct)
		if _, err := w.Write(data); err != nil {
			slog.Warn("query: write ui file", "path", path, "err", err)
		}
		return
	}

	// Not found — serve index.html for SPA routing.
	data, err = uiFS.ReadFile("ui/dist/index.html")
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if _, err := w.Write(data); err != nil {
		slog.Warn("query: write index.html", "err", err)
	}
}

// ServeHandler returns the HTTP handler that serves the embedded UI.
// Callers use this to replace the default no-op UI handler in the router.
func ServeHandler() http.Handler {
	return http.HandlerFunc(serveHandler)
}