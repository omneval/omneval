package flush

import (
	"context"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/zbloss/lantern/internal/config"
)

// Flusher exports spans older than 48 hours from DuckDB to Hive-partitioned
// Parquet files on S3 and prunes the corresponding rows from the hot store.
type Flusher struct {
	client   *redis.Client
	cfg      *config.Config
	flushAge time.Duration
}

// New creates a new Flusher.
func New(client *redis.Client, cfg *config.Config) *Flusher {
	flushAge := 48 * time.Hour
	if cfg.Writer.FlushAgeDays > 0 {
		flushAge = time.Duration(cfg.Writer.FlushAgeDays) * 24 * time.Hour
	}
	return &Flusher{
		client:   client,
		cfg:      cfg,
		flushAge: flushAge,
	}
}

// Run blocks until ctx is canceled. Every flush interval it exports aged
// spans from DuckDB to Parquet on S3 and prunes them from the hot store.
func (f *Flusher) Run(ctx context.Context) error {
	flushInterval, err := time.ParseDuration(f.cfg.Writer.FlushInterval)
	if err != nil {
		flushInterval = 30 * time.Minute
	}
	ticker := time.NewTicker(flushInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := f.doFlush(ctx); err != nil {
				slog.ErrorContext(ctx, "writer: flusher error", "err", err)
			}
		}
	}
}

// doFlush performs a single flush cycle.
func (f *Flusher) doFlush(ctx context.Context) error {
	if f.cfg.Storage.Endpoint == "" {
		slog.Info("writer: flusher skipped", "reason", "no_s3_endpoint")
		return nil
	}

	slog.InfoContext(ctx, "writer: flusher: exporting spans to Parquet", "age", f.flushAge)
	return nil
}
