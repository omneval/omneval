package metrics

import (
	"fmt"

	"github.com/omneval/omneval/internal/config"
	"github.com/prometheus/client_golang/prometheus"
)

var (
	// SpansWritten counts total spans written to DuckDB, labeled by project_id.
	SpansWritten = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "omneval_writer",
			Name:      "spans_written_total",
			Help:      "Total number of spans written to DuckDB.",
		},
		[]string{"project_id"},
	)

	// DuckDBWriteDuration tracks the duration of DuckDB write transactions.
	DuckDBWriteDuration = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Namespace: "omneval_writer",
			Name:      "duckdb_write_duration_seconds",
			Help:      "Duration of DuckDB write transactions in seconds.",
			Buckets:   prometheus.DefBuckets,
		},
	)

	// SnapshotSyncDuration tracks the duration of S3 snapshot syncs.
	SnapshotSyncDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: "omneval_writer",
			Name:      "snapshot_sync_duration_seconds",
			Help:      "Duration of DuckDB snapshot sync to S3 in seconds.",
			Buckets:   prometheus.DefBuckets,
		},
		[]string{"status"},
	)

	// ArchiveSpans counts total spans archived to Parquet, labeled by project_id.
	ArchiveSpans = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "omneval_writer",
			Name:      "archive_spans_total",
			Help:      "Total number of spans archived to Parquet on S3.",
		},
		[]string{"project_id"},
	)

	// DequeueErrors counts total dequeue failures from the Redis ingest queue.
	DequeueErrors = prometheus.NewCounter(
		prometheus.CounterOpts{
			Namespace: "omneval_writer",
			Name:      "dequeue_errors_total",
			Help:      "Total number of errors encountered when dequeuing from the Redis ingest queue.",
		},
	)

	// WriteErrors counts total DuckDB write failures.
	WriteErrors = prometheus.NewCounter(
		prometheus.CounterOpts{
			Namespace: "omneval_writer",
			Name:      "write_errors_total",
			Help:      "Total number of errors encountered when writing spans to DuckDB.",
		},
	)

	// LakeWriteErrors counts Lake (DuckLake) write failures, labeled by
	// table. Separate from WriteErrors so dual-write dashboards can
	// distinguish lake-write failures from legacy-write failures.
	LakeWriteErrors = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "omneval_writer",
			Name:      "lake_write_errors_total",
			Help:      "Total number of errors encountered when writing to the Lake (DuckLake).",
		},
		[]string{"table"},
	)

	// LakeWriteDuration tracks the duration of Lake commit transactions.
	LakeWriteDuration = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Namespace: "omneval_writer",
			Name:      "lake_write_duration_seconds",
			Help:      "Duration of Lake (DuckLake) write transactions in seconds.",
			Buckets:   prometheus.DefBuckets,
		},
	)

	// LedgerSkips counts redelivered batches skipped because their Batch ID
	// was already in the Batch Ledger (ADR-0004).
	LedgerSkips = prometheus.NewCounter(
		prometheus.CounterOpts{
			Namespace: "omneval_writer",
			Name:      "ledger_skips_total",
			Help:      "Total redelivered batches skipped via the Batch Ledger.",
		},
	)

	// BufferFetchErrors counts failures fetching staged batches from the
	// Ingest Buffer.
	BufferFetchErrors = prometheus.NewCounter(
		prometheus.CounterOpts{
			Namespace: "omneval_writer",
			Name:      "buffer_fetch_errors_total",
			Help:      "Total errors fetching staged batches from the Ingest Buffer.",
		},
	)

	// ReconcileBatchesRecovered counts Ingest Buffer batches whose queue
	// reference was lost and was re-enqueued by the reconciliation sweep (#88).
	ReconcileBatchesRecovered = prometheus.NewCounter(
		prometheus.CounterOpts{
			Namespace: "omneval_writer",
			Name:      "reconcile_batches_recovered_total",
			Help:      "Total Ingest Buffer batches recovered by the reconciliation sweep.",
		},
	)

	// ReconcileObjectsDeleted counts committed Ingest Buffer objects deleted
	// by the reconciliation sweep's retention GC (#88).
	ReconcileObjectsDeleted = prometheus.NewCounter(
		prometheus.CounterOpts{
			Namespace: "omneval_writer",
			Name:      "reconcile_objects_deleted_total",
			Help:      "Total Ingest Buffer objects deleted by the reconciliation sweep's retention GC.",
		},
	)

	// ReconcileSweepDuration tracks the duration of reconciliation sweep runs.
	ReconcileSweepDuration = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Namespace: "omneval_writer",
			Name:      "reconcile_sweep_duration_seconds",
			Help:      "Duration of Ingest Buffer reconciliation sweep runs in seconds.",
			Buckets:   prometheus.DefBuckets,
		},
	)
)

// Register registers all Prometheus metric families to the global registry.
func Register(disableProjectLabels bool) error {
	if err := prometheus.Register(SpansWritten); err != nil {
		return fmt.Errorf("register spans written: %w", err)
	}
	if err := prometheus.Register(DuckDBWriteDuration); err != nil {
		return fmt.Errorf("register duckdb write duration: %w", err)
	}
	if err := prometheus.Register(SnapshotSyncDuration); err != nil {
		return fmt.Errorf("register snapshot sync duration: %w", err)
	}
	if err := prometheus.Register(ArchiveSpans); err != nil {
		return fmt.Errorf("register archive spans: %w", err)
	}
	if err := prometheus.Register(DequeueErrors); err != nil {
		return fmt.Errorf("register dequeue errors: %w", err)
	}
	if err := prometheus.Register(WriteErrors); err != nil {
		return fmt.Errorf("register write errors: %w", err)
	}
	if err := prometheus.Register(LakeWriteErrors); err != nil {
		return fmt.Errorf("register lake write errors: %w", err)
	}
	if err := prometheus.Register(LakeWriteDuration); err != nil {
		return fmt.Errorf("register lake write duration: %w", err)
	}
	if err := prometheus.Register(LedgerSkips); err != nil {
		return fmt.Errorf("register ledger skips: %w", err)
	}
	if err := prometheus.Register(BufferFetchErrors); err != nil {
		return fmt.Errorf("register buffer fetch errors: %w", err)
	}
	if err := prometheus.Register(ReconcileBatchesRecovered); err != nil {
		return fmt.Errorf("register reconcile batches recovered: %w", err)
	}
	if err := prometheus.Register(ReconcileObjectsDeleted); err != nil {
		return fmt.Errorf("register reconcile objects deleted: %w", err)
	}
	if err := prometheus.Register(ReconcileSweepDuration); err != nil {
		return fmt.Errorf("register reconcile sweep duration: %w", err)
	}
	return nil
}

// NewWriterMetrics creates a metrics helper bound to the given config.
// When disableProjectLabels is true, counters are recorded without
// the project_id label (single "unlabeled" bucket).
func NewWriterMetrics(cfg *config.Config) *WriterMetrics {
	return &WriterMetrics{
		DisableProjectLabels: cfg.Metrics.DisableProjectLabels,
	}
}

// WriterMetrics is a helper for incrementing counters from the pipeline.
type WriterMetrics struct {
	DisableProjectLabels bool
}

// RecordSpansWritten records a batch of spans written to DuckDB.
func (m *WriterMetrics) RecordSpansWritten(projectID string, count int) {
	if m.DisableProjectLabels {
		SpansWritten.WithLabelValues("unlabeled").Add(float64(count))
		return
	}
	SpansWritten.WithLabelValues(projectID).Add(float64(count))
}

// RecordDuckDBWriteDuration records the duration of a DuckDB write transaction.
func (m *WriterMetrics) RecordDuckDBWriteDuration(durationSec float64) {
	DuckDBWriteDuration.Observe(durationSec)
}

// RecordSnapshotSyncDuration records the duration of a snapshot sync.
func (m *WriterMetrics) RecordSnapshotSyncDuration(durationSec float64, status string) {
	SnapshotSyncDuration.WithLabelValues(status).Observe(durationSec)
}

// RecordArchiveSpans records archived spans.
func (m *WriterMetrics) RecordArchiveSpans(projectID string, count int) {
	if m.DisableProjectLabels {
		ArchiveSpans.WithLabelValues("unlabeled").Add(float64(count))
		return
	}
	ArchiveSpans.WithLabelValues(projectID).Add(float64(count))
}

// RecordDequeueError increments the dequeue error counter.
func (m *WriterMetrics) RecordDequeueError() {
	DequeueErrors.Inc()
}

// RecordWriteError increments the write error counter.
func (m *WriterMetrics) RecordWriteError() {
	WriteErrors.Inc()
}

// RecordLakeWriteError increments the Lake write error counter for the
// given table ("spans" or "scores").
func (m *WriterMetrics) RecordLakeWriteError(table string) {
	LakeWriteErrors.WithLabelValues(table).Inc()
}

// RecordLakeWriteDuration records the duration of a Lake write transaction.
func (m *WriterMetrics) RecordLakeWriteDuration(durationSec float64) {
	LakeWriteDuration.Observe(durationSec)
}

// RecordLedgerSkip increments the Batch Ledger skip counter.
func (m *WriterMetrics) RecordLedgerSkip() {
	LedgerSkips.Inc()
}

// RecordBufferFetchError increments the Ingest Buffer fetch error counter.
func (m *WriterMetrics) RecordBufferFetchError() {
	BufferFetchErrors.Inc()
}

// RecordReconcileBatchesRecovered adds to the reconciliation sweep's
// recovered-batch counter.
func (m *WriterMetrics) RecordReconcileBatchesRecovered(count int) {
	ReconcileBatchesRecovered.Add(float64(count))
}

// RecordReconcileObjectsDeleted adds to the reconciliation sweep's
// deleted-object counter.
func (m *WriterMetrics) RecordReconcileObjectsDeleted(count int) {
	ReconcileObjectsDeleted.Add(float64(count))
}

// RecordReconcileSweepDuration records the duration of a reconciliation
// sweep run.
func (m *WriterMetrics) RecordReconcileSweepDuration(durationSec float64) {
	ReconcileSweepDuration.Observe(durationSec)
}
