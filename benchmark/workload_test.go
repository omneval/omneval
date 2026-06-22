package benchmark

import (
	"strings"
	"testing"
	"time"
)

func TestGenerateTraces_MultiSpanFanOut(t *testing.T) {
	projID := "benchmark-project"
	numTraces := 3
	spansPerTrace := 5

	traces := GenerateTraces(projID, numTraces, spansPerTrace)

	if len(traces) != numTraces {
		t.Fatalf("expected %d traces, got %d", numTraces, len(traces))
	}

	for i, trace := range traces {
		if len(trace.Spans) != spansPerTrace {
			t.Errorf("trace %d: expected %d spans, got %d", i, spansPerTrace, len(trace.Spans))
		}

		// Check that the root span (index 0) has no parent.
		if trace.Spans[0].ParentID != "" {
			t.Errorf("trace %d span 0: expected empty parent_id, got %q", i, trace.Spans[0].ParentID)
		}

		// Check that child spans reference the root span.
		for j := 1; j < len(trace.Spans); j++ {
			if trace.Spans[j].ParentID != trace.Spans[0].SpanID {
				t.Errorf("trace %d span %d: expected parent_id %q, got %q",
					i, j, trace.Spans[0].SpanID, trace.Spans[j].ParentID)
			}
		}

		// All spans in a trace share the same trace_id.
		for j := 1; j < len(trace.Spans); j++ {
			if trace.Spans[j].TraceID != trace.Spans[0].TraceID {
				t.Errorf("trace %d span %d: trace_id mismatch", i, j)
			}
		}

		// Check that each span has ~1.5KB of Input and ~1.5KB of Output.
		for j, span := range trace.Spans {
			inputLen := len(span.Input)
			outputLen := len(span.Output)
			if inputLen < 1400 || inputLen > 1600 {
				t.Errorf("trace %d span %d: input size %d outside ~1.5KB range [1400, 1600]",
					i, j, inputLen)
			}
			if outputLen < 1400 || outputLen > 1600 {
				t.Errorf("trace %d span %d: output size %d outside ~1.5KB range [1400, 1600]",
					i, j, outputLen)
			}
		}
	}
}

func TestGenerateTraces_UniqueTraceIDs(t *testing.T) {
	projID := "benchmark-project"
	numTraces := 10

	traces := GenerateTraces(projID, numTraces, 3)
	seen := make(map[string]bool)

	for _, trace := range traces {
		if seen[trace.TraceID] {
			t.Fatalf("duplicate trace_id: %s", trace.TraceID)
		}
		seen[trace.TraceID] = true
	}
}

func TestGenerateTraces_ProjectID(t *testing.T) {
	traces := GenerateTraces("test-project", 1, 2)
	if len(traces) != 1 {
		t.Fatalf("expected 1 trace, got %d", len(traces))
	}
	for _, span := range traces[0].Spans {
		if span.ProjectID != "test-project" {
			t.Errorf("project_id = %q, want %q", span.ProjectID, "test-project")
		}
	}
}

func TestTraceToNativeSpan(t *testing.T) {
	span := &Span{
		SpanID:    "aa00000000000001",
		TraceID:    "bb000000000000000000000000000001",
		ProjectID:  "proj-1",
		SpanKind:   "tool",
		Name:       "search",
		Input:      strings.Repeat("x", 1500),
		Output:     strings.Repeat("y", 1500),
		StartTime:  time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		EndTime:    time.Date(2026, 1, 1, 0, 0, 1, 0, time.UTC),
	}

	native := TraceToNativeSpan(span)

	if native.SpanID != "aa00000000000001" {
		t.Errorf("span_id = %q, want %q", native.SpanID, "aa00000000000001")
	}
	if len(native.Input) != 1500 {
		t.Errorf("input len = %d, want 1500", len(native.Input))
	}
	if len(native.Output) != 1500 {
		t.Errorf("output len = %d, want 1500", len(native.Output))
	}
}