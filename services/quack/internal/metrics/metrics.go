package metrics

import (
	"fmt"

	"github.com/omneval/omneval/internal/lake/lakeserver"
	"github.com/prometheus/client_golang/prometheus"
)

var (
	// MaintenanceRuns counts Table Maintenance loop passes, labeled by
	// whether the pass skipped compaction (no new snapshot since the last
	// completed pass).
	MaintenanceRuns = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "omneval_quack",
			Name:      "maintenance_runs_total",
			Help:      "Total Table Maintenance loop passes.",
		},
		[]string{"skipped"},
	)

	// RetentionSpansDeleted counts spans deleted by Lake-native retention.
	RetentionSpansDeleted = prometheus.NewCounter(
		prometheus.CounterOpts{
			Namespace: "omneval_quack",
			Name:      "retention_spans_deleted_total",
			Help:      "Total spans deleted by Lake-native retention.",
		},
	)

	// RetentionScoresDeleted counts scores deleted by Lake-native retention.
	RetentionScoresDeleted = prometheus.NewCounter(
		prometheus.CounterOpts{
			Namespace: "omneval_quack",
			Name:      "retention_scores_deleted_total",
			Help:      "Total scores deleted by Lake-native retention.",
		},
	)

	// RetentionDuration tracks how long retention DELETEs took per pass.
	RetentionDuration = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Namespace: "omneval_quack",
			Name:      "retention_duration_seconds",
			Help:      "Duration of Lake-native retention DELETEs per maintenance pass.",
			Buckets:   prometheus.DefBuckets,
		},
	)
)

// Register registers all Prometheus metric families to the global registry.
func Register() error {
	if err := prometheus.Register(MaintenanceRuns); err != nil {
		return fmt.Errorf("register maintenance runs: %w", err)
	}
	if err := prometheus.Register(RetentionSpansDeleted); err != nil {
		return fmt.Errorf("register retention spans deleted: %w", err)
	}
	if err := prometheus.Register(RetentionScoresDeleted); err != nil {
		return fmt.Errorf("register retention scores deleted: %w", err)
	}
	if err := prometheus.Register(RetentionDuration); err != nil {
		return fmt.Errorf("register retention duration: %w", err)
	}
	return nil
}

// RecordMaintenanceResult records one Table Maintenance loop pass.
// retentionEnabled gates the retention sub-metrics: result.Retention is the
// zero value whenever retention is disabled, and recording zero-duration
// observations forever would be misleading.
func RecordMaintenanceResult(result lakeserver.MaintenanceResult, retentionEnabled bool) {
	MaintenanceRuns.WithLabelValues(boolLabel(result.Skipped)).Inc()
	if result.Skipped || !retentionEnabled {
		return
	}
	RetentionSpansDeleted.Add(float64(result.Retention.SpansDeleted))
	RetentionScoresDeleted.Add(float64(result.Retention.ScoresDeleted))
	RetentionDuration.Observe(result.Retention.Duration.Seconds())
}

func boolLabel(b bool) string {
	if b {
		return "true"
	}
	return "false"
}
