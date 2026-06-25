package otlp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/omneval/omneval/internal/domain"
	"github.com/omneval/omneval/internal/normalizer"
)

// translateTest is a convenience wrapper for Translate that uses the real
// normalizer so tests don't need to construct it on every call.
func translateTest(t *testing.T, projectID string, rss []ResourceSpans, opts Options) []*domain.Span {
	t.Helper()
	spans, err := Translate(context.Background(), projectID, rss, opts, normalizer.New())
	if err != nil {
		t.Fatalf("translate error: %v", err)
	}
	return spans
}

func TestTranslate_EmptyInput(t *testing.T) {
	spans := translateTest(t, "proj-1", nil, Options{})
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

	spans := translateTest(t, "proj-1", rss, Options{})
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

	spans := translateTest(t, "proj-1", rss, Options{})

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

	spans := translateTest(t, "proj-1", rss, Options{})

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

	spans := translateTest(t, "proj-1", rss, Options{})

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
				SpanID:    "0123456789abcdef",
				TraceID:   "0123456789abcdef0123456789abcdef",
				Name:      "llm-call",
				StartTime: time.Now(),
				EndTime:   time.Now(),
				Attributes: map[string]any{
					"gen_ai.prompt.0.role":    "user",
					"gen_ai.prompt.0.content": "Hello world",
					"gen_ai.prompt.1.role":    "system",
					"gen_ai.prompt.1.content": "Be helpful",
				},
			}},
		},
	}

	spans := translateTest(t, "proj-1", rss, Options{})

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
				SpanID:    "0123456789abcdef",
				TraceID:   "0123456789abcdef0123456789abcdef",
				Name:      "llm-call",
				StartTime: time.Now(),
				EndTime:   time.Now(),
				Attributes: map[string]any{
					"gen_ai.completion.0.role":    "assistant",
					"gen_ai.completion.0.content": "Response text",
				},
			}},
		},
	}

	spans := translateTest(t, "proj-1", rss, Options{})

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

	spans := translateTest(t, "proj-1", rss, Options{})

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

	spans := translateTest(t, "proj-1", rss, Options{})

	if spans[0].Kind != domain.SpanKindTool {
		t.Errorf("kind: got %q, want %q", spans[0].Kind, domain.SpanKindTool)
	}
}

func TestTranslate_KindDerivation_ExplicitOmnevalKind(t *testing.T) {
	rss := []ResourceSpans{
		{
			Resource: Resource{Attributes: map[string]any{"service.name": "svc"}},
			Spans: []*Span{{
				SpanID:     "0123456789abcdef",
				TraceID:    "0123456789abcdef0123456789abcdef",
				Name:       "internal-work",
				StartTime:  time.Now(),
				EndTime:    time.Now(),
				Attributes: map[string]any{"omneval.kind": "internal", "gen_ai.request.model": "gpt-4"},
			}},
		},
	}

	spans := translateTest(t, "proj-1", rss, Options{})

	// Explicit omneval.kind should win over gen_ai heuristic.
	if spans[0].Kind != domain.SpanKindInternal {
		t.Errorf("kind: got %q, want %q (explicit omneval.kind should win)", spans[0].Kind, domain.SpanKindInternal)
	}
}

func TestTranslate_KindDerivation_OpenInferenceAgent(t *testing.T) {
	rss := []ResourceSpans{
		{
			Resource: Resource{Attributes: map[string]any{"service.name": "svc"}},
			Spans: []*Span{{
				SpanID:     "0123456789abcdef",
				TraceID:    "0123456789abcdef0123456789abcdef",
				Name:       "agent.step",
				StartTime:  time.Now(),
				EndTime:    time.Now(),
				Attributes: map[string]any{"openinference.span.kind": "AGENT"},
			}},
		},
	}

	spans := translateTest(t, "proj-1", rss, Options{})

	if spans[0].Kind != domain.SpanKindAgent {
		t.Errorf("kind: got %q, want %q", spans[0].Kind, domain.SpanKindAgent)
	}
}

func TestTranslate_KindDerivation_OpenInferenceChainAndRetriever(t *testing.T) {
	tests := []struct {
		oiKind string
		want   domain.SpanKind
	}{
		{"CHAIN", domain.SpanKindChain},
		{"RETRIEVER", domain.SpanKindChain},
		{"EMBEDDING", domain.SpanKindChain},
		{"RERANKER", domain.SpanKindChain},
		{"GUARDRAIL", domain.SpanKindChain},
		{"TOOL", domain.SpanKindTool},
		{"LLM", domain.SpanKindLLM},
	}

	for _, tc := range tests {
		rss := []ResourceSpans{
			{
				Resource: Resource{Attributes: map[string]any{"service.name": "svc"}},
				Spans: []*Span{{
					SpanID:     "0123456789abcdef",
					TraceID:    "0123456789abcdef0123456789abcdef",
					Name:       "some-span",
					StartTime:  time.Now(),
					EndTime:    time.Now(),
					Attributes: map[string]any{"openinference.span.kind": tc.oiKind},
				}},
			},
		}

		spans := translateTest(t, "proj-1", rss, Options{})

		if spans[0].Kind != tc.want {
			t.Errorf("openinference.span.kind=%q: kind got %q, want %q", tc.oiKind, spans[0].Kind, tc.want)
		}
	}
}

func TestTranslate_KindDerivation_GenAIOperationName(t *testing.T) {
	tests := []struct {
		operation string
		want      domain.SpanKind
	}{
		{"invoke_agent", domain.SpanKindAgent},
		{"create_agent", domain.SpanKindAgent},
		{"execute_tool", domain.SpanKindTool},
		{"chat", domain.SpanKindLLM},
		{"text_completion", domain.SpanKindLLM},
	}

	for _, tc := range tests {
		rss := []ResourceSpans{
			{
				Resource: Resource{Attributes: map[string]any{"service.name": "svc"}},
				Spans: []*Span{{
					SpanID:     "0123456789abcdef",
					TraceID:    "0123456789abcdef0123456789abcdef",
					Name:       "some-span",
					StartTime:  time.Now(),
					EndTime:    time.Now(),
					Attributes: map[string]any{"gen_ai.operation.name": tc.operation},
				}},
			},
		}

		spans := translateTest(t, "proj-1", rss, Options{})

		if spans[0].Kind != tc.want {
			t.Errorf("gen_ai.operation.name=%q: kind got %q, want %q", tc.operation, spans[0].Kind, tc.want)
		}
	}
}

func TestTranslate_KindDerivation_NameHeuristics(t *testing.T) {
	tests := []struct {
		name string
		want domain.SpanKind
	}{
		{"agent.step", domain.SpanKindAgent},
		{"planner.agent.step", domain.SpanKindAgent},
		{"TerminalAction", domain.SpanKindTool},
		{"FileEditorAction", domain.SpanKindTool},
		{"InvokeSkillAction", domain.SpanKindTool},
		{"search_tool_call", domain.SpanKindTool},
		{"ThinkAction", domain.SpanKindTool},
	}

	for _, tc := range tests {
		rss := []ResourceSpans{
			{
				Resource: Resource{Attributes: map[string]any{"service.name": "svc"}},
				Spans: []*Span{{
					SpanID:     "0123456789abcdef",
					TraceID:    "0123456789abcdef0123456789abcdef",
					Name:       tc.name,
					StartTime:  time.Now(),
					EndTime:    time.Now(),
					Attributes: map[string]any{},
				}},
			},
		}

		spans := translateTest(t, "proj-1", rss, Options{})

		if spans[0].Kind != tc.want {
			t.Errorf("name=%q: kind got %q, want %q", tc.name, spans[0].Kind, tc.want)
		}
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

	spans := translateTest(t, "proj-1", rss, Options{})

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

	spans := translateTest(t, "proj-1", rss, Options{ServiceNameOverride: "api-service"})

	if spans[0].ServiceName != "api-service" {
		t.Errorf("service_name: got %q, want %q (override should win)", spans[0].ServiceName, "api-service")
	}
}

func TestTranslate_AttributesOverflow(t *testing.T) {
	rss := []ResourceSpans{
		{
			Resource: Resource{Attributes: map[string]any{"service.name": "svc"}},
			Spans: []*Span{{
				SpanID:    "0123456789abcdef",
				TraceID:   "0123456789abcdef0123456789abcdef",
				Name:      "test",
				StartTime: time.Now(),
				EndTime:   time.Now(),
				Attributes: map[string]any{
					"gen_ai.request.model":      "gpt-4",
					"gen_ai.usage.input_tokens": 100,
					"http.url":                  "https://example.com",
					"custom.attr":               "value",
				},
			}},
		},
	}

	spans := translateTest(t, "proj-1", rss, Options{})

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

	spans := translateTest(t, "proj-1", rss, Options{})

	if spans[0].ParentID != "0123456789abcdef" {
		t.Errorf("parent_id: got %q, want %q", spans[0].ParentID, "0123456789abcdef")
	}
}

// TestTranslate_NoTokenAttributes verifies that when a span carries no token
// attributes (e.g. a @trace-decorated span without set_tokens()), the
// translated domain.Span has InputTokens == 0 and OutputTokens == 0 rather
// than -1 (the internal sentinel value used by extractAttributeInt64).
func TestTranslate_NoTokenAttributes(t *testing.T) {
	rss := []ResourceSpans{
		{
			Resource: Resource{Attributes: map[string]any{"service.name": "svc"}},
			Spans: []*Span{{
				SpanID:     "aabbccdd11223344",
				TraceID:    "aabbccdd112233440011223344556677",
				Name:       "no-token-span",
				StartTime:  time.Now(),
				EndTime:    time.Now(),
				Attributes: map[string]any{
					// Deliberately no gen_ai.usage.input_tokens / output_tokens
					// or prompt_tokens / completion_tokens.
				},
			}},
		},
	}

	spans := translateTest(t, "proj-1", rss, Options{})
	if len(spans) != 1 {
		t.Fatalf("got %d spans, want 1", len(spans))
	}
	if spans[0].InputTokens != 0 {
		t.Errorf("input_tokens: got %d, want 0 (sentinel -1 must be normalised to 0)", spans[0].InputTokens)
	}
	if spans[0].OutputTokens != 0 {
		t.Errorf("output_tokens: got %d, want 0 (sentinel -1 must be normalised to 0)", spans[0].OutputTokens)
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

	spans := translateTest(t, "proj-1", rss, Options{})

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

func TestTranslate_StartTimeEndTimePreserved(t *testing.T) {
	start := time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)
	end := time.Date(2024, 1, 15, 10, 30, 5, 0, time.UTC)
	rss := []ResourceSpans{{
		Resource: Resource{Attributes: map[string]any{"service.name": "svc-1"}},
		Spans: []*Span{{
			SpanID:    "0123456789abcdef",
			TraceID:   "0123456789abcdef0123456789abcdef",
			Name:      "test-span",
			StartTime: start,
			EndTime:   end,
		}},
	}}

	spans := translateTest(t, "proj-1", rss, Options{})
	if len(spans) != 1 {
		t.Fatalf("got %d spans, want 1", len(spans))
	}

	if !spans[0].StartTime.Equal(start) {
		t.Errorf("StartTime: got %v, want %v", spans[0].StartTime, start)
	}
	if !spans[0].EndTime.Equal(end) {
		t.Errorf("EndTime: got %v, want %v", spans[0].EndTime, end)
	}
}

func TestTranslate_InputOutputFromOmnevalAttributes(t *testing.T) {
	rss := []ResourceSpans{{
		Resource: Resource{Attributes: map[string]any{"service.name": "svc"}},
		Spans: []*Span{{
			SpanID:    "0123456789abcdef",
			TraceID:   "0123456789abcdef0123456789abcdef",
			Name:      "my-function",
			StartTime: time.Now(),
			EndTime:   time.Now(),
			Attributes: map[string]any{
				"omneval.input":  "What is the capital of France?",
				"omneval.output": "Paris.",
			},
		}},
	}}

	spans := translateTest(t, "proj-1", rss, Options{})
	if len(spans) != 1 {
		t.Fatalf("got %d spans, want 1", len(spans))
	}

	if spans[0].Input == "" {
		t.Fatal("input is empty")
	}
	var inputMessages []map[string]any
	if err := json.Unmarshal([]byte(spans[0].Input), &inputMessages); err != nil {
		t.Fatalf("input is not valid JSON: %v", err)
	}
	if len(inputMessages) != 1 || inputMessages[0]["content"] != "What is the capital of France?" {
		t.Errorf("input messages: got %v, want content %q", inputMessages, "What is the capital of France?")
	}

	if spans[0].Output == "" {
		t.Fatal("output is empty")
	}
	var outputMessages []map[string]any
	if err := json.Unmarshal([]byte(spans[0].Output), &outputMessages); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	if len(outputMessages) != 1 || outputMessages[0]["content"] != "Paris." {
		t.Errorf("output messages: got %v, want content %q", outputMessages, "Paris.")
	}

	// omneval.input/omneval.output should not leak into the overflow attributes.
	if _, ok := spans[0].Attributes["omneval.input"]; ok {
		t.Error("omneval.input should not be in overflow attributes")
	}
	if _, ok := spans[0].Attributes["omneval.output"]; ok {
		t.Error("omneval.output should not be in overflow attributes")
	}
}

func TestTranslate_InputOutputFromGenAIMessagesAttributes(t *testing.T) {
	rss := []ResourceSpans{{
		Resource: Resource{Attributes: map[string]any{"service.name": "svc"}},
		Spans: []*Span{{
			SpanID:    "0123456789abcdef",
			TraceID:   "0123456789abcdef0123456789abcdef",
			Name:      "llm-call",
			StartTime: time.Now(),
			EndTime:   time.Now(),
			Attributes: map[string]any{
				"gen_ai.input.messages":  `[{"role":"user","content":"Hello"}]`,
				"gen_ai.output.messages": `[{"role":"assistant","content":"Hi there"}]`,
			},
		}},
	}}

	spans := translateTest(t, "proj-1", rss, Options{})
	if len(spans) != 1 {
		t.Fatalf("got %d spans, want 1", len(spans))
	}

	var inputMessages []map[string]any
	if err := json.Unmarshal([]byte(spans[0].Input), &inputMessages); err != nil {
		t.Fatalf("input is not valid JSON: %v (%q)", err, spans[0].Input)
	}
	if len(inputMessages) != 1 || inputMessages[0]["content"] != "Hello" {
		t.Errorf("input messages: got %v", inputMessages)
	}

	var outputMessages []map[string]any
	if err := json.Unmarshal([]byte(spans[0].Output), &outputMessages); err != nil {
		t.Fatalf("output is not valid JSON: %v (%q)", err, spans[0].Output)
	}
	if len(outputMessages) != 1 || outputMessages[0]["content"] != "Hi there" {
		t.Errorf("output messages: got %v", outputMessages)
	}

	if _, ok := spans[0].Attributes["gen_ai.input.messages"]; ok {
		t.Error("gen_ai.input.messages should not be in overflow attributes")
	}
	if _, ok := spans[0].Attributes["gen_ai.output.messages"]; ok {
		t.Error("gen_ai.output.messages should not be in overflow attributes")
	}
}

func TestTranslate_InputOutputFromOpenInferenceAttributes(t *testing.T) {
	rss := []ResourceSpans{{
		Resource: Resource{Attributes: map[string]any{"service.name": "svc"}},
		Spans: []*Span{{
			SpanID:    "0123456789abcdef",
			TraceID:   "0123456789abcdef0123456789abcdef",
			Name:      "llm-call",
			StartTime: time.Now(),
			EndTime:   time.Now(),
			Attributes: map[string]any{
				"input.value":  "What's the weather in Paris?",
				"output.value": "It's sunny in Paris.",
			},
		}},
	}}

	spans := translateTest(t, "proj-1", rss, Options{})
	if len(spans) != 1 {
		t.Fatalf("got %d spans, want 1", len(spans))
	}

	var inputMessages []map[string]any
	if err := json.Unmarshal([]byte(spans[0].Input), &inputMessages); err != nil {
		t.Fatalf("input is not valid JSON: %v (%q)", err, spans[0].Input)
	}
	if len(inputMessages) != 1 || inputMessages[0]["content"] != "What's the weather in Paris?" {
		t.Errorf("input messages: got %v", inputMessages)
	}

	var outputMessages []map[string]any
	if err := json.Unmarshal([]byte(spans[0].Output), &outputMessages); err != nil {
		t.Fatalf("output is not valid JSON: %v (%q)", err, spans[0].Output)
	}
	if len(outputMessages) != 1 || outputMessages[0]["content"] != "It's sunny in Paris." {
		t.Errorf("output messages: got %v", outputMessages)
	}

	if _, ok := spans[0].Attributes["input.value"]; ok {
		t.Error("input.value should not be in overflow attributes")
	}
	if _, ok := spans[0].Attributes["output.value"]; ok {
		t.Error("output.value should not be in overflow attributes")
	}
}

func TestTranslate_StatusCodeAndMessagePreserved(t *testing.T) {
	rss := []ResourceSpans{{
		Resource: Resource{Attributes: map[string]any{"service.name": "svc-1"}},
		Spans: []*Span{{
			SpanID:     "0123456789abcdef",
			TraceID:    "0123456789abcdef0123456789abcdef",
			Name:       "test-span",
			StartTime:  time.Now(),
			EndTime:    time.Now(),
			StatusCode: "ERROR",
			StatusMsg:  "boom",
		}},
	}}

	spans := translateTest(t, "proj-1", rss, Options{})
	if len(spans) != 1 {
		t.Fatalf("got %d spans, want 1", len(spans))
	}

	if spans[0].StatusCode != "ERROR" {
		t.Errorf("StatusCode: got %q, want %q", spans[0].StatusCode, "ERROR")
	}
	if spans[0].StatusMessage != "boom" {
		t.Errorf("StatusMessage: got %q, want %q", spans[0].StatusMessage, "boom")
	}
}

// --- Diagnostic tests for Issue #279 ---

// TestTranslate_SpanEventsNotCaptured_AttributeGap_DiagnosticIssue279 confirms
// that the translator does NOT read prompt/completion content from Span Events
// because the internal/otlp.Span struct has no Events field (only Attributes).
//
// Source investigation (lmnr/opentelemetry_lib/opentelemetry/instrumentation/litellm/
// wrappers/completions/__init__.py, line 86): set_span_attribute(span,
// "gen_ai.input.messages", json_dumps(attr_messages)) — the Laminar instrumentor
// DOES write prompt/completion content to Attributes (gen_ai.input.messages,
// gen_ai.output.messages).  However the OTLP wire format also supports Span Events,
// and when content is placed in Events (as some instrumentor paths do), Omneval
// drops them because convertToResourceSpans only reads s.GetAttributes(), never
// s.GetEvents().
//
// This test proves: the translator struct has no Events field, so content placed
// in Span Events on the wire is silently dropped.
func TestTranslate_SpanEventsNotCaptured_AttributeGap_DiagnosticIssue279(t *testing.T) {
	// A span that carries ONLY gen_ai attributes (no content) — exactly the shape
	// confirmed in issue #279 for real litellm/Laminar traffic — produces
	// normalized Input/Output of empty message arrays.  The Attributes map
	// contains response metadata, tool definitions, and Laminar bookkeeping
	// attributes but zero conversation content.
	rss := []ResourceSpans{{
		Resource: Resource{Attributes: map[string]any{"service.name": "svc"}},
		Spans: []*Span{{
			SpanID:    "0123456789abcdef",
			TraceID:   "0123456789abcdef0123456789abcdef",
			Name:      "litellm.completion",
			StartTime: time.Now(),
			EndTime:   time.Now(),
			Attributes: map[string]any{
				"gen_ai.response.id":               "chatcmpl-abc123",
				"gen_ai.response.model":            "gpt-4o",
				"gen_ai.response.system_fingerprint": "fp_xyz",
				"gen_ai.system":                    "openai",
				"gen_ai.tool.definitions":          `[{"name":"search","description":"Search"}]`,
				"gen_ai.usage.total_tokens":        100,
				"lmnr.span.ids_path":               "/spans",
				"lmnr.span.instrumentation_scope.name": "litellm",
				"lmnr.span.sdk_version":            "0.7.52",
				"lmnr.span.type":                   "LLM",
			},
		}},
	}}

	spans := translateTest(t, "proj-1", rss, Options{})
	if len(spans) != 1 {
		t.Fatalf("got %d spans, want 1", len(spans))
	}

	// KEY FINDING FOR ISSUE #279: Input and Output are normalized to empty
	// message arrays ({"content":"","role":"user"}) because the Attributes map
	// contains zero gen_ai.input.messages / gen_ai.output.messages content.
	// Laminar's LitellmInstrumentor writes gen_ai.input.messages /
	// gen_ai.output.messages via set_span_attribute when is_litellm_instrumented==true,
	// but the OpenAI instrumentor path (is_litellm_instrumented==false) may place
	// content in Span Events, which Omneval does not read.
	if !strings.Contains(spans[0].Input, `"role":"user"`) || !strings.Contains(spans[0].Input, `"content":""`) {
		t.Errorf("input: got %q (expected normalized empty message — gen_ai.input.messages not in Attributes)", spans[0].Input)
	}
	if !strings.Contains(spans[0].Output, `"role":"assistant"`) || !strings.Contains(spans[0].Output, `"content":""`) {
		t.Errorf("output: got %q (expected normalized empty message — gen_ai.output.messages not in Attributes)", spans[0].Output)
	}
}

// TestTranslate_LitellmCompletionSpan_DiagnosticIssue279 reproduces the exact
// attribute set from the confirmed live production trace in issue #279.  It
// demonstrates that the translator correctly reads response metadata but
// produces empty Input/Output when gen_ai.input.messages / gen_ai.output.messages
// are absent from Attributes (e.g. when the OpenAI instrumentor path is active,
// content goes to Span Events, which Omneval does not read).
func TestTranslate_LitellmCompletionSpan_DiagnosticIssue279(t *testing.T) {
	rss := []ResourceSpans{{
		Resource: Resource{Attributes: map[string]any{"service.name": "agent-runtime"}},
		Spans: []*Span{{
			SpanID:    "aabbccdd00112233",
			TraceID:   "deadbeef12345678deadbeef12345678",
			Name:      "litellm.completion",
			StartTime: time.Now(),
			EndTime:   time.Now(),
			Attributes: map[string]any{
				"gen_ai.response.id":               "chatcmpl-prod123",
				"gen_ai.request.model":             "claude-sonnet-4-20250514",
				"gen_ai.response.system_fingerprint": "fp_prod",
				"gen_ai.system":                    "anthropic",
				"gen_ai.usage.input_tokens":        int64(500),
				"gen_ai.usage.output_tokens":       int64(200),
				"lmnr.span.ids_path":               "/data/otel/spans",
				"lmnr.span.instrumentation_scope.name": "litellm",
				"lmnr.span.instrumentation_scope.version": "1.89.3",
				"lmnr.span.sdk_version":            "0.7.52",
				"lmnr.span.type":                   "span",
			},
		}},
	}}

	spans := translateTest(t, "proj-1", rss, Options{})
	if len(spans) != 1 {
		t.Fatalf("got %d spans, want 1", len(spans))
	}

	// These attributes ARE captured (model via gen_ai.request.model, token counts):
	if spans[0].Model != "claude-sonnet-4-20250514" {
		t.Errorf("model: got %q, want %q", spans[0].Model, "claude-sonnet-4-20250514")
	}
	if spans[0].InputTokens != 500 {
		t.Errorf("input_tokens: got %d, want 500", spans[0].InputTokens)
	}
	if spans[0].OutputTokens != 200 {
		t.Errorf("output_tokens: got %d, want 200", spans[0].OutputTokens)
	}

	// These are NOT captured — gen_ai.input.messages / gen_ai.output.messages
	// are absent from Attributes (in production the OpenAI instrumentor path
	// may place content in Span Events, which Omneval does not read):
	if !strings.Contains(spans[0].Input, `"role":"user"`) || !strings.Contains(spans[0].Input, `"content":""`) {
		t.Errorf("input: got %q (expected normalized empty message — content not in Attributes)", spans[0].Input)
	}
	if !strings.Contains(spans[0].Output, `"role":"assistant"`) || !strings.Contains(spans[0].Output, `"content":""`) {
		t.Errorf("output: got %q (expected normalized empty message — content not in Attributes)", spans[0].Output)
	}
}

// TestTranslate_LitellmInputMessagesFromAttributes_DiagnosticIssue279 confirms
// that Omneval's translator CAN read gen_ai.input.messages / gen_ai.output.messages
// from Attributes when they ARE present.  This is the shape Laminar's
// LitellmInstrumentor (lmnr/opentelemetry_lib/opentelemetry/instrumentation/litellm/
// wrappers/completions/__init__.py:86) writes via set_span_attribute.
//
// Root cause: when Laminar uses the OTel callback path (is_litellm_instrumented==true),
// content goes to gen_ai.input.messages / gen_ai.output.messages Attributes.
// When it uses the OpenAI instrumentor path, content may go to Span Events instead.
// Omneval only reads Attributes — so the fix depends on which path produces the
// problem observed in live production.
// TestTranslate_SpanEventsCaptured_PromptMessage verifies that a span whose
// prompt content lives in Span Events (not Attributes) produces correctly
// populated Input in the domain model.
func TestTranslate_SpanEventsCaptured_PromptMessage(t *testing.T) {
	rss := []ResourceSpans{{
		Resource: Resource{Attributes: map[string]any{"service.name": "svc"}},
		Spans: []*Span{{
			SpanID:    "0123456789abcdef",
			TraceID:   "0123456789abcdef0123456789abcdef",
			Name:      "litellm.completion",
			StartTime: time.Now(),
			EndTime:   time.Now(),
			Attributes: map[string]any{
				"gen_ai.request.model": "gpt-4o",
			},
			// Prompt content is ONLY in Span Events.
			Events: []SpanEvent{{
				Name: "gen_ai.prompt.message",
				Attributes: map[string]string{
					"gen_ai.prompt.message.role":    "user",
					"gen_ai.prompt.message.content": "Hello, world!",
				},
			}},
		}},
	}}

	spans := translateTest(t, "proj-1", rss, Options{})
	if len(spans) != 1 {
		t.Fatalf("got %d spans, want 1", len(spans))
	}

	if spans[0].Input == "" {
		t.Fatal("input is empty, expected prompt message from Span Events")
	}
	if !strings.Contains(spans[0].Input, `"role":"user"`) || !strings.Contains(spans[0].Input, `"content":"Hello, world!"`) {
		t.Errorf("input: got %q (expected user message from Span Events)", spans[0].Input)
	}
}

// TestTranslate_SpanEventsCaptured_CompletionMessage verifies that a span whose
// completion content lives in Span Events produces correctly populated Output.
func TestTranslate_SpanEventsCaptured_CompletionMessage(t *testing.T) {
	rss := []ResourceSpans{{
		Resource: Resource{Attributes: map[string]any{"service.name": "svc"}},
		Spans: []*Span{{
			SpanID:    "0123456789abcdef",
			TraceID:   "0123456789abcdef0123456789abcdef",
			Name:      "litellm.completion",
			StartTime: time.Now(),
			EndTime:   time.Now(),
			Attributes: map[string]any{
				"gen_ai.request.model": "gpt-4o",
			},
			// Completion content is ONLY in Span Events.
			Events: []SpanEvent{{
				Name: "gen_ai.completion.message",
				Attributes: map[string]string{
					"gen_ai.completion.message.role":    "assistant",
					"gen_ai.completion.message.content": "Hi there! How can I help?",
				},
			}},
		}},
	}}

	spans := translateTest(t, "proj-1", rss, Options{})
	if len(spans) != 1 {
		t.Fatalf("got %d spans, want 1", len(spans))
	}

	if spans[0].Output == "" {
		t.Fatal("output is empty, expected completion message from Span Events")
	}
	if !strings.Contains(spans[0].Output, `"role":"assistant"`) || !strings.Contains(spans[0].Output, `"content":"Hi there! How can I help?"`) {
		t.Errorf("output: got %q (expected assistant message from Span Events)", spans[0].Output)
	}
}

// TestTranslate_SpanEventsCaptured_BothPromptAndCompletion verifies that
// prompt and completion content from Span Events both populate Input and Output.
func TestTranslate_SpanEventsCaptured_BothPromptAndCompletion(t *testing.T) {
	rss := []ResourceSpans{{
		Resource: Resource{Attributes: map[string]any{"service.name": "svc"}},
		Spans: []*Span{{
			SpanID:    "0123456789abcdef",
			TraceID:   "0123456789abcdef0123456789abcdef",
			Name:      "litellm.completion",
			StartTime: time.Now(),
			EndTime:   time.Now(),
			Attributes: map[string]any{
				"gen_ai.request.model": "gpt-4o",
			},
			Events: []SpanEvent{
				{
					Name: "gen_ai.prompt.message",
					Attributes: map[string]string{
						"gen_ai.prompt.message.role":    "user",
						"gen_ai.prompt.message.content": "What is the weather?",
					},
				},
				{
					Name: "gen_ai.completion.message",
					Attributes: map[string]string{
						"gen_ai.completion.message.role":    "assistant",
						"gen_ai.completion.message.content": "The weather is sunny.",
					},
				},
			},
		}},
	}}

	spans := translateTest(t, "proj-1", rss, Options{})
	if len(spans) != 1 {
		t.Fatalf("got %d spans, want 1", len(spans))
	}

	if !strings.Contains(spans[0].Input, `"role":"user"`) || !strings.Contains(spans[0].Input, `"content":"What is the weather?"`) {
		t.Errorf("input: got %q (expected user message from Span Events)", spans[0].Input)
	}
	if !strings.Contains(spans[0].Output, `"role":"assistant"`) || !strings.Contains(spans[0].Output, `"content":"The weather is sunny."`) {
		t.Errorf("output: got %q (expected assistant message from Span Events)", spans[0].Output)
	}
}

// TestTranslate_SpanEventsCaptured_MultipleMessages verifies that multiple
// messages within a single event type are all captured in order.
func TestTranslate_SpanEventsCaptured_MultipleMessages(t *testing.T) {
	rss := []ResourceSpans{{
		Resource: Resource{Attributes: map[string]any{"service.name": "svc"}},
		Spans: []*Span{{
			SpanID:    "0123456789abcdef",
			TraceID:   "0123456789abcdef0123456789abcdef",
			Name:      "litellm.completion",
			StartTime: time.Now(),
			EndTime:   time.Now(),
			Attributes: map[string]any{
				"gen_ai.request.model": "gpt-4o",
			},
			Events: []SpanEvent{
				{
					Name: "gen_ai.prompt.message",
					Attributes: map[string]string{
						"gen_ai.prompt.message.role":    "system",
						"gen_ai.prompt.message.content": "Be helpful.",
					},
				},
				{
					Name: "gen_ai.prompt.message",
					Attributes: map[string]string{
						"gen_ai.prompt.message.role":    "user",
						"gen_ai.prompt.message.content": "Tell me about AI.",
					},
				},
				{
					Name: "gen_ai.completion.message",
					Attributes: map[string]string{
						"gen_ai.completion.message.role":    "assistant",
						"gen_ai.completion.message.content": "AI is a field of CS.",
					},
				},
			},
		}},
	}}

	spans := translateTest(t, "proj-1", rss, Options{})
	if len(spans) != 1 {
		t.Fatalf("got %d spans, want 1", len(spans))
	}

	// Input should contain both system and user messages.
	var inputMessages []map[string]any
	if err := json.Unmarshal([]byte(spans[0].Input), &inputMessages); err != nil {
		t.Fatalf("input is not valid JSON: %v", err)
	}
	if len(inputMessages) != 2 {
		t.Fatalf("input messages: got %d, want 2", len(inputMessages))
	}
	if inputMessages[0]["role"] != "system" {
		t.Errorf("input message 0 role: got %q, want %q", inputMessages[0]["role"], "system")
	}
	if inputMessages[1]["role"] != "user" {
		t.Errorf("input message 1 role: got %q, want %q", inputMessages[1]["role"], "user")
	}

	// Output should contain the assistant message.
	var outputMessages []map[string]any
	if err := json.Unmarshal([]byte(spans[0].Output), &outputMessages); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	if len(outputMessages) != 1 {
		t.Fatalf("output messages: got %d, want 1", len(outputMessages))
	}
	if outputMessages[0]["role"] != "assistant" {
		t.Errorf("output message 0 role: got %q, want %q", outputMessages[0]["role"], "assistant")
	}
}

// TestTranslate_SpanEventsNoRegression_AttributeExtraction verifies that
// attribute-based extraction still works when Events are present.
func TestTranslate_SpanEventsNoRegression_AttributeExtraction(t *testing.T) {
	rss := []ResourceSpans{{
		Resource: Resource{Attributes: map[string]any{"service.name": "svc"}},
		Spans: []*Span{{
			SpanID:    "0123456789abcdef",
			TraceID:   "0123456789abcdef0123456789abcdef",
			Name:      "litellm.completion",
			StartTime: time.Now(),
			EndTime:   time.Now(),
			Attributes: map[string]any{
				"gen_ai.request.model":   "gpt-4o",
				"gen_ai.input.messages":  `[{"role":"user","content":"from-attr"}]`,
				"gen_ai.output.messages": `[{"role":"assistant","content":"from-attr-resp"}]`,
			},
			// Also has events — attributes should take precedence.
			Events: []SpanEvent{{
				Name: "gen_ai.prompt.message",
				Attributes: map[string]string{
					"gen_ai.prompt.message.role":    "user",
					"gen_ai.prompt.message.content": "from-event",
				},
			}},
		}},
	}}

	spans := translateTest(t, "proj-1", rss, Options{})
	if len(spans) != 1 {
		t.Fatalf("got %d spans, want 1", len(spans))
	}

	// Attributes should take precedence over events.
	if !strings.Contains(spans[0].Input, `"content":"from-attr"`) {
		t.Errorf("input: got %q (expected attribute-based content, not event content)", spans[0].Input)
	}
	if !strings.Contains(spans[0].Output, `"content":"from-attr-resp"`) {
		t.Errorf("output: got %q (expected attribute-based content, not event content)", spans[0].Output)
	}
}

// TestTranslate_SpanEventsNoEventsSpan verifies that spans without Events
// still work correctly (no regression).
func TestTranslate_SpanEventsNoEventsSpan(t *testing.T) {
	rss := []ResourceSpans{{
		Resource: Resource{Attributes: map[string]any{"service.name": "svc"}},
		Spans: []*Span{{
			SpanID:    "0123456789abcdef",
			TraceID:   "0123456789abcdef0123456789abcdef",
			Name:      "litellm.completion",
			StartTime: time.Now(),
			EndTime:   time.Now(),
			Attributes: map[string]any{
				"gen_ai.request.model": "gpt-4o",
				// No gen_ai.input.messages / gen_ai.output.messages either.
			},
			Events: nil, // explicitly no events
		}},
	}}

	spans := translateTest(t, "proj-1", rss, Options{})
	if len(spans) != 1 {
		t.Fatalf("got %d spans, want 1", len(spans))
	}

	// Without events or message attributes, Input and Output should be
	// normalized to empty placeholder messages (the existing behavior).
	if !strings.Contains(spans[0].Input, `"role":"user"`) || !strings.Contains(spans[0].Input, `"content":""`) {
		t.Errorf("input: got %q (expected normalized empty message for span with no content source)", spans[0].Input)
	}
}

func TestTranslate_LitellmInputMessagesFromAttributes_DiagnosticIssue279(t *testing.T) {
	rss := []ResourceSpans{{
		Resource: Resource{Attributes: map[string]any{"service.name": "svc"}},
		Spans: []*Span{{
			SpanID:    "0123456789abcdef",
			TraceID:   "0123456789abcdef0123456789abcdef",
			Name:      "litellm.completion",
			StartTime: time.Now(),
			EndTime:   time.Now(),
			Attributes: map[string]any{
				// gen_ai.input.messages — Laminar's shape for prompt content in Attributes
				"gen_ai.input.messages":  `[{"role":"user","content":"What is the weather?"}]`,
				// gen_ai.output.messages — Laminar's shape for completion content in Attributes
				"gen_ai.output.messages": `[{"role":"assistant","content":"The weather is sunny."}]`,
				"gen_ai.request.model":   "gpt-4o",
				"gen_ai.system":          "openai",
			},
		}},
	}}

	spans := translateTest(t, "proj-1", rss, Options{})
	if len(spans) != 1 {
		t.Fatalf("got %d spans, want 1", len(spans))
	}

	// gen_ai.input.messages IS read when present in Attributes:
	if !strings.Contains(spans[0].Input, `"role":"user"`) || !strings.Contains(spans[0].Input, `"content":"What is the weather?"`) {
		t.Errorf("input: got %q (expected to contain user message from gen_ai.input.messages)", spans[0].Input)
	}
	if !strings.Contains(spans[0].Output, `"role":"assistant"`) || !strings.Contains(spans[0].Output, `"content":"The weather is sunny."`) {
		t.Errorf("output: got %q (expected to contain assistant message from gen_ai.output.messages)", spans[0].Output)
	}

	// gen_ai.input.messages / gen_ai.output.messages should be removed from overflow:
	if _, ok := spans[0].Attributes["gen_ai.input.messages"]; ok {
		t.Error("gen_ai.input.messages should not be in overflow attributes")
	}
	if _, ok := spans[0].Attributes["gen_ai.output.messages"]; ok {
		t.Error("gen_ai.output.messages should not be in overflow attributes")
	}
}
