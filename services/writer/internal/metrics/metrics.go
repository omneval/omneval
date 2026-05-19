package metrics

import (
	"fmt"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/omneval/omneval/internal/config"
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
