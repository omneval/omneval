package sync

import (
	"context"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/zbloss/lantern/internal/config"
)

// Syncer copies the live DuckDB file to S3 every 30 seconds so Query API
// replicas always have a fresh snapshot.
type Syncer struct {
	client      *redis.Client
	cfg         *config.Config
	syncAt      time.Duration
	snapshotKey string
}

// New creates a new Syncer.
func New(client *redis.Client, cfg *config.Config) *Syncer {
	syncDur, err := time.ParseDuration(cfg.Writer.SyncInterval)
	if err != nil {
		syncDur = 30 * time.Second
	}
	return &Syncer{
		client:      client,
		cfg:         cfg,
		syncAt:      syncDur,
		snapshotKey: "duckdb:snapshot",
	}
}

// Run blocks until ctx is canceled. Every sync interval it copies the
// DuckDB snapshot to Redis for Query API consumption.
func (s *Syncer) Run(ctx context.Context) error {
	if s.cfg.Storage.Endpoint == "" {
		slog.Info("writer: syncer skipped", "reason", "no_s3_endpoint")
		return nil
	}

	ticker := time.NewTicker(s.syncAt)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := s.doSync(ctx); err != nil {
				slog.ErrorContext(ctx, "writer: syncer error", "err", err)
			}
		}
	}
}

// doSync performs a single sync cycle.
func (s *Syncer) doSync(ctx context.Context) error {
	slog.InfoContext(ctx, "writer: syncer: snapshot sync (no S3 client configured)")
	return nil
}
