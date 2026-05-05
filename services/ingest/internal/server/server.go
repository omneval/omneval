package server

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/redis/go-redis/v9"
	"github.com/zbloss/lantern/internal/auth"
	"github.com/zbloss/lantern/internal/config"
	"github.com/zbloss/lantern/internal/metadata"
	"github.com/zbloss/lantern/internal/metadata/postgres"
	"github.com/zbloss/lantern/internal/metadata/sqlite"
	redisqueue "github.com/zbloss/lantern/internal/queue/redis"
	"github.com/zbloss/lantern/services/ingest/internal/handler"
)

// Run starts the Ingest API HTTP server.
func Run() error {
	cfg, err := config.Load("")
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

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

	// Initialize combined router (native REST + OTLP)
	h := handler.CombinedRouter(queue, validator, cfg.Ingest.CORSAllowedOrigins)

	// Start server
	addr := cfg.Ingest.Addr
	slog.Info("ingest API listening", "addr", addr)
	return http.ListenAndServe(addr, h)
}
