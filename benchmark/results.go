package benchmark

import (
	"fmt"
	"io"
	"os"
	"sort"
	"time"
)

// PercentileResult holds the computed p50/p95/p99 for a collection of durations.
type PercentileResult struct {
	P50 time.Duration
	P95 time.Duration
	P99 time.Duration
}

// ComputePercentiles computes p50, p95, and p99 from the given sorted durations.
// The input slice must already be sorted in ascending order.
func ComputePercentiles(sorted []time.Duration) PercentileResult {
	if len(sorted) == 0 {
		return PercentileResult{}
	}
	return PercentileResult{
		P50: percentile(sorted, 0.50),
		P95: percentile(sorted, 0.95),
		P99: percentile(sorted, 0.99),
	}
}

// percentile returns the value at the given percentile (0–1) from a sorted slice.
// idx = floor(p * len), clamped to [0, len-1].
func percentile(sorted []time.Duration, p float64) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	idx := int(p * float64(len(sorted)))
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}

// ThroughputStats holds ingest-throughput measurements across multiple runs.
type ThroughputStats struct {
	// AcceptedSpansPerSec is the accepted spans/sec measured on the Ingest API.
	AcceptedSpansPerSec []float64
	// CommittedSpansPerSec is the committed spans/sec measured on the Writer Lake.
	CommittedSpansPerSec []float64
}

// LatencyStats holds end-to-end ingest-to-queryable latency measurements.
type LatencyStats struct {
	// Latencies is the raw latency for each span across all runs, sorted ascending.
	Latencies []time.Duration
}

// Fprint writes a formatted throughput/latency report to w.
func (t *ThroughputStats) Fprint(w io.Writer, cadence time.Duration) {
	fmt.Fprintf(w, "=== Ingest Throughput Results ===\n")
	fmt.Fprintf(w, "Commit cadence (batch-flush interval): %s\n\n", cadence)

	if len(t.AcceptedSpansPerSec) == 0 {
		fmt.Fprint(w, "No data collected.\n")
		return
	}

	acceptedSorted := make([]float64, len(t.AcceptedSpansPerSec))
	copy(acceptedSorted, t.AcceptedSpansPerSec)
	sort.Float64s(acceptedSorted)

	committedSorted := make([]float64, len(t.CommittedSpansPerSec))
	copy(committedSorted, t.CommittedSpansPerSec)
	sort.Float64s(committedSorted)

	fmt.Fprint(w, "Accepted spans/sec (throughput):\n")
	fmt.Fprintf(w, "  p50:  %.1f\n", float64Value(acceptedSorted, 0.50))
	fmt.Fprintf(w, "  p95:  %.1f\n", float64Value(acceptedSorted, 0.95))
	fmt.Fprintf(w, "  p99:  %.1f\n", float64Value(acceptedSorted, 0.99))
	fmt.Fprint(w, "\n")

	if len(committedSorted) > 0 {
		fmt.Fprint(w, "Committed spans/sec (Writer → Lake):\n")
		fmt.Fprintf(w, "  p50:  %.1f\n", float64Value(committedSorted, 0.50))
		fmt.Fprintf(w, "  p95:  %.1f\n", float64Value(committedSorted, 0.95))
		fmt.Fprintf(w, "  p99:  %.1f\n", float64Value(committedSorted, 0.99))
		fmt.Fprint(w, "\n")
	}
}

// Report writes a formatted throughput/latency report to stdout.
func (t *ThroughputStats) Report(cadence time.Duration) {
	t.Fprint(os.Stdout, cadence)
}

// Fprint writes a formatted latency report to w.
func (l *LatencyStats) Fprint(w io.Writer, cadence time.Duration) {
	fmt.Fprintf(w, "=== End-to-End Ingest-to-Queryable Latency ===\n")
	fmt.Fprintf(w, "Commit cadence (batch-flush interval): %s\n\n", cadence)

	if len(l.Latencies) == 0 {
		fmt.Fprint(w, "No data collected.\n")
		return
	}

	sort.Slice(l.Latencies, func(i, j int) bool { return l.Latencies[i] < l.Latencies[j] })

	p := ComputePercentiles(l.Latencies)

	fmt.Fprint(w, "Latency p50/p95/p99:\n")
	fmt.Fprintf(w, "  p50:  %s\n", p.P50.Round(time.Millisecond))
	fmt.Fprintf(w, "  p95:  %s\n", p.P95.Round(time.Millisecond))
	fmt.Fprintf(w, "  p99:  %s\n", p.P99.Round(time.Millisecond))
	fmt.Fprint(w, "\n")
}

// Report writes a formatted latency report to stdout.
func (l *LatencyStats) Report(cadence time.Duration) {
	l.Fprint(os.Stdout, cadence)
}

// float64Value returns the value at the given percentile (0–1) from a sorted
// float64 slice.  idx = floor(p * len), clamped to [0, len-1].
func float64Value(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	idx := int(p * float64(len(sorted)))
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}

// sortDurations sorts a slice of time.Duration in ascending order in-place.
func sortDurations(durations []time.Duration) {
	sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
}