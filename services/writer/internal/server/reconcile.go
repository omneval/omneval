package server

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"time"

	"github.com/zbloss/lantern/internal/storage"
)

// Reconciler compares the local DuckDB file's mtime with the S3 snapshot's
// LastModified and downloads the S3 snapshot when S3 is newer. This prevents
// dual-writer data corruption during leader failover.
type Reconciler struct {
	store     storage.ObjectStore
	dbPath    string
	snapshotKey string
}

// NewReconciler creates a new reconciler.
func NewReconciler(store storage.ObjectStore, dbPath, snapshotKey string) *Reconciler {
	return &Reconciler{
		store:       store,
		dbPath:      dbPath,
		snapshotKey: snapshotKey,
	}
}

// Reconcile checks whether the S3 snapshot is newer than the local DuckDB file.
// If S3 is newer (or the local file doesn't exist), it downloads the snapshot
// and overwrites the local file. Returns nil when reconciliation succeeds or
// is not needed.
func (r *Reconciler) Reconcile(ctx context.Context) error {
	if r.store == nil {
		return nil // no object store, nothing to reconcile
	}

	localMtime, err := r.localMtime()
	if err != nil {
		// Local file doesn't exist or can't be read — always download.
		slog.Info("writer: reconciler: local file missing, downloading snapshot",
			"path", r.dbPath, "err", err)
		return r.downloadSnapshot(ctx)
	}

	s3Stat, err := r.store.Stat(ctx, r.snapshotKey)
	if err != nil {
		return fmt.Errorf("reconciler: stat s3 snapshot %s: %w", r.snapshotKey, err)
	}

	// If S3 snapshot is newer, download it.
	if s3Stat.LastModified.After(localMtime) {
		slog.Info("writer: reconciler: S3 snapshot is newer, downloading",
			"path", r.dbPath,
			"local_mtime", localMtime,
			"s3_mtime", s3Stat.LastModified,
		)
		return r.downloadSnapshot(ctx)
	}

	slog.Info("writer: reconciler: local file is up to date",
		"path", r.dbPath, "mtime", localMtime)
	return nil
}

// localMtime returns the modification time of the local DuckDB file.
// Returns an error if the file doesn't exist or can't be stat'd.
func (r *Reconciler) localMtime() (time.Time, error) {
	info, err := os.Stat(r.dbPath)
	if err != nil {
		return time.Time{}, err
	}
	return info.ModTime(), nil
}

// downloadSnapshot downloads the S3 snapshot and overwrites the local DuckDB file.
func (r *Reconciler) downloadSnapshot(ctx context.Context) error {
	reader, err := r.store.Get(ctx, r.snapshotKey)
	if err != nil {
		return fmt.Errorf("reconciler: get s3 snapshot %s: %w", r.snapshotKey, err)
	}
	defer reader.Close()

	// Read the entire snapshot into memory.
	data, err := io.ReadAll(reader)
	if err != nil {
		return fmt.Errorf("reconciler: read snapshot: %w", err)
	}

	// Write atomically: write to a temp file, then rename.
	tmpPath := r.dbPath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return fmt.Errorf("reconciler: write tmp file %s: %w", tmpPath, err)
	}

	if err := os.Rename(tmpPath, r.dbPath); err != nil {
		// Clean up temp file on failure.
		_ = os.Remove(tmpPath)
		return fmt.Errorf("reconciler: rename tmp to %s: %w", r.dbPath, err)
	}

	slog.Info("writer: reconciler: snapshot downloaded", "path", r.dbPath)
	return nil
}
