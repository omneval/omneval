package handlers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	coltracev1 "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	commonv1 "go.opentelemetry.io/proto/otlp/common/v1"
	tracev1 "go.opentelemetry.io/proto/otlp/trace/v1"

	"github.com/omneval/omneval/internal/auth"
	"github.com/omneval/omneval/internal/handlers"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// TestOTLPMessageContentRoundTrip asserts that LLM message content placed in
// Span Events (the wire shape that Laminar/OpenAI instrumentors emit) survives
// the full ingest → normalize → enqueue pipeline and is returned intact in the
// normalised domain span.
func TestOTLPMessageContentRoundTrip(t *testing.T) {
	q := &fakeIngestQueue{}
	v := &fakeValidator{}
	h := handlers.NewOTLPHandler(q, v)

	// Build an OTLP request with prompt/completion content in Span Events,
	// exactly the shape that causes the bug (empty input/output when only
	// attributes are read, never events).
	req := &coltracev1.ExportTraceServiceRequest{
		ResourceSpans: []*tracev1.ResourceSpans{
			{
				ScopeSpans: []*tracev1.ScopeSpans{
					{
						Spans: []*tracev1.Span{
							{
								TraceId:           []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10},
								SpanId:            []byte{0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18},
								Name:              "litellm.completion",
								StartTimeUnixNano: uint64(time.Now().UnixNano()),
								EndTimeUnixNano:   uint64(time.Now().UnixNano()),
								Attributes: []*commonv1.KeyValue{
									{Key: "gen_ai.response.id", Value: &commonv1.AnyValue{Value: &commonv1.AnyValue_StringValue{StringValue: "chatcmpl-abc123"}}},
									{Key: "gen_ai.request.model", Value: &commonv1.AnyValue{Value: &commonv1.AnyValue_StringValue{StringValue: "gpt-4o"}}},
									{Key: "llm.usage.total_tokens", Value: &commonv1.AnyValue{Value: &commonv1.AnyValue_IntValue{IntValue: 14865}}},
								},
								// Content is ONLY in Span Events.
								Events: []*tracev1.Span_Event{
									{
										Name:         "gen_ai.prompt.message",
										TimeUnixNano: uint64(time.Now().UnixNano()),
										Attributes: []*commonv1.KeyValue{
											{Key: "gen_ai.prompt.message.role", Value: &commonv1.AnyValue{Value: &commonv1.AnyValue_StringValue{StringValue: "user"}}},
											{Key: "gen_ai.prompt.message.content", Value: &commonv1.AnyValue{Value: &commonv1.AnyValue_StringValue{StringValue: "Hello, world!"}}},
										},
									},
									{
										Name:         "gen_ai.completion.message",
										TimeUnixNano: uint64(time.Now().UnixNano()),
										Attributes: []*commonv1.KeyValue{
											{Key: "gen_ai.completion.message.role", Value: &commonv1.AnyValue{Value: &commonv1.AnyValue_StringValue{StringValue: "assistant"}}},
											{Key: "gen_ai.completion.message.content", Value: &commonv1.AnyValue{Value: &commonv1.AnyValue_StringValue{StringValue: "Hi there! How can I help?"}}},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	body, err := proto.Marshal(req)
	if err != nil {
		t.Fatalf("marshal OTLP request: %v", err)
	}

	ts := httptest.NewServer(h.Router())
	defer ts.Close()

	rq, _ := http.NewRequest("POST", ts.URL+"/v1/traces", bytes.NewReader(body))
	rq.Header.Set("X-API-Key", "valid_project_key")
	rq.Header.Set("Content-Type", "application/x-protobuf")

	resp, err := http.DefaultClient.Do(rq)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("status: got %d, want %d", resp.StatusCode, http.StatusAccepted)
	}

	// Verify the normalised span contains the prompt and completion messages.
	if len(q.batches) != 1 || len(q.batches[0]) != 1 {
		t.Fatalf("expected 1 batch with 1 span, got %d batches", len(q.batches))
	}
	s := q.batches[0][0]

	if s.Input == "" {
		t.Error("input: expected non-empty (prompt message from Span Events), got empty")
	}
	if s.Output == "" {
		t.Error("output: expected non-empty (completion message from Span Events), got empty")
	}
	if !strings.Contains(s.Input, `"role":"user"`) || !strings.Contains(s.Input, `"content":"Hello, world!"`) {
		t.Errorf("input: got %q, expected user message with content 'Hello, world!'", s.Input)
	}
	if !strings.Contains(s.Output, `"role":"assistant"`) || !strings.Contains(s.Output, `"content":"Hi there! How can I help?"`) {
		t.Errorf("output: got %q, expected assistant message with content 'Hi there! How can I help?'", s.Output)
	}
}

// TestNativeMessageContentRoundTrip asserts that LLM message content provided
// in the native REST ingest format (input/output JSON arrays) survives the full
// ingest → normalize → enqueue pipeline.
func TestNativeMessageContentRoundTrip(t *testing.T) {
	q := &fakeIngestQueue{}
	v := &fakeValidator{}
	h := handlers.NewNativeHandler(q, v, nil)

	body := map[string]any{
		"spans": []map[string]any{
			{
				"span_id":  "0102030405060708",
				"trace_id": "0102030405060708090a0b0c0d0e0f10",
				"name":     "agent.step",
				"kind":     "agent",
				"model":    "gpt-4o",
				"input": []map[string]any{
					{"role": "user", "content": "Hello, world!"},
				},
				"output": []map[string]any{
					{"role": "assistant", "content": "Hi there! How can I help?"},
				},
				"attributes": map[string]any{
					"gen_ai.response.id":  "chatcmpl-abc123",
					"llm.usage.total_tokens": json.Number("14865"),
				},
			},
		},
	}
	jsonBody, _ := json.Marshal(body)

	ts := httptest.NewServer(h.Router())
	defer ts.Close()

	rq, _ := http.NewRequest("POST", ts.URL+"/api/v1/spans", bytes.NewReader(jsonBody))
	rq.Header.Set("X-API-Key", "valid_project_key")
	rq.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(rq)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("status: got %d, want %d", resp.StatusCode, http.StatusAccepted)
	}

	if len(q.batches) != 1 || len(q.batches[0]) != 1 {
		t.Fatalf("expected 1 batch with 1 span, got %d batches", len(q.batches))
	}
	s := q.batches[0][0]

	if s.Input == "" {
		t.Error("input: expected non-empty, got empty")
	}
	if s.Output == "" {
		t.Error("output: expected non-empty, got empty")
	}
	if !strings.Contains(s.Input, `"role":"user"`) || !strings.Contains(s.Input, `"content":"Hello, world!"`) {
		t.Errorf("input: got %q, expected user message with content 'Hello, world!'", s.Input)
	}
	if !strings.Contains(s.Output, `"role":"assistant"`) || !strings.Contains(s.Output, `"content":"Hi there! How can I help?"`) {
		t.Errorf("output: got %q, expected assistant message with content 'Hi there! How can I help?'", s.Output)
	}
}

// TestOTLPMessageContentJSONEncoding tests that when the OTLP handler receives
// a JSON-encoded request (as opposed to protobuf), message content from Span
// Events still round-trips correctly.
func TestOTLPMessageContentJSONEncoding(t *testing.T) {
	q := &fakeIngestQueue{}
	v := &fakeValidator{}
	h := handlers.NewOTLPHandler(q, v)

	req := &coltracev1.ExportTraceServiceRequest{
		ResourceSpans: []*tracev1.ResourceSpans{
			{
				ScopeSpans: []*tracev1.ScopeSpans{
					{
						Spans: []*tracev1.Span{
							{
								TraceId:           []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10},
								SpanId:            []byte{0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18},
								Name:              "litellm.completion",
								StartTimeUnixNano: uint64(time.Now().UnixNano()),
								EndTimeUnixNano:   uint64(time.Now().UnixNano()),
								Events: []*tracev1.Span_Event{
									{
										Name:         "gen_ai.prompt.message",
										TimeUnixNano: uint64(time.Now().UnixNano()),
										Attributes: []*commonv1.KeyValue{
											{Key: "gen_ai.prompt.message.role", Value: &commonv1.AnyValue{Value: &commonv1.AnyValue_StringValue{StringValue: "user"}}},
											{Key: "gen_ai.prompt.message.content", Value: &commonv1.AnyValue{Value: &commonv1.AnyValue_StringValue{StringValue: "JSON-encoded prompt"}}},
										},
									},
									{
										Name:         "gen_ai.completion.message",
										TimeUnixNano: uint64(time.Now().UnixNano()),
										Attributes: []*commonv1.KeyValue{
											{Key: "gen_ai.completion.message.role", Value: &commonv1.AnyValue{Value: &commonv1.AnyValue_StringValue{StringValue: "assistant"}}},
											{Key: "gen_ai.completion.message.content", Value: &commonv1.AnyValue{Value: &commonv1.AnyValue_StringValue{StringValue: "JSON-encoded completion"}}},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	body, err := protojson.Marshal(req)
	if err != nil {
		t.Fatalf("marshal OTLP request: %v", err)
	}

	ts := httptest.NewServer(h.Router())
	defer ts.Close()

	rq, _ := http.NewRequest("POST", ts.URL+"/v1/traces", bytes.NewReader(body))
	rq.Header.Set("X-API-Key", "valid_project_key")
	rq.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(rq)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("status: got %d, want %d", resp.StatusCode, http.StatusAccepted)
	}

	if len(q.batches) != 1 || len(q.batches[0]) != 1 {
		t.Fatalf("expected 1 batch with 1 span, got %d batches", len(q.batches))
	}
	s := q.batches[0][0]

	if s.Input == "" {
		t.Error("input: expected non-empty from JSON-encoded OTLP request")
	}
	if s.Output == "" {
		t.Error("output: expected non-empty from JSON-encoded OTLP request")
	}
	if !strings.Contains(s.Input, "JSON-encoded prompt") {
		t.Errorf("input: got %q, expected 'JSON-encoded prompt'", s.Input)
	}
	if !strings.Contains(s.Output, "JSON-encoded completion") {
		t.Errorf("output: got %q, expected 'JSON-encoded completion'", s.Output)
	}
}

// fakeValidatorJSON is a minimal Validator for JSON-encoded OTLP tests.
type fakeValidatorJSON struct{}

func (f *fakeValidatorJSON) Validate(_ context.Context, rawKey string) (*auth.ValidatedKey, error) {
	if rawKey == "valid_project_key" {
		return &auth.ValidatedKey{ProjectID: "proj-1"}, nil
	}
	return nil, fmt.Errorf("invalid API key")
}

// TestNativeMultipleMessagesIngestRoundTrip verifies that multiple input and
// output messages are preserved through the native ingest pipeline.
func TestNativeMultipleMessagesIngestRoundTrip(t *testing.T) {
	q := &fakeIngestQueue{}
	v := &fakeValidator{}
	h := handlers.NewNativeHandler(q, v, nil)

	body := map[string]any{
		"spans": []map[string]any{
			{
				"span_id":  "0102030405060709",
				"trace_id": "0102030405060708090a0b0c0d0e0f10",
				"name":     "agent.step",
				"kind":     "agent",
				"model":    "gpt-4o",
				"input": []map[string]any{
					{"role": "system", "content": "You are a helpful assistant."},
					{"role": "user", "content": "Tell me about Go."},
				},
				"output": []map[string]any{
					{"role": "assistant", "content": "Go is a statically typed, compiled language."},
				},
			},
		},
	}
	jsonBody, _ := json.Marshal(body)

	ts := httptest.NewServer(h.Router())
	defer ts.Close()

	rq, _ := http.NewRequest("POST", ts.URL+"/api/v1/spans", bytes.NewReader(jsonBody))
	rq.Header.Set("X-API-Key", "valid_project_key")
	rq.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(rq)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("status: got %d, want %d", resp.StatusCode, http.StatusAccepted)
	}

	if len(q.batches) != 1 || len(q.batches[0]) != 1 {
		t.Fatalf("expected 1 batch with 1 span, got %d batches", len(q.batches))
	}
	s := q.batches[0][0]

	if s.Input == "" {
		t.Error("input: expected non-empty, got empty")
	}
	if s.Output == "" {
		t.Error("output: expected non-empty, got empty")
	}
	// Check that system message is present
	if !strings.Contains(s.Input, `"role":"system"`) || !strings.Contains(s.Input, `"content":"You are a helpful assistant."`) {
		t.Errorf("input: got %q, expected system message with content 'You are a helpful assistant.'", s.Input)
	}
	// Check that user message is present
	if !strings.Contains(s.Input, `"role":"user"`) || !strings.Contains(s.Input, `"content":"Tell me about Go."`) {
		t.Errorf("input: got %q, expected user message with content 'Tell me about Go.'", s.Input)
	}
	// Check that assistant response is present
	if !strings.Contains(s.Output, `"role":"assistant"`) || !strings.Contains(s.Output, `"content":"Go is a statically typed, compiled language."`) {
		t.Errorf("output: got %q, expected assistant response", s.Output)
	}
}