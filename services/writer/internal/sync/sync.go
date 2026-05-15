package sync

import (
	"context"
	"database/sql"
	"log/slog"
	"os"
	"time"

	"github.com/zbloss/lantern/internal/config"
	"github.com/zbloss/lantern/internal/storage"
	s3 "github.com/zbloss/lantern/internal/storage/s3"
	"github.com/zbloss/lantern/services/writer/internal/metrics"
)

// Syncer copies the live DuckDB file to S3 every sync interval so Query API
// replicas always have a fresh snapshot.
type Syncer struct {
	store        storage.ObjectStore
	db           *sql.DB
	dbPath       string
	cfg          *config.Config
	syncInterval time.Duration
	snapshotKey  string
	metrics      *metrics.WriterMetrics
	// lastSyncMod is the mtime of the last successfully uploaded snapshot.
	// A subsequent sync with the same mtime is skipped.
	lastSyncMod time.Time
}

// New creates a new Syncer.
func New(
	store storage.ObjectStore,
	db *sql.DB,
	dbPath string,
	cfg *config.Config,
	m *metrics.WriterMetrics,
) *Syncer {
	syncInterval, err := time.ParseDuration(cfg.Writer.SyncInterval)
	if err != nil {
		syncInterval = 30 * time.Second
	}

	snapshotKey := s3.SnapshotKey()

	return &Syncer{
		store:        store,
		db:           db,
		dbPath:       dbPath,
		cfg:          cfg,
		syncInterval: syncInterval,
		snapshotKey:  snapshotKey,
		metrics:      m,
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

// doSync performs a single sync cycle. It checkpoints the DuckDB WAL into
// the main file before uploading, ensuring no committed writes are left
// behind in the WAL when the snapshot is taken.
func (s *Syncer) doSync(ctx context.Context) {
	start := time.Now()

	if s.db != nil {
		if _, err := s.db.ExecContext(ctx, "CHECKPOINT"); err != nil {
			slog.WarnContext(ctx, "writer: syncer: checkpoint failed, uploading without checkpoint",
				"db_path", s.dbPath,
				"err", err,
			)
		}
	}

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

	// Check whether the file has changed since the last sync.
	if !info.ModTime().After(s.lastSyncMod) {
		slog.InfoContext(ctx, "writer: syncer: snapshot unchanged, skipping upload",
			"db_path", s.dbPath,
			"size_bytes", info.Size(),
		)
		s.recordDuration(start, "skipped")
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
		s.recordDuration(start, "error")
		return
	}

	s.lastSyncMod = info.ModTime()

	slog.InfoContext(ctx, "writer: syncer: snapshot synced",
		"key", s.snapshotKey,
		"size_bytes", info.Size(),
		"mod_time", s.lastSyncMod,
	)
	s.recordDuration(start, "success")
}

func (s *Syncer) recordDuration(start time.Time, status string) {
	elapsed := time.Since(start).Seconds()
	if s.metrics != nil {
		s.metrics.RecordSnapshotSyncDuration(elapsed, status)
	}
}
