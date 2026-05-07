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
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	_ "github.com/marcboeker/go-duckdb/v2"
	"github.com/zbloss/lantern/internal/config"
	"github.com/zbloss/lantern/internal/metadata"
	metadatapg "github.com/zbloss/lantern/internal/metadata/postgres"
	"github.com/zbloss/lantern/internal/metadata/sqlite"
	"github.com/zbloss/lantern/internal/probe"
	"github.com/zbloss/lantern/internal/storage"
	"github.com/zbloss/lantern/internal/storage/s3"
	"github.com/zbloss/lantern/services/query/internal/auth"
	"github.com/zbloss/lantern/services/query/internal/handler"
	"github.com/zbloss/lantern/services/query/internal/metrics"
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

// Run starts the Query API: opens the DuckDB snapshot from S3 and the
// metadata store, bootstraps the admin user, and serves auth, span,
// analytics, prompt, and metrics endpoints.
func Run() error {
	// Load config.
	cfgPath := ""
	if p := os.Getenv("LANTERN_CONFIG"); p != "" {
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
		dbPath = "/tmp/lantern-snapshot.duckdb"
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
		slog.Info("query: snapshot downloaded from S3", "path", dbPath)
		// Try to get the last modified time from S3.
		if stat, err := s3Store.Stat(context.Background(), "snapshots/duckdb.db"); err == nil && stat != nil {
			snapshotLastModified = stat.LastModified
		}
	} else {
		slog.Info("query: no S3 configured, skipping snapshot download")
	}

	// Open the snapshot database read-write (for score writes from eval workers).
	// The Writer Service syncs snapshots to S3, so score writes here are
	// eventually included in the snapshot.
	db, err := openSnapshotDBRW(dbPath)
	if err != nil {
		return fmt.Errorf("query: open snapshot: %w", err)
	}
	defer db.Close()

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
			slog.Warn("query: no admin configured and no users exist — set LANTERN_AUTH_ADMIN_EMAIL and LANTERN_AUTH_ADMIN_PASSWORD to create the first admin user")
		}
	}

	// Create handlers.
	spanHandler := &handler.SpanHandler{
		DB:           db,
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
			Store:        store,
			SessionStore: h,
		}
	}

	// Build the router.
	mux := http.NewServeMux()

	// Register auth routes (login, logout, invite, change password, projects).
	h.Register(mux)

	// Span list with keyset pagination.
	mux.HandleFunc("POST /api/v1/spans/query", spanHandler.HandleSpansQuery)

	// Analytics: parameterized SQL compilation from structured DSL queries.
	mux.HandleFunc("POST /api/v1/analytics/spans", spanHandler.HandleAnalyticsSpans)

	// Trace detail waterfall.
	mux.HandleFunc("GET /api/v1/traces/{traceId}", spanHandler.HandleTraceDetail)

	// Prompt Registry endpoints (require metadata store).
	if promptHandler != nil {
		mux.HandleFunc("POST /api/v1/prompts", promptHandler.HandleCreatePrompt)
		mux.HandleFunc("GET /api/v1/prompts/{name}", promptHandler.HandleGetPrompt)
		mux.HandleFunc("GET /api/v1/prompts/{name}/versions", promptHandler.HandleListPromptVersions)
		mux.HandleFunc("PUT /api/v1/prompts/{name}/labels/{label}", promptHandler.HandleSetLabel)
	}

	// Eval rules endpoints (require metadata store).
	if evalRuleHandler != nil {
		mux.HandleFunc("POST /api/v1/eval-rules", evalRuleHandler.HandleCreate)
		mux.HandleFunc("GET /api/v1/eval-rules", evalRuleHandler.HandleList)
		mux.HandleFunc("DELETE /api/v1/eval-rules/{id}", evalRuleHandler.HandleDelete)
	}

	// Score write endpoint (for eval worker score write-back).
	mux.HandleFunc("POST /api/v1/scores", handler.NewScoreHandler(db).ServeHTTP)

	// Prometheus metrics.
	mux.HandleFunc("GET /metrics", promhttp.Handler().ServeHTTP)

	// Serve embedded UI for all other routes (SPA fallback to index.html).
	// NOTE: GET /api/v1/projects is already registered by h.Register(mux) above.
	mux.HandleFunc("/", serveUI)

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
					if err := pollAndDownload(ctx, s3Store, dbPath, &snapshotLastModified); err != nil {
						slog.Warn("query: snapshot poll/download failed", "err", err)
					}
					// Update snapshot age metric.
					if !snapshotLastModified.IsZero() {
						age := time.Since(snapshotLastModified).Seconds()
						queryMetrics.RecordSnapshotAge(age)
					}
				case <-ctx.Done():
					// Trigger one final sync before exit.
					if err := pollAndDownload(ctx, s3Store, dbPath, &snapshotLastModified); err != nil {
						slog.Warn("query: final sync failed", "err", err)
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
			mux.ServeHTTP(w, r)
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

// downloadSnapshot downloads the DuckDB snapshot from S3 to the local path.
func downloadSnapshot(ctx context.Context, store storage.ObjectStore, dbPath string) error {
	// Ensure parent directory exists.
	dir := filepath.Dir(dbPath)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("snapshot: create dir %s: %w", dir, err)
		}
	}

	// Get the latest snapshot from S3.
	snapshotKey := "snapshots/duckdb.db"
	reader, err := store.Get(ctx, snapshotKey)
	if err != nil {
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
func pollAndDownload(ctx context.Context, store storage.ObjectStore, dbPath string, lastModified *time.Time) error {
	snapshotKey := "snapshots/duckdb.db"

	// Stat the S3 object to check for changes.
	info, err := store.Stat(ctx, snapshotKey)
	if err != nil {
		return fmt.Errorf("snapshot: stat %s: %w", snapshotKey, err)
	}

	// Check if local file exists and has the same ETag/LastModified.
	localInfo, statErr := os.Stat(dbPath)
	if statErr == nil && info != nil {
		if localInfo.ModTime().Equal(info.LastModified) || localInfo.ModTime().After(info.LastModified) {
			slog.Debug("query: snapshot unchanged, skipping download")
			return nil
		}
	}

	// Download new snapshot.
	slog.Info("query: downloading updated snapshot from S3", "etag", info.ETag)
	if err := downloadSnapshot(ctx, store, dbPath); err != nil {
		return err
	}

	// Update the last modified time.
	if info != nil {
		*lastModified = info.LastModified
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
			dsn = "lantern.db"
		}
		slog.Info("query: opening SQLite metadata store", "path", dsn)
		store, err := sqlite.New(dsn)
		if err != nil {
			return nil, err
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
