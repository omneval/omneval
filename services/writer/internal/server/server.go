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

	"github.com/omneval/omneval/internal/config"
	"github.com/omneval/omneval/internal/metadata"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Run starts the Writer Service: drains the Redis ingest queue, computes
// cost, commits batches to the Lake (ADR-0004), and serves
// POST /internal/v1/scores for Eval Worker score write-back.
func Run() error {
	// Load config.
	cfgPath := ""
	if p := os.Getenv("OMNEVAL_CONFIG"); p != "" {
		cfgPath = p
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return fmt.Errorf("writer: load config: %w", err)
	}

	deps, err := WireDeps(cfg)
	if err != nil {
		return err
	}
	return RunWired(deps)
}

// RunWired runs the writer with pre-constructed dependencies: it wires
// routes, starts the background goroutines, runs the pipeline until a
// signal or fatal error, then shuts everything down gracefully.
func RunWired(deps *WiredDeps) error {
	cfg := deps.Cfg

	// Set up signal handling.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	// Start the dedicated Prometheus metrics server on cfg.Metrics.Addr (:9090).
	if err := StartMetricsServer(ctx, cfg.Metrics.Addr); err != nil {
		return fmt.Errorf("writer: start metrics server: %w", err)
	}

	// Build the full router: /metrics + /internal/v1/scores + probes.
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())
	mux.Handle("/internal/v1/scores", deps.ScoreHandler)
	combined := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" || r.URL.Path == "/readyz" {
			deps.Prober.Router().ServeHTTP(w, r)
		} else {
			mux.ServeHTTP(w, r)
		}
	})

	scoreServer := &http.Server{Addr: cfg.Writer.Addr, Handler: combined}
	go func() {
		slog.Info("writer: score handler listening", "addr", cfg.Writer.Addr)
		if err := scoreServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("writer: score handler error", "err", err)
		}
	}()

	// Start background workers. Every replica runs the Ingest Buffer
	// reconciliation sweep on its own ticker (#90): DuckLake supports
	// multi-writer, so there is no leader-election gate.
	if deps.Reconcile != nil {
		go func() {
			slog.Info("writer: reconciliation worker started")
			if err := deps.Reconcile.RunLoop(ctx); err != nil && err != context.Canceled {
				slog.Error("writer: reconciliation worker error", "err", err)
			}
		}()
	}

	// Start pipeline (blocks until ctx is canceled).
	slog.Info("writer: pipeline started")
	pipelineErr := deps.Pipeline.Run(ctx)

	// Graceful shutdown.
	cancel()
	deps.Meta.Close()
	if err := gracefulShutdown(scoreServer, 30*time.Second); err != nil {
		return fmt.Errorf("writer: shutdown: %w", err)
	}
	slog.Info("writer: stopped")
	return pipelineErr
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

// openMetadataStore opens the configured metadata store via the shared
// factory, applying the writer's default SQLite path when no DSN is set.
func openMetadataStore(cfg *config.Config) (metadata.Store, error) {
	dsn := cfg.Database.DSN
	if dsn == "" && (cfg.Database.Driver == "" || cfg.Database.Driver == "sqlite") {
		dsn = "omneval_meta.db"
	}
	return metadata.Open(cfg.Database.Driver, dsn)
}
