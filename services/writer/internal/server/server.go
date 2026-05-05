package server

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	redisgo "github.com/redis/go-redis/v9"
	"github.com/zbloss/lantern/internal/config"
	"github.com/zbloss/lantern/internal/duckdb"
	"github.com/zbloss/lantern/internal/metadata/sqlite"
	"github.com/zbloss/lantern/internal/pricing"
	"github.com/zbloss/lantern/internal/probe"
	qredis "github.com/zbloss/lantern/internal/queue/redis"
	"github.com/zbloss/lantern/internal/storage/s3"
	"github.com/zbloss/lantern/services/writer/internal/handler"
	"github.com/zbloss/lantern/services/writer/internal/metrics"
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

	// Register Prometheus metrics.
	if err := metrics.Register(cfg.Metrics.DisableProjectLabels); err != nil {
		return fmt.Errorf("writer: register metrics: %w", err)
	}

	metricsHelper := metrics.NewWriterMetrics(cfg)

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
	pl := pipeline.New(ingestQ, db, pricingTable, meta, evalQ, metricsHelper)

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
	syncer := sync.New(s3store, dbPath, cfg, metricsHelper)

	// Create score handler (handles POST /internal/v1/scores).
	scoreMux := handler.New(db)

	// Set up health and readiness probes.
	p := probe.New()
	p.AddCheck("duckdb", &probe.DuckDBWritable{
		Open: func(path string) (probe.WritableView, error) {
			return duckdb.Open(path)
		},
		Path: dbPath,
	})
	p.AddCheck("redis", &probe.RedisPing{Pinger: func(ctx context.Context) error {
		return rc.Ping(ctx).Err()
	}})

	// Set up signal handling.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	// Build the full router: /metrics + /internal/v1/scores + probes.
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())
	mux.Handle("/internal/v1/scores", scoreMux)
	
	// Combine with probe routes.
	combined := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" || r.URL.Path == "/readyz" {
			p.Router().ServeHTTP(w, r)
		} else {
			mux.ServeHTTP(w, r)
		}
	})

	// Start server.
	scoreServer := &http.Server{
		Addr:    cfg.Writer.Addr,
		Handler: combined,
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

	// Start pipeline (blocks until ctx is canceled).
	slog.Info("writer: pipeline started")
	if err := pl.Run(ctx); err != nil {
		cancel()
		if shutdownErr := gracefulShutdown(scoreServer, 30*time.Second); shutdownErr != nil {
			return fmt.Errorf("writer: pipeline shutdown: %w", shutdownErr)
		}
		return fmt.Errorf("writer: pipeline: %w", err)
	}

	// Graceful shutdown.
	cancel()
	if err := gracefulShutdown(scoreServer, 30*time.Second); err != nil {
		return fmt.Errorf("writer: shutdown: %w", err)
	}
	slog.Info("writer: stopped")
	return nil
}

// gracefulShutdown shuts down an HTTP server with the given drain timeout.
func gracefulShutdown(srv *http.Server, timeout time.Duration) error {
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), timeout)
	defer shutdownCancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("server shutdown: %w", err)
	}
	return nil
}
