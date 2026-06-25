package server

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/omneval/omneval/internal/config"
	"github.com/omneval/omneval/internal/harness"
	"github.com/omneval/omneval/internal/metadata"
	redisqueue "github.com/omneval/omneval/internal/queue/redis"
	"github.com/omneval/omneval/services/eval/internal/judge"
	"github.com/omneval/omneval/services/eval/internal/prompts"
	"github.com/omneval/omneval/services/eval/internal/worker"
	"github.com/redis/go-redis/v9"
)

// openMetadataStore creates a metadata store from config. It supports
// "sqlite" (default) and "postgres" drivers, mirroring the other services.
func openMetadataStore(cfg *config.Config) (metadata.Store, error) {
	driver := cfg.Database.Driver
	dsn := cfg.Database.DSN

	switch driver {
	case "", "sqlite":
		if dsn == "" {
			dsn = "omneval.db"
		}
		return metadata.Open("sqlite", dsn)
	case "postgres":
		if dsn == "" {
			return nil, fmt.Errorf("eval: postgres driver requires database.dsn")
		}
		return metadata.Open("postgres", dsn)
	default:
		return nil, fmt.Errorf("eval: unknown database driver: %s", driver)
	}
}

// Run starts the eval worker pool: drains the Redis eval queue and
// dispatches LLM-as-a-Judge jobs with graceful shutdown.
//
// Graceful shutdown behavior:
// - Stops dequeuing new jobs on SIGTERM/SIGINT
// - Finishes the current LLM call within the drain window
func Run() error {
	h, err := harness.New("")
	if err != nil {
		return fmt.Errorf("create harness: %w", err)
	}

	return h.Run(context.Background(), func(ctx *harness.HarnessContext) (string, http.Handler, harness.ShutdownFunc, harness.ShutdownFunc, error) {
		cfg := ctx.Cfg

		// Create Redis client.
		rdb := redis.NewClient(&redis.Options{
			Addr:     cfg.Redis.Addr,
			Password: cfg.Redis.Password,
			DB:       cfg.Redis.DB,
		})

		// Create queue client.
		evalQ := redisqueue.NewEvalQueue(rdb)

		// Open the metadata store so the judge can resolve prompt templates
		// from the Prompt Registry. We only need the focused PromptStore.
		store, err := openMetadataStore(cfg)
		if err != nil {
			return "", nil, nil, nil, fmt.Errorf("open metadata store: %w", err)
		}
		promptStore := store.PromptStore()

		// Initialize judge with the Prompt Registry resolver.
		judgeLLM := judge.New(cfg, prompts.NewCachingResolver(promptStore))

		// Create worker.
		w := worker.New(evalQ, judgeLLM, cfg)

		// Start worker in background.
		ctx.StartBackground(func(ctx context.Context) error {
			slog.Info("eval worker: started")
			if err := w.Run(ctx); err != nil {
				slog.Error("eval worker: error", "err", err)
			}
			return nil
		})

		// Teardown function called during graceful shutdown.
		teardown := func() {
			store.Close()
		}

		// No HTTP server for eval — it's a background worker.
		return "", nil, nil, teardown, nil
	})
}
