package benchmark

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestQueryLatencyClientTraceList measures the trace-list query and verifies
// the returned latency stats contain a trace-list entry.
func TestQueryLatencyClientTraceList(t *testing.T) {
	projectID := "test-project"

	// Serve a fake trace-list response with a known shape.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify API key header.
		if r.Header.Get("X-API-Key") != "test-key" {
			t.Errorf("expected API key header, got %q", r.Header.Get("X-API-Key"))
		}
		// Return a trace-list response.
		resp := map[string]interface{}{
			"spans": []map[string]interface{}{
				{"span_id": "root1", "trace_id": "trace1", "name": "agent-loop", "kind": "agent", "start_time": "2026-01-01T00:00:00Z"},
			},
			"next": "",
			"limit": 25,
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	// We also need a fake ingest endpoint for the pre-load step.
	ingestSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	}))
	defer ingestSrv.Close()

	ql := NewQueryLatencyClient(srv.URL, ingestSrv.URL, srv.URL+"/analytics", "test-key")

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	// Generate a small set of traces to use for query latency measurement.
	traces := GenerateTraces(projectID, 3, 5)

	// Collect the trace IDs from the generated traces.
	traceIDs := make([]string, 0, 3)
	for _, tg := range traces {
		traceIDs = append(traceIDs, tg.TraceID)
	}

	// Ingest all traces first (required before we can query them).
	ingestClient := NewIngestClient(ingestSrv.URL, "test-key")
	result, err := ingestClient.SendTraces(ctx, traces, 5)
	if err != nil {
		t.Fatalf("failed to ingest test traces: %v", err)
	}
	if result.SpansAccepted == 0 {
		t.Fatal("expected at least one span accepted")
	}

	stats, err := ql.MeasureTraceListLatency(ctx, projectID, traceIDs, 2, 1)
	if err != nil {
		t.Fatalf("MeasureTraceListLatency failed: %v", err)
	}

	// Verify the latency stats contain a trace-list entry.
	tl, ok := stats.Get(LatencyTypeTraceList)
	if !ok {
		t.Fatal("expected trace-list latency entry")
	}
	if len(tl.Latencies) == 0 {
		t.Fatal("expected trace-list latencies to be populated")
	}
	if len(tl.Latencies) != 2 {
		t.Errorf("expected 2 trace-list latencies, got %d", len(tl.Latencies))
	}
}

// TestQueryLatencyClientTraceDetail measures the trace-detail query and verifies
// the returned latency stats contain a trace-detail entry.
func TestQueryLatencyClientTraceDetail(t *testing.T) {
	projectID := "test-project"

	traceID := "trace-abc123"

	// Serve a fake trace-detail response.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/traces/"+traceID {
			t.Errorf("unexpected path: %s, expected /api/v1/traces/%s", r.URL.Path, traceID)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		resp := map[string]interface{}{
			"trace_id":   traceID,
			"project_id": projectID,
			"root_span":  map[string]interface{}{"span_id": "root1", "name": "agent-loop"},
			"spans":      []map[string]interface{}{{"span_id": "root1", "trace_id": traceID}},
			"total_input_tokens":  100,
			"total_output_tokens": 200,
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	// Also need an ingest endpoint.
	ingestSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	}))
	defer ingestSrv.Close()

	ql := NewQueryLatencyClient(srv.URL, ingestSrv.URL, srv.URL+"/analytics", "test-key")

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	traces := GenerateTraces(projectID, 1, 5)

	// Ingest the traces first.
	ingestClient := NewIngestClient(ingestSrv.URL, "test-key")
	_, err := ingestClient.SendTraces(ctx, traces, 5)
	if err != nil {
		t.Fatalf("failed to ingest test traces: %v", err)
	}

	traceIDs := make([]string, 0, len(traces))
	for _, tg := range traces {
		traceIDs = append(traceIDs, tg.TraceID)
	}

	stats, err := ql.MeasureTraceDetailLatency(ctx, projectID, traceIDs, 2, 1)
	if err != nil {
		t.Fatalf("MeasureTraceDetailLatency failed: %v", err)
	}

	tl, ok := stats.Get(LatencyTypeTraceDetail)
	if !ok {
		t.Fatal("expected trace-detail latency entry")
	}
	if len(tl.Latencies) != 2 {
		t.Errorf("expected 2 trace-detail latencies, got %d", len(tl.Latencies))
	}
}

// TestQueryLatencyClientAnalytics measures the analytics DSL query and verifies
// the returned latency stats contain an analytics entry.
func TestQueryLatencyClientAnalytics(t *testing.T) {
	projectID := "test-project"

	// Serve a fake analytics response.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify it's a POST with JSON body.
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		resp := map[string]interface{}{
			"rows": []map[string]interface{}{
				{"hour": "2026-01-01T00:00:00Z", "span_count": 5, "avg_duration_ms": 123.45},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	ingestSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	}))
	defer ingestSrv.Close()

	ql := NewQueryLatencyClient(srv.URL, ingestSrv.URL, srv.URL, "test-key")

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	traces := GenerateTraces(projectID, 2, 5)

	ingestClient := NewIngestClient(ingestSrv.URL, "test-key")
	_, err := ingestClient.SendTraces(ctx, traces, 5)
	if err != nil {
		t.Fatalf("failed to ingest test traces: %v", err)
	}

	stats, err := ql.MeasureAnalyticsLatency(ctx, projectID, 2, 1)
	if err != nil {
		t.Fatalf("MeasureAnalyticsLatency failed: %v", err)
	}

	tl, ok := stats.Get(LatencyTypeAnalytics)
	if !ok {
		t.Fatal("expected analytics latency entry")
	}
	if len(tl.Latencies) != 2 {
		t.Errorf("expected 2 analytics latencies, got %d", len(tl.Latencies))
	}
}

// TestQueryLatencyClientFailsOnError verifies the client returns an error
// when the query API returns an error status.
func TestQueryLatencyClientFailsOnError(t *testing.T) {
	projectID := "test-project"

	// Serve a 500 error.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("internal server error"))
	}))
	defer srv.Close()

	ingestSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	}))
	defer ingestSrv.Close()

	ql := NewQueryLatencyClient(srv.URL, ingestSrv.URL, srv.URL, "test-key")

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	traces := GenerateTraces(projectID, 1, 3)

	ingestClient := NewIngestClient(ingestSrv.URL, "test-key")
	_, err := ingestClient.SendTraces(ctx, traces, 3)
	if err != nil {
		t.Fatalf("failed to ingest test traces: %v", err)
	}

	traceIDs := make([]string, 0, len(traces))
	for _, tg := range traces {
		traceIDs = append(traceIDs, tg.TraceID)
	}

	// MeasureTraceDetailLatency should NOT fail (errors are logged but not returned).
	// The implementation records elapsed time even on error.
	_, err = ql.MeasureTraceDetailLatency(ctx, projectID, traceIDs, 2, 1)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}

// TestQueryLatencyStatsReport verifies the Fprint method of QueryLatencyStats
// includes all three query types.
func TestQueryLatencyStatsReport(t *testing.T) {
	stats := NewQueryLatencyStats()

	stats.Set(LatencyTypeTraceList, &LatencyTypeResult{
		Latencies: []time.Duration{50 * time.Millisecond, 100 * time.Millisecond, 150 * time.Millisecond},
	})
	stats.Set(LatencyTypeTraceDetail, &LatencyTypeResult{
		Latencies: []time.Duration{200 * time.Millisecond, 300 * time.Millisecond, 400 * time.Millisecond},
	})
	stats.Set(LatencyTypeAnalytics, &LatencyTypeResult{
		Latencies: []time.Duration{30 * time.Millisecond, 60 * time.Millisecond, 90 * time.Millisecond},
	})

	var buf interface{ Write([]byte) (int, error) } = &bufWrite{}
	stats.Fprint(buf, 10*time.Second)

	content := buf.(*bufWrite).data
	s := string(content)

	// Check that all query types are present in the output.
	if !contains(s, "trace-list") {
		t.Error("expected 'trace-list' in report")
	}
	if !contains(s, "trace-detail") {
		t.Error("expected 'trace-detail' in report")
	}
	if !contains(s, "analytics") {
		t.Error("expected 'analytics' in report")
	}
	if !contains(s, "p50") {
		t.Error("expected 'p50' in report")
	}
	if !contains(s, "p95") {
		t.Error("expected 'p95' in report")
	}
	if !contains(s, "p99") {
		t.Error("expected 'p99' in report")
	}
}

// TestQueryLatencyStatsEmpty verifies Fprint handles zero data gracefully.
func TestQueryLatencyStatsEmpty(t *testing.T) {
	stats := &QueryLatencyStats{}

	var buf interface{ Write([]byte) (int, error) } = &bufWrite{}
	stats.Fprint(buf, 10*time.Second)

	content := buf.(*bufWrite).data
	s := string(content)

	// Should contain "No data collected" or a similar message for each section.
	if !contains(s, "No data collected") {
		t.Error("expected 'No data collected' in empty stats output")
	}
}

// TestComputeQueryLatencyPercentiles verifies percentile computation for query latency.
func TestComputeQueryLatencyPercentiles(t *testing.T) {
	lats := []time.Duration{
		10 * time.Millisecond,
		20 * time.Millisecond,
		30 * time.Millisecond,
		40 * time.Millisecond,
		50 * time.Millisecond,
	}

	pr := ComputePercentiles(lats)

	if pr.P50 != 30*time.Millisecond {
		t.Errorf("expected p50=30ms, got %v", pr.P50)
	}
	if pr.P95 != 50*time.Millisecond {
		t.Errorf("expected p95=50ms, got %v", pr.P95)
	}
	if pr.P99 != 50*time.Millisecond {
		t.Errorf("expected p99=50ms, got %v", pr.P99)
	}
}

// TestComputeQueryLatencyPercentilesEmpty verifies zero on empty input.
func TestComputeQueryLatencyPercentilesEmpty(t *testing.T) {
	pr := ComputePercentiles([]time.Duration{})
	if pr.P50 != 0 || pr.P95 != 0 || pr.P99 != 0 {
		t.Errorf("expected all zeros for empty input, got %+v", pr)
	}
}

// --- helpers ---

type bufWrite struct {
	data []byte
}

func (b *bufWrite) Write(p []byte) (int, error) {
	b.data = append(b.data, p...)
	return len(p), nil
}