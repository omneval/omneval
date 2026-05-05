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

	"github.com/redis/go-redis/v9"
	"github.com/zbloss/lantern/internal/config"
	"github.com/zbloss/lantern/internal/probe"
)

// Run starts the Eval Worker pool: drains the Redis eval queue and
// dispatches LLM-as-a-Judge jobs. Serves /healthz and /readyz probes.
func Run() error {
	// Load config.
	cfgPath := ""
	if p := os.Getenv("LANTERN_CONFIG"); p != "" {
		cfgPath = p
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return fmt.Errorf("eval: load config: %w", err)
	}

	// Connect to Redis.
	rdb := redis.NewClient(&redis.Options{
		Addr:     cfg.Redis.Addr,
		Password: cfg.Redis.Password,
		DB:       cfg.Redis.DB,
	})
	if err := rdb.Ping(context.Background()).Err(); err != nil {
		return fmt.Errorf("eval: redis ping: %w", err)
	}

	// Set up health and readiness probes.
	p := probe.New()
	p.AddCheck("redis", &probe.RedisPing{Pinger: func(ctx context.Context) error {
		return rdb.Ping(ctx).Err()
	}})

	// Health + readiness router.
	probeHandler := p.Router()

	// Set up signal handling.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	// Start server (ready to accept score write-back and future endpoints).
	addr := cfg.Eval.Addr
	srv := &http.Server{
		Addr:    addr,
		Handler: probeHandler,
	}

	go func() {
		slog.Info("eval workers listening", "addr", addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("eval: server error", "err", err)
		}
	}()

	// Block until signal.
	<-sigCh
	slog.Info("eval: shutting down")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("eval: shutdown: %w", err)
	}

	slog.Info("eval: stopped")
	return nil
}
