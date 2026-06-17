package server

import (
	"context"
	"embed"
	"fmt"
	"log/slog"
	"mime"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	_ "github.com/duckdb/duckdb-go/v2"
	"github.com/omneval/omneval/internal/config"
	_ "github.com/omneval/omneval/internal/duckdbfix"
	"github.com/omneval/omneval/internal/metadata"
	"github.com/omneval/omneval/services/query/internal/handler"
)

//go:embed ui/dist
var uiFS embed.FS

// serveUI serves static files from the embedded UI dist directory.
// It handles MIME type detection and falls back to index.html for SPA routing.
func serveUI(w http.ResponseWriter, r *http.Request) {
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

// publicAPIPaths is the set of API and service paths that bypass session
// authentication. The router treats anything in this set as public.
var publicAPIPaths = map[string]struct{}{
	"/login":         {},
	"/logout":        {},
	"/healthz":       {},
	"/readyz":        {},
	"/metrics":       {},
	"/api/v1/scores": {},
}

// IsPublicAPIPath reports whether the given path is a public API route
// that does not require authentication.
func IsPublicAPIPath(path string) bool {
	// Direct match for known public endpoints.
	if _, ok := publicAPIPaths[path]; ok {
		return true
	}
	// Prefix match for health check variants like /healthz/readyz.
	if strings.HasPrefix(path, "/healthz") || strings.HasPrefix(path, "/readyz") {
		return true
	}
	return false
}

// Run starts the Query API: opens the DuckDB snapshot from S3 and the
// metadata store, bootstraps the admin user, and serves auth, span,
// analytics, prompt, and metrics endpoints.
func Run() error {
	// Load config.
	cfgPath := ""
	if p := os.Getenv("OMNEVAL_CONFIG"); p != "" {
		cfgPath = p
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return fmt.Errorf("query: load config: %w", err)
	}

	deps, err := WireDeps(cfg)
	if err != nil {
		return err
	}
	return RunWired(deps)
}

// RunWired runs the Query API with pre-constructed dependencies: it wires
// routes, starts the snapshot poller, handles signals, and shuts down
// gracefully.
func RunWired(deps *WiredDeps) error {
	cfg := deps.Cfg
	defer deps.Store.Close()
	defer deps.Lake.Close()

	router := buildRouter(deps)

	// Combine the main router with probe routes.
	combined := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" || r.URL.Path == "/readyz" {
			deps.Prober.Router().ServeHTTP(w, r)
		} else {
			router.ServeHTTP(w, r)
		}
	})

	// Graceful shutdown on SIGINT/SIGTERM.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Start the dedicated Prometheus metrics server on cfg.Metrics.Addr (:9090).
	if err := StartMetricsServer(ctx, cfg.Metrics.Addr); err != nil {
		return fmt.Errorf("query: start metrics server: %w", err)
	}

	// Parse query listen address.
	addr := cfg.Query.Addr
	if addr == "" {
		addr = ":8002"
	}
	srv := &http.Server{Addr: addr, Handler: combined}
	go func() {
		slog.Info("query: listening", "addr", addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("query: server error", "error", err)
		}
	}()

	<-ctx.Done()
	slog.Info("query: shutting down...")

	// Graceful shutdown with 30-second drain timeout.
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("query: shutdown: %w", err)
	}

	slog.Info("query: stopped")
	return nil
}

// buildRouter wires every route against the handlers in deps and wraps them
// in the session/API-key auth middleware, using the unified Router module.
func buildRouter(deps *WiredDeps) http.Handler {
	// Wire the UI server function into the router so it can serve static files.
	handler.InitServeUI(serveUI)

	// Build the Router from wired deps and register all routes.
	router := handler.NewRouter(&handler.RouterDeps{
		Cfg:             deps.Cfg,
		Store:           deps.Store,
		Auth:            deps.Auth,
		Span:            deps.Span,
		Bookmark:        deps.Bookmark,
		Conversation:    deps.Conversation,
		Prompt:          deps.Prompt,
		EvalRule:        deps.EvalRule,
		Admin:           deps.Admin,
		Dataset:         deps.Dataset,
		DatasetRun:      deps.DatasetRun,
		Playground:      deps.Playground,
		AdminLake:       deps.AdminLake,
		SessionTTL:      deps.SessionTTL,
		APIKeyValidator: deps.APIKeyValidator,
	})

	return router.RegisterRoutes(http.NewServeMux())
}

// openMetadataStore opens the configured metadata store via the shared
// factory. The factory applies the default SQLite path when no DSN is set.
func openMetadataStore(cfg *config.Config) (metadata.Store, error) {
	return metadata.Open(cfg.Database.Driver, cfg.Database.DSN)
}
