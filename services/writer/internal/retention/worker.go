package retention

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/omneval/omneval/internal/config"
	"github.com/omneval/omneval/internal/domain"
	s3pkg "github.com/omneval/omneval/internal/storage/s3"
)

// Store abstracts the S3 operations needed for retention.
type Store interface {
	ListObjectsOlderThan(ctx context.Context, prefix string, cutoff time.Time) ([]s3pkg.ObjectInfo, error)
	CopyObject(ctx context.Context, dstBucket, dstKey, srcKey, storageClass string) error
	DeleteObjectsBatch(ctx context.Context, bucket string, keys []string) error
}

// Worker scans S3 for objects older than the configured MaxAgeDays and applies
// the configured retention action (delete or move).
type Worker struct {
	store Store
	cfg   *config.RetentionConfig
}

// New creates a retention Worker. Returns nil if retention is disabled in cfg.
func New(store Store, cfg *config.RetentionConfig) *Worker {
	if !cfg.Enabled {
		return nil
	}
	return &Worker{
		store: store,
		cfg:   cfg,
	}
}

// Run executes a single retention pass and returns the result.
func (w *Worker) Run(ctx context.Context) (domain.RotationResult, error) {
	start := time.Now()
	result := domain.RotationResult{}

	cutoff := time.Now().AddDate(0, 0, -w.cfg.MaxAgeDays)

	objects, err := w.store.ListObjectsOlderThan(ctx, "", cutoff)
	if err != nil {
		return result, fmt.Errorf("retention: list objects: %w", err)
	}

	result.ObjectsScanned = len(objects)
	if len(objects) == 0 {
		result.Duration = time.Since(start)
		slog.Info("retention: no eligible objects", "max_age_days", w.cfg.MaxAgeDays)
		return result, nil
	}

	switch w.cfg.Action {
	case "delete":
		err = w.deleteObjects(ctx, objects, &result)
	case "move":
		err = w.moveObjects(ctx, objects, &result)
	default:
		return result, fmt.Errorf("retention: unknown action %q", w.cfg.Action)
	}

	if err != nil {
		result.Errors = append(result.Errors, err)
		return result, err
	}

	result.Duration = time.Since(start)
	slog.Info("retention: run complete",
		"action", w.cfg.Action,
		"scanned", result.ObjectsScanned,
		"acted_on", result.ObjectsActedOn,
		"bytes_acted_on", result.BytesActedOn,
		"duration", result.Duration,
	)
	return result, nil
}

// RunLoop starts the retention ticker. It blocks until ctx is canceled.
func (w *Worker) RunLoop(ctx context.Context) error {
	interval := time.Duration(w.cfg.IntervalMinutes) * time.Minute

	slog.Info("retention: ticker started",
		"action", w.cfg.Action,
		"interval_minutes", w.cfg.IntervalMinutes,
		"max_age_days", w.cfg.MaxAgeDays,
	)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			slog.Info("retention: ticker stopped (context canceled)")
			return ctx.Err()
		case <-ticker.C:
			if _, err := w.Run(ctx); err != nil {
				slog.Error("retention: run error", "err", err)
			}
		}
	}
}

// deleteObjects removes the given objects from their buckets.
func (w *Worker) deleteObjects(ctx context.Context, objects []s3pkg.ObjectInfo, result *domain.RotationResult) error {
	keys := make([]string, 0, len(objects))
	var totalBytes int64
	for _, obj := range objects {
		keys = append(keys, obj.Key)
		totalBytes += obj.Size
	}

	if err := w.store.DeleteObjectsBatch(ctx, objects[0].Bucket, keys); err != nil {
		return fmt.Errorf("retention: delete batch: %w", err)
	}

	result.ObjectsActedOn = len(objects)
	result.BytesActedOn = totalBytes
	return nil
}

// moveObjects copies each object to the configured destination bucket, then
// deletes the source objects.
func (w *Worker) moveObjects(ctx context.Context, objects []s3pkg.ObjectInfo, result *domain.RotationResult) error {
	dstBucket := w.cfg.Destination.Bucket
	dstPrefix := w.cfg.Destination.Prefix
	storageClass := w.cfg.Destination.StorageClass

	for _, obj := range objects {
		dstKey := dstPrefix + obj.Key
		if err := w.store.CopyObject(ctx, dstBucket, dstKey, obj.Key, storageClass); err != nil {
			return fmt.Errorf("retention: copy %s: %w", obj.Key, err)
		}
	}

	keys := make([]string, 0, len(objects))
	var totalBytes int64
	for _, obj := range objects {
		keys = append(keys, obj.Key)
		totalBytes += obj.Size
	}

	if err := w.store.DeleteObjectsBatch(ctx, objects[0].Bucket, keys); err != nil {
		return fmt.Errorf("retention: delete source batch: %w", err)
	}

	result.ObjectsActedOn = len(objects)
	result.BytesActedOn = totalBytes
	return nil
}
