package main

import (
	"os"
	"strings"
	"testing"
	"time"

	benchmark "github.com/omneval/omneval/benchmark"
)

func TestPercentile(t *testing.T) {
	tests := []struct {
		name   string
		sorted []float64
		p      float64
		want   float64
	}{
		{
			name:   "median of three values",
			sorted: []float64{100, 200, 300},
			p:      0.50,
			want:   200,
		},
		{
			name:   "p95 of ten values",
			sorted: []float64{10, 20, 30, 40, 50, 60, 70, 80, 90, 100},
			p:      0.95,
			// idx = int(0.95 * 9) = int(8.55) = 8, so sorted[8] = 90
			want: 90,
		},
		{
			name:   "single element",
			sorted: []float64{42},
			p:      0.50,
			want:   42,
		},
		{
			name:   "zero index",
			sorted: []float64{10, 20, 30},
			p:      0.00,
			want:   10,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := percentile(tt.sorted, tt.p)
			if got != tt.want {
				t.Errorf("percentile(%v, %.2f) = %v, want %v", tt.sorted, tt.p, got, tt.want)
			}
		})
	}
}

func TestPercentileEmpty(t *testing.T) {
	got := percentile([]float64{}, 0.50)
	if got != 0 {
		t.Errorf("percentile([]float64{}, 0.50) = %v, want 0", got)
	}
}

func TestWriteScalingResultsDocGeneratesMarkdown(t *testing.T) {
	result := &benchmark.ScalingResult{
		Replicas: []int{1, 2},
		Stats: map[int]*benchmark.ThroughputStats{
			1: {
				AcceptedSpansPerSec:  []float64{100, 110, 120},
				CommittedSpansPerSec: []float64{90, 100, 110},
			},
			2: {
				AcceptedSpansPerSec:  []float64{200, 210, 220},
				CommittedSpansPerSec: []float64{180, 190, 200},
			},
		},
	}

	cfg := writeScalingConfig{
		Endpoint:        "http://localhost:8000/api/v1/spans",
		QueryEndpoint:   "http://localhost:8000/api/v1/spans/query",
		ProjectID:       "test-project",
		CommitCadence:   10 * time.Second,
		Replicas:        []int{1, 2},
		RunCount:        5,
		WarmupRuns:      2,
		ScalingResult:   result,
		GenerateTime:    time.Date(2026, 6, 22, 0, 0, 0, 0, time.UTC),
		KubectlDeployment: "omneval-writer",
	}

	content, err := writeScalingResultsDocContent(cfg)
	if err != nil {
		t.Fatalf("writeScalingResultsDocContent failed: %v", err)
	}

	// Check key sections are present.
	if !strings.Contains(content, "# Horizontal Scaling Chart") {
		t.Error("expected scaling chart title")
	}
	if !strings.Contains(content, "## Methodology") {
		t.Error("expected methodology section")
	}
	if !strings.Contains(content, "## Results") {
		t.Error("expected results section")
	}
	if !strings.Contains(content, "## Interpretation") {
		t.Error("expected interpretation section")
	}
	if !strings.Contains(content, "## Reproducing This Benchmark") {
		t.Error("expected reproducing section")
	}
	if !strings.Contains(content, "Scaling factor") {
		t.Error("expected scaling factor in table")
	}
}

func TestWriteScalingResultsDocEmptyScalingResult(t *testing.T) {
	result := &benchmark.ScalingResult{
		Replicas: nil,
		Stats:    nil,
	}

	cfg := writeScalingConfig{
		Endpoint:        "http://localhost:8000/api/v1/spans",
		QueryEndpoint:   "http://localhost:8000/api/v1/spans/query",
		ProjectID:       "test-project",
		CommitCadence:   10 * time.Second,
		Replicas:        []int{1, 2},
		RunCount:        5,
		WarmupRuns:      2,
		ScalingResult:   result,
		GenerateTime:    time.Date(2026, 6, 22, 0, 0, 0, 0, time.UTC),
		KubectlDeployment: "omneval-writer",
	}

	content, err := writeScalingResultsDocContent(cfg)
	if err != nil {
		t.Fatalf("writeScalingResultsDocContent failed: %v", err)
	}

	// Should include "No data collected" from WriteMarkdown.
	if !strings.Contains(content, "No data collected") {
		t.Error("expected 'No data collected' in empty result")
	}
}

func TestWriteScalingResultsDocContainsScalingBehavior(t *testing.T) {
	// Simulate linear scaling: N=1 = 100/s, N=2 = 200/s, N=4 = 400/s
	result := &benchmark.ScalingResult{
		Replicas: []int{1, 2, 4},
		Stats: map[int]*benchmark.ThroughputStats{
			1: {
				AcceptedSpansPerSec: []float64{100},
			},
			2: {
				AcceptedSpansPerSec: []float64{200},
			},
			4: {
				AcceptedSpansPerSec: []float64{400},
			},
		},
	}

	cfg := writeScalingConfig{
		Endpoint:        "http://localhost:8000/api/v1/spans",
		QueryEndpoint:   "http://localhost:8000/api/v1/spans/query",
		ProjectID:       "test-project",
		CommitCadence:   10 * time.Second,
		Replicas:        []int{1, 2, 4},
		RunCount:        5,
		WarmupRuns:      2,
		ScalingResult:   result,
		GenerateTime:    time.Date(2026, 6, 22, 0, 0, 0, 0, time.UTC),
		KubectlDeployment: "omneval-writer",
	}

	content, err := writeScalingResultsDocContent(cfg)
	if err != nil {
		t.Fatalf("writeScalingResultsDocContent failed: %v", err)
	}

	// Should contain scaling behavior analysis with N=2 and N=4 entries.
	if !strings.Contains(content, "**Observed scaling**") {
		t.Error("expected observed scaling section")
	}
	if !strings.Contains(content, "N=2") {
		t.Error("expected N=2 in scaling behavior")
	}
	if !strings.Contains(content, "N=4") {
		t.Error("expected N=4 in scaling behavior")
	}
}

// Helper: writeScalingResultsDocContent generates the markdown content
// without writing to disk (for testing).
func writeScalingResultsDocContent(cfg writeScalingConfig) (string, error) {
	// Write to a temporary file, read it back, and return content.
	tmpFile := "/tmp/omneval-scaling-test.md"
	if err := writeScalingResultsDoc(tmpFile, cfg); err != nil {
		return "", err
	}
	content, err := os.ReadFile(tmpFile)
	if err != nil {
		return "", err
	}
	// Clean up.
	_ = os.Remove(tmpFile)
	return string(content), nil
}