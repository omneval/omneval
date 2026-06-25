package server

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/omneval/omneval/internal/auth"
	"github.com/omneval/omneval/internal/buffer"
	"github.com/omneval/omneval/internal/handlers"
	"github.com/omneval/omneval/internal/harness"
	"github.com/omneval/omneval/internal/metadata"
	"github.com/omneval/omneval/internal/probe"
	redisqueue "github.com/omneval/omneval/internal/queue/redis"
	s3pkg "github.com/omneval/omneval/internal/storage/s3"
	"github.com/omneval/omneval/services/ingest/internal/handler"
	"github.com/omneval/omneval/services/ingest/internal/metrics"
	"github.com/redis/go-redis/v9"
)

// Run starts the Ingest service: loads config, initializes dependencies, and
// boots the HTTP server on cfg.Ingest.Addr with graceful shutdown.
//
// Graceful shutdown behavior:
// - Stops accepting new connections on SIGTERM/SIGINT
// - Waits up to 20s for in-flight HTTP requests to complete
func Run() error {
	h, err := harness.New("")
	if err != nil {
		return fmt.Errorf("create harness: %w", err)
	}
	h = h.WithRegisterMetrics(metrics.Register)

	return h.Run(context.Background(), func(ctx *harness.HarnessContext) (string, http.Handler, harness.ShutdownFunc, harness.ShutdownFunc, error) {
		cfg := ctx.Cfg

		metricsHelper := metrics.NewIngestMetrics(cfg)

		// Initialize metadata store.
		store, err := metadata.Open(cfg.Database.Driver, cfg.Database.DSN)
		if err != nil {
			return "", nil, nil, nil, fmt.Errorf("opening metadata store: %w", err)
		}

		// Initialize Redis client.
		rdb := redis.NewClient(&redis.Options{
			Addr:     cfg.Redis.Addr,
			Password: cfg.Redis.Password,
			DB:       cfg.Redis.DB,
		})

		if err := rdb.Ping(context.Background()).Err(); err != nil {
			return "", nil, nil, nil, fmt.Errorf("connecting to redis at %s: %w", cfg.Redis.Addr, err)
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
				return "", nil, nil, nil, fmt.Errorf("ingest: buffer enabled but storage (S3) is not configured")
			}
			if err := s3store.EnsureBucket(context.Background()); err != nil {
				slog.Warn("ingest: ensure bucket", "err", err)
			}
			spanQ = buffer.NewStagedQueue(buffer.New(s3store), redisQ, metricsHelper)
			slog.Info("ingest: Ingest Buffer enabled, staging batches in S3")
		}

		// Initialize validator.
		validator := auth.NewCachingValidator(store)

		// Initialize native REST handler with CORS middleware and metrics.
		nativeH := handler.NewNativeHandler(spanQ, validator, cfg.Ingest.CORSAllowedOrigins, metricsHelper)

		// Initialize OTLP handler.
		otlpH := handlers.NewOTLPHandler(spanQ, validator)

		// Combine handlers on a single router.
		router := http.NewServeMux()
		router.Handle("/", nativeH.Router())
		router.Handle("/v1/traces", otlpH.Router())

		// Set up health and readiness probes.
		ctx.Prober.AddCheck("redis", &probe.RedisPing{Pinger: func(ctx context.Context) error {
			return rdb.Ping(ctx).Err()
		}})

		// Teardown function called during graceful shutdown.
		teardown := func() {
			store.Close()
		}

		return cfg.Ingest.Addr, router, nil, teardown, nil
	})
}
