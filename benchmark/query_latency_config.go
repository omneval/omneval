package benchmark

import "time"

// WriteQueryLatencyConfig holds the configuration for writing a query-latency
// benchmark results doc.
type WriteQueryLatencyConfig struct {
	// Endpoint is the Ingest API base URL.
	Endpoint string
	// QueryEndpoint is the Query API base URL.
	QueryEndpoint string
	// TracesEndpoint is the Traces API base URL.
	TracesEndpoint string
	// AnalyticsEndpoint is the Analytics DSL endpoint URL.
	AnalyticsEndpoint string
	// ProjectID is the project ID used for benchmark queries.
	ProjectID string
	// CommitCadence is the Writer commit cadence (reported in results).
	CommitCadence time.Duration
	// RunCount is the number of benchmark runs per query type.
	RunCount int
	// WarmupRuns is the number of warm-up runs (not recorded).
	WarmupRuns int

	// Data volume — how the Lake was pre-loaded.
	PreLoadDescription   string // human-readable description
	TotalSpansIngested   int    // exact span count ingested before benchmark
	TotalTracesIngested  int    // exact trace count ingested before benchmark

	// Query shapes (for reproducibility documentation).
	TraceListQueryShape  string
	TraceDetailQueryShape string
	AnalyticsQueryShape  string

	// Latency measurements collected during the benchmark.
	QueryLatencyStats *QueryLatencyStats
	// GenerateTime is when the doc was written.
	GenerateTime time.Time
}