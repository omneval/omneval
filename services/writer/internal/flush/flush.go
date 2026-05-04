package flush

import (
	"context"
	"log"
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
				log.Printf("writer: flusher error: %v", err)
			}
		}
	}
}

// doFlush performs a single flush cycle.
func (f *Flusher) doFlush(ctx context.Context) error {
	if f.cfg.Storage.Endpoint == "" {
		log.Println("writer: flusher skipped (no S3 endpoint)")
		return nil
	}

	log.Printf("writer: flusher: exporting spans older than %v to Parquet", f.flushAge)
	// TODO: Export to Parquet on S3 with hive partitioning by date.
	// TODO: Prune flushed rows from the hot store.
	return nil
}
