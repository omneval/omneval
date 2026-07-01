package normalizer_test

import (
	"strings"
	"testing"
	"time"

	commonv1 "go.opentelemetry.io/proto/otlp/common/v1"
	resourcev1 "go.opentelemetry.io/proto/otlp/resource/v1"
	tracev1 "go.opentelemetry.io/proto/otlp/trace/v1"

	"github.com/omneval/omneval/internal/domain"
	"github.com/omneval/omneval/internal/normalizer"
)

// TestNormalizeOTLP_SingleSpan verifies that NormalizeOTLP converts a single
// OTLP ResourceSpan into a valid domain.Span via the normalizer.
func TestNormalizeOTLP_SingleSpan(t *testing.T) {
	rss := []*tracev1.ResourceSpans{
		{
			Resource: &resourcev1.Resource{
				Attributes: []*commonv1.KeyValue{
					{Key: "service.name", Value: &commonv1.AnyValue{Value: &commonv1.AnyValue_StringValue{StringValue: "test-service"}}},
				},
			},
			ScopeSpans: []*tracev1.ScopeSpans{
				{
					Spans: []*tracev1.Span{
						{
							TraceId:   []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10},
							SpanId:    []byte{0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18},
							Name:      "test-span",
							StartTimeUnixNano: uint64(time.Date(2025, 1, 1, 10, 0, 0, 0, time.UTC).UnixNano()),
							EndTimeUnixNano:   uint64(time.Date(2025, 1, 1, 10, 0, 1, 0, time.UTC).UnixNano()),
						},
					},
				},
			},
		},
	}

	n := normalizer.New()
	spans, err := normalizer.NormalizeOTLP(nil, "proj-1", rss, normalizer.Options{}, n)
	if err != nil {
		t.Fatalf("NormalizeOTLP error: %v", err)
	}
	if len(spans) != 1 {
		t.Fatalf("got %d spans, want 1", len(spans))
	}
	s := spans[0]
	if s.SpanID != "1112131415161718" {
		t.Errorf("span_id: got %q, want %q", s.SpanID, "1112131415161718")
	}
	if s.TraceID != "0102030405060708090a0b0c0d0e0f10" {
		t.Errorf("trace_id: got %q, want %q", s.TraceID, "0102030405060708090a0b0c0d0e0f10")
	}
	if s.ProjectID != "proj-1" {
		t.Errorf("project_id: got %q, want %q", s.ProjectID, "proj-1")
	}
	if s.ServiceName != "test-service" {
		t.Errorf("service_name: got %q, want %q", s.ServiceName, "test-service")
	}
	if s.Name != "test-span" {
		t.Errorf("name: got %q, want %q", s.Name, "test-span")
	}
}

// TestNormalizeOTLP_MultipleSpans verifies that NormalizeOTLP handles multiple
// spans across multiple ResourceSpans.
func TestNormalizeOTLP_MultipleSpans(t *testing.T) {
	rss := []*tracev1.ResourceSpans{
		{
			Resource: &resourcev1.Resource{
				Attributes: []*commonv1.KeyValue{
					{Key: "service.name", Value: &commonv1.AnyValue{Value: &commonv1.AnyValue_StringValue{StringValue: "svc-a"}}},
				},
			},
			ScopeSpans: []*tracev1.ScopeSpans{
				{
					Spans: []*tracev1.Span{
						{
							TraceId:   []byte{0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff, 0x00, 0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77, 0x88, 0x99},
							SpanId:    []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08},
							Name:      "span-a",
							StartTimeUnixNano: uint64(time.Now().UnixNano()),
							EndTimeUnixNano:   uint64(time.Now().UnixNano()),
						},
						{
							TraceId:   []byte{0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff, 0x00, 0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77, 0x88, 0x99},
							SpanId:    []byte{0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10},
							Name:      "span-b",
							StartTimeUnixNano: uint64(time.Now().UnixNano()),
							EndTimeUnixNano:   uint64(time.Now().UnixNano()),
						},
					},
				},
			},
		},
		{
			Resource: &resourcev1.Resource{
				Attributes: []*commonv1.KeyValue{
					{Key: "service.name", Value: &commonv1.AnyValue{Value: &commonv1.AnyValue_StringValue{StringValue: "svc-b"}}},
				},
			},
			ScopeSpans: []*tracev1.ScopeSpans{
				{
					Spans: []*tracev1.Span{
						{
							TraceId:   []byte{0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77, 0x88, 0x99, 0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff, 0x00},
							SpanId:    []byte{0x11, 0x11, 0x11, 0x11, 0x11, 0x11, 0x11, 0x11},
							Name:      "span-c",
							StartTimeUnixNano: uint64(time.Now().UnixNano()),
							EndTimeUnixNano:   uint64(time.Now().UnixNano()),
						},
					},
				},
			},
		},
	}

	n := normalizer.New()
	spans, err := normalizer.NormalizeOTLP(nil, "proj-2", rss, normalizer.Options{}, n)
	if err != nil {
		t.Fatalf("NormalizeOTLP error: %v", err)
	}
	if len(spans) != 3 {
		t.Fatalf("got %d spans, want 3", len(spans))
	}
	// Check first span from svc-a
	if spans[0].ServiceName != "svc-a" {
		t.Errorf("span[0] service_name: got %q, want %q", spans[0].ServiceName, "svc-a")
	}
	if spans[0].SpanID != "0102030405060708" {
		t.Errorf("span[0] span_id: got %q, want %q", spans[0].SpanID, "0102030405060708")
	}
	// Second span from same resource
	if spans[1].ServiceName != "svc-a" {
		t.Errorf("span[1] service_name: got %q, want %q", spans[1].ServiceName, "svc-a")
	}
	if spans[1].SpanID != "090a0b0c0d0e0f10" {
		t.Errorf("span[1] span_id: got %q, want %q", spans[1].SpanID, "090a0b0c0d0e0f10")
	}
	// Third span from svc-b
	if spans[2].ServiceName != "svc-b" {
		t.Errorf("span[2] service_name: got %q, want %q", spans[2].ServiceName, "svc-b")
	}
}

// TestNormalizeOTLP_StatusConversion verifies that OTLP status codes are
// converted correctly (OK → "OK", ERROR → "ERROR", UNSET → "UNSET", absent → "").
func TestNormalizeOTLP_StatusConversion(t *testing.T) {
	tests := []struct {
		name     string
		status   *tracev1.Status
		wantCode string
		wantMsg  string
	}{
		{
			name:     "status_ok",
			status:   &tracev1.Status{Code: tracev1.Status_STATUS_CODE_OK, Message: "fine"},
			wantCode: "OK",
			wantMsg:  "fine",
		},
		{
			name:     "status_error",
			status:   &tracev1.Status{Code: tracev1.Status_STATUS_CODE_ERROR, Message: "boom"},
			wantCode: "ERROR",
			wantMsg:  "boom",
		},
		{
			name:     "status_unset",
			status:   &tracev1.Status{Code: tracev1.Status_STATUS_CODE_UNSET},
			wantCode: "UNSET",
			wantMsg:  "",
		},
		{
			name:     "status_absent",
			status:   nil,
			wantCode: "",
			wantMsg:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rss := []*tracev1.ResourceSpans{
				{
					ScopeSpans: []*tracev1.ScopeSpans{
						{
							Spans: []*tracev1.Span{
								{
									TraceId:   []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10},
									SpanId:    []byte{0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18},
									Name:      "status-span",
									Status:    tt.status,
									StartTimeUnixNano: uint64(time.Now().UnixNano()),
									EndTimeUnixNano:   uint64(time.Now().UnixNano()),
								},
							},
						},
					},
				},
			}

			n := normalizer.New()
			spans, err := normalizer.NormalizeOTLP(nil, "proj-1", rss, normalizer.Options{}, n)
			if err != nil {
				t.Fatalf("NormalizeOTLP error: %v", err)
			}
			if got := spans[0].StatusCode; got != tt.wantCode {
				t.Errorf("status_code: got %q, want %q", got, tt.wantCode)
			}
			if got := spans[0].StatusMessage; got != tt.wantMsg {
				t.Errorf("status_message: got %q, want %q", got, tt.wantMsg)
			}
		})
	}
}

// TestNormalizeOTLP_EmptyInput verifies that an empty ResourceSpans slice
// returns zero spans without error.
func TestNormalizeOTLP_EmptyInput(t *testing.T) {
	n := normalizer.New()
	spans, err := normalizer.NormalizeOTLP(nil, "proj-1", nil, normalizer.Options{}, n)
	if err != nil {
		t.Fatalf("NormalizeOTLP error: %v", err)
	}
	if len(spans) != 0 {
		t.Errorf("got %d spans, want 0", len(spans))
	}
}

// TestNormalizeOTLP_AttributesPropagation verifies that OTLP span attributes
// end up in the domain span's Attributes map.
func TestNormalizeOTLP_AttributesPropagation(t *testing.T) {
	rss := []*tracev1.ResourceSpans{
		{
			ScopeSpans: []*tracev1.ScopeSpans{
				{
					Spans: []*tracev1.Span{
						{
							TraceId:   []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10},
							SpanId:    []byte{0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18},
							Name:      "attr-span",
							StartTimeUnixNano: uint64(time.Now().UnixNano()),
							EndTimeUnixNano:   uint64(time.Now().UnixNano()),
							Attributes: []*commonv1.KeyValue{
								{Key: "custom.key", Value: &commonv1.AnyValue{Value: &commonv1.AnyValue_StringValue{StringValue: "custom-value"}}},
								{Key: "bool.key", Value: &commonv1.AnyValue{Value: &commonv1.AnyValue_BoolValue{BoolValue: true}}},
								{Key: "int.key", Value: &commonv1.AnyValue{Value: &commonv1.AnyValue_IntValue{IntValue: 42}}},
							},
						},
					},
				},
			},
		},
	}

	n := normalizer.New()
	spans, err := normalizer.NormalizeOTLP(nil, "proj-1", rss, normalizer.Options{}, n)
	if err != nil {
		t.Fatalf("NormalizeOTLP error: %v", err)
	}
	if len(spans) != 1 {
		t.Fatalf("got %d spans, want 1", len(spans))
	}
	s := spans[0]
	if s.Attributes == nil {
		t.Fatal("attributes should not be nil")
	}
	if s.Attributes["custom.key"] != "custom-value" {
		t.Errorf("attributes.custom.key: got %v, want %q", s.Attributes["custom.key"], "custom-value")
	}
	if s.Attributes["bool.key"] != true {
		t.Errorf("attributes.bool.key: got %v, want true", s.Attributes["bool.key"])
	}
	if s.Attributes["int.key"] != int64(42) {
		t.Errorf("attributes.int.key: got %v, want 42", s.Attributes["int.key"])
	}
}

// TestNormalizeOTLP_GenAIAttributes verifies that GenAI attributes are
// extracted into model, input, output, and token count fields.
func TestNormalizeOTLP_GenAIAttributes(t *testing.T) {
	rss := []*tracev1.ResourceSpans{
		{
			ScopeSpans: []*tracev1.ScopeSpans{
				{
					Spans: []*tracev1.Span{
						{
							TraceId:   []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10},
							SpanId:    []byte{0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18},
							Name:      "genai-span",
							StartTimeUnixNano: uint64(time.Now().UnixNano()),
							EndTimeUnixNano:   uint64(time.Now().UnixNano()),
							Attributes: []*commonv1.KeyValue{
								{Key: "gen_ai.request.model", Value: &commonv1.AnyValue{Value: &commonv1.AnyValue_StringValue{StringValue: "gpt-4o"}}},
								{Key: "gen_ai.usage.input_tokens", Value: &commonv1.AnyValue{Value: &commonv1.AnyValue_IntValue{IntValue: 100}}},
								{Key: "gen_ai.usage.output_tokens", Value: &commonv1.AnyValue{Value: &commonv1.AnyValue_IntValue{IntValue: 50}}},
								{Key: "gen_ai.prompt.0.role", Value: &commonv1.AnyValue{Value: &commonv1.AnyValue_StringValue{StringValue: "user"}}},
								{Key: "gen_ai.prompt.0.content", Value: &commonv1.AnyValue{Value: &commonv1.AnyValue_StringValue{StringValue: "Hello"}}},
								{Key: "gen_ai.completion.0.role", Value: &commonv1.AnyValue{Value: &commonv1.AnyValue_StringValue{StringValue: "assistant"}}},
								{Key: "gen_ai.completion.0.content", Value: &commonv1.AnyValue{Value: &commonv1.AnyValue_StringValue{StringValue: "Hi there"}}},
							},
						},
					},
				},
			},
		},
	}

	n := normalizer.New()
	spans, err := normalizer.NormalizeOTLP(nil, "proj-1", rss, normalizer.Options{}, n)
	if err != nil {
		t.Fatalf("NormalizeOTLP error: %v", err)
	}
	if len(spans) != 1 {
		t.Fatalf("got %d spans, want 1", len(spans))
	}
	s := spans[0]
	if s.Model != "gpt-4o" {
		t.Errorf("model: got %q, want %q", s.Model, "gpt-4o")
	}
	if s.InputTokens != 100 {
		t.Errorf("input_tokens: got %d, want 100", s.InputTokens)
	}
	if s.OutputTokens != 50 {
		t.Errorf("output_tokens: got %d, want 50", s.OutputTokens)
	}
	if s.Input == "" {
		t.Error("input should not be empty")
	}
	if s.Output == "" {
		t.Error("output should not be empty")
	}
}

// TestNormalizeOTLP_KindDerivation verifies that the span kind is derived
// from OTel GenAI/LLM attributes.
func TestNormalizeOTLP_KindDerivation(t *testing.T) {
	tests := []struct {
		name     string
		attrs    []*commonv1.KeyValue
		wantKind domain.SpanKind
	}{
		{
			name: "genai_model",
			attrs: []*commonv1.KeyValue{
				{Key: "gen_ai.request.model", Value: &commonv1.AnyValue{Value: &commonv1.AnyValue_StringValue{StringValue: "gpt-4"}}},
			},
			wantKind: domain.SpanKindLLM,
		},
		{
			name: "tool_name",
			attrs: []*commonv1.KeyValue{
				{Key: "tool.name", Value: &commonv1.AnyValue{Value: &commonv1.AnyValue_StringValue{StringValue: "search"}}},
			},
			wantKind: domain.SpanKindTool,
		},
		{
			name: "openinference_agent",
			attrs: []*commonv1.KeyValue{
				{Key: "openinference.span.kind", Value: &commonv1.AnyValue{Value: &commonv1.AnyValue_StringValue{StringValue: "AGENT"}}},
			},
			wantKind: domain.SpanKindAgent,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rss := []*tracev1.ResourceSpans{
				{
					ScopeSpans: []*tracev1.ScopeSpans{
						{
							Spans: []*tracev1.Span{
								{
									TraceId:   []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10},
									SpanId:    []byte{0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18},
									Name:      "kind-span",
									StartTimeUnixNano: uint64(time.Now().UnixNano()),
									EndTimeUnixNano:   uint64(time.Now().UnixNano()),
									Attributes: tt.attrs,
								},
							},
						},
					},
				},
			}

			n := normalizer.New()
			spans, err := normalizer.NormalizeOTLP(nil, "proj-1", rss, normalizer.Options{}, n)
			if err != nil {
				t.Fatalf("NormalizeOTLP error: %v", err)
			}
			if got := spans[0].Kind; got != tt.wantKind {
				t.Errorf("kind: got %q, want %q", got, tt.wantKind)
			}
		})
	}
}

// TestNormalizeOTLP_ServiceNameOverride verifies that the ServiceNameOverride
// option takes precedence over the resource-level service.name attribute.
func TestNormalizeOTLP_ServiceNameOverride(t *testing.T) {
	rss := []*tracev1.ResourceSpans{
		{
			Resource: &resourcev1.Resource{
				Attributes: []*commonv1.KeyValue{
					{Key: "service.name", Value: &commonv1.AnyValue{Value: &commonv1.AnyValue_StringValue{StringValue: "resource-svc"}}},
				},
			},
			ScopeSpans: []*tracev1.ScopeSpans{
				{
					Spans: []*tracev1.Span{
						{
							TraceId:   []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10},
							SpanId:    []byte{0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18},
							Name:      "override-span",
							StartTimeUnixNano: uint64(time.Now().UnixNano()),
							EndTimeUnixNano:   uint64(time.Now().UnixNano()),
						},
					},
				},
			},
		},
	}

	n := normalizer.New()

	// Without override, resource-level service.name should be used.
	t.Run("no override", func(t *testing.T) {
		spans, err := normalizer.NormalizeOTLP(nil, "proj-1", rss, normalizer.Options{}, n)
		if err != nil {
			t.Fatalf("NormalizeOTLP error: %v", err)
		}
		if spans[0].ServiceName != "resource-svc" {
			t.Errorf("service_name: got %q, want %q", spans[0].ServiceName, "resource-svc")
		}
	})

	// With ServiceNameOverride set, it must take precedence.
	t.Run("with override", func(t *testing.T) {
		spans, err := normalizer.NormalizeOTLP(nil, "proj-1", rss, normalizer.Options{ServiceNameOverride: "api-service"}, n)
		if err != nil {
			t.Fatalf("NormalizeOTLP error: %v", err)
		}
		if spans[0].ServiceName != "api-service" {
			t.Errorf("service_name: got %q, want %q (override should win)", spans[0].ServiceName, "api-service")
		}
	})
}

// TestNormalizeOTLP_ParentSpanID verifies that parent span IDs are propagated.
func TestNormalizeOTLP_ParentSpanID(t *testing.T) {
	rss := []*tracev1.ResourceSpans{
		{
			ScopeSpans: []*tracev1.ScopeSpans{
				{
					Spans: []*tracev1.Span{
						{
							TraceId:   []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10},
							SpanId:    []byte{0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18},
							ParentSpanId: []byte{0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff, 0x00, 0x11},
							Name:      "child-span",
							StartTimeUnixNano: uint64(time.Now().UnixNano()),
							EndTimeUnixNano:   uint64(time.Now().UnixNano()),
						},
					},
				},
			},
		},
	}

	n := normalizer.New()
	spans, err := normalizer.NormalizeOTLP(nil, "proj-1", rss, normalizer.Options{}, n)
	if err != nil {
		t.Fatalf("NormalizeOTLP error: %v", err)
	}
	if len(spans) != 1 {
		t.Fatalf("got %d spans, want 1", len(spans))
	}
	if spans[0].ParentID != "aabbccddeeff0011" {
		t.Errorf("parent_id: got %q, want %q", spans[0].ParentID, "aabbccddeeff0011")
	}
}

// --- Additional migrated test scenarios ---

// TestNormalizeOTLP_LegacyTokenFallback verifies that legacy prompt_tokens and
// completion_tokens attributes are used as fallbacks when the newer
// gen_ai.usage.* attributes are absent.
func TestNormalizeOTLP_LegacyTokenFallback(t *testing.T) {
	rss := []*tracev1.ResourceSpans{
		{
			ScopeSpans: []*tracev1.ScopeSpans{
				{
					Spans: []*tracev1.Span{
						{
							TraceId:   []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10},
							SpanId:    []byte{0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18},
							Name:      "llm-call",
							StartTimeUnixNano: uint64(time.Now().UnixNano()),
							EndTimeUnixNano:   uint64(time.Now().UnixNano()),
							Attributes: []*commonv1.KeyValue{
								{Key: "prompt_tokens", Value: &commonv1.AnyValue{Value: &commonv1.AnyValue_IntValue{IntValue: 200}}},
								{Key: "completion_tokens", Value: &commonv1.AnyValue{Value: &commonv1.AnyValue_IntValue{IntValue: 100}}},
							},
						},
					},
				},
			},
		},
	}

	n := normalizer.New()
	spans, err := normalizer.NormalizeOTLP(nil, "proj-1", rss, normalizer.Options{}, n)
	if err != nil {
		t.Fatalf("NormalizeOTLP error: %v", err)
	}
	if spans[0].InputTokens != 200 {
		t.Errorf("input_tokens: got %d, want 200 (legacy fallback)", spans[0].InputTokens)
	}
	if spans[0].OutputTokens != 100 {
		t.Errorf("output_tokens: got %d, want 100 (legacy fallback)", spans[0].OutputTokens)
	}
}

// TestNormalizeOTLP_AttributesOverflow verifies that unmapped span attributes
// end up in the overflow attributes map, while extracted fields (model, tokens,
// prompt, completion) are removed.
func TestNormalizeOTLP_AttributesOverflow(t *testing.T) {
	rss := []*tracev1.ResourceSpans{
		{
			ScopeSpans: []*tracev1.ScopeSpans{
				{
					Spans: []*tracev1.Span{
						{
							TraceId:   []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10},
							SpanId:    []byte{0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18},
							Name:      "test",
							StartTimeUnixNano: uint64(time.Now().UnixNano()),
							EndTimeUnixNano:   uint64(time.Now().UnixNano()),
							Attributes: []*commonv1.KeyValue{
								{Key: "gen_ai.request.model", Value: &commonv1.AnyValue{Value: &commonv1.AnyValue_StringValue{StringValue: "gpt-4"}}},
								{Key: "gen_ai.usage.input_tokens", Value: &commonv1.AnyValue{Value: &commonv1.AnyValue_IntValue{IntValue: 100}}},
								{Key: "http.url", Value: &commonv1.AnyValue{Value: &commonv1.AnyValue_StringValue{StringValue: "https://example.com"}}},
								{Key: "custom.attr", Value: &commonv1.AnyValue{Value: &commonv1.AnyValue_StringValue{StringValue: "value"}}},
							},
						},
					},
				},
			},
		},
	}

	n := normalizer.New()
	spans, err := normalizer.NormalizeOTLP(nil, "proj-1", rss, normalizer.Options{}, n)
	if err != nil {
		t.Fatalf("NormalizeOTLP error: %v", err)
	}

	if spans[0].Attributes == nil {
		t.Fatal("overflow attributes should not be nil")
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

// TestNormalizeOTLP_ExplicitOmnevalKind verifies that an explicit omneval.kind
// attribute takes precedence over other kind derivation heuristics.
func TestNormalizeOTLP_ExplicitOmnevalKind(t *testing.T) {
	rss := []*tracev1.ResourceSpans{
		{
			ScopeSpans: []*tracev1.ScopeSpans{
				{
					Spans: []*tracev1.Span{
						{
							TraceId:   []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10},
							SpanId:    []byte{0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18},
							Name:      "internal-work",
							StartTimeUnixNano: uint64(time.Now().UnixNano()),
							EndTimeUnixNano:   uint64(time.Now().UnixNano()),
							Attributes: []*commonv1.KeyValue{
								{Key: "omneval.kind", Value: &commonv1.AnyValue{Value: &commonv1.AnyValue_StringValue{StringValue: "internal"}}},
								{Key: "gen_ai.request.model", Value: &commonv1.AnyValue{Value: &commonv1.AnyValue_StringValue{StringValue: "gpt-4"}}},
							},
						},
					},
				},
			},
		},
	}

	n := normalizer.New()
	spans, err := normalizer.NormalizeOTLP(nil, "proj-1", rss, normalizer.Options{}, n)
	if err != nil {
		t.Fatalf("NormalizeOTLP error: %v", err)
	}
	if spans[0].Kind != domain.SpanKindInternal {
		t.Errorf("kind: got %q, want %q (explicit omneval.kind should win over gen_ai)", spans[0].Kind, domain.SpanKindInternal)
	}
}

// TestNormalizeOTLP_KindDerivation_OpenInferenceChainAndRetriever verifies
// that OpenInference span kinds CHAIN, RETRIEVER, EMBEDDING, RERANKER, and
// GUARDRAIL all map to SpanKindChain.
func TestNormalizeOTLP_KindDerivation_OpenInferenceChainAndRetriever(t *testing.T) {
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

	for _, tt := range tests {
		t.Run(tt.oiKind, func(t *testing.T) {
			rss := []*tracev1.ResourceSpans{
				{
					ScopeSpans: []*tracev1.ScopeSpans{
						{
							Spans: []*tracev1.Span{
								{
									TraceId:   []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10},
									SpanId:    []byte{0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18},
									Name:      "some-span",
									StartTimeUnixNano: uint64(time.Now().UnixNano()),
									EndTimeUnixNano:   uint64(time.Now().UnixNano()),
									Attributes: []*commonv1.KeyValue{
										{Key: "openinference.span.kind", Value: &commonv1.AnyValue{Value: &commonv1.AnyValue_StringValue{StringValue: tt.oiKind}}},
									},
								},
							},
						},
					},
				},
			}

			n := normalizer.New()
			spans, err := normalizer.NormalizeOTLP(nil, "proj-1", rss, normalizer.Options{}, n)
			if err != nil {
				t.Fatalf("NormalizeOTLP error: %v", err)
			}
			if spans[0].Kind != tt.want {
				t.Errorf("openinference.span.kind=%q: kind got %q, want %q", tt.oiKind, spans[0].Kind, tt.want)
			}
		})
	}
}

// TestNormalizeOTLP_KindDerivation_GenAIOperationName verifies that the
// gen_ai.operation.name attribute correctly derives span kinds for agent,
// tool, and LLM operations.
func TestNormalizeOTLP_KindDerivation_GenAIOperationName(t *testing.T) {
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

	for _, tt := range tests {
		t.Run(tt.operation, func(t *testing.T) {
			rss := []*tracev1.ResourceSpans{
				{
					ScopeSpans: []*tracev1.ScopeSpans{
						{
							Spans: []*tracev1.Span{
								{
									TraceId:   []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10},
									SpanId:    []byte{0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18},
									Name:      "some-span",
									StartTimeUnixNano: uint64(time.Now().UnixNano()),
									EndTimeUnixNano:   uint64(time.Now().UnixNano()),
									Attributes: []*commonv1.KeyValue{
										{Key: "gen_ai.operation.name", Value: &commonv1.AnyValue{Value: &commonv1.AnyValue_StringValue{StringValue: tt.operation}}},
									},
								},
							},
						},
					},
				},
			}

			n := normalizer.New()
			spans, err := normalizer.NormalizeOTLP(nil, "proj-1", rss, normalizer.Options{}, n)
			if err != nil {
				t.Fatalf("NormalizeOTLP error: %v", err)
			}
			if spans[0].Kind != tt.want {
				t.Errorf("gen_ai.operation.name=%q: kind got %q, want %q", tt.operation, spans[0].Kind, tt.want)
			}
		})
	}
}

// TestNormalizeOTLP_KindDerivation_NameHeuristics verifies that span names
// are used as a fallback for kind derivation (e.g. ".step" → Agent,
// "*Action" → Tool).
func TestNormalizeOTLP_KindDerivation_NameHeuristics(t *testing.T) {
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

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rss := []*tracev1.ResourceSpans{
				{
					ScopeSpans: []*tracev1.ScopeSpans{
						{
							Spans: []*tracev1.Span{
								{
									TraceId:   []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10},
									SpanId:    []byte{0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18},
									Name:      tt.name,
									StartTimeUnixNano: uint64(time.Now().UnixNano()),
									EndTimeUnixNano:   uint64(time.Now().UnixNano()),
								},
							},
						},
					},
				},
			}

			n := normalizer.New()
			spans, err := normalizer.NormalizeOTLP(nil, "proj-1", rss, normalizer.Options{}, n)
			if err != nil {
				t.Fatalf("NormalizeOTLP error: %v", err)
			}
			if spans[0].Kind != tt.want {
				t.Errorf("name=%q: kind got %q, want %q", tt.name, spans[0].Kind, tt.want)
			}
		})
	}
}

// TestNormalizeOTLP_KindDerivation_DefaultInternal verifies that when no
// attributes or name patterns match, the span kind defaults to Internal.
func TestNormalizeOTLP_KindDerivation_DefaultInternal(t *testing.T) {
	rss := []*tracev1.ResourceSpans{
		{
			ScopeSpans: []*tracev1.ScopeSpans{
				{
					Spans: []*tracev1.Span{
						{
							TraceId:   []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10},
							SpanId:    []byte{0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18},
							Name:      "some-operation",
							StartTimeUnixNano: uint64(time.Now().UnixNano()),
							EndTimeUnixNano:   uint64(time.Now().UnixNano()),
							Attributes: []*commonv1.KeyValue{
								{Key: "http.url", Value: &commonv1.AnyValue{Value: &commonv1.AnyValue_StringValue{StringValue: "https://example.com"}}},
							},
						},
					},
				},
			},
		},
	}

	n := normalizer.New()
	spans, err := normalizer.NormalizeOTLP(nil, "proj-1", rss, normalizer.Options{}, n)
	if err != nil {
		t.Fatalf("NormalizeOTLP error: %v", err)
	}
	if spans[0].Kind != domain.SpanKindInternal {
		t.Errorf("kind: got %q, want %q (default should be internal)", spans[0].Kind, domain.SpanKindInternal)
	}
}

// TestNormalizeOTLP_NoTokens verifies that spans without any token attributes
// get zero for both input and output tokens.
func TestNormalizeOTLP_NoTokens(t *testing.T) {
	rss := []*tracev1.ResourceSpans{
		{
			ScopeSpans: []*tracev1.ScopeSpans{
				{
					Spans: []*tracev1.Span{
						{
							TraceId:   []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10},
							SpanId:    []byte{0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18},
							Name:      "no-token-span",
							StartTimeUnixNano: uint64(time.Now().UnixNano()),
							EndTimeUnixNano:   uint64(time.Now().UnixNano()),
						},
					},
				},
			},
		},
	}

	n := normalizer.New()
	spans, err := normalizer.NormalizeOTLP(nil, "proj-1", rss, normalizer.Options{}, n)
	if err != nil {
		t.Fatalf("NormalizeOTLP error: %v", err)
	}
	if spans[0].InputTokens != 0 {
		t.Errorf("input_tokens: got %d, want 0 (no token attributes)", spans[0].InputTokens)
	}
	if spans[0].OutputTokens != 0 {
		t.Errorf("output_tokens: got %d, want 0 (no token attributes)", spans[0].OutputTokens)
	}
}

// TestNormalizeOTLP_StartEndTimes verifies that explicit start and end times
// from OTLP unix nanoseconds are preserved in the resulting span.
func TestNormalizeOTLP_StartEndTimes(t *testing.T) {
	startTime := uint64(time.Date(2025, 6, 15, 14, 30, 0, 0, time.UTC).UnixNano())
	endTime := uint64(time.Date(2025, 6, 15, 14, 31, 30, 0, time.UTC).UnixNano())

	rss := []*tracev1.ResourceSpans{
		{
			ScopeSpans: []*tracev1.ScopeSpans{
				{
					Spans: []*tracev1.Span{
						{
							TraceId:           []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10},
							SpanId:            []byte{0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18},
							Name:              "timed-span",
							StartTimeUnixNano: startTime,
							EndTimeUnixNano:   endTime,
						},
					},
				},
			},
		},
	}

	n := normalizer.New()
	spans, err := normalizer.NormalizeOTLP(nil, "proj-1", rss, normalizer.Options{}, n)
	if err != nil {
		t.Fatalf("NormalizeOTLP error: %v", err)
	}
	if len(spans) != 1 {
		t.Fatalf("got %d spans, want 1", len(spans))
	}
	wantStart := time.Date(2025, 6, 15, 14, 30, 0, 0, time.UTC)
	if spans[0].StartTime != wantStart {
		t.Errorf("start_time: got %v, want %v", spans[0].StartTime, wantStart)
	}
	wantEnd := time.Date(2025, 6, 15, 14, 31, 30, 0, time.UTC)
	if spans[0].EndTime != wantEnd {
		t.Errorf("end_time: got %v, want %v", spans[0].EndTime, wantEnd)
	}
}

// TestNormalizeOTLP_OmnevalInputOutput verifies that input and output values
// from omneval.input and omneval.output attributes are extracted.
func TestNormalizeOTLP_OmnevalInputOutput(t *testing.T) {
	rss := []*tracev1.ResourceSpans{
		{
			ScopeSpans: []*tracev1.ScopeSpans{
				{
					Spans: []*tracev1.Span{
						{
							TraceId:   []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10},
							SpanId:    []byte{0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18},
							Name:      "omneval-span",
							StartTimeUnixNano: uint64(time.Now().UnixNano()),
							EndTimeUnixNano:   uint64(time.Now().UnixNano()),
							Attributes: []*commonv1.KeyValue{
								{Key: "omneval.input", Value: &commonv1.AnyValue{Value: &commonv1.AnyValue_StringValue{StringValue: `{"query":"hello"}`}}},
								{Key: "omneval.output", Value: &commonv1.AnyValue{Value: &commonv1.AnyValue_StringValue{StringValue: `{"result":"hi"}`}}},
							},
						},
					},
				},
			},
		},
	}

	n := normalizer.New()
	spans, err := normalizer.NormalizeOTLP(nil, "proj-1", rss, normalizer.Options{}, n)
	if err != nil {
		t.Fatalf("NormalizeOTLP error: %v", err)
	}
	if spans[0].Input != `{"query":"hello"}` {
		t.Errorf("input: got %q, want %q", spans[0].Input, `{"query":"hello"}`)
	}
	if spans[0].Output != `{"result":"hi"}` {
		t.Errorf("output: got %q, want %q", spans[0].Output, `{"result":"hi"}`)
	}
}

// TestNormalizeOTLP_ConversationIDOmnevalFallback verifies that the
// omneval.conversation.id attribute is used as fallback when
// gen_ai.conversation.id is absent.
func TestNormalizeOTLP_ConversationIDOmnevalFallback(t *testing.T) {
	rss := []*tracev1.ResourceSpans{
		{
			ScopeSpans: []*tracev1.ScopeSpans{
				{
					Spans: []*tracev1.Span{
						{
							TraceId:   []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10},
							SpanId:    []byte{0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18},
							Name:      "conv-span",
							StartTimeUnixNano: uint64(time.Now().UnixNano()),
							EndTimeUnixNano:   uint64(time.Now().UnixNano()),
							Attributes: []*commonv1.KeyValue{
								{Key: "omneval.conversation.id", Value: &commonv1.AnyValue{Value: &commonv1.AnyValue_StringValue{StringValue: "conv-omneval-123"}}},
							},
						},
					},
				},
			},
		},
	}

	n := normalizer.New()
	spans, err := normalizer.NormalizeOTLP(nil, "proj-1", rss, normalizer.Options{}, n)
	if err != nil {
		t.Fatalf("NormalizeOTLP error: %v", err)
	}
	if spans[0].ConversationID != "conv-omneval-123" {
		t.Errorf("conversation_id: got %q, want %q", spans[0].ConversationID, "conv-omneval-123")
	}
}

// TestNormalizeOTLP_SpanEventsCaptured verifies that when an OTLP span carries
// SpanEvents with gen_ai.prompt.message and gen_ai.completion.message attributes,
// the normalizer extracts their role/content pairs into the domain span's Input
// and Output fields.
func TestNormalizeOTLP_SpanEventsCaptured(t *testing.T) {
	rss := []*tracev1.ResourceSpans{
		{
			ScopeSpans: []*tracev1.ScopeSpans{
				{
					Spans: []*tracev1.Span{
						{
							TraceId:   []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10},
							SpanId:    []byte{0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18},
							Name:      "litellm.completion",
							StartTimeUnixNano: uint64(time.Now().UnixNano()),
							EndTimeUnixNano:   uint64(time.Now().UnixNano()),
							// No gen_ai.prompt / gen_ai.completion numbered attributes.
							// Content lives ONLY in SpanEvents.
							Events: []*tracev1.Span_Event{
								{
									Name:         "gen_ai.prompt.message",
									TimeUnixNano: uint64(time.Now().Add(-1 * time.Second).UnixNano()),
									Attributes: []*commonv1.KeyValue{
										{Key: "gen_ai.prompt.message.role", Value: &commonv1.AnyValue{Value: &commonv1.AnyValue_StringValue{StringValue: "user"}}},
										{Key: "gen_ai.prompt.message.content", Value: &commonv1.AnyValue{Value: &commonv1.AnyValue_StringValue{StringValue: "What is the weather?"}}},
									},
								},
								{
									Name:         "gen_ai.completion.message",
									TimeUnixNano: uint64(time.Now().UnixNano()),
									Attributes: []*commonv1.KeyValue{
										{Key: "gen_ai.completion.message.role", Value: &commonv1.AnyValue{Value: &commonv1.AnyValue_StringValue{StringValue: "assistant"}}},
										{Key: "gen_ai.completion.message.content", Value: &commonv1.AnyValue{Value: &commonv1.AnyValue_StringValue{StringValue: "The weather is sunny."}}},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	n := normalizer.New()
	spans, err := normalizer.NormalizeOTLP(nil, "proj-1", rss, normalizer.Options{}, n)
	if err != nil {
		t.Fatalf("NormalizeOTLP error: %v", err)
	}
	if len(spans) != 1 {
		t.Fatalf("got %d spans, want 1", len(spans))
	}

	// Input should contain the user message from the prompt event.
	if !strings.Contains(spans[0].Input, `"role":"user"`) || !strings.Contains(spans[0].Input, `"content":"What is the weather?"`) {
		t.Errorf("input: got %q (expected user message from Span Events)", spans[0].Input)
	}
	// Output should contain the assistant message from the completion event.
	if !strings.Contains(spans[0].Output, `"role":"assistant"`) || !strings.Contains(spans[0].Output, `"content":"The weather is sunny."`) {
		t.Errorf("output: got %q (expected assistant message from Span Events)", spans[0].Output)
	}
}

// TestNormalizeOTLP_AttributesPriorityOverEvents verifies that when both
// span attributes (e.g. gen_ai.input.messages) and span events carry content,
// the attribute path wins and the event content is ignored.
func TestNormalizeOTLP_AttributesPriorityOverEvents(t *testing.T) {
	rss := []*tracev1.ResourceSpans{
		{
			ScopeSpans: []*tracev1.ScopeSpans{
				{
					Spans: []*tracev1.Span{
						{
							TraceId:   []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10},
							SpanId:    []byte{0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18},
							Name:      "litellm.completion",
							StartTimeUnixNano: uint64(time.Now().UnixNano()),
							EndTimeUnixNano:   uint64(time.Now().UnixNano()),
							Attributes: []*commonv1.KeyValue{
								{Key: "gen_ai.input.messages", Value: &commonv1.AnyValue{Value: &commonv1.AnyValue_StringValue{
									StringValue: `[{"role":"user","content":"Attribute content"}]`,
								}}},
								{Key: "gen_ai.output.messages", Value: &commonv1.AnyValue{Value: &commonv1.AnyValue_StringValue{
									StringValue: `[{"role":"assistant","content":"Attribute output"}]`,
								}}},
							},
							Events: []*tracev1.Span_Event{
								{
									Name:         "gen_ai.prompt.message",
									TimeUnixNano: uint64(time.Now().Add(-1 * time.Second).UnixNano()),
									Attributes: []*commonv1.KeyValue{
										{Key: "gen_ai.prompt.message.role", Value: &commonv1.AnyValue{Value: &commonv1.AnyValue_StringValue{StringValue: "user"}}},
										{Key: "gen_ai.prompt.message.content", Value: &commonv1.AnyValue{Value: &commonv1.AnyValue_StringValue{StringValue: "Event content"}}},
									},
								},
								{
									Name:         "gen_ai.completion.message",
									TimeUnixNano: uint64(time.Now().UnixNano()),
									Attributes: []*commonv1.KeyValue{
										{Key: "gen_ai.completion.message.role", Value: &commonv1.AnyValue{Value: &commonv1.AnyValue_StringValue{StringValue: "assistant"}}},
										{Key: "gen_ai.completion.message.content", Value: &commonv1.AnyValue{Value: &commonv1.AnyValue_StringValue{StringValue: "Event output"}}},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	n := normalizer.New()
	spans, err := normalizer.NormalizeOTLP(nil, "proj-1", rss, normalizer.Options{}, n)
	if err != nil {
		t.Fatalf("NormalizeOTLP error: %v", err)
	}
	if len(spans) != 1 {
		t.Fatalf("got %d spans, want 1", len(spans))
	}

	// Input should come from attributes, not events.
	if !strings.Contains(spans[0].Input, "Attribute content") {
		t.Errorf("input should contain 'Attribute content', got: %q", spans[0].Input)
	}
	if strings.Contains(spans[0].Input, "Event content") {
		t.Errorf("input should NOT contain 'Event content' (attributes should win), got: %q", spans[0].Input)
	}

	// Output should come from attributes, not events.
	if !strings.Contains(spans[0].Output, "Attribute output") {
		t.Errorf("output should contain 'Attribute output', got: %q", spans[0].Output)
	}
	if strings.Contains(spans[0].Output, "Event output") {
		t.Errorf("output should NOT contain 'Event output' (attributes should win), got: %q", spans[0].Output)
	}
}

// TestNormalizeOTLP_SessionIDMapping verifies that session-id attributes from
// OTLP instrumentation map into the domain conversation id, with SDK
// conversation-id attributes taking precedence over session-id forms:
// gen_ai.conversation.id > omneval.conversation.id > session.id >
// lanr.association.properties.session_id.
func TestNormalizeOTLP_SessionIDMapping(t *testing.T) {
	strAttr := func(key, val string) *commonv1.KeyValue {
		return &commonv1.KeyValue{Key: key, Value: &commonv1.AnyValue{Value: &commonv1.AnyValue_StringValue{StringValue: val}}}
	}

	cases := []struct {
		name  string
		attrs []*commonv1.KeyValue
		want  string
	}{
		{
			name:  "session.id maps to conversation id",
			attrs: []*commonv1.KeyValue{strAttr("session.id", "sess-otel-1")},
			want:  "sess-otel-1",
		},
		{
			name:  "lanr association properties session_id maps to conversation id",
			attrs: []*commonv1.KeyValue{strAttr("lanr.association.properties.session_id", "sess-lanr-1")},
			want:  "sess-lanr-1",
		},
		{
			name: "gen_ai.conversation.id wins over session.id",
			attrs: []*commonv1.KeyValue{
				strAttr("session.id", "sess-otel-1"),
				strAttr("gen_ai.conversation.id", "conv-sdk-1"),
			},
			want: "conv-sdk-1",
		},
		{
			name: "session.id wins over lanr association form",
			attrs: []*commonv1.KeyValue{
				strAttr("lanr.association.properties.session_id", "sess-lanr-1"),
				strAttr("session.id", "sess-otel-1"),
			},
			want: "sess-otel-1",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rss := []*tracev1.ResourceSpans{
				{
					ScopeSpans: []*tracev1.ScopeSpans{
						{
							Spans: []*tracev1.Span{
								{
									TraceId:           []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10},
									SpanId:            []byte{0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18},
									Name:              "session-span",
									StartTimeUnixNano: uint64(time.Now().UnixNano()),
									EndTimeUnixNano:   uint64(time.Now().UnixNano()),
									Attributes:        tc.attrs,
								},
							},
						},
					},
				},
			}

			n := normalizer.New()
			spans, err := normalizer.NormalizeOTLP(nil, "proj-1", rss, normalizer.Options{}, n)
			if err != nil {
				t.Fatalf("NormalizeOTLP error: %v", err)
			}
			if spans[0].ConversationID != tc.want {
				t.Errorf("conversation_id: got %q, want %q", spans[0].ConversationID, tc.want)
			}
		})
	}
}

// TestNormalizeOTLP_ZeroTimeSpan verifies that a span with zero start/end times
// does not set start_time / end_time fields (time.Time{} should be absent).
func TestNormalizeOTLP_ZeroTimeSpan(t *testing.T) {
	rss := []*tracev1.ResourceSpans{
		{
			ScopeSpans: []*tracev1.ScopeSpans{
				{
					Spans: []*tracev1.Span{
						{
							TraceId:           []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10},
							SpanId:            []byte{0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18},
							Name:              "zero-time-span",
							StartTimeUnixNano: 0,
							EndTimeUnixNano:   0,
						},
					},
				},
			},
		},
	}

	n := normalizer.New()
	spans, err := normalizer.NormalizeOTLP(nil, "proj-1", rss, normalizer.Options{}, n)
	if err != nil {
		t.Fatalf("NormalizeOTLP error: %v", err)
	}
	if len(spans) != 1 {
		t.Fatalf("got %d spans, want 1", len(spans))
	}
	// Zero times should result in zero-time span, which is the zero value of time.Time{}
	if !spans[0].StartTime.IsZero() {
		t.Errorf("start_time: got %v, want zero time", spans[0].StartTime)
	}
	if !spans[0].EndTime.IsZero() {
		t.Errorf("end_time: got %v, want zero time", spans[0].EndTime)
	}
}