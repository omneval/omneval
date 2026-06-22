package benchmark

import (
	"fmt"
	"sort"
	"strings"
)

// ScalingResult holds the result of a horizontal scaling benchmark: throughput
// stats collected at each Writer replica count.
type ScalingResult struct {
	Replicas []int
	Stats    map[int]*ThroughputStats
}

// WriteMarkdown writes a markdown table of the scaling results.
func (r *ScalingResult) WriteMarkdown() string {
	var sb strings.Builder

	if len(r.Stats) == 0 {
		sb.WriteString("_No data collected._\n")
		return sb.String()
	}

	// Sort replica counts ascending for consistent output.
	replicas := r.Replicas
	if len(replicas) == 0 {
		replicas = sortedKeys(r.Stats)
	}
	sort.Ints(replicas)

	sb.WriteString("| Replicas | Accepted spans/s (p50) | Accepted spans/s (p95) | Accepted spans/s (p99) | " +
		"Committed spans/s (p50) | Committed spans/s (p95) | Committed spans/s (p99) | Scaling factor |\n")
	sb.WriteString("|----------|----------------------|----------------------|----------------------|----------------------|----------------------|----------------------|---------------|")

	// Find the baseline (lowest replica count, typically 1).
	var baselineRate float64
	for _, rc := range replicas {
		if rc <= 1 {
			baselineRate = statsP50(r.Stats[rc], true)
			break
		}
	}
	if baselineRate <= 0 {
		for _, rc := range replicas {
			v := statsP50(r.Stats[rc], true)
			if v > baselineRate {
				baselineRate = v
			}
		}
	}

	for _, rc := range replicas {
		stats := r.Stats[rc]
		acceptedP50 := statsP50(stats, true)
		acceptedP95 := statsP95(stats, true)
		acceptedP99 := statsP99(stats, true)
		committedP50 := statsP50(stats, false)
		committedP95 := statsP95(stats, false)
		committedP99 := statsP99(stats, false)

		scalingFactor := float64(1.0)
		if baselineRate > 0 && rc > 0 {
			scalingFactor = acceptedP50 / baselineRate
		}

		sb.WriteString(fmt.Sprintf("\n| %d | %.1f | %.1f | %.1f | %.1f | %.1f | %.1f | %.2fx |",
			rc, acceptedP50, acceptedP95, acceptedP99, committedP50, committedP95, committedP99, scalingFactor))
	}

	return sb.String()
}

// statsP50 returns the p50 of the stats slice; if accepted is true, returns
// AcceptedSpansPerSec, otherwise CommittedSpansPerSec.
func statsP50(s *ThroughputStats, accepted bool) float64 {
	if s == nil {
		return 0
	}
	if accepted {
		sorted := make([]float64, len(s.AcceptedSpansPerSec))
		copy(sorted, s.AcceptedSpansPerSec)
		sort.Float64s(sorted)
		return float64Value(sorted, 0.50)
	}
	sorted := make([]float64, len(s.CommittedSpansPerSec))
	copy(sorted, s.CommittedSpansPerSec)
	sort.Float64s(sorted)
	return float64Value(sorted, 0.50)
}

// statsP95 returns the p95 of the stats slice.
func statsP95(s *ThroughputStats, accepted bool) float64 {
	if s == nil {
		return 0
	}
	if accepted {
		sorted := make([]float64, len(s.AcceptedSpansPerSec))
		copy(sorted, s.AcceptedSpansPerSec)
		sort.Float64s(sorted)
		return float64Value(sorted, 0.95)
	}
	sorted := make([]float64, len(s.CommittedSpansPerSec))
	copy(sorted, s.CommittedSpansPerSec)
	sort.Float64s(sorted)
	return float64Value(sorted, 0.95)
}

// statsP99 returns the p99 of the stats slice.
func statsP99(s *ThroughputStats, accepted bool) float64 {
	if s == nil {
		return 0
	}
	if accepted {
		sorted := make([]float64, len(s.AcceptedSpansPerSec))
		copy(sorted, s.AcceptedSpansPerSec)
		sort.Float64s(sorted)
		return float64Value(sorted, 0.99)
	}
	sorted := make([]float64, len(s.CommittedSpansPerSec))
	copy(sorted, s.CommittedSpansPerSec)
	sort.Float64s(sorted)
	return float64Value(sorted, 0.99)
}

// ScalingBenchConfig holds the configuration for a scaling benchmark run.
type ScalingBenchConfig struct {
	// Replicas lists the Writer replica counts to test.
	Replicas []int
	// RunCount is the number of benchmark runs per replica count.
	RunCount int
	// WriterID is the writer identifier (for Helm value overrides).
	WriterID string
	// InjectStat is a callback that performs the actual benchmark at a given
	// replica count and returns the resulting ThroughputStats.  The harness
	// calls this RunCount times per replica count, appending each result.
	InjectStat func(replica int) (*ThroughputStats, error)
}

// RunScalingBench executes the scaling benchmark: for each replica count, it
// calls cfg.InjectStat cfg.RunCount times, accumulating throughput data.
// Returns a ScalingResult ready for WriteMarkdown().
func RunScalingBench(cfg *ScalingBenchConfig) *ScalingResult {
	result := &ScalingResult{
		Replicas: cfg.Replicas,
		Stats:    make(map[int]*ThroughputStats),
	}

	if cfg.InjectStat == nil {
		return result
	}

	for _, replica := range cfg.Replicas {
		stats := &ThroughputStats{}
		for i := 0; i < cfg.RunCount; i++ {
			runStats, err := cfg.InjectStat(replica)
			if err != nil {
				// Log but continue to next replica count.
				continue
			}
			stats.AcceptedSpansPerSec = append(stats.AcceptedSpansPerSec, runStats.AcceptedSpansPerSec...)
			stats.CommittedSpansPerSec = append(stats.CommittedSpansPerSec, runStats.CommittedSpansPerSec...)
		}
		result.Stats[replica] = stats
	}

	return result
}

// sortedKeys returns the integer keys from m in ascending order.
func sortedKeys(m map[int]*ThroughputStats) []int {
	keys := make([]int, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Ints(keys)
	return keys
}