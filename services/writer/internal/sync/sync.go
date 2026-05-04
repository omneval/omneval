package sync

import (
	"context"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/zbloss/lantern/internal/config"
	"github.com/zbloss/lantern/internal/storage"
)

// Syncer copies the live DuckDB file to S3 every sync interval so Query API
// replicas always have a fresh snapshot.
type Syncer struct {
	store        storage.ObjectStore
	dbPath       string
	cfg          *config.Config
	syncInterval time.Duration
	snapshotKey  string
	syncDuration *prometheus.HistogramVec
	syncTotal    prometheus.Counter
	syncFailures prometheus.Counter
}

// New creates a new Syncer.
func New(
	store storage.ObjectStore,
	dbPath string,
	cfg *config.Config,
	reg prometheus.Registerer,
) *Syncer {
	syncInterval, err := time.ParseDuration(cfg.Writer.SyncInterval)
	if err != nil {
		syncInterval = 30 * time.Second
	}

	snapshotKey := cfg.Storage.Bucket
	if cfg.Storage.Bucket != "" {
		parts := []string{"snapshots", "duckdb.db"}
		if cfg.Storage.Region != "" {
			parts = append([]string{cfg.Storage.Region}, parts...)
		}
		snapshotKey = strings.Join(parts, "/")
	} else {
		snapshotKey = "duckdb:snapshot"
	}

	syncDuration := prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: "lantern_writer",
			Name:      "snapshot_sync_duration_seconds",
			Help:      "Duration of DuckDB snapshot sync to S3 in seconds.",
			Buckets:   prometheus.DefBuckets,
		},
		[]string{"status"},
	)

	syncTotal := prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: "lantern_writer",
		Name:      "snapshot_sync_total",
		Help:      "Total number of snapshot sync attempts.",
	})

	syncFailures := prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: "lantern_writer",
		Name:      "snapshot_sync_failures_total",
		Help:      "Total number of failed snapshot sync attempts.",
	})

	if reg != nil {
		reg.MustRegister(syncDuration)
		reg.MustRegister(syncTotal)
		reg.MustRegister(syncFailures)
	}

	return &Syncer{
		store:        store,
		dbPath:       dbPath,
		cfg:          cfg,
		syncInterval: syncInterval,
		snapshotKey:  snapshotKey,
		syncDuration: syncDuration,
		syncTotal:    syncTotal,
		syncFailures: syncFailures,
	}
}

// Run blocks until ctx is canceled. Every sync interval it copies the
// DuckDB snapshot to S3. On shutdown it performs one final sync.
func (s *Syncer) Run(ctx context.Context) error {
	if s.store == nil {
		slog.Info("writer: syncer skipped", "reason", "no_object_store")
		return nil
	}

	ticker := time.NewTicker(s.syncInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			slog.InfoContext(ctx, "writer: syncer: shutting down, performing final sync")
			s.doSync(ctx)
			return ctx.Err()
		case <-ticker.C:
			s.doSync(ctx)
		}
	}
}

// doSync performs a single sync cycle. It logs errors at Warn level and
// does not panic — the next tick will retry.
func (s *Syncer) doSync(ctx context.Context) {
	s.syncTotal.Inc()
	start := time.Now()

	info, err := os.Stat(s.dbPath)
	if err != nil {
		slog.WarnContext(ctx, "writer: syncer: cannot stat DuckDB file",
			"db_path", s.dbPath,
			"err", err,
		)
		s.recordDuration(start, "error")
		return
	}

	if info.IsDir() {
		slog.WarnContext(ctx, "writer: syncer: DuckDB path is a directory",
			"db_path", s.dbPath,
		)
		s.recordDuration(start, "error")
		return
	}

	f, err := os.Open(s.dbPath)
	if err != nil {
		slog.WarnContext(ctx, "writer: syncer: cannot open DuckDB file",
			"db_path", s.dbPath,
			"err", err,
		)
		s.recordDuration(start, "error")
		return
	}
	defer f.Close()

	if err := s.store.Put(ctx, s.snapshotKey, f); err != nil {
		slog.WarnContext(ctx, "writer: syncer: upload failed",
			"key", s.snapshotKey,
			"err", err,
		)
		s.syncFailures.Inc()
		s.recordDuration(start, "error")
		return
	}

	slog.InfoContext(ctx, "writer: syncer: snapshot synced",
		"key", s.snapshotKey,
		"size_bytes", info.Size(),
	)
	s.recordDuration(start, "success")
}

func (s *Syncer) recordDuration(start time.Time, status string) {
	elapsed := time.Since(start).Seconds()
	s.syncDuration.WithLabelValues(status).Observe(elapsed)
}
