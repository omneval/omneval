package metrics

import (
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/zbloss/lantern/internal/config"
)

func TestRegister_Metrics(t *testing.T) {
	// Clear the registry.
	prometheus.DefaultRegisterer.Unregister(SpansReceived)
	prometheus.DefaultRegisterer.Unregister(EnqueueErrors)
	prometheus.DefaultRegisterer.Unregister(RequestDuration)

	if err := Register(false); err != nil {
		t.Fatalf("Register: %v", err)
	}
}

func TestRecordSpan(t *testing.T) {
	// Clear previously registered metrics.
	prometheus.DefaultRegisterer.Unregister(SpansReceived)
	prometheus.DefaultRegisterer.Unregister(EnqueueErrors)
	prometheus.DefaultRegisterer.Unregister(RequestDuration)
	defer prometheus.DefaultRegisterer.Unregister(SpansReceived)
	defer prometheus.DefaultRegisterer.Unregister(EnqueueErrors)
	defer prometheus.DefaultRegisterer.Unregister(RequestDuration)

	if err := Register(false); err != nil {
		t.Fatalf("Register: %v", err)
	}

	m := NewIngestMetrics(&config.Config{})

	// Record a batch of 5 spans for project "test-proj".
	m.RecordSpan("test-proj", 5)

	// Verify the counter value.
	expected := `
		# HELP lantern_ingest_spans_received_total Total number of spans received by the ingest API.
		# TYPE lantern_ingest_spans_received_total counter
		lantern_ingest_spans_received_total{project_id="test-proj"} 5
	`
	if err := testutil.GatherAndCompare(SpansReceived, strings.NewReader(expected)); err != nil {
		t.Errorf("SpansReceived: %v", err)
	}
}

func TestRecordSpan_DisabledProjectLabels(t *testing.T) {
	prometheus.DefaultRegisterer.Unregister(SpansReceived)
	prometheus.DefaultRegisterer.Unregister(EnqueueErrors)
	prometheus.DefaultRegisterer.Unregister(RequestDuration)
	defer prometheus.DefaultRegisterer.Unregister(SpansReceived)
	defer prometheus.DefaultRegisterer.Unregister(EnqueueErrors)
	defer prometheus.DefaultRegisterer.Unregister(RequestDuration)

	if err := Register(true); err != nil {
		t.Fatalf("Register: %v", err)
	}

	m := NewIngestMetrics(&config.Config{
		Metrics: config.MetricsConfig{
			DisableProjectLabels: true,
		},
	})

	// Record spans — should use "unlabeled" bucket.
	m.RecordSpan("any-project", 3)

	expected := `
		# HELP lantern_ingest_spans_received_total Total number of spans received by the ingest API.
		# TYPE lantern_ingest_spans_received_total counter
		lantern_ingest_spans_received_total{project_id="unlabeled"} 3
	`
	if err := testutil.GatherAndCompare(SpansReceived, strings.NewReader(expected)); err != nil {
		t.Errorf("SpansReceived: %v", err)
	}
}

func TestRecordEnqueueError(t *testing.T) {
	prometheus.DefaultRegisterer.Unregister(SpansReceived)
	prometheus.DefaultRegisterer.Unregister(EnqueueErrors)
	prometheus.DefaultRegisterer.Unregister(RequestDuration)
	defer prometheus.DefaultRegisterer.Unregister(SpansReceived)
	defer prometheus.DefaultRegisterer.Unregister(EnqueueErrors)
	defer prometheus.DefaultRegisterer.Unregister(RequestDuration)

	if err := Register(false); err != nil {
		t.Fatalf("Register: %v", err)
	}

	m := NewIngestMetrics(&config.Config{})

	m.RecordEnqueueError()
	m.RecordEnqueueError()

	expected := `
		# HELP lantern_ingest_enqueue_errors_total Total number of errors when enqueuing spans to Redis.
		# TYPE lantern_ingest_enqueue_errors_total counter
		lantern_ingest_enqueue_errors_total 2
	`
	if err := testutil.GatherAndCompare(EnqueueErrors, strings.NewReader(expected)); err != nil {
		t.Errorf("EnqueueErrors: %v", err)
	}
}

func TestRecordRequestDuration(t *testing.T) {
	prometheus.DefaultRegisterer.Unregister(SpansReceived)
	prometheus.DefaultRegisterer.Unregister(EnqueueErrors)
	prometheus.DefaultRegisterer.Unregister(RequestDuration)
	defer prometheus.DefaultRegisterer.Unregister(SpansReceived)
	defer prometheus.DefaultRegisterer.Unregister(EnqueueErrors)
	defer prometheus.DefaultRegisterer.Unregister(RequestDuration)

	if err := Register(false); err != nil {
		t.Fatalf("Register: %v", err)
	}

	m := NewIngestMetrics(&config.Config{})

	m.RecordRequestDuration(0.05)
	m.RecordRequestDuration(0.12)

	// Just verify no error — histogram buckets are hard to assert exactly.
	expected := `
		# HELP lantern_ingest_request_duration_seconds Duration of ingest API requests in seconds.
		# TYPE lantern_ingest_request_duration_seconds histogram
		lantern_ingest_request_duration_seconds_bucket{le="0.005"} 0
		lantern_ingest_request_duration_seconds_bucket{le="0.01"} 0
		lantern_ingest_request_duration_seconds_bucket{le="0.025"} 0
		lantern_ingest_request_duration_seconds_bucket{le="0.05"} 1
		lantern_ingest_request_duration_seconds_bucket{le="0.1"} 1
		lantern_ingest_request_duration_seconds_bucket{le="0.25"} 2
		lantern_ingest_request_duration_seconds_bucket{le="+Inf"} 2
		# HELP lantern_ingest_request_duration_seconds_count Duration of ingest API requests in seconds.
		# TYPE lantern_ingest_request_duration_seconds_count counter
		lantern_ingest_request_duration_seconds_count 2
		# HELP lantern_ingest_request_duration_seconds_sum Duration of ingest API requests in seconds.
		# TYPE lantern_ingest_request_duration_seconds_sum counter
		lantern_ingest_request_duration_seconds_sum 0.17
	`
	if err := testutil.GatherAndCompare(RequestDuration, strings.NewReader(expected)); err != nil {
		t.Errorf("RequestDuration: %v", err)
	}
}
