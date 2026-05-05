package otlp_test

import (
	"encoding/json"
	"testing"

	"github.com/zbloss/lantern/internal/domain"
	"github.com/zbloss/lantern/internal/otlp"
	"github.com/zbloss/lantern/internal/otlp/protobuf"
)

// makeFlat creates a FlatResourceSpans from resource attrs and spans.
func makeFlat(resAttrs []*protobuf.KeyValue, spans ...*protobuf.Span) protobuf.FlatResourceSpans {
	return protobuf.FlatResourceSpans{
		Resource: &protobuf.Resource{Attributes: resAttrs},
		Spans:    spans,
	}
}

func TestTranslate_BasicSpan(t *testing.T) {
	rss := []protobuf.FlatResourceSpans{
		makeFlat([]*protobuf.KeyValue{
			{Key: "service.name", Value: &protobuf.AnyValue{StringValue: strPtr("my-service")}},
		},
			&protobuf.Span{
				TraceId:             []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10},
				SpanId:              []byte{0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18},
				ParentSpanId:        []byte{0x19, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e, 0x1f, 0x20},
				Name:                "test-span",
				StartTimeUnixNano:   1000000000, // 1s
				EndTimeUnixNano:     2000000000, // 2s
				Attributes:          []*protobuf.KeyValue{},
			},
		),
	}

	opts := otlp.Options{}
	spans, err := otlp.Translate("proj-1", rss, opts)
	if err != nil {
		t.Fatalf("Translate: %v", err)
	}
	if len(spans) != 1 {
		t.Fatalf("spans: got %d, want 1", len(spans))
	}

	s := spans[0]
	if s.TraceID != "0102030405060708090a0b0c0d0e0f10" {
		t.Errorf("trace_id: got %q", s.TraceID)
	}
	if s.SpanID != "1112131415161718" {
		t.Errorf("span_id: got %q", s.SpanID)
	}
	if s.ParentID != "191a1b1c1d1e1f20" {
		t.Errorf("parent_id: got %q", s.ParentID)
	}
	if s.Name != "test-span" {
		t.Errorf("name: got %q", s.Name)
	}
	if s.Kind != domain.SpanKindInternal {
		t.Errorf("kind: got %q, want %q", s.Kind, domain.SpanKindInternal)
	}
	if s.ServiceName != "my-service" {
		t.Errorf("service_name: got %q", s.ServiceName)
	}
	if s.StartTime.Unix() != 1 {
		t.Errorf("start_time: got %d, want 1", s.StartTime.Unix())
	}
	if s.EndTime.Unix() != 2 {
		t.Errorf("end_time: got %d, want 2", s.EndTime.Unix())
	}
	if s.ProjectID != "proj-1" {
		t.Errorf("project_id: got %q", s.ProjectID)
	}
}

func TestTranslate_ModelMapping(t *testing.T) {
	rss := []protobuf.FlatResourceSpans{
		makeFlat(nil,
			&protobuf.Span{
				Name: "chat",
				Attributes: []*protobuf.KeyValue{
					{Key: "gen_ai.request.model", Value: &protobuf.AnyValue{StringValue: strPtr("gpt-4")}},
				},
			},
		),
	}

	spans, err := otlp.Translate("proj-1", rss, otlp.Options{})
	if err != nil {
		t.Fatalf("Translate: %v", err)
	}
	if spans[0].Model != "gpt-4" {
		t.Errorf("model: got %q, want %q", spans[0].Model, "gpt-4")
	}
}

func TestTranslate_TokenCountsWithModernNames(t *testing.T) {
	rss := []protobuf.FlatResourceSpans{
		makeFlat(nil,
			&protobuf.Span{
				Name: "chat",
				Attributes: []*protobuf.KeyValue{
					{Key: "gen_ai.usage.input_tokens", Value: &protobuf.AnyValue{IntValue: intPtr(100)}},
					{Key: "gen_ai.usage.output_tokens", Value: &protobuf.AnyValue{IntValue: intPtr(50)}},
				},
			},
		),
	}

	spans, err := otlp.Translate("proj-1", rss, otlp.Options{})
	if err != nil {
		t.Fatalf("Translate: %v", err)
	}
	if spans[0].InputTokens != 100 {
		t.Errorf("input_tokens: got %d, want 100", spans[0].InputTokens)
	}
	if spans[0].OutputTokens != 50 {
		t.Errorf("output_tokens: got %d, want 50", spans[0].OutputTokens)
	}
}

func TestTranslate_TokenCountsWithLegacyNames(t *testing.T) {
	rss := []protobuf.FlatResourceSpans{
		makeFlat(nil,
			&protobuf.Span{
				Name: "chat",
				Attributes: []*protobuf.KeyValue{
					{Key: "prompt_tokens", Value: &protobuf.AnyValue{IntValue: intPtr(200)}},
					{Key: "completion_tokens", Value: &protobuf.AnyValue{IntValue: intPtr(100)}},
				},
			},
		),
	}

	spans, err := otlp.Translate("proj-1", rss, otlp.Options{})
	if err != nil {
		t.Fatalf("Translate: %v", err)
	}
	if spans[0].InputTokens != 200 {
		t.Errorf("input_tokens: got %d, want 200", spans[0].InputTokens)
	}
	if spans[0].OutputTokens != 100 {
		t.Errorf("output_tokens: got %d, want 100", spans[0].OutputTokens)
	}
}

func TestTranslate_TokenCountsWithModernNamesWins(t *testing.T) {
	// Modern names should take precedence over legacy ones when both are present.
	rss := []protobuf.FlatResourceSpans{
		makeFlat(nil,
			&protobuf.Span{
				Name: "chat",
				Attributes: []*protobuf.KeyValue{
					{Key: "gen_ai.usage.input_tokens", Value: &protobuf.AnyValue{IntValue: intPtr(100)}},
					{Key: "prompt_tokens", Value: &protobuf.AnyValue{IntValue: intPtr(200)}},
					{Key: "gen_ai.usage.output_tokens", Value: &protobuf.AnyValue{IntValue: intPtr(50)}},
					{Key: "completion_tokens", Value: &protobuf.AnyValue{IntValue: intPtr(100)}},
				},
			},
		),
	}

	spans, err := otlp.Translate("proj-1", rss, otlp.Options{})
	if err != nil {
		t.Fatalf("Translate: %v", err)
	}
	if spans[0].InputTokens != 100 {
		t.Errorf("input_tokens: got %d, want 100 (modern wins)", spans[0].InputTokens)
	}
	if spans[0].OutputTokens != 50 {
		t.Errorf("output_tokens: got %d, want 50 (modern wins)", spans[0].OutputTokens)
	}
}

func TestTranslate_PromptCompletion(t *testing.T) {
	rss := []protobuf.FlatResourceSpans{
		makeFlat(nil,
			&protobuf.Span{
				Name: "chat",
				Attributes: []*protobuf.KeyValue{
					{Key: "gen_ai.prompt.0.role", Value: &protobuf.AnyValue{StringValue: strPtr("system")}},
					{Key: "gen_ai.prompt.0.content", Value: &protobuf.AnyValue{StringValue: strPtr("You are helpful")}},
					{Key: "gen_ai.prompt.1.role", Value: &protobuf.AnyValue{StringValue: strPtr("user")}},
					{Key: "gen_ai.prompt.1.content", Value: &protobuf.AnyValue{StringValue: strPtr("Hello")}},
					{Key: "gen_ai.completion.0.role", Value: &protobuf.AnyValue{StringValue: strPtr("assistant")}},
					{Key: "gen_ai.completion.0.content", Value: &protobuf.AnyValue{StringValue: strPtr("Hi there!")}},
				},
			},
		),
	}

	spans, err := otlp.Translate("proj-1", rss, otlp.Options{})
	if err != nil {
		t.Fatalf("Translate: %v", err)
	}

	// Check Input (prompt).
	var inputMsgs []map[string]string
	if err := jsonUnmarshal([]byte(spans[0].Input), &inputMsgs); err != nil {
		t.Fatalf("input not valid JSON: %v", err)
	}
	if len(inputMsgs) != 2 {
		t.Fatalf("input messages: got %d, want 2", len(inputMsgs))
	}
	if inputMsgs[0]["role"] != "system" {
		t.Errorf("input[0].role: got %q, want %q", inputMsgs[0]["role"], "system")
	}
	if inputMsgs[0]["content"] != "You are helpful" {
		t.Errorf("input[0].content: got %q", inputMsgs[0]["content"])
	}
	if inputMsgs[1]["role"] != "user" {
		t.Errorf("input[1].role: got %q", inputMsgs[1]["role"])
	}
	if inputMsgs[1]["content"] != "Hello" {
		t.Errorf("input[1].content: got %q", inputMsgs[1]["content"])
	}

	// Check Output (completion).
	var outputMsgs []map[string]string
	if err := jsonUnmarshal([]byte(spans[0].Output), &outputMsgs); err != nil {
		t.Fatalf("output not valid JSON: %v", err)
	}
	if len(outputMsgs) != 1 {
		t.Fatalf("output messages: got %d, want 1", len(outputMsgs))
	}
	if outputMsgs[0]["role"] != "assistant" {
		t.Errorf("output[0].role: got %q", outputMsgs[0]["role"])
	}
	if outputMsgs[0]["content"] != "Hi there!" {
		t.Errorf("output[0].content: got %q", outputMsgs[0]["content"])
	}
}

func TestTranslate_PromptCompletionDefaultRole(t *testing.T) {
	// If no role is specified, default to the provided role.
	rss := []protobuf.FlatResourceSpans{
		makeFlat(nil,
			&protobuf.Span{
				Name: "chat",
				Attributes: []*protobuf.KeyValue{
					{Key: "gen_ai.prompt.0.content", Value: &protobuf.AnyValue{StringValue: strPtr("Hello")}},
				},
			},
		),
	}

	spans, err := otlp.Translate("proj-1", rss, otlp.Options{})
	if err != nil {
		t.Fatalf("Translate: %v", err)
	}

	var msgs []map[string]string
	if err := jsonUnmarshal([]byte(spans[0].Input), &msgs); err != nil {
		t.Fatalf("input not valid JSON: %v", err)
	}
	if msgs[0]["role"] != "user" {
		t.Errorf("role: got %q, want %q (default)", msgs[0]["role"], "user")
	}
}

func TestTranslate_Kind_LanternKindWins(t *testing.T) {
	rss := []protobuf.FlatResourceSpans{
		makeFlat(nil,
			&protobuf.Span{
				Name: "tool-call",
				Attributes: []*protobuf.KeyValue{
					{Key: "lantern.kind", Value: &protobuf.AnyValue{StringValue: strPtr("tool")}},
					{Key: "gen_ai.request.model", Value: &protobuf.AnyValue{StringValue: strPtr("gpt-4")}},
				},
			},
		),
	}

	spans, err := otlp.Translate("proj-1", rss, otlp.Options{})
	if err != nil {
		t.Fatalf("Translate: %v", err)
	}
	if spans[0].Kind != domain.SpanKindTool {
		t.Errorf("kind: got %q, want %q (lantern.kind wins over gen_ai.*)", spans[0].Kind, domain.SpanKindTool)
	}
}

func TestTranslate_Kind_GenAITriggerLLM(t *testing.T) {
	rss := []protobuf.FlatResourceSpans{
		makeFlat(nil,
			&protobuf.Span{
				Name: "chat",
				Attributes: []*protobuf.KeyValue{
					{Key: "gen_ai.request.model", Value: &protobuf.AnyValue{StringValue: strPtr("gpt-4")}},
					{Key: "gen_ai.usage.input_tokens", Value: &protobuf.AnyValue{IntValue: intPtr(100)}},
				},
			},
		),
	}

	spans, err := otlp.Translate("proj-1", rss, otlp.Options{})
	if err != nil {
		t.Fatalf("Translate: %v", err)
	}
	if spans[0].Kind != domain.SpanKindLLM {
		t.Errorf("kind: got %q, want %q (gen_ai.* → llm)", spans[0].Kind, domain.SpanKindLLM)
	}
}

func TestTranslate_Kind_ToolTrigger(t *testing.T) {
	rss := []protobuf.FlatResourceSpans{
		makeFlat(nil,
			&protobuf.Span{
				Name: "tool-use",
				Attributes: []*protobuf.KeyValue{
					{Key: "tool.name", Value: &protobuf.AnyValue{StringValue: strPtr("search")}},
				},
			},
		),
	}

	spans, err := otlp.Translate("proj-1", rss, otlp.Options{})
	if err != nil {
		t.Fatalf("Translate: %v", err)
	}
	if spans[0].Kind != domain.SpanKindTool {
		t.Errorf("kind: got %q, want %q (tool.* → tool)", spans[0].Kind, domain.SpanKindTool)
	}
}

func TestTranslate_Kind_InternalDefault(t *testing.T) {
	rss := []protobuf.FlatResourceSpans{
		makeFlat(nil,
			&protobuf.Span{
				Name: "internal-work",
				Attributes: []*protobuf.KeyValue{
					{Key: "my.custom.attr", Value: &protobuf.AnyValue{StringValue: strPtr("value")}},
				},
			},
		),
	}

	spans, err := otlp.Translate("proj-1", rss, otlp.Options{})
	if err != nil {
		t.Fatalf("Translate: %v", err)
	}
	if spans[0].Kind != domain.SpanKindInternal {
		t.Errorf("kind: got %q, want %q (no gen_ai or tool attrs → internal)", spans[0].Kind, domain.SpanKindInternal)
	}
}

func TestTranslate_ServiceNameOverride(t *testing.T) {
	rss := []protobuf.FlatResourceSpans{
		makeFlat([]*protobuf.KeyValue{
			{Key: "service.name", Value: &protobuf.AnyValue{StringValue: strPtr("from-resource")}},
		},
			&protobuf.Span{Name: "span"},
		),
	}

	spans, err := otlp.Translate("proj-1", rss, otlp.Options{
		ServiceNameOverride: "from-api-key",
	})
	if err != nil {
		t.Fatalf("Translate: %v", err)
	}
	if spans[0].ServiceName != "from-api-key" {
		t.Errorf("service_name: got %q, want %q (API key wins over resource)", spans[0].ServiceName, "from-api-key")
	}
}

func TestTranslate_UnmappedAttributes(t *testing.T) {
	rss := []protobuf.FlatResourceSpans{
		makeFlat(nil,
			&protobuf.Span{
				Name: "span",
				Attributes: []*protobuf.KeyValue{
					{Key: "my.custom.attr", Value: &protobuf.AnyValue{StringValue: strPtr("value")}},
					{Key: "another.attr", Value: &protobuf.AnyValue{IntValue: intPtr(42)}},
				},
			},
		),
	}

	spans, err := otlp.Translate("proj-1", rss, otlp.Options{})
	if err != nil {
		t.Fatalf("Translate: %v", err)
	}
	if len(spans[0].Attributes) != 2 {
		t.Fatalf("attributes: got %d, want 2", len(spans[0].Attributes))
	}
	if spans[0].Attributes["my.custom.attr"] != "value" {
		t.Errorf("my.custom.attr: got %v", spans[0].Attributes["my.custom.attr"])
	}
	if spans[0].Attributes["another.attr"] != int64(42) {
		t.Errorf("another.attr: got %v", spans[0].Attributes["another.attr"])
	}
}

func TestTranslate_ConsumedAttributesNotInOverflow(t *testing.T) {
	rss := []protobuf.FlatResourceSpans{
		makeFlat(nil,
			&protobuf.Span{
				Name: "span",
				Attributes: []*protobuf.KeyValue{
					{Key: "gen_ai.request.model", Value: &protobuf.AnyValue{StringValue: strPtr("gpt-4")}},
					{Key: "gen_ai.usage.input_tokens", Value: &protobuf.AnyValue{IntValue: intPtr(100)}},
					{Key: "prompt_tokens", Value: &protobuf.AnyValue{IntValue: intPtr(200)}},
					{Key: "completion_tokens", Value: &protobuf.AnyValue{IntValue: intPtr(50)}},
					{Key: "gen_ai.prompt.0.content", Value: &protobuf.AnyValue{StringValue: strPtr("hello")}},
					{Key: "gen_ai.completion.0.content", Value: &protobuf.AnyValue{StringValue: strPtr("hi")}},
					{Key: "lantern.prompt.name", Value: &protobuf.AnyValue{StringValue: strPtr("my-prompt")}},
					{Key: "lantern.prompt.version", Value: &protobuf.AnyValue{IntValue: intPtr(1)}},
					{Key: "lantern.kind", Value: &protobuf.AnyValue{StringValue: strPtr("llm")}},
				},
			},
		),
	}

	spans, err := otlp.Translate("proj-1", rss, otlp.Options{})
	if err != nil {
		t.Fatalf("Translate: %v", err)
	}
	if len(spans[0].Attributes) != 0 {
		t.Errorf("attributes: got %d, want 0 (all consumed attrs filtered)", len(spans[0].Attributes))
		for k := range spans[0].Attributes {
			t.Errorf("  unexpected attr: %q", k)
		}
	}
}

func TestTranslate_PromptVersion(t *testing.T) {
	rss := []protobuf.FlatResourceSpans{
		makeFlat(nil,
			&protobuf.Span{
				Name: "span",
				Attributes: []*protobuf.KeyValue{
					{Key: "lantern.prompt.name", Value: &protobuf.AnyValue{StringValue: strPtr("my-prompt")}},
					{Key: "lantern.prompt.version", Value: &protobuf.AnyValue{IntValue: intPtr(3)}},
				},
			},
		),
	}

	spans, err := otlp.Translate("proj-1", rss, otlp.Options{})
	if err != nil {
		t.Fatalf("Translate: %v", err)
	}
	if spans[0].PromptName != "my-prompt" {
		t.Errorf("prompt_name: got %q", spans[0].PromptName)
	}
	if spans[0].PromptVersion != 3 {
		t.Errorf("prompt_version: got %d, want 3", spans[0].PromptVersion)
	}
}

func TestTranslate_MultipleSpans(t *testing.T) {
	rss := []protobuf.FlatResourceSpans{
		makeFlat([]*protobuf.KeyValue{
			{Key: "service.name", Value: &protobuf.AnyValue{StringValue: strPtr("svc")}},
		},
			&protobuf.Span{
				Name: "span-1",
			},
			&protobuf.Span{
				Name: "span-2",
			},
		),
	}

	spans, err := otlp.Translate("proj-1", rss, otlp.Options{})
	if err != nil {
		t.Fatalf("Translate: %v", err)
	}
	if len(spans) != 2 {
		t.Fatalf("spans: got %d, want 2", len(spans))
	}
	if spans[0].Name != "span-1" {
		t.Errorf("span[0].name: got %q", spans[0].Name)
	}
	if spans[1].Name != "span-2" {
		t.Errorf("span[1].name: got %q", spans[1].Name)
	}
}

func TestTranslate_MultipleResourceSpans(t *testing.T) {
	rss := []protobuf.FlatResourceSpans{
		makeFlat([]*protobuf.KeyValue{
			{Key: "service.name", Value: &protobuf.AnyValue{StringValue: strPtr("svc-a")}},
		},
			&protobuf.Span{Name: "a-1"},
		),
		makeFlat([]*protobuf.KeyValue{
			{Key: "service.name", Value: &protobuf.AnyValue{StringValue: strPtr("svc-b")}},
		},
			&protobuf.Span{Name: "b-1"},
		),
	}

	spans, err := otlp.Translate("proj-1", rss, otlp.Options{})
	if err != nil {
		t.Fatalf("Translate: %v", err)
	}
	if len(spans) != 2 {
		t.Fatalf("spans: got %d, want 2", len(spans))
	}
	if spans[0].ServiceName != "svc-a" {
		t.Errorf("span[0].service_name: got %q", spans[0].ServiceName)
	}
	if spans[1].ServiceName != "svc-b" {
		t.Errorf("span[1].service_name: got %q", spans[1].ServiceName)
	}
}

func TestTranslate_EmptyInput(t *testing.T) {
	spans, err := otlp.Translate("proj-1", []protobuf.FlatResourceSpans{}, otlp.Options{})
	if err != nil {
		t.Fatalf("Translate: %v", err)
	}
	if len(spans) != 0 {
		t.Errorf("spans: got %d, want 0", len(spans))
	}
}

// -----------------------------------------------------------------------
// Wire format decoding tests
// -----------------------------------------------------------------------

func TestDecodeJSON_Basic(t *testing.T) {
	jsonData := []byte(`{
		"resource_spans": [{
			"resource": {
				"attributes": [
					{"key": "service.name", "value": {"string_value": "my-service"}}
				]
			},
			"scope_spans": [{
				"spans": [{
					"trace_id": "0102030405060708090a0b0c0d0e0f10",
					"span_id": "1112131415161718",
					"parent_span_id": "191a1b1c1d1e1f20",
					"name": "test-span",
					"start_time_unix_nano": 1000000000,
					"end_time_unix_nano": 2000000000,
					"attributes": [
						{"key": "gen_ai.request.model", "value": {"string_value": "gpt-4"}},
						{"key": "gen_ai.usage.input_tokens", "value": {"int_value": 100}},
						{"key": "gen_ai.usage.output_tokens", "value": {"int_value": 50}}
					]
				}]
			}]
		}]
	}`)

	flat, err := protobuf.DecodeJSON(jsonData)
	if err != nil {
		t.Fatalf("DecodeJSON: %v", err)
	}
	if len(flat) != 1 {
		t.Fatalf("flat: got %d, want 1", len(flat))
	}
	if len(flat[0].Spans) != 1 {
		t.Fatalf("spans: got %d, want 1", len(flat[0].Spans))
	}

	// Verify the span was decoded correctly.
	s := flat[0].Spans[0]
	if s.TraceId == nil {
		t.Fatal("trace_id is nil")
	}
	if s.Name != "test-span" {
		t.Errorf("name: got %q", s.Name)
	}

	// Verify full translation pipeline.
	spans, err := otlp.Translate("proj-1", flat, otlp.Options{})
	if err != nil {
		t.Fatalf("Translate: %v", err)
	}
	if len(spans) != 1 {
		t.Fatalf("translated spans: got %d, want 1", len(spans))
	}
	if spans[0].Model != "gpt-4" {
		t.Errorf("model: got %q, want %q", spans[0].Model, "gpt-4")
	}
	if spans[0].InputTokens != 100 {
		t.Errorf("input_tokens: got %d, want 100", spans[0].InputTokens)
	}
	if spans[0].OutputTokens != 50 {
		t.Errorf("output_tokens: got %d, want 50", spans[0].OutputTokens)
	}
}

func TestDecodeJSON_MultipleSpansSameResource(t *testing.T) {
	jsonData := []byte(`{
		"resource_spans": [{
			"resource": {
				"attributes": [
					{"key": "service.name", "value": {"string_value": "my-svc"}}
				]
			},
			"scope_spans": [{
				"spans": [
					{"trace_id": "0102030405060708090a0b0c0d0e0f10", "span_id": "1111111111111111", "name": "span-1"},
					{"trace_id": "0102030405060708090a0b0c0d0e0f10", "span_id": "2222222222222222", "name": "span-2"}
				]
			}]
		}]
	}`)

	flat, err := protobuf.DecodeJSON(jsonData)
	if err != nil {
		t.Fatalf("DecodeJSON: %v", err)
	}
	if len(flat) != 1 {
		t.Fatalf("flat groups: got %d, want 1", len(flat))
	}
	if len(flat[0].Spans) != 2 {
		t.Fatalf("spans in group: got %d, want 2", len(flat[0].Spans))
	}
}

func TestDecodeJSON_MultipleScopeSpans(t *testing.T) {
	jsonData := []byte(`{
		"resource_spans": [{
			"resource": {"attributes": []},
			"scope_spans": [
				{"spans": [{"trace_id": "0102030405060708090a0b0c0d0e0f10", "span_id": "1111111111111111", "name": "s1"}]},
				{"spans": [{"trace_id": "0102030405060708090a0b0c0d0e0f10", "span_id": "2222222222222222", "name": "s2"}]}
			]
		}]
	}`)

	flat, err := protobuf.DecodeJSON(jsonData)
	if err != nil {
		t.Fatalf("DecodeJSON: %v", err)
	}
	// All spans from the same resource are grouped together.
	if len(flat) != 1 {
		t.Fatalf("flat groups: got %d, want 1", len(flat))
	}
	if len(flat[0].Spans) != 2 {
		t.Fatalf("spans in group: got %d, want 2", len(flat[0].Spans))
	}
}

func TestDecodeJSON_MultipleResourceSpans(t *testing.T) {
	jsonData := []byte(`{
		"resource_spans": [
			{
				"resource": {"attributes": [{"key":"service.name","value":{"string_value":"svc-a"}}]},
				"scope_spans": [{"spans": [{"trace_id":"0102030405060708090a0b0c0d0e0f10","span_id":"1111111111111111","name":"a"}]}]
			},
			{
				"resource": {"attributes": [{"key":"service.name","value":{"string_value":"svc-b"}}]},
				"scope_spans": [{"spans": [{"trace_id":"0102030405060708090a0b0c0d0e0f10","span_id":"2222222222222222","name":"b"}]}]
			}
		]
	}`)

	flat, err := protobuf.DecodeJSON(jsonData)
	if err != nil {
		t.Fatalf("DecodeJSON: %v", err)
	}
	if len(flat) != 2 {
		t.Fatalf("flat groups: got %d, want 2", len(flat))
	}
}

func TestDecodeJSON_Empty(t *testing.T) {
	jsonData := []byte(`{"resource_spans": []}`)
	flat, err := protobuf.DecodeJSON(jsonData)
	if err != nil {
		t.Fatalf("DecodeJSON: %v", err)
	}
	if len(flat) != 0 {
		t.Errorf("flat: got %d, want 0", len(flat))
	}
}

func TestDecodeJSON_InvalidJSON(t *testing.T) {
	_, err := protobuf.DecodeJSON([]byte("not json"))
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

// -----------------------------------------------------------------------
// Response encoding
// -----------------------------------------------------------------------

func TestEncodeJSON_Response(t *testing.T) {
	resp := &protobuf.ExportTraceServiceResponse{}
	data, err := protobuf.EncodeJSON(resp)
	if err != nil {
		t.Fatalf("EncodeJSON: %v", err)
	}
	if string(data) != "{}" {
		t.Errorf("JSON: got %q, want %q", string(data), "{}")
	}
}

func TestEncodeProtobuf_Response(t *testing.T) {
	resp := &protobuf.ExportTraceServiceResponse{}
	data, err := protobuf.EncodeProtobuf(resp)
	if err != nil {
		t.Fatalf("EncodeProtobuf: %v", err)
	}
	if len(data) != 0 {
		t.Errorf("protobuf: got %d bytes, want 0", len(data))
	}
}

// -----------------------------------------------------------------------
// Content-Type helpers
// -----------------------------------------------------------------------

func TestIsProtobuf(t *testing.T) {
	tests := []struct {
		ct   string
		want bool
	}{
		{"application/x-protobuf", true},
		{"Application/X-Protobuf", false}, // case sensitive
		{"application/json", false},
		{"text/plain", false},
	}
	for _, tt := range tests {
		if got := protobuf.IsProtobuf(tt.ct); got != tt.want {
			t.Errorf("IsProtobuf(%q): got %v, want %v", tt.ct, got, tt.want)
		}
	}
}

func TestIsJSON(t *testing.T) {
	tests := []struct {
		ct   string
		want bool
	}{
		{"application/json", true},
		{"application/json; charset=utf-8", true},
		{"application/x-protobuf", false},
		{"text/plain", false},
	}
	for _, tt := range tests {
		if got := protobuf.IsJSON(tt.ct); got != tt.want {
			t.Errorf("IsJSON(%q): got %v, want %v", tt.ct, got, tt.want)
		}
	}
}

func TestContentType_Fallback(t *testing.T) {
	if got := protobuf.ContentType(""); got != "application/x-protobuf" {
		t.Errorf("ContentType(\"\"): got %q, want %q", got, "application/x-protobuf")
	}
	if got := protobuf.ContentType("application/json; charset=utf-8"); got != "application/json" {
		t.Errorf("ContentType with charset: got %q", got)
	}
}

// -----------------------------------------------------------------------
// Helpers
// -----------------------------------------------------------------------

func strPtr(s string) *string { return &s }
func intPtr(i int64) *int64   { return &i }
func jsonUnmarshal(data []byte, v any) error {
	return json.Unmarshal(data, v)
}
