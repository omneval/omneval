// Package reconcile implements the Ingest Buffer reconciliation sweep
// (ADR-0004, #88): a leader-elected job that recovers buffered batches whose
// queue reference was lost (writer crash after dequeue, Redis restart) by
// re-enqueueing their Batch ID, and garbage-collects committed buffer
// objects past their retention window so the buffer stays bounded.
package reconcile

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/omneval/omneval/internal/buffer"
	"github.com/omneval/omneval/internal/config"
	"github.com/omneval/omneval/internal/leader"
	"github.com/omneval/omneval/internal/queue"
	s3pkg "github.com/omneval/omneval/internal/storage/s3"
)

// Store abstracts the S3 operations needed for the sweep.
type Store interface {
	ListObjectsOlderThan(ctx context.Context, prefix string, cutoff time.Time) ([]s3pkg.ObjectInfo, error)
	DeleteObjectsBatch(ctx context.Context, bucket string, keys []string) error
}

// Ledger is the Batch Ledger lookup the sweep uses to tell recovered
// batches apart from ones already committed to the Lake. Satisfied by
// metadata.Store.
type Ledger interface {
	IsBatchCommitted(ctx context.Context, batchID string) (bool, error)
}

// RefEnqueuer pushes recovered Batch ID references back onto the ingest
// queue. Satisfied by *redis.IngestQueue.
type RefEnqueuer interface {
	EnqueueRef(ctx context.Context, ref queue.BatchRef) error
}

// Metrics receives sweep outcomes. Implemented by the Writer's metrics
// helper; nil-safe.
type Metrics interface {
	RecordReconcileBatchesRecovered(count int)
	RecordReconcileObjectsDeleted(count int)
	RecordReconcileSweepDuration(durationSec float64)
}

// Result summarizes one sweep run.
type Result struct {
	BatchesRecovered int
	ObjectsDeleted   int
	Duration         time.Duration
	Errors           []error
}

// Worker runs the Ingest Buffer reconciliation sweep.
type Worker struct {
	store   Store
	ledger  Ledger
	refs    RefEnqueuer
	metrics Metrics
	cfg     *config.ReconciliationConfig

	mu        sync.Mutex
	lastRunAt time.Time
}

// New creates a reconciliation Worker. Returns nil if reconciliation is
// disabled in cfg. metrics may be nil.
func New(store Store, ledger Ledger, refs RefEnqueuer, metrics Metrics, cfg *config.ReconciliationConfig) *Worker {
	if !cfg.Enabled {
		return nil
	}
	return &Worker{
		store:   store,
		ledger:  ledger,
		refs:    refs,
		metrics: metrics,
		cfg:     cfg,
	}
}

// LastRunAt returns the time of the last completed sweep.
func (w *Worker) LastRunAt() time.Time {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.lastRunAt
}

// Run executes a single reconciliation sweep and returns the result.
//
// It lists every Ingest Buffer object older than the configured grace
// period. For each object whose Batch ID is absent from the Batch Ledger,
// it re-enqueues the reference (recovery). For each object whose Batch ID
// *is* in the ledger and whose age exceeds the retention window, it deletes
// the object (GC). Uncommitted objects are never deleted, and a batch
// already in the ledger is never re-enqueued.
func (w *Worker) Run(ctx context.Context) (Result, error) {
	start := time.Now()
	result := Result{}

	grace := time.Duration(w.cfg.GracePeriodMinutes) * time.Minute
	retention := time.Duration(w.cfg.RetentionHours) * time.Hour
	cutoff := time.Now().Add(-grace)

	objects, err := w.store.ListObjectsOlderThan(ctx, buffer.Prefix, cutoff)
	if err != nil {
		return result, fmt.Errorf("reconcile: list objects: %w", err)
	}

	var toDelete []string
	var deleteBucket string
	now := time.Now()
	for _, obj := range objects {
		batchID, ok := buffer.BatchIDFromKey(obj.Key)
		if !ok {
			continue
		}

		committed, err := w.ledger.IsBatchCommitted(ctx, batchID)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Errorf("reconcile: check ledger for %s: %w", batchID, err))
			continue
		}

		if !committed {
			if err := w.refs.EnqueueRef(ctx, queue.BatchRef{BatchID: batchID}); err != nil {
				result.Errors = append(result.Errors, fmt.Errorf("reconcile: recover %s: %w", batchID, err))
				continue
			}
			result.BatchesRecovered++
			continue
		}

		if now.Sub(obj.LastModified) >= retention {
			toDelete = append(toDelete, obj.Key)
			deleteBucket = obj.Bucket
		}
	}

	if len(toDelete) > 0 {
		if err := w.store.DeleteObjectsBatch(ctx, deleteBucket, toDelete); err != nil {
			result.Errors = append(result.Errors, fmt.Errorf("reconcile: delete batch: %w", err))
		} else {
			result.ObjectsDeleted = len(toDelete)
		}
	}

	result.Duration = time.Since(start)

	w.mu.Lock()
	w.lastRunAt = time.Now()
	w.mu.Unlock()

	if w.metrics != nil {
		w.metrics.RecordReconcileBatchesRecovered(result.BatchesRecovered)
		w.metrics.RecordReconcileObjectsDeleted(result.ObjectsDeleted)
		w.metrics.RecordReconcileSweepDuration(result.Duration.Seconds())
	}

	if len(result.Errors) > 0 {
		slog.Warn("reconcile: sweep completed with errors",
			"recovered", result.BatchesRecovered,
			"deleted", result.ObjectsDeleted,
			"duration", result.Duration,
			"errors", len(result.Errors),
		)
		return result, fmt.Errorf("reconcile: sweep completed with %d error(s): %v", len(result.Errors), result.Errors)
	}

	slog.Info("reconcile: sweep complete",
		"recovered", result.BatchesRecovered,
		"deleted", result.ObjectsDeleted,
		"duration", result.Duration,
	)
	return result, nil
}

// RunLoop starts the sweep ticker. It blocks until ctx is canceled. When
// election is non-nil, only the leader runs each tick; followers skip it.
func (w *Worker) RunLoop(ctx context.Context, election *leader.LeaderElection) error {
	interval := time.Duration(w.cfg.IntervalMinutes) * time.Minute

	slog.Info("reconcile: ticker started",
		"interval_minutes", w.cfg.IntervalMinutes,
		"grace_period_minutes", w.cfg.GracePeriodMinutes,
		"retention_hours", w.cfg.RetentionHours,
	)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			slog.Info("reconcile: ticker stopped (context canceled)")
			return ctx.Err()
		case <-ticker.C:
			if election != nil && !election.IsLeader() {
				slog.Debug("reconcile: not leader, skipping sweep")
				continue
			}
			if _, err := w.Run(ctx); err != nil {
				slog.Error("reconcile: sweep error", "err", err)
			}
		}
	}
}
