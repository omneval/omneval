package benchmark

import (
	"strings"
	"testing"
)

func TestScalingResultChartMarkdown(t *testing.T) {
	result := &ScalingResult{
		Replicas: []int{1, 2, 4, 8},
		Stats: map[int]*ThroughputStats{
			1: {
				AcceptedSpansPerSec:  []float64{1000, 1050, 1100},
				CommittedSpansPerSec: []float64{900, 950, 1000},
			},
			2: {
				AcceptedSpansPerSec:  []float64{1900, 2000, 2100},
				CommittedSpansPerSec: []float64{1800, 1900, 2000},
			},
			4: {
				AcceptedSpansPerSec:  []float64{3600, 3800, 4000},
				CommittedSpansPerSec: []float64{3400, 3600, 3800},
			},
			8: {
				AcceptedSpansPerSec:  []float64{6500, 7000, 7500},
				CommittedSpansPerSec: []float64{6000, 6500, 7000},
			},
		},
	}

	md := result.WriteMarkdown()

	if !strings.Contains(md, "| Replicas") {
		t.Errorf("expected table header with 'Replicas', got:\n%s", md)
	}
	if !strings.Contains(md, "Accepted spans/s") {
		t.Errorf("expected 'Accepted spans/s' column, got:\n%s", md)
	}
	if !strings.Contains(md, "1000") {
		t.Errorf("expected first data row value 1000, got:\n%s", md)
	}
	if !strings.Contains(md, "Scaling factor") {
		t.Errorf("expected 'Scaling factor' column, got:\n%s", md)
	}
}

func TestScalingResultChartMarkdownEmpty(t *testing.T) {
	result := &ScalingResult{
		Replicas: nil,
		Stats:    nil,
	}

	md := result.WriteMarkdown()

	if !strings.Contains(md, "No data collected") {
		t.Errorf("expected 'No data collected' for empty result, got:\n%s", md)
	}
}

func TestScalingResultScalingFactor(t *testing.T) {
	// At N=1, accepted = 1000/s. At N=8, accepted = 8000/s => scaling factor = 8.0x.
	result := &ScalingResult{
		Replicas: []int{1, 2, 4, 8},
		Stats: map[int]*ThroughputStats{
			1: {
				AcceptedSpansPerSec: []float64{1000},
			},
			2: {
				AcceptedSpansPerSec: []float64{2000},
			},
			4: {
				AcceptedSpansPerSec: []float64{4000},
			},
			8: {
				AcceptedSpansPerSec: []float64{8000},
			},
		},
	}

	md := result.WriteMarkdown()

	// The 8-replica row should show ~8.0x scaling factor relative to N=1.
	if !strings.Contains(md, "8.00x") {
		t.Errorf("expected '8.00x' scaling factor for N=8, got:\n%s", md)
	}
	// The 2-replica row should show ~2.0x.
	if !strings.Contains(md, "2.00x") {
		t.Errorf("expected '2.00x' scaling factor for N=2, got:\n%s", md)
	}
}

func TestScalingResultIncludesPercentiles(t *testing.T) {
	// With 3 runs each at every replica count, the table should include
	// both p50 and p95 columns for accepted and committed throughput.
	result := &ScalingResult{
		Replicas: []int{1},
		Stats: map[int]*ThroughputStats{
			1: {
				AcceptedSpansPerSec:  []float64{100, 200, 300},
				CommittedSpansPerSec: []float64{90, 190, 290},
			},
		},
	}

	md := result.WriteMarkdown()

	// p50 of [100,200,300] = 200. p95 of [100,200,300] = 300.
	// The table should contain these values.
	if !strings.Contains(md, "Accepted spans/s (p50)") {
		t.Errorf("expected 'Accepted spans/s (p50)' header, got:\n%s", md)
	}
	if !strings.Contains(md, "Committed spans/s (p50)") {
		t.Errorf("expected 'Committed spans/s (p50)' header, got:\n%s", md)
	}
}

func TestRunScalingBenchCollectsDataPerReplica(t *testing.T) {
	// Use a mock runner that returns a single data point per call,
	// simulating the result of one ingest run at the given replica count.
	callCount := map[int]int{}

	cfg := &ScalingBenchConfig{
		Replicas: []int{1, 2},
		RunCount: 3,
		WriterID: "writer-1",
		InjectStat: func(replica int) (*ThroughputStats, error) {
			callCount[replica]++
			// Each call returns one data point.
			// N=1: ~100/s, N=2: ~200/s.
			rate := float64(100*replica) + float64(callCount[replica])
			return &ThroughputStats{
				AcceptedSpansPerSec:  []float64{rate},
				CommittedSpansPerSec: []float64{rate - 10},
			}, nil
		},
	}

	result := RunScalingBench(cfg)

	if len(result.Replicas) != 2 {
		t.Errorf("expected 2 replica counts, got %d", len(result.Replicas))
	}
	if result.Stats[1] == nil {
		t.Fatal("expected stats for replica 1")
	}
	if result.Stats[2] == nil {
		t.Fatal("expected stats for replica 2")
	}
	// Each injected run has 1 data point, called 3 times.
	if len(result.Stats[1].AcceptedSpansPerSec) != 3 {
		t.Errorf("expected 3 accepted data points for N=1, got %d",
			len(result.Stats[1].AcceptedSpansPerSec))
	}
	if len(result.Stats[2].AcceptedSpansPerSec) != 3 {
		t.Errorf("expected 3 accepted data points for N=2, got %d",
			len(result.Stats[2].AcceptedSpansPerSec))
	}
}

func TestRunScalingBenchEmptyReplicas(t *testing.T) {
	cfg := &ScalingBenchConfig{
		Replicas:   nil,
		WriterID:   "writer-1",
		InjectStat: nil,
	}

	result := RunScalingBench(cfg)

	if result == nil {
		t.Fatal("expected non-nil result for empty replicas")
	}
	if len(result.Replicas) != 0 {
		t.Errorf("expected 0 replicas, got %d", len(result.Replicas))
	}
}