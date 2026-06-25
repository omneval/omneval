package normalizer_test

import (
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
	spans, err := normalizer.NormalizeOTLP(nil, "proj-1", rss, normalizer.Options{}, n)
	if err != nil {
		t.Fatalf("NormalizeOTLP error: %v", err)
	}
	// Without override, resource-level service.name should be used
	if spans[0].ServiceName != "resource-svc" {
		t.Errorf("service_name: got %q, want %q", spans[0].ServiceName, "resource-svc")
	}
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