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

// Percentile returns the value at the given percentile from a sorted float64 slice.
func Percentile(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	idx := int(p * float64(len(sorted)-1))
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}