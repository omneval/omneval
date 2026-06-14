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
	"github.com/omneval/omneval/services/query/internal/auth"
	"github.com/omneval/omneval/services/query/internal/handler"
	"github.com/prometheus/client_golang/prometheus/promhttp"
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
// in the session/API-key auth middleware.
func buildRouter(deps *WiredDeps) http.Handler {
	cfg := deps.Cfg
	store := deps.Store
	h := deps.Auth

	mux := http.NewServeMux()

	// Register auth routes (login, logout, invite, change password).
	h.Register(mux)

	// Admin routes (require admin session).
	adminMw := auth.RequireAdmin(store, cfg.Auth.SecureCookie, deps.SessionTTL, cfg.Auth.AdminEmail)
	mux.HandleFunc("GET /api/v1/admin/api-keys", adminMw(http.HandlerFunc(deps.Admin.HandleAdminAPIKeysList)).ServeHTTP)
	mux.HandleFunc("DELETE /api/v1/admin/api-keys/", adminMw(http.HandlerFunc(deps.Admin.HandleAdminAPIKeyDelete)).ServeHTTP)
	mux.HandleFunc("GET /api/v1/admin/traces/", adminMw(http.HandlerFunc(deps.Admin.HandleAdminTracesCount)).ServeHTTP)
	mux.HandleFunc("DELETE /api/v1/admin/traces/", adminMw(http.HandlerFunc(deps.Admin.HandleAdminTracesDelete)).ServeHTTP)
	mux.HandleFunc("DELETE /api/v1/admin/projects/", adminMw(http.HandlerFunc(deps.Admin.HandleAdminProjectsDelete)).ServeHTTP)

	// Projects list for the UI project switcher.
	mux.HandleFunc("GET /api/v1/projects", deps.Span.HandleProjects)

	// Span list with keyset pagination.
	mux.HandleFunc("POST /api/v1/spans/query", deps.Span.HandleSpansQuery)

	// Analytics: parameterized SQL compilation from structured DSL queries.
	mux.HandleFunc("POST /api/v1/analytics/spans", deps.Span.HandleAnalyticsSpans)

	// Trace detail waterfall.
	mux.HandleFunc("GET /api/v1/traces/{traceId}", deps.Span.HandleTraceDetail)

	// Trace bookmark toggle.
	mux.HandleFunc("POST /api/v1/traces/{traceId}/bookmark", deps.Bookmark.HandleBookmark)

	// Conversation list and detail endpoints.
	mux.HandleFunc("GET /api/v1/conversations", deps.Conversation.HandleListConversations)
	mux.HandleFunc("GET /api/v1/conversations/{conversationId}", deps.Conversation.HandleConversationDetail)

	// Prompt Registry endpoints.
	mux.HandleFunc("GET /api/v1/prompts", deps.Prompt.HandleListPrompts)
	mux.HandleFunc("POST /api/v1/prompts", deps.Prompt.HandleCreatePrompt)
	mux.HandleFunc("GET /api/v1/prompts/{name}", deps.Prompt.HandleGetPrompt)
	mux.HandleFunc("GET /api/v1/prompts/{name}/versions", deps.Prompt.HandleListPromptVersions)
	mux.HandleFunc("PUT /api/v1/prompts/{name}/labels/{label}", deps.Prompt.HandleSetLabel)

	// Eval rules endpoints.
	mux.HandleFunc("POST /api/v1/eval-rules", deps.EvalRule.HandleCreate)
	mux.HandleFunc("GET /api/v1/eval-rules", deps.EvalRule.HandleList)
	mux.HandleFunc("POST /api/v1/eval-rules/preview", deps.EvalRule.HandlePreview)
	mux.HandleFunc("DELETE /api/v1/eval-rules/{id}", deps.EvalRule.HandleDelete)

	// Dataset endpoints.
	mux.HandleFunc("POST /api/v1/datasets", deps.Dataset.HandleCreate)
	mux.HandleFunc("GET /api/v1/datasets", deps.Dataset.HandleList)
	mux.HandleFunc("GET /api/v1/datasets/{id}", deps.Dataset.HandleGet)
	mux.HandleFunc("POST /api/v1/datasets/{id}/items", deps.Dataset.HandleAddItems)
	mux.HandleFunc("POST /api/v1/datasets/{id}/items/batch", deps.Dataset.HandleAddItemsBatch)
	mux.HandleFunc("GET /api/v1/datasets/{id}/items", deps.Dataset.HandleListItems)
	mux.HandleFunc("DELETE /api/v1/datasets/{id}", deps.Dataset.HandleDelete)

	// Dataset run endpoints — read endpoints (list, get, status) are always
	// available. POST (create run) requires judge LLM config.
	if deps.DatasetRun.JudgeClient != nil {
		mux.HandleFunc("POST /api/v1/datasets/{id}/runs", deps.DatasetRun.HandleRun)
	}
	mux.HandleFunc("GET /api/v1/datasets/{id}/runs", deps.DatasetRun.HandleListRuns)
	mux.HandleFunc("GET /api/v1/datasets/{id}/runs/{runId}", deps.DatasetRun.HandleGetRun)
	mux.HandleFunc("GET /api/v1/datasets/{id}/runs/{runId}/status", deps.DatasetRun.HandleGetRunStatus)

	// Playground endpoint (route always registered; the handler returns 503
	// when the LLM is not configured).
	mux.HandleFunc("POST /api/v1/playground/run", deps.Playground.HandleRun)

	// Score write endpoint (for eval worker score write-back, no auth required).
	// Scores are committed directly to the Lake via the AdminLake attachment
	// (ADR-0004/#91); SpanDB resolves span_start_time for partitioning.
	var spanDB handler.DBHandle = deps.AdminLake.DB()
	mux.HandleFunc("POST /api/v1/scores", handler.NewScoreHandler(deps.AdminLake, spanDB).ServeHTTP)

	// Prometheus metrics.
	mux.HandleFunc("GET /metrics", promhttp.Handler().ServeHTTP)

	// Serve embedded UI for all other routes (SPA fallback to index.html).
	mux.HandleFunc("/", serveUI)

	sessionMw := auth.RequireAuth(store, cfg.Auth.SecureCookie, deps.SessionTTL)
	promptGetMw := auth.RequireSessionOrAPIKey(store, deps.APIKeyValidator, cfg.Auth.SecureCookie, deps.SessionTTL, handler.APIKeyProjectIDKey)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path

		// Public routes bypass authentication entirely.
		if IsPublicAPIPath(path) {
			mux.ServeHTTP(w, r)
			return
		}

		// Prompt and eval-rule endpoints accept X-API-Key (for SDKs) or session cookie.
		if strings.HasPrefix(path, "/api/v1/prompts") || strings.HasPrefix(path, "/api/v1/eval-rules") {
			promptGetMw(mux).ServeHTTP(w, r)
			return
		}

		// All other protected API routes require a valid session cookie.
		if strings.HasPrefix(path, "/api/v1/") {
			sessionMw(mux).ServeHTTP(w, r)
			return
		}

		// SPA fallback and anything else.
		mux.ServeHTTP(w, r)
	})
}

// openMetadataStore opens the configured metadata store via the shared
// factory. The factory applies the default SQLite path when no DSN is set.
func openMetadataStore(cfg *config.Config) (metadata.Store, error) {
	return metadata.Open(cfg.Database.Driver, cfg.Database.DSN)
}
