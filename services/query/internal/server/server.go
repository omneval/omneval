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

	_ "github.com/marcboeker/go-duckdb/v2"
	"github.com/minio/minio-go/v7"
	"github.com/omneval/omneval/internal/config"
	"github.com/omneval/omneval/internal/metadata"
	metadatapg "github.com/omneval/omneval/internal/metadata/postgres"
	"github.com/omneval/omneval/internal/metadata/sqlite"
	"github.com/omneval/omneval/internal/probe"
	"github.com/omneval/omneval/internal/storage"
	s3 "github.com/omneval/omneval/internal/storage/s3"
	"github.com/omneval/omneval/services/query/internal/auth"
	"github.com/omneval/omneval/services/query/internal/handler"
	"github.com/omneval/omneval/services/query/internal/metrics"
	"github.com/omneval/omneval/services/query/internal/playground"
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

	// Register Prometheus metrics.
	if err := metrics.Register(cfg); err != nil {
		return fmt.Errorf("query: register metrics: %w", err)
	}

	queryMetrics := metrics.NewQueryMetrics(cfg)

	// Open metadata store
	store, err := openMetadataStore(cfg)
	if err != nil {
		return fmt.Errorf("query: open metadata store: %w", err)
	}
	defer store.Close()

	// Resolve DuckDB snapshot path.
	dbPath := cfg.Query.DuckDBPath
	if dbPath == "" {
		dbPath = "/tmp/omneval-snapshot.duckdb"
	}

	// Parse sync interval (default 30s).
	syncInterval, err := time.ParseDuration(cfg.Query.SyncInterval)
	if err != nil {
		syncInterval = 30 * time.Second
		slog.Warn("query: invalid sync_interval, using default 30s",
			"raw", cfg.Query.SyncInterval)
	}

	// Parse session TTL
	sessionTTL, err := time.ParseDuration(cfg.Auth.SessionTTL)
	if err != nil {
		sessionTTL = 168 * time.Hour // default 7 days
		slog.Warn("invalid session_ttl, using default 168h", "given", cfg.Auth.SessionTTL)
	}

	// Connect to S3 (may be nil if no storage config).
	var s3Store *s3.Store
	if cfg.Storage.Bucket != "" || cfg.Storage.Endpoint != "" {
		s3Store = s3.New(&cfg.Storage)
	}

	// Download snapshot from S3 (if configured).
	var snapshotLastModified time.Time
	if s3Store != nil {
		if err := downloadSnapshot(context.Background(), s3Store, dbPath); err != nil {
			return fmt.Errorf("query: download snapshot: %w", err)
		}
		// Try to get the last modified time from S3.
		if stat, err := s3Store.Stat(context.Background(), s3.SnapshotKey()); err == nil && stat != nil {
			slog.Info("query: snapshot downloaded from S3", "path", dbPath, "last_modified", stat.LastModified)
			snapshotLastModified = stat.LastModified
		} else {
			slog.Info("query: snapshot not yet available in S3")
		}
	} else {
		slog.Info("query: no S3 configured, skipping snapshot download")
	}

	// Open the snapshot database via SwappableDB so that pollAndDownload can
	// atomically reopen the connection each time S3 delivers a new snapshot.
	sdb, err := NewSwappableDB(dbPath)
	if err != nil {
		return fmt.Errorf("query: open snapshot: %w", err)
	}
	defer sdb.Close()

	// Bootstrap admin user if no users exist
	h := auth.NewHandler(store, cfg.Auth.SecureCookie, sessionTTL, cfg.Auth.AdminEmail, cfg.Auth.AdminPassword)
	created, err := h.BootstrapAdmin(context.Background())
	if err != nil {
		return fmt.Errorf("query: bootstrap admin: %w", err)
	}
	if created {
		slog.Info("query: admin user bootstrapped", "email", cfg.Auth.AdminEmail)
	} else {
		if count, _ := store.CountUsers(context.Background()); count == 0 {
			slog.Warn("query: no admin configured and no users exist — set OMNEVAL_AUTH_ADMIN_EMAIL and OMNEVAL_AUTH_ADMIN_PASSWORD to create the first admin user")
		}
	}

	// Create handlers.
	spanHandler := &handler.SpanHandler{
		DB:           sdb,
		SessionStore: h,
	}

	// Bookmark handler (toggle trace bookmarks).
	bookmarkHandler := &handler.BookmarkHandler{
		DB:           sdb,
		SessionStore: h,
	}

	// Prompt registry handler (requires metadata store).
	var promptHandler *handler.PromptHandler
	var promptCache *handler.PromptCache
	if store != nil {
		promptCache = handler.NewPromptCache(store)
		promptHandler = &handler.PromptHandler{
			Store:        store,
			Cache:        promptCache,
			SessionStore: h,
		}
	}

	// Eval rules handler (requires metadata store).
	var evalRuleHandler *handler.EvalRuleHandler
	if store != nil {
		evalRuleHandler = &handler.EvalRuleHandler{
			DB:                sdb,
			Store:             store,
			SessionStore:      h,
			DefaultJudgeModel: cfg.Eval.LLMModel,
		}
	}

	// Admin handler (requires DB, metadata store, and session store).
	adminHandler := &handler.AdminHandler{
		DB:           sdb,
		Store:        store,
		SessionStore: h,
	}

	// Playground handler (requires metadata store).
	// Always create the handler so the route is registered even when the LLM
	// is not configured — the handler itself returns 503 in that case.
	var playgroundHandler *playground.PlaygroundHandler
	if store != nil {
		var llmClient playground.LLMClient
		if cfg.Query.PlaygroundLLMBaseURL != "" && cfg.Query.PlaygroundLLMAPIKey != "" {
			llmClient = playground.NewHTTPClient(cfg.Query.PlaygroundLLMBaseURL, cfg.Query.PlaygroundLLMAPIKey)
		}
		playgroundHandler = &playground.PlaygroundHandler{
			Cache:        promptCache,
			LLMClient:    llmClient,
			SessionStore: h,
		}
	}

	// Build the router.
	mux := http.NewServeMux()

	// Register auth routes (login, logout, invite, change password).
	h.Register(mux)

	// Admin routes (require admin session).
	adminMw := auth.RequireAdmin(store, cfg.Auth.SecureCookie, sessionTTL, cfg.Auth.AdminEmail)
	mux.HandleFunc("GET /api/v1/admin/api-keys", adminMw(http.HandlerFunc(adminHandler.HandleAdminAPIKeysList)).ServeHTTP)
	mux.HandleFunc("DELETE /api/v1/admin/api-keys/", adminMw(http.HandlerFunc(adminHandler.HandleAdminAPIKeyDelete)).ServeHTTP)
	mux.HandleFunc("GET /api/v1/admin/traces/", adminMw(http.HandlerFunc(adminHandler.HandleAdminTracesCount)).ServeHTTP)
	mux.HandleFunc("DELETE /api/v1/admin/traces/", adminMw(http.HandlerFunc(adminHandler.HandleAdminTracesDelete)).ServeHTTP)
	mux.HandleFunc("DELETE /api/v1/admin/projects/", adminMw(http.HandlerFunc(adminHandler.HandleAdminProjectsDelete)).ServeHTTP)

	// Projects list for the UI project switcher.
	mux.HandleFunc("GET /api/v1/projects", spanHandler.HandleProjects)

	// Span list with keyset pagination.
	mux.HandleFunc("POST /api/v1/spans/query", spanHandler.HandleSpansQuery)

	// Analytics: parameterized SQL compilation from structured DSL queries.
	mux.HandleFunc("POST /api/v1/analytics/spans", spanHandler.HandleAnalyticsSpans)

	// Trace detail waterfall.
	mux.HandleFunc("GET /api/v1/traces/{traceId}", spanHandler.HandleTraceDetail)

	// Trace bookmark toggle.
	mux.HandleFunc("POST /api/v1/traces/{traceId}/bookmark", bookmarkHandler.HandleBookmark)

	// Prompt Registry endpoints (require metadata store).
	if promptHandler != nil {
		mux.HandleFunc("GET /api/v1/prompts", promptHandler.HandleListPrompts)
		mux.HandleFunc("POST /api/v1/prompts", promptHandler.HandleCreatePrompt)
		mux.HandleFunc("GET /api/v1/prompts/{name}", promptHandler.HandleGetPrompt)
		mux.HandleFunc("GET /api/v1/prompts/{name}/versions", promptHandler.HandleListPromptVersions)
		mux.HandleFunc("PUT /api/v1/prompts/{name}/labels/{label}", promptHandler.HandleSetLabel)
	}

	// Eval rules endpoints (require metadata store).
	if evalRuleHandler != nil {
		mux.HandleFunc("POST /api/v1/eval-rules", evalRuleHandler.HandleCreate)
		mux.HandleFunc("GET /api/v1/eval-rules", evalRuleHandler.HandleList)
		mux.HandleFunc("POST /api/v1/eval-rules/preview", evalRuleHandler.HandlePreview)
		mux.HandleFunc("DELETE /api/v1/eval-rules/{id}", evalRuleHandler.HandleDelete)
	}

	// Dataset endpoints (require metadata store).
	if store != nil {
		datasetHandler := &handler.DatasetHandler{
			Store:        store,
			SessionStore: h,
		}
		mux.HandleFunc("POST /api/v1/datasets", datasetHandler.HandleCreate)
		mux.HandleFunc("GET /api/v1/datasets", datasetHandler.HandleList)
		mux.HandleFunc("GET /api/v1/datasets/{id}", datasetHandler.HandleGet)
		mux.HandleFunc("POST /api/v1/datasets/{id}/items", datasetHandler.HandleAddItems)
		mux.HandleFunc("POST /api/v1/datasets/{id}/items/batch", datasetHandler.HandleAddItemsBatch)
		mux.HandleFunc("GET /api/v1/datasets/{id}/items", datasetHandler.HandleListItems)
		mux.HandleFunc("DELETE /api/v1/datasets/{id}", datasetHandler.HandleDelete)

		// Dataset run endpoints — read endpoints (list, get, status) are
		// always available. POST (create run) requires judge LLM config.
		datasetRunHandler := &handler.DatasetRunHandler{
			Store:        store,
			SessionStore: h,
		}
		if cfg.Query.JudgeLLMBaseURL != "" && cfg.Query.JudgeLLMAPIKey != "" {
			judgeClient := playground.NewHTTPClient(cfg.Query.JudgeLLMBaseURL, cfg.Query.JudgeLLMAPIKey)
			datasetRunHandler.JudgeClient = judgeClient
			datasetRunHandler.Cache = promptCache
			mux.HandleFunc("POST /api/v1/datasets/{id}/runs", datasetRunHandler.HandleRun)
		}
		// Read endpoints don't need a judge LLM client — always register.
		mux.HandleFunc("GET /api/v1/datasets/{id}/runs", datasetRunHandler.HandleListRuns)
		mux.HandleFunc("GET /api/v1/datasets/{id}/runs/{runId}", datasetRunHandler.HandleGetRun)
		mux.HandleFunc("GET /api/v1/datasets/{id}/runs/{runId}/status", datasetRunHandler.HandleGetRunStatus)
	}

	// Playground endpoint (requires metadata store + LLM config).
	if playgroundHandler != nil {
		mux.HandleFunc("POST /api/v1/playground/run", playgroundHandler.HandleRun)
	}

	// Score write endpoint (for eval worker score write-back, no auth required).
	mux.HandleFunc("POST /api/v1/scores", handler.NewScoreHandler(sdb).ServeHTTP)

	// Prometheus metrics.
	mux.HandleFunc("GET /metrics", promhttp.Handler().ServeHTTP)

	// Serve embedded UI for all other routes (SPA fallback to index.html).
	mux.HandleFunc("/", serveUI)

	sessionMw := auth.RequireAuth(store, cfg.Auth.SecureCookie, sessionTTL)
	router := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path

		// Public routes bypass authentication entirely.
		if IsPublicAPIPath(path) {
			mux.ServeHTTP(w, r)
			return
		}

		// Protected API routes require a valid session cookie.
		if strings.HasPrefix(path, "/api/v1/") {
			sessionMw(mux).ServeHTTP(w, r)
			return
		}

		// SPA fallback and anything else.
		mux.ServeHTTP(w, r)
	})

	// Start S3 snapshot poller (separate goroutine).
	if s3Store != nil {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		go func() {
			ticker := time.NewTicker(syncInterval)
			defer ticker.Stop()
			for {
				select {
				case <-ticker.C:
					downloaded, err := pollAndDownload(ctx, s3Store, dbPath, &snapshotLastModified)
					if err != nil {
						slog.Warn("query: snapshot poll/download failed", "err", err)
					} else if downloaded {
						if err := sdb.Swap(dbPath); err != nil {
							slog.Warn("query: snapshot swap failed", "err", err)
						} else {
							slog.Info("query: snapshot swapped — new data now visible")
						}
					}
					// Update snapshot age metric.
					if !snapshotLastModified.IsZero() {
						age := time.Since(snapshotLastModified).Seconds()
						queryMetrics.RecordSnapshotAge(age)
					}
				case <-ctx.Done():
					// Trigger one final sync before exit.
					if downloaded, err := pollAndDownload(ctx, s3Store, dbPath, &snapshotLastModified); err != nil {
						slog.Warn("query: final sync failed", "err", err)
					} else if downloaded {
						if err := sdb.Swap(dbPath); err != nil {
							slog.Warn("query: snapshot swap on shutdown failed", "err", err)
						}
					}
					return
				}
			}
		}()
	}

	// Set up health and readiness probes.
	p := probe.New()
	p.AddCheck("snapshot", &probe.FileExists{Path: dbPath})

	// Combine the main router with probe routes.
	combined := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" || r.URL.Path == "/readyz" {
			p.Router().ServeHTTP(w, r)
		} else {
			router.ServeHTTP(w, r)
		}
	})

	// Parse query listen address
	addr := cfg.Query.Addr
	if addr == "" {
		addr = ":8002"
	}

	// Start server.
	srv := &http.Server{
		Addr:    addr,
		Handler: combined,
	}

	// Graceful shutdown
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Start the dedicated Prometheus metrics server on cfg.Metrics.Addr (:9090).
	if err := StartMetricsServer(ctx, cfg.Metrics.Addr); err != nil {
		return fmt.Errorf("query: start metrics server: %w", err)
	}

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
			span_id        VARCHAR      NOT NULL,
			trace_id       VARCHAR      NOT NULL,
			parent_id      VARCHAR,
			project_id     VARCHAR      NOT NULL,
			service_name   VARCHAR,
			name           VARCHAR,
			kind           VARCHAR,
			start_time     TIMESTAMPTZ  NOT NULL,
			end_time       TIMESTAMPTZ,
			model          VARCHAR,
			input          JSON,
			output         JSON,
			input_tokens   BIGINT,
			output_tokens  BIGINT,
			cost_usd       DOUBLE,
			prompt_name    VARCHAR,
			prompt_version BIGINT,
			status_code    VARCHAR,
			status_message VARCHAR,
			attributes     JSON,
			PRIMARY KEY (trace_id, span_id)
		);
		CREATE INDEX IF NOT EXISTS idx_spans_project_time
			ON spans (project_id, start_time);

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

// openMetadataStore creates a metadata store from config.
func openMetadataStore(cfg *config.Config) (metadata.Store, error) {
	driver := cfg.Database.Driver
	dsn := cfg.Database.DSN

	switch driver {
	case "", "sqlite":
		if dsn == "" {
			dsn = "omneval.db"
		}
		slog.Info("query: opening SQLite metadata store", "path", dsn)
		store, err := sqlite.New(dsn)
		if err != nil {
			return nil, fmt.Errorf("query: open sqlite metadata store: %w", err)
		}
		if err := store.Migrate(context.Background()); err != nil {
			store.Close()
			return nil, fmt.Errorf("query: migrate: %w", err)
		}
		return store, nil
	case "postgres":
		store, err := metadatapg.New(dsn)
		if err != nil {
			return nil, fmt.Errorf("query: postgres metadata store: %w", err)
		}
		if err := store.Migrate(context.Background()); err != nil {
			store.Close()
			return nil, fmt.Errorf("query: migrate: %w", err)
		}
		slog.Info("query: opening Postgres metadata store", "dsn", dsn)
		return store, nil
	default:
		return nil, fmt.Errorf("query: unknown database driver: %s", driver)
	}
}
