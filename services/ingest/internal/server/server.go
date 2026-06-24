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

	"github.com/omneval/omneval/internal/auth"
	"github.com/omneval/omneval/internal/buffer"
	"github.com/omneval/omneval/internal/config"
	"github.com/omneval/omneval/internal/handlers"
	"github.com/omneval/omneval/internal/metadata"
	"github.com/omneval/omneval/internal/probe"
	redisqueue "github.com/omneval/omneval/internal/queue/redis"
	s3pkg "github.com/omneval/omneval/internal/storage/s3"
	"github.com/omneval/omneval/services/ingest/internal/handler"
	"github.com/omneval/omneval/services/ingest/internal/metrics"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/redis/go-redis/v9"
)

// levelFromString maps a string to an slog.Level, defaulting to info.
func levelFromString(s string) slog.Level {
	switch s {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// Run starts the Ingest API HTTP server with graceful shutdown.
//
// Graceful shutdown behavior:
// - Stops accepting new connections on SIGTERM/SIGINT
// - Waits up to 30s for in-flight HTTP requests to complete
func Run() error {
	cfgPath := ""
	if p := os.Getenv("OMNEVAL_CONFIG"); p != "" {
		cfgPath = p
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	// Configure slog with the configured log level.
	logLevel := levelFromString(cfg.LogLevel)
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: logLevel})))

	// Register Prometheus metrics.
	if err := metrics.Register(cfg.Metrics.DisableProjectLabels); err != nil {
		return fmt.Errorf("register metrics: %w", err)
	}

	metricsHelper := metrics.NewIngestMetrics(cfg)

	// Initialize metadata store
	store, err := metadata.Open(cfg.Database.Driver, cfg.Database.DSN)
	if err != nil {
		return fmt.Errorf("opening metadata store: %w", err)
	}
	defer store.Close()

	// Initialize Redis client
	rdb := redis.NewClient(&redis.Options{
		Addr:     cfg.Redis.Addr,
		Password: cfg.Redis.Password,
		DB:       cfg.Redis.DB,
	})

	if err := rdb.Ping(context.Background()).Err(); err != nil {
		return fmt.Errorf("connecting to redis at %s: %w", cfg.Redis.Addr, err)
	}

	// Initialize queue. With the Ingest Buffer enabled (ADR-0004), batches
	// are staged in S3 and only Batch ID references enter Redis; both the
	// native and OTLP handlers go through the same SpanQueue seam, so the
	// staged queue covers every ingest path.
	redisQ := redisqueue.NewIngestQueue(rdb)
	var spanQ handler.SpanQueue = redisQ
	if cfg.Ingest.Buffer.Enabled {
		s3store := s3pkg.New(&cfg.Storage)
		if s3store == nil {
			return fmt.Errorf("ingest: buffer enabled but storage (S3) is not configured")
		}
		if err := s3store.EnsureBucket(context.Background()); err != nil {
			slog.Warn("ingest: ensure bucket", "err", err)
		}
		spanQ = buffer.NewStagedQueue(buffer.New(s3store), redisQ, metricsHelper)
		slog.Info("ingest: Ingest Buffer enabled, staging batches in S3")
	}

	// Initialize validator
	validator := auth.NewCachingValidator(store)

	// Initialize native REST handler with CORS middleware and metrics.
	nativeH := handler.NewNativeHandler(spanQ, validator, cfg.Ingest.CORSAllowedOrigins, metricsHelper)

	// Initialize OTLP handler
	otlpH := handlers.NewOTLPHandler(spanQ, validator)

	// Combine handlers on a single router
	router := http.NewServeMux()
	router.Handle("/", nativeH.Router())
	router.Handle("/v1/traces", otlpH.Router())

	// Set up health and readiness probes.
	p := probe.New()
	p.AddCheck("redis", &probe.RedisPing{Pinger: func(ctx context.Context) error {
		return rdb.Ping(ctx).Err()
	}})

	// Build router with /metrics endpoint and combined probe routes.
	metricsHandler := promhttp.Handler()
	combined := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/metrics" {
			metricsHandler.ServeHTTP(w, r)
		} else if r.URL.Path == "/healthz" || r.URL.Path == "/readyz" {
			p.Router().ServeHTTP(w, r)
		} else {
			router.ServeHTTP(w, r)
		}
	})

	// Start the dedicated Prometheus metrics server on cfg.Metrics.Addr (:9090).
	metricsCtx, metricsCancel := context.WithCancel(context.Background())
	defer metricsCancel()
	if err := StartMetricsServer(metricsCtx, cfg.Metrics.Addr); err != nil {
		return fmt.Errorf("ingest: start metrics server: %w", err)
	}

	// Start server
	addr := cfg.Ingest.Addr
	srv := &http.Server{
		Addr:    addr,
		Handler: combined,
	}

	go func() {
		slog.Info("ingest API listening", "addr", addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("ingest API server error", "err", err)
		}
	}()

	// Wait for shutdown signal.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh
	slog.Info("ingest API: shutting down")

	// Graceful shutdown with 30-second drain timeout.
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("ingest API: shutdown: %w", err)
	}

	slog.Info("ingest API: stopped")
	return nil
}
