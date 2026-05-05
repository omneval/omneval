package metrics

import (
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/zbloss/lantern/internal/config"
)

func TestRegister_Metrics(t *testing.T) {
	// Clear all metrics from the default registry.
	prometheus.DefaultRegisterer.Unregister(QueryDuration)
	prometheus.DefaultRegisterer.Unregister(QueryErrors)
	prometheus.DefaultRegisterer.Unregister(SnapshotDownloads)
	prometheus.DefaultRegisterer.Unregister(SnapshotDownloadsFailed)
	prometheus.DefaultRegisterer.Unregister(RequestDuration)
	prometheus.DefaultRegisterer.Unregister(SnapshotAge)

	if err := Register(&config.Config{}); err != nil {
		t.Fatalf("Register: %v", err)
	}
}

func TestRecordRequestDuration(t *testing.T) {
	prometheus.DefaultRegisterer.Unregister(QueryDuration)
	prometheus.DefaultRegisterer.Unregister(QueryErrors)
	prometheus.DefaultRegisterer.Unregister(SnapshotDownloads)
	prometheus.DefaultRegisterer.Unregister(SnapshotDownloadsFailed)
	prometheus.DefaultRegisterer.Unregister(RequestDuration)
	prometheus.DefaultRegisterer.Unregister(SnapshotAge)
	defer prometheus.DefaultRegisterer.Unregister(QueryDuration)
	defer prometheus.DefaultRegisterer.Unregister(QueryErrors)
	defer prometheus.DefaultRegisterer.Unregister(SnapshotDownloads)
	defer prometheus.DefaultRegisterer.Unregister(SnapshotDownloadsFailed)
	defer prometheus.DefaultRegisterer.Unregister(RequestDuration)
	defer prometheus.DefaultRegisterer.Unregister(SnapshotAge)

	if err := Register(&config.Config{}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	m := NewQueryMetrics(&config.Config{})

	m.RecordRequestDuration("/api/v1/spans/query", 0.05)
	m.RecordRequestDuration("/api/v1/spans/query", 0.12)
	m.RecordRequestDuration("/api/v1/traces/{traceId}", 0.08)

	expected := `
		# HELP lantern_query_request_duration_seconds Duration of Query API requests in seconds.
		# TYPE lantern_query_request_duration_seconds histogram
		lantern_query_request_duration_seconds_bucket{endpoint="/api/v1/spans/query",le="0.005"} 0
		lantern_query_request_duration_seconds_bucket{endpoint="/api/v1/spans/query",le="0.01"} 0
		lantern_query_request_duration_seconds_bucket{endpoint="/api/v1/spans/query",le="0.025"} 0
		lantern_query_request_duration_seconds_bucket{endpoint="/api/v1/spans/query",le="0.05"} 1
		lantern_query_request_duration_seconds_bucket{endpoint="/api/v1/spans/query",le="0.1"} 1
		lantern_query_request_duration_seconds_bucket{endpoint="/api/v1/spans/query",le="0.25"} 2
		lantern_query_request_duration_seconds_bucket{endpoint="/api/v1/spans/query",le="+Inf"} 2
		lantern_query_request_duration_seconds_count{endpoint="/api/v1/spans/query"} 2
		lantern_query_request_duration_seconds_sum{endpoint="/api/v1/spans/query"} 0.17
		lantern_query_request_duration_seconds_bucket{endpoint="/api/v1/traces/{traceId}",le="0.005"} 0
		lantern_query_request_duration_seconds_bucket{endpoint="/api/v1/traces/{traceId}",le="0.01"} 0
		lantern_query_request_duration_seconds_bucket{endpoint="/api/v1/traces/{traceId}",le="0.025"} 0
		lantern_query_request_duration_seconds_bucket{endpoint="/api/v1/traces/{traceId}",le="0.05"} 0
		lantern_query_request_duration_seconds_bucket{endpoint="/api/v1/traces/{traceId}",le="0.1"} 1
		lantern_query_request_duration_seconds_bucket{endpoint="/api/v1/traces/{traceId}",le="+Inf"} 1
		lantern_query_request_duration_seconds_count{endpoint="/api/v1/traces/{traceId}"} 1
		lantern_query_request_duration_seconds_sum{endpoint="/api/v1/traces/{traceId}"} 0.08
	`
	if err := testutil.GatherAndCompare(RequestDuration, strings.NewReader(expected)); err != nil {
		t.Errorf("RequestDuration: %v", err)
	}
}

func TestRecordSnapshotAge(t *testing.T) {
	prometheus.DefaultRegisterer.Unregister(QueryDuration)
	prometheus.DefaultRegisterer.Unregister(QueryErrors)
	prometheus.DefaultRegisterer.Unregister(SnapshotDownloads)
	prometheus.DefaultRegisterer.Unregister(SnapshotDownloadsFailed)
	prometheus.DefaultRegisterer.Unregister(RequestDuration)
	prometheus.DefaultRegisterer.Unregister(SnapshotAge)
	defer prometheus.DefaultRegisterer.Unregister(QueryDuration)
	defer prometheus.DefaultRegisterer.Unregister(QueryErrors)
	defer prometheus.DefaultRegisterer.Unregister(SnapshotDownloads)
	defer prometheus.DefaultRegisterer.Unregister(SnapshotDownloadsFailed)
	defer prometheus.DefaultRegisterer.Unregister(RequestDuration)
	defer prometheus.DefaultRegisterer.Unregister(SnapshotAge)

	if err := Register(&config.Config{}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	m := NewQueryMetrics(&config.Config{})

	m.RecordSnapshotAge(45.5)
	m.RecordSnapshotAge(30.0)

	expected := `
		# HELP lantern_query_snapshot_age_seconds How stale the current DuckDB snapshot is, in seconds since LastModified.
		# TYPE lantern_query_snapshot_age_seconds gauge
		lantern_query_snapshot_age_seconds 30
	`
	if err := testutil.GatherAndCompare(SnapshotAge, strings.NewReader(expected)); err != nil {
		t.Errorf("SnapshotAge: %v", err)
	}
}
