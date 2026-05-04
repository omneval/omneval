package metrics

import (
	"fmt"

	"github.com/prometheus/client_golang/prometheus"
)

var (
	// QueryDuration tracks the duration of span query requests.
	QueryDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "lantern_query_span_duration_seconds",
			Help:    "Duration of span query requests",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"status"},
	)

	// QueryErrors tracks the total number of span query errors.
	QueryErrors = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "lantern_query_span_errors_total",
			Help: "Total number of span query errors",
		},
		[]string{"reason"},
	)

	// SnapshotDownloads tracks the total number of snapshot downloads.
	SnapshotDownloads = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "lantern_query_snapshot_downloads_total",
			Help: "Total number of snapshot downloads from S3",
		},
	)

	// SnapshotDownloadsFailed tracks the total number of failed snapshot downloads.
	SnapshotDownloadsFailed = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "lantern_query_snapshot_downloads_failed_total",
			Help: "Total number of failed snapshot downloads from S3",
		},
	)
)

// Register registers all Prometheus metric families to the global registry
// so they appear at /metrics via promhttp.Handler().
func Register() error {
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
	return nil
}
