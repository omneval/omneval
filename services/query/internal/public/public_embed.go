//go:build embedui

package public

import (
	"embed"
	"log/slog"
	"mime"
	"net/http"
	"path/filepath"
)

//go:embed ui/dist
var uiFS embed.FS

func init() {
	serveHandler = func(w http.ResponseWriter, r *http.Request) {
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
}