package normalizer_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/omneval/omneval/internal/domain"
	"github.com/omneval/omneval/internal/normalizer"
)

// --- Validation Tests ---

func TestNormalize_RejectsInvalidSpanID(t *testing.T) {
	n := normalizer.New()
	_, err := n.Normalize(context.Background(), map[string]any{
		"span_id":    "short",
		"trace_id":   "0123456789abcdef0123456789abcdef",
		"project_id": "proj-1",
	})
	if err == nil {
		t.Fatal("expected error for short span_id, got nil")
	}
}

func TestNormalize_RejectsNonHexSpanID(t *testing.T) {
	n := normalizer.New()
	_, err := n.Normalize(context.Background(), map[string]any{
		"span_id":    "qa01234567890001",
		"trace_id":   "0123456789abcdef0123456789abcdef",
		"project_id": "proj-1",
	})
	if err == nil {
		t.Fatal("expected error for non-hex span_id, got nil")
	}
	if strings.Contains(err.Error(), "encoding/hex") {
		t.Errorf("error message should not expose internal Go encoding: %q", err.Error())
	}
	if !strings.Contains(err.Error(), "16-character lowercase hex string") {
		t.Errorf("error message should mention format: %q", err.Error())
	}
}

func TestNormalize_RejectsUppercaseSpanID(t *testing.T) {
	n := normalizer.New()
	_, err := n.Normalize(context.Background(), map[string]any{
		"span_id":    "0123456789ABCDEF",
		"trace_id":   "0123456789abcdef0123456789abcdef",
		"project_id": "proj-1",
	})
	if err == nil {
		t.Fatal("expected error for uppercase span_id, got nil")
	}
}

func TestNormalize_RejectsUppercaseTraceID(t *testing.T) {
	n := normalizer.New()
	_, err := n.Normalize(context.Background(), map[string]any{
		"span_id":    "0123456789abcdef",
		"trace_id":   "0123456789ABCDEF0123456789ABCDEF",
		"project_id": "proj-1",
	})
	if err == nil {
		t.Fatal("expected error for uppercase trace_id, got nil")
	}
}

func TestNormalize_RejectsEmptySpanID(t *testing.T) {
	n := normalizer.New()
	_, err := n.Normalize(context.Background(), map[string]any{
		"span_id":    "",
		"trace_id":   "0123456789abcdef0123456789abcdef",
		"project_id": "proj-1",
	})
	if err == nil {
		t.Fatal("expected error for empty span_id, got nil")
	}
}

func TestNormalize_RejectsInvalidTraceID(t *testing.T) {
	n := normalizer.New()
	_, err := n.Normalize(context.Background(), map[string]any{
		"span_id":    "0123456789abcdef",
		"trace_id":   "short",
		"project_id": "proj-1",
	})
	if err == nil {
		t.Fatal("expected error for short trace_id, got nil")
	}
}

func TestNormalize_RejectsEmptyTraceID(t *testing.T) {
	n := normalizer.New()
	_, err := n.Normalize(context.Background(), map[string]any{
		"span_id":    "0123456789abcdef",
		"trace_id":   "",
		"project_id": "proj-1",
	})
	if err == nil {
		t.Fatal("expected error for empty trace_id, got nil")
	}
}

func TestNormalize_RejectsInvalidKind(t *testing.T) {
	n := normalizer.New()
	_, err := n.Normalize(context.Background(), map[string]any{
		"span_id":    "0123456789abcdef",
		"trace_id":   "0123456789abcdef0123456789abcdef",
		"kind":       "unknown_kind",
		"project_id": "proj-1",
	})
	if err == nil {
		t.Fatal("expected error for invalid kind, got nil")
	}
}

// --- Normalization Tests ---

func TestNormalize_StringInputWrappedAsUserMessage(t *testing.T) {
	n := normalizer.New()
	span, err := n.Normalize(context.Background(), map[string]any{
		"span_id":    "0123456789abcdef",
		"trace_id":   "0123456789abcdef0123456789abcdef",
		"project_id": "proj-1",
		"input":      "Hello world",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var messages []map[string]any
	if err := json.Unmarshal([]byte(span.Input), &messages); err != nil {
		t.Fatalf("input is not valid JSON: %v", err)
	}
	if len(messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(messages))
	}
	if messages[0]["role"] != "user" {
		t.Errorf("role: got %q, want %q", messages[0]["role"], "user")
	}
	if messages[0]["content"] != "Hello world" {
		t.Errorf("content: got %q, want %q", messages[0]["content"], "Hello world")
	}
}

func TestNormalize_StringOutputWrappedAsAssistantMessage(t *testing.T) {
	n := normalizer.New()
	span, err := n.Normalize(context.Background(), map[string]any{
		"span_id":    "0123456789abcdef",
		"trace_id":   "0123456789abcdef0123456789abcdef",
		"project_id": "proj-1",
		"output":     "Response text",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var messages []map[string]any
	if err := json.Unmarshal([]byte(span.Output), &messages); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	if len(messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(messages))
	}
	if messages[0]["role"] != "assistant" {
		t.Errorf("role: got %q, want %q", messages[0]["role"], "assistant")
	}
}

func TestNormalize_JsonArrayInputPassthrough(t *testing.T) {
	n := normalizer.New()
	inputJSON := `[{"role":"user","content":"test"}]`
	span, err := n.Normalize(context.Background(), map[string]any{
		"span_id":    "0123456789abcdef",
		"trace_id":   "0123456789abcdef0123456789abcdef",
		"project_id": "proj-1",
		"input":      inputJSON,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if span.Input != inputJSON {
		t.Errorf("input: got %q, want %q", span.Input, inputJSON)
	}
}

func TestNormalize_SliceInputMarshaledToJSON(t *testing.T) {
	n := normalizer.New()
	span, err := n.Normalize(context.Background(), map[string]any{
		"span_id":    "0123456789abcdef",
		"trace_id":   "0123456789abcdef0123456789abcdef",
		"project_id": "proj-1",
		"input":      []any{map[string]any{"role": "user", "content": "hi"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var messages []map[string]any
	if err := json.Unmarshal([]byte(span.Input), &messages); err != nil {
		t.Fatalf("input is not valid JSON: %v", err)
	}
	if len(messages) != 1 || messages[0]["content"] != "hi" {
		t.Errorf("unexpected input: %s", span.Input)
	}
}

// --- Attribute Conversion Tests ---

func TestNormalize_Float64TokensConvertedToInt64(t *testing.T) {
	n := normalizer.New()
	span, err := n.Normalize(context.Background(), map[string]any{
		"span_id":       "0123456789abcdef",
		"trace_id":      "0123456789abcdef0123456789abcdef",
		"project_id":    "proj-1",
		"input_tokens":  float64(100),
		"output_tokens": float64(200),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if span.InputTokens != 100 {
		t.Errorf("input_tokens: got %d, want 100", span.InputTokens)
	}
	if span.OutputTokens != 200 {
		t.Errorf("output_tokens: got %d, want 200", span.OutputTokens)
	}
}

func TestNormalize_AttributesPreserved(t *testing.T) {
	n := normalizer.New()
	attrs := map[string]any{
		"custom.key": "value",
		"nested":     map[string]any{"a": 1},
	}
	span, err := n.Normalize(context.Background(), map[string]any{
		"span_id":    "0123456789abcdef",
		"trace_id":   "0123456789abcdef0123456789abcdef",
		"project_id": "proj-1",
		"attributes": attrs,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if span.Attributes == nil {
		t.Fatal("attributes should not be nil")
	}
	if span.Attributes["custom.key"] != "value" {
		t.Errorf("attributes.custom.key: got %v, want value", span.Attributes["custom.key"])
	}
}

// --- Nil Safety Tests ---

func TestNormalize_NilMapReturnsError(t *testing.T) {
	n := normalizer.New()
	_, err := n.Normalize(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error for nil input, got nil")
	}
}

func TestNormalize_EmptyMapReturnsError(t *testing.T) {
	n := normalizer.New()
	_, err := n.Normalize(context.Background(), map[string]any{})
	if err == nil {
		t.Fatal("expected error for empty map, got nil")
	}
}

// --- Full Valid Span Test ---

func TestNormalize_ValidSpanReturnsCorrectDomainSpan(t *testing.T) {
	n := normalizer.New()
	span, err := n.Normalize(context.Background(), map[string]any{
		"span_id":         "0123456789abcdef",
		"trace_id":        "0123456789abcdef0123456789abcdef",
		"parent_id":       "abcdef0123456789",
		"conversation_id": "conv-123",
		"project_id":      "proj-1",
		"service_name":    "svc-1",
		"name":            "test-span",
		"kind":            "llm",
		"model":           "gpt-4",
		"input_tokens":    int64(50),
		"output_tokens":   int64(25),
		"prompt_name":     "greeting",
		"prompt_version":  int64(3),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if span.SpanID != "0123456789abcdef" {
		t.Errorf("span_id: got %q, want %q", span.SpanID, "0123456789abcdef")
	}
	if span.TraceID != "0123456789abcdef0123456789abcdef" {
		t.Errorf("trace_id mismatch")
	}
	if span.ParentID != "abcdef0123456789" {
		t.Errorf("parent_id: got %q, want %q", span.ParentID, "abcdef0123456789")
	}
	if span.ConversationID != "conv-123" {
		t.Errorf("conversation_id: got %q, want %q", span.ConversationID, "conv-123")
	}
	if span.ProjectID != "proj-1" {
		t.Errorf("project_id: got %q, want %q", span.ProjectID, "proj-1")
	}
	if span.ServiceName != "svc-1" {
		t.Errorf("service_name: got %q, want %q", span.ServiceName, "svc-1")
	}
	if span.Name != "test-span" {
		t.Errorf("name: got %q, want %q", span.Name, "test-span")
	}
	if span.Kind != domain.SpanKindLLM {
		t.Errorf("kind: got %q, want %q", span.Kind, domain.SpanKindLLM)
	}
	if span.Model != "gpt-4" {
		t.Errorf("model: got %q, want %q", span.Model, "gpt-4")
	}
	if span.InputTokens != 50 {
		t.Errorf("input_tokens: got %d, want 50", span.InputTokens)
	}
	if span.OutputTokens != 25 {
		t.Errorf("output_tokens: got %d, want 25", span.OutputTokens)
	}
	if span.PromptName != "greeting" {
		t.Errorf("prompt_name: got %q, want %q", span.PromptName, "greeting")
	}
	if span.PromptVersion != 3 {
		t.Errorf("prompt_version: got %d, want 3", span.PromptVersion)
	}
}

func TestNormalize_EmptyKindDefaultsToEmpty(t *testing.T) {
	n := normalizer.New()
	span, err := n.Normalize(context.Background(), map[string]any{
		"span_id":    "0123456789abcdef",
		"trace_id":   "0123456789abcdef0123456789abcdef",
		"project_id": "proj-1",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if span.Kind != "" {
		t.Errorf("kind: got %q, want empty (default)", span.Kind)
	}
}

func TestNormalize_AllValidKindsAccepted(t *testing.T) {
	n := normalizer.New()
	for _, kind := range []string{"llm", "tool", "agent", "chain", "internal"} {
		t.Run(kind, func(t *testing.T) {
			span, err := n.Normalize(context.Background(), map[string]any{
				"span_id":    "0123456789abcdef",
				"trace_id":   "0123456789abcdef0123456789abcdef",
				"project_id": "proj-1",
				"kind":       kind,
			})
			if err != nil {
				t.Fatalf("unexpected error for kind %q: %v", kind, err)
			}
			if span.Kind != domain.SpanKind(kind) {
				t.Errorf("kind: got %q, want %q", span.Kind, kind)
			}
		})
	}
}

// --- Timestamp Tests ---

func TestNormalize_TimestampsPropagatedFromRawMap(t *testing.T) {
	n := normalizer.New()
	start := time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)
	end := time.Date(2024, 1, 15, 10, 30, 5, 0, time.UTC)

	span, err := n.Normalize(context.Background(), map[string]any{
		"span_id":    "0123456789abcdef",
		"trace_id":   "0123456789abcdef0123456789abcdef",
		"project_id": "proj-1",
		"start_time": start,
		"end_time":   end,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !span.StartTime.Equal(start) {
		t.Errorf("StartTime: got %v, want %v", span.StartTime, start)
	}
	if !span.EndTime.Equal(end) {
		t.Errorf("EndTime: got %v, want %v", span.EndTime, end)
	}
}

func TestNormalize_ZeroTimestampsWhenMissing(t *testing.T) {
	n := normalizer.New()
	span, err := n.Normalize(context.Background(), map[string]any{
		"span_id":    "0123456789abcdef",
		"trace_id":   "0123456789abcdef0123456789abcdef",
		"project_id": "proj-1",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !span.StartTime.IsZero() {
		t.Errorf("StartTime should be zero when missing, got %v", span.StartTime)
	}
	if !span.EndTime.IsZero() {
		t.Errorf("EndTime should be zero when missing, got %v", span.EndTime)
	}
}
