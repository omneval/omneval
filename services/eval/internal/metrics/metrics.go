package metrics

import (
	"fmt"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/omneval/omneval/internal/config"
)

var (
	// JobsProcessed counts total eval jobs processed, labeled by project_id and rule_id.
	JobsProcessed = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "omneval_eval",
			Name:      "jobs_processed_total",
			Help:      "Total number of eval jobs processed by the judge.",
		},
		[]string{"project_id", "rule_id"},
	)

	// JobDuration tracks the duration of a single eval job.
	JobDuration = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Namespace: "omneval_eval",
			Name:      "job_duration_seconds",
			Help:      "Duration of eval jobs in seconds.",
			Buckets:   prometheus.DefBuckets,
		},
	)

	// JudgeErrors counts total judge LLM errors.
	JudgeErrors = prometheus.NewCounter(
		prometheus.CounterOpts{
			Namespace: "omneval_eval",
			Name:      "judge_errors_total",
			Help:      "Total number of errors when calling the judge LLM.",
		},
	)
)

// Register registers all Prometheus metric families to the global registry.
func Register(disableProjectLabels bool) error {
	if err := prometheus.Register(JobsProcessed); err != nil {
		return fmt.Errorf("register jobs processed: %w", err)
	}
	if err := prometheus.Register(JobDuration); err != nil {
		return fmt.Errorf("register job duration: %w", err)
	}
	if err := prometheus.Register(JudgeErrors); err != nil {
		return fmt.Errorf("register judge errors: %w", err)
	}
	return nil
}

// NewEvalMetrics creates a metrics helper bound to the given config.
// When disableProjectLabels is true, counters are recorded without
// the project_id label.
func NewEvalMetrics(cfg *config.Config) *EvalMetrics {
	return &EvalMetrics{
		DisableProjectLabels: cfg.Metrics.DisableProjectLabels,
	}
}

// EvalMetrics is a helper for incrementing counters from the worker.
type EvalMetrics struct {
	DisableProjectLabels bool
}

// RecordJobProcessed records a successfully processed eval job.
func (m *EvalMetrics) RecordJobProcessed(projectID string, ruleID string) {
	if m.DisableProjectLabels {
		JobsProcessed.WithLabelValues("unlabeled", ruleID).Inc()
		return
	}
	JobsProcessed.WithLabelValues(projectID, ruleID).Inc()
}

// RecordJobDuration records the duration of an eval job.
func (m *EvalMetrics) RecordJobDuration(durationSec float64) {
	JobDuration.Observe(durationSec)
}

// RecordJudgeError increments the judge error counter.
func (m *EvalMetrics) RecordJudgeError() {
	JudgeErrors.Inc()
}
