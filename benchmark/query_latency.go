package benchmark

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sort"
	"time"
)

// LatencyType is the kind of query being benchmarked.
type LatencyType string

const (
	LatencyTypeTraceList LatencyType = "trace-list"
	LatencyTypeTraceDetail LatencyType = "trace-detail"
	LatencyTypeAnalytics LatencyType = "analytics"
)

// LatencyTypeResult holds latencies for a single query type.
type LatencyTypeResult struct {
	Latencies []time.Duration
}

// QueryLatencyStats holds latency measurements across all query types.
type QueryLatencyStats struct {
	Stats map[LatencyType]*LatencyTypeResult
}

// NewQueryLatencyStats creates an empty QueryLatencyStats.
func NewQueryLatencyStats() *QueryLatencyStats {
	return &QueryLatencyStats{
		Stats: make(map[LatencyType]*LatencyTypeResult),
	}
}

// Set records the latency results for the given query type.
func (q *QueryLatencyStats) Set(typ LatencyType, result *LatencyTypeResult) {
	q.Stats[typ] = result
}

// Get returns the latency result for the given query type, if present.
func (q *QueryLatencyStats) Get(typ LatencyType) (*LatencyTypeResult, bool) {
	r, ok := q.Stats[typ]
	return r, ok
}

// Fprint writes a formatted query-latency report to w.
func (q *QueryLatencyStats) Fprint(w interface{ Write([]byte) (int, error) }, cadence time.Duration) {
	fmt.Fprint(w, "=== Query Latency Results ===\n")
	fmt.Fprint(w, "\n")
	for _, typ := range []LatencyType{LatencyTypeTraceList, LatencyTypeTraceDetail, LatencyTypeAnalytics} {
		r, ok := q.Stats[typ]
		if !ok || len(r.Latencies) == 0 {
			fmt.Fprintf(w, "[%s] No data collected\n\n", typ)
			continue
		}
		sorted := make([]time.Duration, len(r.Latencies))
		copy(sorted, r.Latencies)
		sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })

		pr := ComputePercentiles(sorted)
		fmt.Fprintf(w, "[%s] p50:  %s  p95:  %s  p99:  %s  (runs: %d)\n",
			typ, pr.P50.Round(time.Millisecond), pr.P95.Round(time.Millisecond), pr.P99.Round(time.Millisecond), len(sorted))
	}
}

// QueryLatencyClient issues query API requests and measures their latency.
type QueryLatencyClient struct {
	apiKey       string
	client       *http.Client
	queryURL     string // e.g. http://localhost:8000/api/v1/spans/query
	tracesBaseURL string // e.g. http://localhost:8000/api/v1/traces
	analyticsURL string // e.g. http://localhost:8000/api/v1/analytics/spans
}

// NewQueryLatencyClient creates a client that measures query latency.
//
//   - queryBaseURL is the base path for span queries, e.g.
//     http://localhost:8000/api/v1/spans/query.  The trace-list endpoint is
//     derived by appending nothing (it is the URL itself).
//   - tracesBaseURL is the base path for trace-detail requests, e.g.
//     http://localhost:8000/api/v1/traces.
//   - analyticsBaseURL is the analytics endpoint URL, e.g.
//     http://localhost:8000/api/v1/analytics/spans.
func NewQueryLatencyClient(queryBaseURL, tracesBaseURL, analyticsBaseURL, apiKey string) *QueryLatencyClient {
	return &QueryLatencyClient{
		apiKey:        apiKey,
		queryURL:      queryBaseURL,
		tracesBaseURL: tracesBaseURL,
		analyticsURL:  analyticsBaseURL,
		client: &http.Client{
			Timeout: 60 * time.Second,
		},
	}
}

// MeasureTraceListLatency runs the trace-list query (POST /spans/query)
// repeatedly and records the wall-clock latency of each request.
//
// The query shape matches the Traces page root-span-rollup filter:
//
//	project_id = ? AND kind IN ('agent', 'tool', 'llm') ORDER BY start_time DESC LIMIT 25
//
// warmupCount iterations are run first (not recorded), then runCount
// iterations are measured.
func (c *QueryLatencyClient) MeasureTraceListLatency(ctx context.Context, projectID string, traceIDs []string, runCount, warmupCount int) (*QueryLatencyStats, error) {
	stats := NewQueryLatencyStats()
	reqBody := spanQueryRequestBody{
		From:    "lake.spans",
		ProjectID: projectID,
		Filters: []queryFilter{
			{Field: "project_id", Op: "eq", Value: projectID},
		},
		Limit: 25,
	}
	data, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal trace-list query: %w", err)
	}

	return c.measureLatencyRound(ctx, LatencyTypeTraceList, data, "POST", c.queryURL, runCount, warmupCount, stats)
}

// MeasureTraceDetailLatency runs the trace-detail query (GET /traces/{traceId})
// for each trace ID repeatedly and records the wall-clock latency of each request.
//
// warmupCount iterations are run first (not recorded), then runCount
// iterations are measured.  Across all runs, one trace is selected at random
// (first trace in the list) to keep the query consistent.
func (c *QueryLatencyClient) MeasureTraceDetailLatency(ctx context.Context, projectID string, traceIDs []string, runCount, warmupCount int) (*QueryLatencyStats, error) {
	stats := NewQueryLatencyStats()

	if len(traceIDs) == 0 {
		return stats, nil
	}

	// Use the first trace for all runs.
	traceID := traceIDs[0]
	url := fmt.Sprintf("%s/%s", c.tracesBaseURL, traceID)

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	req.Header.Set("X-API-Key", c.apiKey)

	return c.measureDirectLatency(ctx, LatencyTypeTraceDetail, req, runCount, warmupCount, stats)
}

// MeasureAnalyticsLatency runs a representative analytics DSL aggregation
// query (POST /analytics/spans) repeatedly and records the wall-clock latency.
//
// The query shape is a typical dashboard aggregation:
//
//	COUNT(*) + AVG(duration_ms) + SUM(input_tokens) + SUM(output_tokens)
//	GROUP BY time_bucket('1h', start_time)
//
// warmupCount iterations are run first (not recorded), then runCount
// iterations are measured.
func (c *QueryLatencyClient) MeasureAnalyticsLatency(ctx context.Context, projectID string, runCount, warmupCount int) (*QueryLatencyStats, error) {
	stats := NewQueryLatencyStats()

	now := time.Now()
	from := now.Add(-30 * 24 * time.Hour)

	reqBody := analyticsRequestBody{
		From:    from,
		To:      now,
		ProjectID: projectID,
		Filters: []analyticsFilter{
			{Field: "kind", Op: "in", Value: []string{"llm", "tool", "agent"}},
		},
		Aggregations: []analyticsAgg{
			{Function: "count", Field: "*", Alias: "span_count"},
			{Function: "avg", Field: "duration_ms", Alias: "avg_duration_ms"},
			{Function: "sum", Field: "input_tokens", Alias: "total_input_tokens"},
			{Function: "sum", Field: "output_tokens", Alias: "total_output_tokens"},
		},
		GroupBy: []groupByField{
			{Field: "time_bucket", Interval: "1h"},
		},
		Limit: 720,
	}

	data, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal analytics query: %w", err)
	}

	return c.measureLatencyRound(ctx, LatencyTypeAnalytics, data, "POST", c.analyticsURL, runCount, warmupCount, stats)
}

// measureLatencyRound measures a single query body repeated runCount + warmupCount
// times and records latencies for the runCount (non-warmup) iterations.
func (c *QueryLatencyClient) measureLatencyRound(ctx context.Context, typ LatencyType, body []byte, method, endpoint string, runCount, warmupCount int, stats *QueryLatencyStats) (*QueryLatencyStats, error) {
	var latencies []time.Duration

	for i := 0; i < runCount+warmupCount; i++ {
		isBenchmark := i >= warmupCount

		start := time.Now()
		resp, err := c.doRequest(ctx, method, endpoint, body)
		elapsed := time.Since(start)

		if err != nil {
			slog.Warn("query request failed", "type", typ, "run", i, "err", err)
			// Still record the elapsed time even on error — a slow failure
			// is a meaningful latency measurement.
		}

		if isBenchmark && resp != nil {
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
		}

		if isBenchmark {
			latencies = append(latencies, elapsed)
		}
	}

	sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
	stats.Set(typ, &LatencyTypeResult{Latencies: latencies})
	return stats, nil
}

// measureDirectLatency measures a raw HTTP request repeated runCount + warmupCount
// times. The caller builds the request with the desired method, URL, headers, etc.
func (c *QueryLatencyClient) measureDirectLatency(ctx context.Context, typ LatencyType, req *http.Request, runCount, warmupCount int, stats *QueryLatencyStats) (*QueryLatencyStats, error) {
	var latencies []time.Duration

	for i := 0; i < runCount+warmupCount; i++ {
		isBenchmark := i >= warmupCount

		start := time.Now()

		// Clone the request for each iteration so headers/body can be re-used.
		reqClone := req.Clone(ctx)
		resp, err := c.client.Do(reqClone)
		elapsed := time.Since(start)

		if err != nil {
			slog.Warn("query request failed", "type", typ, "run", i, "err", err)
		}

		if isBenchmark && resp != nil {
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
		}

		if isBenchmark {
			latencies = append(latencies, elapsed)
		}
	}

	sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
	stats.Set(typ, &LatencyTypeResult{Latencies: latencies})
	return stats, nil
}

func (c *QueryLatencyClient) doRequest(ctx context.Context, method, url string, body []byte) (*http.Response, error) {
	var req *http.Request
	var err error

	if len(body) > 0 {
		req, err = http.NewRequestWithContext(ctx, method, url, bytes.NewReader(body))
	} else {
		req, err = http.NewRequestWithContext(ctx, method, url, nil)
	}
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", c.apiKey)

	return c.client.Do(req)
}

// --- request/response types ---

// spanQueryRequestBody is the JSON body for POST /spans/query.
type spanQueryRequestBody struct {
	From      string        `json:"from"`
	ProjectID string        `json:"project_id,omitempty"`
	Filters   []queryFilter `json:"filters"`
	Limit     int           `json:"limit"`
}

// analyticsRequestBody is the JSON body for POST /analytics/spans.
type analyticsRequestBody struct {
	From         time.Time           `json:"from"`
	To           time.Time           `json:"to"`
	ProjectID    string              `json:"project_id,omitempty"`
	Filters      []analyticsFilter   `json:"filters"`
	Aggregations []analyticsAgg      `json:"aggregations"`
	GroupBy      []groupByField      `json:"group_by"`
	OrderBy      []orderClause       `json:"order_by"`
	Limit        int                 `json:"limit"`
}

// analyticsFilter mirrors the DSL Filter struct for analytics queries.
type analyticsFilter struct {
	Field string `json:"field"`
	Op    string `json:"op"`
	Value any    `json:"value"`
}

// analyticsAgg mirrors the DSL Aggregation struct.
type analyticsAgg struct {
	Function string `json:"function"`
	Field    string `json:"field"`
	Alias    string `json:"alias"`
}

// groupByField mirrors the DSL GroupByField struct.
type groupByField struct {
	Field    string `json:"field"`
	Truncate string `json:"truncate,omitempty"`
	Interval string `json:"interval,omitempty"`
}

// orderClause mirrors the DSL OrderClause struct.
type orderClause struct {
	Field string `json:"field"`
	Desc  bool   `json:"desc"`
}