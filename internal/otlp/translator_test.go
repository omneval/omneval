package otlp

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/zbloss/lantern/internal/domain"
)

func TestTranslate_EmptyInput(t *testing.T) {
	spans, err := Translate("proj-1", nil, Options{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(spans) != 0 {
		t.Errorf("got %d spans, want 0", len(spans))
	}
}

func TestTranslate_SingleSpan(t *testing.T) {
	rss := []ResourceSpans{
		{
			Resource: Resource{Attributes: map[string]any{"service.name": "my-service"}},
			Spans: []*Span{{
				SpanID:    "0123456789abcdef",
				TraceID:   "0123456789abcdef0123456789abcdef",
				Name:      "test-span",
				StartTime: time.Date(2025, 1, 1, 10, 0, 0, 0, time.UTC),
				EndTime:   time.Date(2025, 1, 1, 10, 0, 1, 0, time.UTC),
			}},
		},
	}

	spans, err := Translate("proj-1", rss, Options{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(spans) != 1 {
		t.Fatalf("got %d spans, want 1", len(spans))
	}

	s := spans[0]
	if s.SpanID != "0123456789abcdef" {
		t.Errorf("span_id: got %q, want %q", s.SpanID, "0123456789abcdef")
	}
	if s.TraceID != "0123456789abcdef0123456789abcdef" {
		t.Errorf("trace_id: got %q, want %q", s.TraceID, "0123456789abcdef0123456789abcdef")
	}
	if s.ProjectID != "proj-1" {
		t.Errorf("project_id: got %q, want %q", s.ProjectID, "proj-1")
	}
	if s.ServiceName != "my-service" {
		t.Errorf("service_name: got %q, want %q", s.ServiceName, "my-service")
	}
	if s.Name != "test-span" {
		t.Errorf("name: got %q, want %q", s.Name, "test-span")
	}
}

func TestTranslate_ModelFromGenAIAttributes(t *testing.T) {
	rss := []ResourceSpans{
		{
			Resource: Resource{Attributes: map[string]any{"service.name": "svc"}},
			Spans: []*Span{{
				SpanID:     "0123456789abcdef",
				TraceID:    "0123456789abcdef0123456789abcdef",
				Name:       "llm-call",
				StartTime:  time.Now(),
				EndTime:    time.Now(),
				Attributes: map[string]any{"gen_ai.request.model": "gpt-4o"},
			}},
		},
	}

	spans, err := Translate("proj-1", rss, Options{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if spans[0].Model != "gpt-4o" {
		t.Errorf("model: got %q, want %q", spans[0].Model, "gpt-4o")
	}
}

func TestTranslate_TokenCountsFromGenAIAttributes(t *testing.T) {
	rss := []ResourceSpans{
		{
			Resource: Resource{Attributes: map[string]any{"service.name": "svc"}},
			Spans: []*Span{{
				SpanID:     "0123456789abcdef",
				TraceID:    "0123456789abcdef0123456789abcdef",
				Name:       "llm-call",
				StartTime:  time.Now(),
				EndTime:    time.Now(),
				Attributes: map[string]any{"gen_ai.usage.input_tokens": 100, "gen_ai.usage.output_tokens": 50},
			}},
		},
	}

	spans, err := Translate("proj-1", rss, Options{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if spans[0].InputTokens != 100 {
		t.Errorf("input_tokens: got %d, want 100", spans[0].InputTokens)
	}
	if spans[0].OutputTokens != 50 {
		t.Errorf("output_tokens: got %d, want 50", spans[0].OutputTokens)
	}
}

func TestTranslate_TokenCountsFromLegacyAttributes(t *testing.T) {
	rss := []ResourceSpans{
		{
			Resource: Resource{Attributes: map[string]any{"service.name": "svc"}},
			Spans: []*Span{{
				SpanID:     "0123456789abcdef",
				TraceID:    "0123456789abcdef0123456789abcdef",
				Name:       "llm-call",
				StartTime:  time.Now(),
				EndTime:    time.Now(),
				Attributes: map[string]any{"prompt_tokens": 200, "completion_tokens": 100},
			}},
		},
	}

	spans, err := Translate("proj-1", rss, Options{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if spans[0].InputTokens != 200 {
		t.Errorf("input_tokens: got %d, want 200", spans[0].InputTokens)
	}
	if spans[0].OutputTokens != 100 {
		t.Errorf("output_tokens: got %d, want 100", spans[0].OutputTokens)
	}
}

func TestTranslate_InputOutputFromGenAIPrompt(t *testing.T) {
	rss := []ResourceSpans{
		{
			Resource: Resource{Attributes: map[string]any{"service.name": "svc"}},
			Spans: []*Span{{
				SpanID:     "0123456789abcdef",
				TraceID:    "0123456789abcdef0123456789abcdef",
				Name:       "llm-call",
				StartTime:  time.Now(),
				EndTime:    time.Now(),
				Attributes: map[string]any{
					"gen_ai.prompt.0.role":    "user",
					"gen_ai.prompt.0.content": "Hello world",
					"gen_ai.prompt.1.role":    "system",
					"gen_ai.prompt.1.content": "Be helpful",
				},
			}},
		},
	}

	spans, err := Translate("proj-1", rss, Options{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if spans[0].Input == "" {
		t.Fatal("input is empty")
	}

	var messages []map[string]any
	if err := json.Unmarshal([]byte(spans[0].Input), &messages); err != nil {
		t.Fatalf("input is not valid JSON: %v", err)
	}
	if len(messages) != 2 {
		t.Fatalf("messages length: got %d, want 2", len(messages))
	}
	if messages[0]["role"] != "user" {
		t.Errorf("message 0 role: got %q, want %q", messages[0]["role"], "user")
	}
	if messages[1]["role"] != "system" {
		t.Errorf("message 1 role: got %q, want %q", messages[1]["role"], "system")
	}
}

func TestTranslate_OutputFromGenAICompletion(t *testing.T) {
	rss := []ResourceSpans{
		{
			Resource: Resource{Attributes: map[string]any{"service.name": "svc"}},
			Spans: []*Span{{
				SpanID:     "0123456789abcdef",
				TraceID:    "0123456789abcdef0123456789abcdef",
				Name:       "llm-call",
				StartTime:  time.Now(),
				EndTime:    time.Now(),
				Attributes: map[string]any{
					"gen_ai.completion.0.role":    "assistant",
					"gen_ai.completion.0.content": "Response text",
				},
			}},
		},
	}

	spans, err := Translate("proj-1", rss, Options{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if spans[0].Output == "" {
		t.Fatal("output is empty")
	}

	var messages []map[string]any
	if err := json.Unmarshal([]byte(spans[0].Output), &messages); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	if len(messages) != 1 {
		t.Fatalf("messages length: got %d, want 1", len(messages))
	}
	if messages[0]["role"] != "assistant" {
		t.Errorf("role: got %q, want %q", messages[0]["role"], "assistant")
	}
	if messages[0]["content"] != "Response text" {
		t.Errorf("content: got %q, want %q", messages[0]["content"], "Response text")
	}
}

func TestTranslate_KindDerivation_GenAI(t *testing.T) {
	rss := []ResourceSpans{
		{
			Resource: Resource{Attributes: map[string]any{"service.name": "svc"}},
			Spans: []*Span{{
				SpanID:     "0123456789abcdef",
				TraceID:    "0123456789abcdef0123456789abcdef",
				Name:       "llm-call",
				StartTime:  time.Now(),
				EndTime:    time.Now(),
				Attributes: map[string]any{"gen_ai.request.model": "gpt-4"},
			}},
		},
	}

	spans, err := Translate("proj-1", rss, Options{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if spans[0].Kind != domain.SpanKindLLM {
		t.Errorf("kind: got %q, want %q", spans[0].Kind, domain.SpanKindLLM)
	}
}

func TestTranslate_KindDerivation_Tool(t *testing.T) {
	rss := []ResourceSpans{
		{
			Resource: Resource{Attributes: map[string]any{"service.name": "svc"}},
			Spans: []*Span{{
				SpanID:     "0123456789abcdef",
				TraceID:    "0123456789abcdef0123456789abcdef",
				Name:       "tool-call",
				StartTime:  time.Now(),
				EndTime:    time.Now(),
				Attributes: map[string]any{"tool.name": "search"},
			}},
		},
	}

	spans, err := Translate("proj-1", rss, Options{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if spans[0].Kind != domain.SpanKindTool {
		t.Errorf("kind: got %q, want %q", spans[0].Kind, domain.SpanKindTool)
	}
}

func TestTranslate_KindDerivation_ExplicitLanternKind(t *testing.T) {
	rss := []ResourceSpans{
		{
			Resource: Resource{Attributes: map[string]any{"service.name": "svc"}},
			Spans: []*Span{{
				SpanID:     "0123456789abcdef",
				TraceID:    "0123456789abcdef0123456789abcdef",
				Name:       "internal-work",
				StartTime:  time.Now(),
				EndTime:    time.Now(),
				Attributes: map[string]any{"lantern.kind": "internal", "gen_ai.request.model": "gpt-4"},
			}},
		},
	}

	spans, err := Translate("proj-1", rss, Options{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Explicit lantern.kind should win over gen_ai heuristic.
	if spans[0].Kind != domain.SpanKindInternal {
		t.Errorf("kind: got %q, want %q (explicit lantern.kind should win)", spans[0].Kind, domain.SpanKindInternal)
	}
}

func TestTranslate_KindDerivation_DefaultInternal(t *testing.T) {
	rss := []ResourceSpans{
		{
			Resource: Resource{Attributes: map[string]any{"service.name": "svc"}},
			Spans: []*Span{{
				SpanID:     "0123456789abcdef",
				TraceID:    "0123456789abcdef0123456789abcdef",
				Name:       "some-operation",
				StartTime:  time.Now(),
				EndTime:    time.Now(),
				Attributes: map[string]any{"http.url": "https://example.com"},
			}},
		},
	}

	spans, err := Translate("proj-1", rss, Options{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if spans[0].Kind != domain.SpanKindInternal {
		t.Errorf("kind: got %q, want %q (default should be internal)", spans[0].Kind, domain.SpanKindInternal)
	}
}

func TestTranslate_ServiceNameOverride(t *testing.T) {
	rss := []ResourceSpans{
		{
			Resource: Resource{Attributes: map[string]any{"service.name": "resource-svc"}},
			Spans: []*Span{{
				SpanID:     "0123456789abcdef",
				TraceID:    "0123456789abcdef0123456789abcdef",
				Name:       "test",
				StartTime:  time.Now(),
				EndTime:    time.Now(),
				Attributes: map[string]any{},
			}},
		},
	}

	spans, err := Translate("proj-1", rss, Options{ServiceNameOverride: "api-service"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if spans[0].ServiceName != "api-service" {
		t.Errorf("service_name: got %q, want %q (override should win)", spans[0].ServiceName, "api-service")
	}
}

func TestTranslate_AttributesOverflow(t *testing.T) {
	rss := []ResourceSpans{
		{
			Resource: Resource{Attributes: map[string]any{"service.name": "svc"}},
			Spans: []*Span{{
				SpanID:     "0123456789abcdef",
				TraceID:    "0123456789abcdef0123456789abcdef",
				Name:       "test",
				StartTime:  time.Now(),
				EndTime:    time.Now(),
				Attributes: map[string]any{
					"gen_ai.request.model": "gpt-4",
					"gen_ai.usage.input_tokens": 100,
					"http.url": "https://example.com",
					"custom.attr": "value",
				},
			}},
		},
	}

	spans, err := Translate("proj-1", rss, Options{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Extracted fields should NOT be in overflow.
	if _, hasModel := spans[0].Attributes["gen_ai.request.model"]; hasModel {
		t.Error("gen_ai.request.model should not be in overflow attributes")
	}
	if _, hasInputTokens := spans[0].Attributes["gen_ai.usage.input_tokens"]; hasInputTokens {
		t.Error("gen_ai.usage.input_tokens should not be in overflow attributes")
	}

	// Unmapped fields should be in overflow.
	if spans[0].Attributes["http.url"] != "https://example.com" {
		t.Errorf("http.url: got %v, want %q", spans[0].Attributes["http.url"], "https://example.com")
	}
	if spans[0].Attributes["custom.attr"] != "value" {
		t.Errorf("custom.attr: got %v, want %q", spans[0].Attributes["custom.attr"], "value")
	}
}

func TestTranslate_ParentSpan(t *testing.T) {
	rss := []ResourceSpans{
		{
			Resource: Resource{Attributes: map[string]any{"service.name": "svc"}},
			Spans: []*Span{{
				SpanID:     "abcdef0123456789",
				TraceID:    "0123456789abcdef0123456789abcdef",
				ParentID:   "0123456789abcdef",
				Name:       "child-span",
				StartTime:  time.Now(),
				EndTime:    time.Now(),
				Attributes: map[string]any{},
			}},
		},
	}

	spans, err := Translate("proj-1", rss, Options{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if spans[0].ParentID != "0123456789abcdef" {
		t.Errorf("parent_id: got %q, want %q", spans[0].ParentID, "0123456789abcdef")
	}
}

func TestTranslate_MultipleResourceSpans(t *testing.T) {
	rss := []ResourceSpans{
		{
			Resource: Resource{Attributes: map[string]any{"service.name": "svc-a"}},
			Spans: []*Span{
				{SpanID: "aaaaaaaaaaaaaaaa", TraceID: "0123456789abcdef0123456789abcdef", Name: "span-1", StartTime: time.Now(), EndTime: time.Now()},
				{SpanID: "bbbbbbbbbbbbbbbb", TraceID: "0123456789abcdef0123456789abcdef", Name: "span-2", StartTime: time.Now(), EndTime: time.Now()},
			},
		},
		{
			Resource: Resource{Attributes: map[string]any{"service.name": "svc-b"}},
			Spans: []*Span{
				{SpanID: "cccccccccccccccc", TraceID: "fedcba9876543210fedcba9876543210", Name: "span-3", StartTime: time.Now(), EndTime: time.Now()},
			},
		},
	}

	spans, err := Translate("proj-1", rss, Options{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(spans) != 3 {
		t.Fatalf("got %d spans, want 3", len(spans))
	}
	if spans[0].ServiceName != "svc-a" {
		t.Errorf("span 0 service_name: got %q, want %q", spans[0].ServiceName, "svc-a")
	}
	if spans[2].ServiceName != "svc-b" {
		t.Errorf("span 2 service_name: got %q, want %q", spans[2].ServiceName, "svc-b")
	}
}
