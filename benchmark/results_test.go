package benchmark

import (
	"testing"
	"time"
)

func TestPercentile(t *testing.T) {
	// Sorted durations: 1, 2, 3, 4, 5, 6, 7, 8, 9, 10
	durations := make([]time.Duration, 10)
	for i := range durations {
		durations[i] = time.Duration(i+1) * time.Second
	}

	// p50: index = int(0.50 * 10) = 5 → durations[5] = 6s
	got := percentile(durations, 0.50)
	if got != 6*time.Second {
		t.Errorf("percentile(0.50) = %v, want 6s", got)
	}

	// p95: index = int(0.95 * 10) = 9 → durations[9] = 10s
	got = percentile(durations, 0.95)
	if got != 10*time.Second {
		t.Errorf("percentile(0.95) = %v, want 10s", got)
	}

	// p99: index = int(0.99 * 10) = 9 → durations[9] = 10s
	got = percentile(durations, 0.99)
	if got != 10*time.Second {
		t.Errorf("percentile(0.99) = %v, want 10s", got)
	}

	// p0: index = 0 → durations[0] = 1s
	got = percentile(durations, 0.0)
	if got != 1*time.Second {
		t.Errorf("percentile(0.0) = %v, want 1s", got)
	}
}

func TestPercentileSingleElement(t *testing.T) {
	durations := []time.Duration{5 * time.Second}

	for _, p := range []float64{0.0, 0.50, 0.90, 0.95, 0.99, 1.0} {
		got := percentile(durations, p)
		if got != 5*time.Second {
			t.Errorf("percentile(%.2f) with single element = %v, want 5s", p, got)
		}
	}
}

func TestPercentileEmpty(t *testing.T) {
	got := percentile(nil, 0.50)
	if got != 0 {
		t.Errorf("percentile on empty = %v, want 0", got)
	}
}