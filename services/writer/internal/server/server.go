package server

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	redisgo "github.com/redis/go-redis/v9"
	"github.com/zbloss/lantern/internal/config"
	"github.com/zbloss/lantern/internal/duckdb"
	"github.com/zbloss/lantern/internal/metadata/sqlite"
	"github.com/zbloss/lantern/internal/pricing"
	qredis "github.com/zbloss/lantern/internal/queue/redis"
	"github.com/zbloss/lantern/internal/storage/s3"
	"github.com/zbloss/lantern/services/writer/internal/flush"
	"github.com/zbloss/lantern/services/writer/internal/handler"
	"github.com/zbloss/lantern/services/writer/internal/pipeline"
	"github.com/zbloss/lantern/services/writer/internal/sync"
)

// Run starts the Writer Service: drains the Redis ingest queue, writes to
// DuckDB, syncs snapshots to S3, flushes aged partitions as Parquet, and
// serves POST /internal/v1/scores for Eval Worker score write-back.
func Run() error {
	// Load config.
	cfgPath := ""
	if p := os.Getenv("LANTERN_CONFIG"); p != "" {
		cfgPath = p
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return fmt.Errorf("writer: load config: %w", err)
	}

	// Initialize bundled pricing (runs once, lazy).
	pricing.InitBundledPricing()

	// Load pricing table (live fetch, fallback to bundled).
	overrides := make(map[string]pricing.ModelOverride)
	for model, ov := range cfg.Pricing.ModelOverrides {
		overrides[model] = pricing.ModelOverride{
			InputPerMillion:  ov.InputPerMillion,
			OutputPerMillion: ov.OutputPerMillion,
		}
	}
	pricingTable, err := pricing.Fetch(overrides)
	if err != nil {
		return fmt.Errorf("writer: load pricing: %w", err)
	}

	// Open DuckDB.
	dbPath := cfg.Writer.DuckDBPath
	if dbPath == "" {
		dbPath = "lantern.db"
	}
	db, err := duckdb.Open(dbPath)
	if err != nil {
		return fmt.Errorf("writer: open duckdb: %w", err)
	}
	defer db.Close()

	// Open metadata store (SQLite for now).
	meta, err := sqlite.New("lantern_meta.db")
	if err != nil {
		return fmt.Errorf("writer: open metadata: %w", err)
	}
	defer meta.Close()

	// Run migrations.
	if err := meta.Migrate(context.Background()); err != nil {
		return fmt.Errorf("writer: migrate: %w", err)
	}

	// Connect to Redis.
	rc := redisgo.NewClient(&redisgo.Options{
		Addr:     cfg.Redis.Addr,
		Password: cfg.Redis.Password,
		DB:       cfg.Redis.DB,
	})
	if err := rc.Ping(context.Background()).Err(); err != nil {
		return fmt.Errorf("writer: redis ping: %w", err)
	}

	// Create queue clients.
	ingestQ := qredis.NewIngestQueue(rc)
	evalQ := qredis.NewEvalQueue(rc)

	// Create pipeline.
	pl := pipeline.New(ingestQ, db, pricingTable, meta, evalQ)

	// Create S3 store (nil if no S3 config).
	var s3store *s3.Store
	if cfg.Storage.Bucket != "" || cfg.Storage.Endpoint != "" {
		s3store = s3.New(&cfg.Storage)
		if s3store != nil {
			if err := s3store.EnsureBucket(context.Background()); err != nil {
				slog.Warn("writer: ensure bucket", "err", err)
			}
		}
	}

	// Create syncer (S3 snapshot sync).
	syncer := sync.New(s3store, dbPath, cfg, nil)

	// Create flusher (aged partition flush to Parquet on S3).
	flusher := flush.NewWithDB(s3store, db, cfg)

	// Create score handler.
	scoreHandler := handler.New(db)

	// Set up signal handling.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	// Start score handler server (separate goroutine).
	scoreServer := &http.Server{
		Addr:    cfg.Writer.Addr,
		Handler: scoreHandler,
	}
	go func() {
		slog.Info("writer: score handler listening", "addr", cfg.Writer.Addr)
		if err := scoreServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("writer: score handler error", "err", err)
		}
	}()

	// Start syncer (separate goroutine).
	go func() {
		if err := syncer.Run(ctx); err != nil {
			slog.Error("writer: syncer error", "err", err)
		}
	}()

	// Start flusher (separate goroutine).
	go func() {
		slog.Info("writer: flusher started")
		if err := flusher.Run(ctx); err != nil && err != context.Canceled {
			slog.Error("writer: flusher error", "err", err)
		}
	}()

	// Start pipeline (blocks until ctx is canceled).
	slog.Info("writer: pipeline started")
	if err := pl.Run(ctx); err != nil {
		cancel()
		scoreServer.Shutdown(context.Background())
		return fmt.Errorf("writer: pipeline: %w", err)
	}

	// Shutdown.
	cancel()
	scoreServer.Shutdown(context.Background())
	return nil
}
