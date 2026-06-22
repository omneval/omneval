package benchmark

import "time"

// BenchmarkConfig holds the full configuration used to run the benchmark, for
// documentation in the results markdown.
type BenchmarkConfig struct {
	Endpoint        string
	QueryEndpoint   string
	ProjectID       string
	CommitCadence   time.Duration
	NumTraces       int
	SpansPerTrace   int
	BatchSize       int
	RunCount        int
	WarmupRuns      int
	Throughput      ThroughputStats
	Latency         LatencyStats
	GenerateTime    time.Time
	TotalSpans      int
}

// WriteScalingConfig holds the configuration for writing a scaling results doc.
type WriteScalingConfig struct {
	Endpoint          string
	QueryEndpoint     string
	ProjectID         string
	CommitCadence     time.Duration
	Replicas          []int
	RunCount          int
	WarmupRuns        int
	ScalingResult     *ScalingResult
	GenerateTime      time.Time
	KubectlDeployment string
}