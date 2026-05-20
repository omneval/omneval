package metrics

import (
	"fmt"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/omneval/omneval/internal/config"
)

var (
	// QueryDuration tracks the duration of span query requests (legacy).
	QueryDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: "omneval_query",
			Name:      "span_duration_seconds",
			Help:      "Duration of span query requests.",
			Buckets:   prometheus.DefBuckets,
		},
		[]string{"status"},
	)

	// QueryErrors tracks the total number of span query errors.
	QueryErrors = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "omneval_query",
			Name:      "span_errors_total",
			Help:      "Total number of span query errors.",
		},
		[]string{"reason"},
	)

	// SnapshotDownloads tracks the total number of snapshot downloads.
	SnapshotDownloads = prometheus.NewCounter(
		prometheus.CounterOpts{
			Namespace: "omneval_query",
			Name:      "snapshot_downloads_total",
			Help:      "Total number of snapshot downloads from S3.",
		},
	)

	// SnapshotDownloadsFailed tracks the total number of failed snapshot downloads.
	SnapshotDownloadsFailed = prometheus.NewCounter(
		prometheus.CounterOpts{
			Namespace: "omneval_query",
			Name:      "snapshot_downloads_failed_total",
			Help:      "Total number of failed snapshot downloads from S3.",
		},
	)

	// RequestDuration tracks the duration of all API requests, labeled by endpoint.
	RequestDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: "omneval_query",
			Name:      "request_duration_seconds",
			Help:      "Duration of Query API requests in seconds.",
			Buckets:   prometheus.DefBuckets,
		},
		[]string{"endpoint"},
	)

	// SnapshotAge tracks how stale the current DuckDB snapshot is (in seconds).
	SnapshotAge = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Namespace: "omneval_query",
			Name:      "snapshot_age_seconds",
			Help:      "How stale the current DuckDB snapshot is, in seconds since LastModified.",
		},
	)
)

// Register registers all Prometheus metric families to the global registry.
func Register(_ *config.Config) error {
	if err := prometheus.Register(QueryDuration); err != nil {
		return fmt.Errorf("register query duration: %w", err)
	}
	if err := prometheus.Register(QueryErrors); err != nil {
		return fmt.Errorf("register query errors: %w", err)
	}
	if err := prometheus.Register(SnapshotDownloads); err != nil {
		return fmt.Errorf("register snapshot downloads: %w", err)
	}
	if err := prometheus.Register(SnapshotDownloadsFailed); err != nil {
		return fmt.Errorf("register snapshot download failures: %w", err)
	}
	if err := prometheus.Register(RequestDuration); err != nil {
		return fmt.Errorf("register request duration: %w", err)
	}
	if err := prometheus.Register(SnapshotAge); err != nil {
		return fmt.Errorf("register snapshot age: %w", err)
	}
	return nil
}

// NewQueryMetrics creates a metrics helper bound to the given config.
func NewQueryMetrics(cfg *config.Config) *QueryMetrics {
	return &QueryMetrics{}
}

// QueryMetrics is a helper for incrementing counters and recording durations
// from the handler and server.
type QueryMetrics struct{}

// RecordRequestDuration records the duration of an API request.
func (m *QueryMetrics) RecordRequestDuration(endpoint string, durationSec float64) {
	RequestDuration.WithLabelValues(endpoint).Observe(durationSec)
}

// RecordSnapshotAge records how stale the snapshot is in seconds.
func (m *QueryMetrics) RecordSnapshotAge(seconds float64) {
	SnapshotAge.Set(seconds)
}
