package server

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
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
	metadatasqlite "github.com/zbloss/lantern/internal/metadata/sqlite"
	"github.com/zbloss/lantern/internal/storage"
	"github.com/zbloss/lantern/internal/storage/s3"
	"github.com/zbloss/lantern/services/query/internal/handler"
	"github.com/zbloss/lantern/services/query/internal/metrics"
)

// Run starts the Query API: pulls the latest DuckDB snapshot from S3,
// and serves REST, analytics DSL, and Prometheus metrics endpoints.
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
	if err := metrics.Register(); err != nil {
		return fmt.Errorf("query: register metrics: %w", err)
	}

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

	// Connect to S3 (may be nil if no storage config).
	var s3Store *s3.Store
	if cfg.Storage.Bucket != "" || cfg.Storage.Endpoint != "" {
		s3Store = s3.New(&cfg.Storage)
	}

	// Download snapshot from S3 (if configured).
	if s3Store != nil {
		if err := downloadSnapshot(context.Background(), s3Store, dbPath); err != nil {
			return fmt.Errorf("query: download snapshot: %w", err)
		}
		slog.Info("query: snapshot downloaded from S3", "path", dbPath)
	} else {
		slog.Info("query: no S3 configured, skipping snapshot download")
	}

	// Open the snapshot database read-only.
	db, err := openSnapshotDB(dbPath)
	if err != nil {
		return fmt.Errorf("query: open snapshot: %w", err)
	}
	defer db.Close()

	// Set up signal handling.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	// Start S3 snapshot poller (separate goroutine).
	if s3Store != nil {
		go func() {
			ticker := time.NewTicker(syncInterval)
			defer ticker.Stop()
			for {
				select {
				case <-ticker.C:
					if err := pollAndDownload(ctx, s3Store, dbPath); err != nil {
						slog.Warn("query: snapshot poll/download failed", "err", err)
					}
				case <-ctx.Done():
					// Trigger one final sync before exit.
					if err := pollAndDownload(ctx, s3Store, dbPath); err != nil {
						slog.Warn("query: final sync failed", "err", err)
					}
					return
				}
			}
		}()
	}

	// Create metadata store (SQLite for demo, Postgres for production).
	var metaStore metadata.Store
	metaDBPath := cfg.Database.DSN
	if metaDBPath == "" {
		metaDBPath = "/tmp/lantern-metadata.db"
	}
	metaDriver := cfg.Database.Driver
	if metaDriver == "" {
		metaDriver = "sqlite"
	}
	var metaErr error
	switch metaDriver {
	case "postgres":
		metaStore, metaErr = metadatapg.New(metaDBPath)
	default:
		metaStore, metaErr = metadatasqlite.New(metaDBPath)
	}
	if metaErr != nil {
		slog.Warn("query: metadata store init failed, prompt endpoints disabled", "driver", metaDriver, "err", metaErr)
		metaStore = nil
	}
	if metaStore != nil {
		if err := metaStore.Migrate(context.Background()); err != nil {
			return fmt.Errorf("query: migrate metadata store: %w", err)
		}
		defer metaStore.Close()
		slog.Info("query: metadata store initialized", "driver", metaDriver)
	} else {
		slog.Info("query: no metadata store configured, prompt endpoints disabled")
	}

	// Create handlers.
	sessStore := &noopSessionStore{}
	spanHandler := &handler.SpanHandler{
		DB:           db,
		SessionStore: sessStore,
	}

	var promptHandler *handler.PromptHandler
	var promptCache *handler.PromptCache
	if metaStore != nil {
		promptCache = handler.NewPromptCache(metaStore)
		promptHandler = &handler.PromptHandler{
			Store:     metaStore,
			Cache:     promptCache,
			StoreImpl: metaStore,
		}
		// Wire the session store for auth.
		promptHandler.SessionStore = sessStore
	}

	// Build the router.
	mux := http.NewServeMux()

	// Span list with keyset pagination.
	mux.HandleFunc("POST /api/v1/spans/query", spanHandler.HandleSpansQuery)

	// Trace detail waterfall.
	mux.HandleFunc("GET /api/v1/traces/{traceId}", spanHandler.HandleTraceDetail)

	// Prompt Registry endpoints (require metadata store).
	if promptHandler != nil {
		// Wrap HandleCreatePrompt to inject the session store.
		createFn := func(w http.ResponseWriter, r *http.Request) {
			promptHandler.HandleCreatePrompt(w, r, sessStore)
		}
		mux.HandleFunc("POST /api/v1/prompts", createFn)
		mux.HandleFunc("GET /api/v1/prompts/{name}", promptHandler.HandleGetPrompt)
		mux.HandleFunc("GET /api/v1/prompts/{name}/versions", promptHandler.HandleListPromptVersions)
		mux.HandleFunc("PUT /api/v1/prompts/{name}/labels/{label}", promptHandler.HandleSetLabel)
	}

	// Prometheus metrics.
	mux.HandleFunc("GET /metrics", promhttp.Handler().ServeHTTP)

	// Start server.
	addr := cfg.Query.Addr
	srv := &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	go func() {
		slog.Info("query: listening", "addr", addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("query: server error", "err", err)
		}
	}()

	// Block until signal.
	<-sigCh
	slog.Info("query: shutting down")
	cancel()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
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
func pollAndDownload(ctx context.Context, store storage.ObjectStore, dbPath string) error {
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
	return downloadSnapshot(ctx, store, dbPath)
}

// openSnapshotDB opens a DuckDB database in read-only mode.
func openSnapshotDB(path string) (*sql.DB, error) {
	// Open DuckDB in read-only mode using the file URI.
	dsn := "file:" + path + "?mode=ro"

	db, err := sql.Open("duckdb", dsn)
	if err != nil {
		return nil, fmt.Errorf("duckdb: open %s: %w", path, err)
	}

	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("duckdb: ping: %w", err)
	}

	return db, nil
}

// noopSessionStore is a no-op session store that always returns the configured
// project ID from the LANTERN_PROJECT environment variable.
// In production, this would be backed by real session middleware.
type noopSessionStore struct{}

func (s *noopSessionStore) ProjectID(r *http.Request) (string, bool) {
	if pid := os.Getenv("LANTERN_PROJECT"); pid != "" {
		return pid, true
	}
	// Fallback: accept project_id from query param for development.
	if pid := r.URL.Query().Get("project_id"); pid != "" {
		return pid, true
	}
	return "", false
}
