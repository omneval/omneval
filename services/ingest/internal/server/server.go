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
	"github.com/redis/go-redis/v9"
	"github.com/zbloss/lantern/internal/auth"
	"github.com/zbloss/lantern/internal/config"
	"github.com/zbloss/lantern/internal/handlers"
	"github.com/zbloss/lantern/internal/metadata"
	"github.com/zbloss/lantern/internal/metadata/postgres"
	"github.com/zbloss/lantern/internal/metadata/sqlite"
	"github.com/zbloss/lantern/internal/probe"
	redisqueue "github.com/zbloss/lantern/internal/queue/redis"
	"github.com/zbloss/lantern/services/ingest/internal/handler"
	"github.com/zbloss/lantern/services/ingest/internal/metrics"
)

// Run starts the Ingest API HTTP server with graceful shutdown.
//
// Graceful shutdown behavior:
// - Stops accepting new connections on SIGTERM/SIGINT
// - Waits up to 30s for in-flight HTTP requests to complete
func Run() error {
	cfg, err := config.Load("")
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	// Register Prometheus metrics.
	if err := metrics.Register(cfg.Metrics.DisableProjectLabels); err != nil {
		return fmt.Errorf("register metrics: %w", err)
	}

	metricsHelper := metrics.NewIngestMetrics(cfg)

	// Initialize metadata store
	var store metadata.Store
	if cfg.Database.Driver == "postgres" {
		store, err = postgres.New(cfg.Database.DSN)
		if err != nil {
			return fmt.Errorf("connecting to postgres: %w", err)
		}
	} else {
		store, err = sqlite.New("") // in-memory SQLite
		if err != nil {
			return fmt.Errorf("opening sqlite store: %w", err)
		}
		if err := store.Migrate(context.Background()); err != nil {
			return fmt.Errorf("running migrations: %w", err)
		}
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

	// Initialize queue
	queue := redisqueue.NewIngestQueue(rdb)

	// Initialize validator
	validator := auth.NewCachingValidator(store)

	// Initialize native REST handler with CORS middleware and metrics.
	nativeH := handler.NewNativeHandler(queue, validator, cfg.Ingest.CORSAllowedOrigins, metricsHelper)

	// Initialize OTLP handler
	otlpH := handlers.NewOTLPHandler(queue, validator)

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
