package benchmark

import (
	"testing"
	"time"
)

func TestWriteQueryLatencyConfigFields(t *testing.T) {
	// Verify the config type holds the fields needed for a query-latency
	// results doc: data-volume description, trace-list query shape,
	// trace-detail query shape, analytics query shape, and latency results.
	cfg := WriteQueryLatencyConfig{
		Endpoint:             "http://localhost:8000/api/v1/spans",
		QueryEndpoint:        "http://localhost:8000/api/v1/spans/query",
		TracesEndpoint:       "http://localhost:8000/api/v1/traces",
		AnalyticsEndpoint:    "http://localhost:8000/api/v1/analytics/spans",
		ProjectID:            "test-project",
		CommitCadence:        10 * time.Second,
		RunCount:             10,
		WarmupRuns:           3,
		PreLoadDescription:   "30 days of traces at ~10 spans/sec ingest rate, ~25 million spans",
		TraceListQueryShape:  "project_id=eq(test-project) kind=in(agent,tool,llm) order=start_time desc limit 25",
		TraceDetailQueryShape: "GET /api/v1/traces/{trace_id} (single trace with all children)",
		AnalyticsQueryShape:  "COUNT(*)+AVG(duration_ms)+SUM(input_tokens)+SUM(output_tokens) group_by=time_bucket('1h',start_time) from=30dago to=now",
		QueryLatencyStats:    NewQueryLatencyStats(),
		GenerateTime:         time.Date(2026, 6, 22, 0, 0, 0, 0, time.UTC),
		TotalSpansIngested:   100000,
		TotalTracesIngested:  20000,
	}

	if cfg.PreLoadDescription == "" {
		t.Error("PreLoadDescription should be settable")
	}
	if cfg.TraceListQueryShape == "" {
		t.Error("TraceListQueryShape should be settable")
	}
	if cfg.TraceDetailQueryShape == "" {
		t.Error("TraceDetailQueryShape should be settable")
	}
	if cfg.AnalyticsQueryShape == "" {
		t.Error("AnalyticsQueryShape should be settable")
	}
	if cfg.QueryLatencyStats == nil {
		t.Error("QueryLatencyStats should be settable")
	}
	if cfg.TotalSpansIngested == 0 {
		t.Error("TotalSpansIngested should be settable")
	}
	if cfg.TotalTracesIngested == 0 {
		t.Error("TotalTracesIngested should be settable")
	}
}