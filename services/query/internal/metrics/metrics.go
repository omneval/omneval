package metrics

import (
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

// Register initialises and registers all Prometheus metric families.
// Metrics are labeled by project_id where applicable.
func Register() error {
	reg := prometheus.NewRegistry()
	if err := reg.Register(QueryDuration); err != nil {
		return err
	}
	if err := reg.Register(QueryErrors); err != nil {
		return err
	}
	if err := reg.Register(SnapshotDownloads); err != nil {
		return err
	}
	if err := reg.Register(SnapshotDownloadsFailed); err != nil {
		return err
	}
	return nil
}
