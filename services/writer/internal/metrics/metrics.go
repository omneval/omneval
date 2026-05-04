package metrics

import "github.com/prometheus/client_golang/prometheus"

// Register initialises and registers all Prometheus metric families for the
// Writer service. Metrics are registered against the provided registry.
func Register(reg prometheus.Registerer) error {
	// Snapshot sync duration histogram.
	syncDuration := prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: "lantern_writer",
			Name:      "snapshot_sync_duration_seconds",
			Help:      "Duration of DuckDB snapshot sync to S3 in seconds.",
			Buckets:   prometheus.DefBuckets,
		},
		[]string{"status"},
	)
	if err := reg.Register(syncDuration); err != nil {
		return err
	}
	_ = syncDuration // used at registration time

	return nil
}
