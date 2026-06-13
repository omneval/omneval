package server

import (
	"context"
	"database/sql"
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
	"github.com/minio/minio-go/v7"
	"github.com/omneval/omneval/internal/config"
	"github.com/omneval/omneval/internal/metadata"
	"github.com/omneval/omneval/internal/storage"
	s3 "github.com/omneval/omneval/internal/storage/s3"
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
	defer deps.SDB.Close()

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

	// Start S3 snapshot poller (separate goroutine). Lake mode has no
	// snapshot to poll — reads see committed Lake data directly (ADR-0004).
	if deps.S3 != nil && !cfg.Query.Lake.Enabled {
		go pollSnapshotLoop(ctx, deps)
	}

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
	mux.HandleFunc("POST /api/v1/scores", handler.NewScoreHandler(deps.SDB).ServeHTTP)

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

// pollSnapshotLoop periodically polls S3 for an updated snapshot, downloads
// it, and swaps the live DuckDB connection. On shutdown it triggers one
// final sync.
func pollSnapshotLoop(ctx context.Context, deps *WiredDeps) {
	ticker := time.NewTicker(deps.SyncInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			downloaded, err := pollAndDownload(ctx, deps.S3, deps.DBPath, &deps.SnapshotLastModified)
			if err != nil {
				slog.Warn("query: snapshot poll/download failed", "err", err)
			} else if downloaded {
				if err := deps.SDB.Swap(deps.DBPath); err != nil {
					slog.Warn("query: snapshot swap failed", "err", err)
				} else {
					slog.Info("query: snapshot swapped — new data now visible")
				}
			}
			// Update snapshot age metric.
			if !deps.SnapshotLastModified.IsZero() {
				age := time.Since(deps.SnapshotLastModified).Seconds()
				deps.QueryMetrics.RecordSnapshotAge(age)
			}
		case <-ctx.Done():
			// Trigger one final sync before exit.
			if downloaded, err := pollAndDownload(ctx, deps.S3, deps.DBPath, &deps.SnapshotLastModified); err != nil {
				slog.Warn("query: final sync failed", "err", err)
			} else if downloaded {
				if err := deps.SDB.Swap(deps.DBPath); err != nil {
					slog.Warn("query: snapshot swap on shutdown failed", "err", err)
				}
			}
			return
		}
	}
}

// isS3NotFound checks whether the error indicates that the S3 object
// does not exist (NoSuchKey or NoSuchBucket). Used to distinguish
// "not ready yet" from genuine failures.
func isS3NotFound(err error) bool {
	if err == nil {
		return false
	}
	resp := minio.ToErrorResponse(err)
	return resp.Code == minio.NoSuchKey || resp.Code == minio.NoSuchBucket
}

// downloadSnapshot downloads the DuckDB snapshot from S3 to the local path.
// If the snapshot does not exist yet (NoSuchKey), it creates an empty
// DuckDB file with the required schema instead of failing.
func downloadSnapshot(ctx context.Context, store storage.ObjectStore, dbPath string) error {
	// Ensure parent directory exists.
	dir := filepath.Dir(dbPath)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("snapshot: create dir %s: %w", dir, err)
		}
	}

	// Get the latest snapshot from S3.
	snapshotKey := s3.SnapshotKey()
	reader, err := store.Get(ctx, snapshotKey)
	if err != nil {
		// If the snapshot doesn't exist yet, create an empty DB.
		if isS3NotFound(err) {
			slog.Warn("query: no snapshot found in S3, starting with empty database")
			return createEmptyDB(dbPath)
		}
		return fmt.Errorf("snapshot: get %s: %w", snapshotKey, err)
	}
	defer reader.Close()

	// Write to local file.
	tmpPath := dbPath + ".tmp"
	f, err := os.Create(tmpPath)
	if err != nil {
		return fmt.Errorf("snapshot: create temp file %s: %w", tmpPath, err)
	}

	if _, err := f.ReadFrom(reader); err != nil {
		f.Close()
		os.Remove(tmpPath)
		// minio.GetObject is lazy: the actual GET fires on first Read, so
		// NoSuchKey surfaces here rather than at store.Get().
		if isS3NotFound(err) {
			slog.Warn("query: no snapshot found in S3, starting with empty database")
			return createEmptyDB(dbPath)
		}
		return fmt.Errorf("snapshot: write temp file: %w", err)
	}
	if err := f.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("snapshot: close temp file: %w", err)
	}

	// Atomically replace the snapshot.
	if err := os.Rename(tmpPath, dbPath); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("snapshot: rename temp to %s: %w", dbPath, err)
	}

	return nil
}

// pollAndDownload checks if the S3 snapshot has changed and re-downloads if needed.
// It compares the S3 object's LastModified against the previously stored value
// to avoid the stale-mod-time problem where the local file's filesystem timestamp
// reflects the download time rather than the S3 object's LastModified.
// If the snapshot does not exist yet (NoSuchKey), it logs a warning and returns nil.
// It returns (true, nil) when a new snapshot was downloaded, (false, nil) when
// unchanged or unavailable, and (false, err) on unexpected errors.
func pollAndDownload(ctx context.Context, store storage.ObjectStore, dbPath string, lastModified *time.Time) (bool, error) {
	snapshotKey := s3.SnapshotKey()

	// Stat the S3 object to check for changes.
	info, err := store.Stat(ctx, snapshotKey)
	if err != nil {
		// Snapshot doesn't exist yet — log a warning but don't fail.
		// The writer will produce one within ~30 seconds.
		if isS3NotFound(err) {
			slog.Warn("query: snapshot not yet available in S3, waiting for writer", "key", snapshotKey)
			// Ensure we have at least an empty DB.
			if _, statErr := os.Stat(dbPath); os.IsNotExist(statErr) {
				if createErr := createEmptyDB(dbPath); createErr != nil {
					slog.Warn("query: failed to create empty DB as fallback", "err", createErr)
				}
			}
			return false, nil
		}
		return false, fmt.Errorf("snapshot: stat %s: %w", snapshotKey, err)
	}

	// Skip download if the S3 object hasn't changed since our last known version.
	if lastModified != nil && !lastModified.IsZero() {
		if info.LastModified.Equal(*lastModified) {
			slog.Debug("query: snapshot unchanged, skipping download",
				"last_modified", info.LastModified)
			return false, nil
		}
	}

	// Download new snapshot.
	slog.Info("query: downloading updated snapshot from S3",
		"etag", info.ETag,
		"last_modified", info.LastModified)
	if err := downloadSnapshot(ctx, store, dbPath); err != nil {
		return false, err
	}

	// Update the last modified time.
	*lastModified = info.LastModified

	return true, nil
}

// createEmptyDB creates a new DuckDB file with the Omneval schema.
// Used when the S3 snapshot does not exist yet (first boot).
func createEmptyDB(path string) error {
	db, err := sql.Open("duckdb", path+"?access_mode=read_write")
	if err != nil {
		return fmt.Errorf("duckdb: create empty db: %w", err)
	}
	defer db.Close()

	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS spans (
			span_id          VARCHAR      NOT NULL,
			trace_id         VARCHAR      NOT NULL,
			parent_id        VARCHAR,
			conversation_id  VARCHAR,
			project_id       VARCHAR      NOT NULL,
			service_name     VARCHAR,
			name             VARCHAR,
			kind             VARCHAR,
			start_time       TIMESTAMPTZ  NOT NULL,
			end_time         TIMESTAMPTZ,
			model            VARCHAR,
			input            JSON,
			output           JSON,
			input_tokens     BIGINT,
			output_tokens    BIGINT,
			cost_usd         DOUBLE,
			prompt_name      VARCHAR,
			prompt_version   BIGINT,
			status_code      VARCHAR,
			status_message   VARCHAR,
			attributes       JSON,
			PRIMARY KEY (trace_id, span_id)
		);
		CREATE INDEX IF NOT EXISTS idx_spans_project_time
			ON spans (project_id, start_time);
		CREATE INDEX IF NOT EXISTS idx_spans_conversation
			ON spans (project_id, conversation_id);

		CREATE TABLE IF NOT EXISTS bookmarks (
			trace_id       VARCHAR      NOT NULL,
			project_id     VARCHAR      NOT NULL,
			created_at     TIMESTAMPTZ  NOT NULL,
			PRIMARY KEY (trace_id, project_id)
		);

		CREATE TABLE IF NOT EXISTS scores (
			score_id       VARCHAR      NOT NULL PRIMARY KEY,
			span_id        VARCHAR      NOT NULL,
			trace_id       VARCHAR      NOT NULL,
			project_id     VARCHAR      NOT NULL,
			eval_name      VARCHAR,
			value          DOUBLE,
			reasoning      VARCHAR,
			judge_model    VARCHAR,
			prompt_name    VARCHAR,
			prompt_version BIGINT,
			created_at     TIMESTAMPTZ  NOT NULL
		);
	`); err != nil {
		return fmt.Errorf("duckdb: create schema: %w", err)
	}

	return nil
}

// openSnapshotDBRW opens a DuckDB database in read-write mode.
// Used by the Query API for score writes.
func openSnapshotDBRW(path string) (*sql.DB, error) {
	return openDuckDB(path + "?access_mode=read_write")
}

// openDuckDB opens a DuckDB database with the given DSN and verifies connectivity.
func openDuckDB(dsn string) (*sql.DB, error) {
	db, err := sql.Open("duckdb", dsn)
	if err != nil {
		return nil, fmt.Errorf("duckdb: open %s: %w", dsn, err)
	}

	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("duckdb: ping: %w", err)
	}

	return db, nil
}

// openMetadataStore opens the configured metadata store via the shared
// factory. The factory applies the default SQLite path when no DSN is set.
func openMetadataStore(cfg *config.Config) (metadata.Store, error) {
	return metadata.Open(cfg.Database.Driver, cfg.Database.DSN)
}
