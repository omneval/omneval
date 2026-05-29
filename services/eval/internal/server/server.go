package server

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/omneval/omneval/internal/config"
	redisqueue "github.com/omneval/omneval/internal/queue/redis"
	"github.com/omneval/omneval/services/eval/internal/judge"
	"github.com/omneval/omneval/services/eval/internal/metrics"
	"github.com/omneval/omneval/services/eval/internal/worker"
	"github.com/redis/go-redis/v9"
)

const workerShutdownTimeout = 120 * time.Second

// Run starts the eval worker pool: drains the Redis eval queue and
// dispatches LLM-as-a-Judge jobs with graceful shutdown.
//
// Graceful shutdown behavior:
// - Stops dequeuing new jobs on SIGTERM/SIGINT
// - Finishes the current LLM call within the drain window
func Run() error {
	cfgPath := ""
	if p := os.Getenv("OMNEVAL_CONFIG"); p != "" {
		cfgPath = p
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return fmt.Errorf("eval: load config: %w", err)
	}

	// Initialize Redis client.
	rdb := redis.NewClient(&redis.Options{
		Addr:     cfg.Redis.Addr,
		Password: cfg.Redis.Password,
		DB:       cfg.Redis.DB,
	})
	if err := rdb.Ping(context.Background()).Err(); err != nil {
		return fmt.Errorf("eval: redis ping: %w", err)
	}

	// Register Prometheus metrics.
	if err := metrics.Register(cfg.Metrics.DisableProjectLabels); err != nil {
		return fmt.Errorf("eval: register metrics: %w", err)
	}

	// Start the dedicated Prometheus metrics server on cfg.Metrics.Addr (:9090).
	metricsCtx, metricsCancel := context.WithCancel(context.Background())
	defer metricsCancel()
	if err := StartMetricsServer(metricsCtx, cfg.Metrics.Addr); err != nil {
		return fmt.Errorf("eval: start metrics server: %w", err)
	}

	// Create queue client.
	evalQ := redisqueue.NewEvalQueue(rdb)

	// Initialize judge.
	judgeLLM := judge.New(cfg)

	// Create worker.
	w := worker.New(evalQ, judgeLLM, cfg)

	// Set up context with cancellation.
	ctx, cancel := context.WithCancel(context.Background())

	// Start worker in background.
	var workerErr error
	workerDone := make(chan struct{})
	go func() {
		slog.Info("eval worker: started")
		workerErr = w.Run(ctx)
		close(workerDone)
	}()

	// Wait for shutdown signal.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh
	slog.Info("eval worker: shutting down")

	// Cancel context — worker will finish current job and exit.
	cancel()

	// Wait for worker to finish. Covers the in-flight LLM call
	// (judge timeout up to ~100s) plus margin.
	deadline := time.After(workerShutdownTimeout)
	select {
	case <-workerDone:
		if workerErr != nil && workerErr != context.Canceled {
			return fmt.Errorf("eval worker: %w", workerErr)
		}
		slog.Info("eval worker: stopped")
	case <-deadline:
		slog.Warn("eval worker: timed out waiting for in-flight job to finish")
		return fmt.Errorf("eval worker: shutdown timeout after %v", workerShutdownTimeout)
	}

	return nil
}
