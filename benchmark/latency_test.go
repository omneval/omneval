package benchmark

import (
	"strings"
	"testing"
	"time"
)

func TestSpanSendTime(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	s := &Span{
		SpanID:   "aa00000000000001",
		SendTime: now,
	}

	if !s.SendTime.Equal(now) {
		t.Errorf("SendTime = %v, want %v", s.SendTime, now)
	}
}

func TestSpanSendTimeZero(t *testing.T) {
	s := &Span{SpanID: "aa00000000000001"}

	if !s.SendTime.IsZero() {
		t.Errorf("expected zero SendTime, got %v", s.SendTime)
	}
}

func TestLatencyStatsReportEmpty(t *testing.T) {
	stats := &LatencyStats{}
	var buf strings.Builder
	stats.Fprint(&buf, 0)
	out := buf.String()
	if !strings.Contains(out, "No data collected") {
		t.Errorf("expected 'No data collected' output, got %q", out)
	}
}

func TestLatencyStatsReportNonEmpty(t *testing.T) {
	stats := &LatencyStats{
		Latencies: []time.Duration{
			100 * time.Millisecond,
			200 * time.Millisecond,
			300 * time.Millisecond,
		},
	}
	var buf strings.Builder
	stats.Fprint(&buf, 5*time.Second)
	out := buf.String()
	if !strings.Contains(out, "p50") {
		t.Errorf("expected 'p50' in report, got %q", out)
	}
	if !strings.Contains(out, "200ms") {
		t.Errorf("expected '200ms' p50 value in report, got %q", out)
	}
}

func TestDefaultLatencyPollConfig(t *testing.T) {
	cfg := defaultLatencyPollConfig()
	if cfg.PollInterval != 250*time.Millisecond {
		t.Errorf("PollInterval = %v, want %v", cfg.PollInterval, 250*time.Millisecond)
	}
	if cfg.Timeout != 60*time.Second {
		t.Errorf("Timeout = %v, want %v", cfg.Timeout, 60*time.Second)
	}
}

func TestLatencyClientNew(t *testing.T) {
	c := NewLatencyClient("http://localhost:8000/api/v1/spans/query", "test-key")
	if c == nil {
		t.Fatal("expected non-nil LatencyClient")
	}
	if c.queryEndpoint != "http://localhost:8000/api/v1/spans/query" {
		t.Errorf("queryEndpoint = %q, want %q", c.queryEndpoint, "http://localhost:8000/api/v1/spans/query")
	}
	if c.apiKey != "test-key" {
		t.Errorf("apiKey = %q, want %q", c.apiKey, "test-key")
	}
}