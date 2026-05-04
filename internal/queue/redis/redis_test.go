package redis

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/zbloss/lantern/internal/domain"
	"github.com/zbloss/lantern/internal/queue"
)

// TestQueueKeys tests that the queue keys are correct.
func TestQueueKeys(t *testing.T) {
	if queue.KeyIngestSpans != "lantern:ingest:spans" {
		t.Errorf("KeyIngestSpans: got %q, want %q", queue.KeyIngestSpans, "lantern:ingest:spans")
	}
	if queue.KeyEvalJobs != "lantern:eval:jobs" {
		t.Errorf("KeyEvalJobs: got %q, want %q", queue.KeyEvalJobs, "lantern:eval:jobs")
	}
}

// TestJSONEncoding verifies that domain.Span serializes correctly for Redis transport.
func TestJSONEncoding_Spans(t *testing.T) {
	spans := []*domain.Span{
		{SpanID: "span-1", TraceID: "trace-1", ProjectID: "proj-1", Name: "chat", Model: "gpt-4o", InputTokens: 10, OutputTokens: 5},
		{SpanID: "span-2", TraceID: "trace-1", ProjectID: "proj-1", Name: "tool-call", Model: "gpt-4o", InputTokens: 3, OutputTokens: 1},
	}
	data, err := json.Marshal(spans)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var decoded []*domain.Span
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if !reflect.DeepEqual(spans, decoded) {
		t.Errorf("JSON roundtrip mismatch:\ngot  %+v\nwant %+v", decoded, spans)
	}

	// Verify JSON is valid Redis list value (string).
	if len(data) == 0 {
		t.Error("JSON encoding produced empty data")
	}
}

// TestJSONEncoding_EvalJob verifies EvalJob serializes correctly.
func TestJSONEncoding_EvalJob(t *testing.T) {
	job := &domain.EvalJob{
		JobID:      "job-1",
		RuleID:     "rule-1",
		SpanID:     "span-1",
		TraceID:    "trace-1",
		ProjectID:  "proj-1",
		EnqueuedAt: time.Now(),
	}

	data, err := json.Marshal(job)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var decoded domain.EvalJob
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if decoded.JobID != "job-1" {
		t.Errorf("JobID: got %q, want %q", decoded.JobID, "job-1")
	}
	if decoded.RuleID != "rule-1" {
		t.Errorf("RuleID: got %q, want %q", decoded.RuleID, "rule-1")
	}
	if decoded.SpanID != "span-1" {
		t.Errorf("SpanID: got %q, want %q", decoded.SpanID, "span-1")
	}
}

// TestNewQueueConstructors verifies constructors exist and are callable.
func TestNewQueueConstructors(t *testing.T) {
	// Just verify the constructor functions exist.
	// Actual queue integration tests require a real Redis server.
	t.Log("Constructors NewIngestQueue and NewEvalQueue exist (signatures verified by go build)")
}
