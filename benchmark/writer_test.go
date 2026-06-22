package benchmark

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"
)

func TestWriterClientMeasureCommitted(t *testing.T) {
	// Serve Prometheus metrics with a known counter value.
	metrics := `# HELP omneval_writer_spans_written_total Total number of spans written to DuckDB.
# TYPE omneval_writer_spans_written_total counter
omneval_writer_spans_written_total{project_id="demo-project",} 10000
`
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.Header().Set("Content-Type", `text/plain; version=0.0.4; charset=utf-8`)
		w.Write([]byte(metrics))
	}))
	defer srv.Close()

	c := NewWriterClient(srv.URL, "test-key")

	// Use a short timeout so MeasureCommitted exits after a couple samples.
	ctx, cancel := context.WithTimeout(context.Background(), 2500*time.Millisecond)
	defer cancel()

	var counts []int64
	err := c.MeasureCommitted(ctx, "demo-project", 1*time.Second, func(count int64, rate float64) {
		counts = append(counts, count)
	})

	if !called {
		t.Fatal("expected Prometheus /metrics endpoint to be called")
	}
	if err != nil && err != context.DeadlineExceeded {
		t.Fatalf("expected deadline or no error, got %v", err)
	}
	if len(counts) == 0 {
		t.Fatal("expected at least one counter value")
	}
	if counts[0] != 10000 {
		t.Errorf("count = %d, want 10000", counts[0])
	}
}

func TestWriterClientBadStatus(t *testing.T) {
	reqCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqCount++
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	c := NewWriterClient(srv.URL, "test-key")

	// Timeout so the retry loop in MeasureCommitted exits.
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	var called bool
	err := c.MeasureCommitted(ctx, "demo-project", 100*time.Millisecond, func(_ int64, _ float64) {
		called = true
	})

	// We expect the HTTP call to fail; the function itself may exit via context deadline.
	if reqCount == 0 {
		t.Fatal("expected at least one HTTP request")
	}
	// The callback should never fire because every response is non-200.
	if called {
		t.Fatal("callback should not have been called for bad status")
	}
	if err != context.DeadlineExceeded {
		t.Logf("got error %v (expected context deadline)", err)
	}
}

func TestFetchCounterParsing(t *testing.T) {
	line := `omneval_writer_spans_written_total{project_id="demo-project",} 10000`

	if !contains(line, "omneval_writer_spans_written_total") {
		t.Fatal("line should contain metric name")
	}
	if !contains(line, `project_id="demo-project"`) {
		t.Fatal("line should contain project_id label")
	}

	lastSpace := lastIndexOf(line, " ")
	if lastSpace == -1 {
		t.Fatal("expected to find last space")
	}
	valStr := line[lastSpace+1:]
	val, err := strconv.ParseInt(valStr, 10, 64)
	if err != nil {
		t.Fatalf("failed to parse %q: %v", valStr, err)
	}
	if val != 10000 {
		t.Errorf("parsed value = %d, want 10000", val)
	}

	line2 := `some_metric 42`
	lastSpace2 := lastIndexOf(line2, " ")
	valStr2 := line2[lastSpace2+1:]
	val2, err := strconv.ParseInt(valStr2, 10, 64)
	if err != nil {
		t.Fatalf("failed to parse %q: %v", valStr2, err)
	}
	if val2 != 42 {
		t.Errorf("parsed value = %d, want 42", val2)
	}
}

func TestFetchCounterFullLine(t *testing.T) {
	// Full mock: verify fetchCounter actually returns a value.
	metrics := "omneval_writer_spans_written_total{project_id=\"demo-project\",} 7777\n"

	reqNum := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqNum++
		w.Write([]byte(metrics))
	}))
	defer srv.Close()

	c := NewWriterClient(srv.URL, "")

	val, err := c.fetchCounter(context.Background(), "omneval_writer_spans_written_total", "demo-project")
	if reqNum == 0 {
		t.Fatal("expected HTTP request to be made")
	}
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if val != 7777 {
		t.Errorf("value = %d, want 7777", val)
	}
}

func TestFetchCounterConcatenated(t *testing.T) {
	reqNum := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqNum++
		w.Write([]byte("omneval_writer_spans_written_total{project_id=\"demo-project\",} " + strconv.FormatInt(5000, 10) + "\n"))
	}))
	defer srv.Close()

	c := NewWriterClient(srv.URL, "")

	val, err := c.fetchCounter(context.Background(), "omneval_writer_spans_written_total", "demo-project")
	if reqNum == 0 {
		t.Fatal("expected HTTP request to be made")
	}
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if val != 5000 {
		t.Errorf("value = %d, want 5000", val)
	}
}

func TestWriterClientTwoSamplesYieldRate(t *testing.T) {
	sampleNo := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sampleNo++
		w.Header().Set("Content-Type", `text/plain; version=0.0.4; charset=utf-8`)
		w.Write([]byte("omneval_writer_spans_written_total{project_id=\"demo-project\",} " + strconv.FormatInt(
			func() int64 { if sampleNo == 1 { return 5000 }; return 10000 }(), 10) + "\n"))
	}))
	defer srv.Close()

	c := NewWriterClient(srv.URL, "test-key")

	// Use a short context timeout so MeasureCommitted exits after ~3 samples.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	type sample struct {
		count int64
		rate  float64
	}
	var samples []sample
	err := c.MeasureCommitted(ctx, "demo-project", 1*time.Second, func(count int64, rate float64) {
		samples = append(samples, sample{count: count, rate: rate})
	})

	if err != nil && err != context.DeadlineExceeded {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(samples) < 2 {
		t.Fatalf("expected at least 2 samples, got %d", len(samples))
	}
	if samples[0].count != 5000 {
		t.Errorf("sample[0].count = %d, want 5000", samples[0].count)
	}
	if samples[1].count != 10000 {
		t.Errorf("sample[1].count = %d, want 10000", samples[1].count)
	}
	// Rate should be ~5000 spans/sec (5000 deltas / 1 second)
	if samples[1].rate < 4000 || samples[1].rate > 6000 {
		t.Errorf("sample[1].rate = %.1f, expected ~5000", samples[1].rate)
	}
}

func itoa(v int64) string {
	if v == 0 {
		return "0"
	}
	neg := v < 0
	v = -v
	var b []byte
	for v > 0 {
		b = append([]byte{'0' + byte(v%10)}, b...)
		v /= 10
	}
	if neg {
		b = append([]byte{'-'}, b...)
	}
	return string(b)
}