package metrics

import (
	"fmt"

	"github.com/omneval/omneval/internal/config"
	"github.com/prometheus/client_golang/prometheus"
)

var (
	// SpansReceived counts total spans received, labeled by project_id.
	SpansReceived = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "omneval_ingest",
			Name:      "spans_received_total",
			Help:      "Total number of spans received by the ingest API.",
		},
		[]string{"project_id"},
	)

	// EnqueueErrors counts total enqueue failures.
	EnqueueErrors = prometheus.NewCounter(
		prometheus.CounterOpts{
			Namespace: "omneval_ingest",
			Name:      "enqueue_errors_total",
			Help:      "Total number of errors when enqueuing spans to Redis.",
		},
	)

	// RequestDuration tracks request processing time.
	RequestDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: "omneval_ingest",
			Name:      "request_duration_seconds",
			Help:      "Duration of ingest API requests in seconds.",
			Buckets:   prometheus.DefBuckets,
		},
		[]string{},
	)
)

// Register registers all Prometheus metric families to the global registry.
func Register(disableProjectLabels bool) error {
	if err := prometheus.Register(SpansReceived); err != nil {
		return fmt.Errorf("register spans received: %w", err)
	}
	if err := prometheus.Register(EnqueueErrors); err != nil {
		return fmt.Errorf("register enqueue errors: %w", err)
	}
	if err := prometheus.Register(RequestDuration); err != nil {
		return fmt.Errorf("register request duration: %w", err)
	}
	return nil
}

// NewIngestMetrics creates a metrics helper bound to the given config.
// When disableProjectLabels is true, SpansReceived is recorded without
// the project_id label (single "unlabeled" bucket).
func NewIngestMetrics(cfg *config.Config) *IngestMetrics {
	return &IngestMetrics{
		DisableProjectLabels: cfg.Metrics.DisableProjectLabels,
	}
}

// IngestMetrics is a helper for incrementing counters from the handler.
type IngestMetrics struct {
	DisableProjectLabels bool
}

// RecordSpan records a successfully ingested span batch.
func (m *IngestMetrics) RecordSpan(projectID string, count int) {
	if m.DisableProjectLabels {
		SpansReceived.WithLabelValues("unlabeled").Add(float64(count))
		return
	}
	SpansReceived.WithLabelValues(projectID).Add(float64(count))
}

// RecordEnqueueError increments the enqueue error counter.
func (m *IngestMetrics) RecordEnqueueError() {
	EnqueueErrors.Inc()
}

// RecordRequestDuration records the duration of a single request.
func (m *IngestMetrics) RecordRequestDuration(durationSec float64) {
	RequestDuration.WithLabelValues().Observe(durationSec)
}
