package handler_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/zbloss/lantern/internal/domain"
	"github.com/zbloss/lantern/services/ingest/internal/handler"
)

// --- OTLP Tests ---

func TestOTLPHandler_JSON_200OnSuccess(t *testing.T) {
	q := &fakeIngestQueue{}
	v := &fakeValidator{}
	h := handler.NewOTLPHandler(q, v, nil)
	ts := httptest.NewServer(h.Router())
	defer ts.Close()

	jsonData := `{
		"resource_spans": [{
			"resource": {"attributes": [{"key": "service.name", "value": {"string_value": "my-svc"}}]},
			"scope_spans": [{
				"spans": [{
					"trace_id": "0102030405060708090a0b0c0d0e0f10",
					"span_id": "1111111111111111",
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
	}`

	req, _ := http.NewRequest("POST", ts.URL+"/v1/traces", bytes.NewReader([]byte(jsonData)))
	req.Header.Set("X-API-Key", "valid_project_key")
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status: got %d, want %d", resp.StatusCode, http.StatusOK)
	}
	if resp.Header.Get("Content-Type") != "application/json" {
		t.Errorf("Content-Type: got %q, want %q", resp.Header.Get("Content-Type"), "application/json")
	}
	if len(q.batches) != 1 {
		t.Fatalf("batches: got %d, want 1", len(q.batches))
	}
	if len(q.batches[0]) != 1 {
		t.Fatalf("spans: got %d, want 1", len(q.batches[0]))
	}

	// Verify translated span
	span := q.batches[0][0]
	if span.Model != "gpt-4" {
		t.Errorf("model: got %q, want %q", span.Model, "gpt-4")
	}
	if span.InputTokens != 100 {
		t.Errorf("input_tokens: got %d, want 100", span.InputTokens)
	}
	if span.OutputTokens != 50 {
		t.Errorf("output_tokens: got %d, want 50", span.OutputTokens)
	}
	if span.Kind != domain.SpanKindLLM {
		t.Errorf("kind: got %q, want %q", span.Kind, domain.SpanKindLLM)
	}
}

func TestOTLPHandler_JSON_KindDerivation_LLM(t *testing.T) {
	q := &fakeIngestQueue{}
	v := &fakeValidator{}
	h := handler.NewOTLPHandler(q, v, nil)
	ts := httptest.NewServer(h.Router())
	defer ts.Close()

	jsonData := `{
		"resource_spans": [{
			"scope_spans": [{
				"spans": [{
					"trace_id": "0102030405060708090a0b0c0d0e0f10",
					"span_id": "1111111111111111",
					"attributes": [
						{"key": "gen_ai.request.model", "value": {"string_value": "gpt-4"}}
					]
				}]
			}]
		}]
	}`

	req, _ := http.NewRequest("POST", ts.URL+"/v1/traces", bytes.NewReader([]byte(jsonData)))
	req.Header.Set("X-API-Key", "valid_project_key")
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: got %d, want %d", resp.StatusCode, http.StatusOK)
	}
	span := q.batches[0][0]
	if span.Kind != domain.SpanKindLLM {
		t.Errorf("kind: got %q, want %q (gen_ai.* → llm)", span.Kind, domain.SpanKindLLM)
	}
}

func TestOTLPHandler_JSON_KindDerivation_Tool(t *testing.T) {
	q := &fakeIngestQueue{}
	v := &fakeValidator{}
	h := handler.NewOTLPHandler(q, v, nil)
	ts := httptest.NewServer(h.Router())
	defer ts.Close()

	jsonData := `{
		"resource_spans": [{
			"scope_spans": [{
				"spans": [{
					"trace_id": "0102030405060708090a0b0c0d0e0f10",
					"span_id": "1111111111111111",
					"attributes": [
						{"key": "tool.name", "value": {"string_value": "search"}}
					]
				}]
			}]
		}]
	}`

	req, _ := http.NewRequest("POST", ts.URL+"/v1/traces", bytes.NewReader([]byte(jsonData)))
	req.Header.Set("X-API-Key", "valid_project_key")
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	span := q.batches[0][0]
	if span.Kind != domain.SpanKindTool {
		t.Errorf("kind: got %q, want %q (tool.* → tool)", span.Kind, domain.SpanKindTool)
	}
}

func TestOTLPHandler_JSON_KindDerivation_ExplicitLanternKind(t *testing.T) {
	q := &fakeIngestQueue{}
	v := &fakeValidator{}
	h := handler.NewOTLPHandler(q, v, nil)
	ts := httptest.NewServer(h.Router())
	defer ts.Close()

	jsonData := `{
		"resource_spans": [{
			"scope_spans": [{
				"spans": [{
					"trace_id": "0102030405060708090a0b0c0d0e0f10",
					"span_id": "1111111111111111",
					"attributes": [
						{"key": "lantern.kind", "value": {"string_value": "tool"}},
						{"key": "gen_ai.request.model", "value": {"string_value": "gpt-4"}}
					]
				}]
			}]
		}]
	}`

	req, _ := http.NewRequest("POST", ts.URL+"/v1/traces", bytes.NewReader([]byte(jsonData)))
	req.Header.Set("X-API-Key", "valid_project_key")
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	span := q.batches[0][0]
	if span.Kind != domain.SpanKindTool {
		t.Errorf("kind: got %q, want %q (lantern.kind wins)", span.Kind, domain.SpanKindTool)
	}
}

func TestOTLPHandler_JSON_PromptCompletion(t *testing.T) {
	q := &fakeIngestQueue{}
	v := &fakeValidator{}
	h := handler.NewOTLPHandler(q, v, nil)
	ts := httptest.NewServer(h.Router())
	defer ts.Close()

	jsonData := `{
		"resource_spans": [{
			"scope_spans": [{
				"spans": [{
					"trace_id": "0102030405060708090a0b0c0d0e0f10",
					"span_id": "1111111111111111",
					"attributes": [
						{"key": "gen_ai.prompt.0.role", "value": {"string_value": "user"}},
						{"key": "gen_ai.prompt.0.content", "value": {"string_value": "Hello"}},
						{"key": "gen_ai.completion.0.role", "value": {"string_value": "assistant"}},
						{"key": "gen_ai.completion.0.content", "value": {"string_value": "Hi!"}}
					]
				}]
			}]
		}]
	}`

	req, _ := http.NewRequest("POST", ts.URL+"/v1/traces", bytes.NewReader([]byte(jsonData)))
	req.Header.Set("X-API-Key", "valid_project_key")
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	span := q.batches[0][0]

	var inputMsgs []map[string]string
	if err := json.Unmarshal([]byte(span.Input), &inputMsgs); err != nil {
		t.Fatalf("input not valid JSON: %v", err)
	}
	if len(inputMsgs) != 1 {
		t.Fatalf("input messages: got %d, want 1", len(inputMsgs))
	}
	if inputMsgs[0]["role"] != "user" {
		t.Errorf("input[0].role: got %q", inputMsgs[0]["role"])
	}
	if inputMsgs[0]["content"] != "Hello" {
		t.Errorf("input[0].content: got %q", inputMsgs[0]["content"])
	}

	var outputMsgs []map[string]string
	if err := json.Unmarshal([]byte(span.Output), &outputMsgs); err != nil {
		t.Fatalf("output not valid JSON: %v", err)
	}
	if outputMsgs[0]["role"] != "assistant" {
		t.Errorf("output[0].role: got %q", outputMsgs[0]["role"])
	}
	if outputMsgs[0]["content"] != "Hi!" {
		t.Errorf("output[0].content: got %q", outputMsgs[0]["content"])
	}
}

func TestOTLPHandler_JSON_UnmappedAttributesInOverflow(t *testing.T) {
	q := &fakeIngestQueue{}
	v := &fakeValidator{}
	h := handler.NewOTLPHandler(q, v, nil)
	ts := httptest.NewServer(h.Router())
	defer ts.Close()

	jsonData := `{
		"resource_spans": [{
			"scope_spans": [{
				"spans": [{
					"trace_id": "0102030405060708090a0b0c0d0e0f10",
					"span_id": "1111111111111111",
					"attributes": [
						{"key": "gen_ai.request.model", "value": {"string_value": "gpt-4"}},
						{"key": "my.custom.attr", "value": {"string_value": "value"}},
						{"key": "another.attr", "value": {"int_value": 42}}
					]
				}]
			}]
		}]
	}`

	req, _ := http.NewRequest("POST", ts.URL+"/v1/traces", bytes.NewReader([]byte(jsonData)))
	req.Header.Set("X-API-Key", "valid_project_key")
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	span := q.batches[0][0]
	if span.Attributes["my.custom.attr"] != "value" {
		t.Errorf("my.custom.attr: got %v", span.Attributes["my.custom.attr"])
	}
	if span.Attributes["another.attr"] != int64(42) {
		t.Errorf("another.attr: got %v", span.Attributes["another.attr"])
	}
}

func TestOTLPHandler_JSON_ServiceNameOverride(t *testing.T) {
	q := &fakeIngestQueue{}
	v := &fakeValidator{}
	h := handler.NewOTLPHandler(q, v, nil)
	ts := httptest.NewServer(h.Router())
	defer ts.Close()

	jsonData := `{
		"resource_spans": [{
			"resource": {"attributes": [{"key": "service.name", "value": {"string_value": "from-resource"}}]},
			"scope_spans": [{
				"spans": [{
					"trace_id": "0102030405060708090a0b0c0d0e0f10",
					"span_id": "1111111111111111",
					"attributes": [
						{"key": "gen_ai.request.model", "value": {"string_value": "gpt-4"}}
					]
				}]
			}]
		}]
	}`

	req, _ := http.NewRequest("POST", ts.URL+"/v1/traces", bytes.NewReader([]byte(jsonData)))
	req.Header.Set("X-API-Key", "valid_service_key")
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	span := q.batches[0][0]
	if span.ServiceName != "my-service" {
		t.Errorf("service_name: got %q, want %q (API key wins over resource)", span.ServiceName, "my-service")
	}
}

func TestOTLPHandler_JSON_LegacyTokenNames(t *testing.T) {
	q := &fakeIngestQueue{}
	v := &fakeValidator{}
	h := handler.NewOTLPHandler(q, v, nil)
	ts := httptest.NewServer(h.Router())
	defer ts.Close()

	jsonData := `{
		"resource_spans": [{
			"scope_spans": [{
				"spans": [{
					"trace_id": "0102030405060708090a0b0c0d0e0f10",
					"span_id": "1111111111111111",
					"attributes": [
						{"key": "prompt_tokens", "value": {"int_value": 200}},
						{"key": "completion_tokens", "value": {"int_value": 100}}
					]
				}]
			}]
		}]
	}`

	req, _ := http.NewRequest("POST", ts.URL+"/v1/traces", bytes.NewReader([]byte(jsonData)))
	req.Header.Set("X-API-Key", "valid_project_key")
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	span := q.batches[0][0]
	if span.InputTokens != 200 {
		t.Errorf("input_tokens: got %d, want 200", span.InputTokens)
	}
	if span.OutputTokens != 100 {
		t.Errorf("output_tokens: got %d, want 100", span.OutputTokens)
	}
}

func TestOTLPHandler_JSON_PromptVersion(t *testing.T) {
	q := &fakeIngestQueue{}
	v := &fakeValidator{}
	h := handler.NewOTLPHandler(q, v, nil)
	ts := httptest.NewServer(h.Router())
	defer ts.Close()

	jsonData := `{
		"resource_spans": [{
			"scope_spans": [{
				"spans": [{
					"trace_id": "0102030405060708090a0b0c0d0e0f10",
					"span_id": "1111111111111111",
					"attributes": [
						{"key": "lantern.prompt.name", "value": {"string_value": "my-prompt"}},
						{"key": "lantern.prompt.version", "value": {"int_value": 3}}
					]
				}]
			}]
		}]
	}`

	req, _ := http.NewRequest("POST", ts.URL+"/v1/traces", bytes.NewReader([]byte(jsonData)))
	req.Header.Set("X-API-Key", "valid_project_key")
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	span := q.batches[0][0]
	if span.PromptName != "my-prompt" {
		t.Errorf("prompt_name: got %q", span.PromptName)
	}
	if span.PromptVersion != 3 {
		t.Errorf("prompt_version: got %d, want 3", span.PromptVersion)
	}
}

func TestOTLPHandler_JSON_MultipleSpans(t *testing.T) {
	q := &fakeIngestQueue{}
	v := &fakeValidator{}
	h := handler.NewOTLPHandler(q, v, nil)
	ts := httptest.NewServer(h.Router())
	defer ts.Close()

	jsonData := `{
		"resource_spans": [{
			"scope_spans": [{
				"spans": [
					{"trace_id": "0102030405060708090a0b0c0d0e0f10", "span_id": "1111111111111111", "name": "span-1"},
					{"trace_id": "0102030405060708090a0b0c0d0e0f10", "span_id": "2222222222222222", "name": "span-2"}
				]
			}]
		}]
	}`

	req, _ := http.NewRequest("POST", ts.URL+"/v1/traces", bytes.NewReader([]byte(jsonData)))
	req.Header.Set("X-API-Key", "valid_project_key")
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: got %d, want %d", resp.StatusCode, http.StatusOK)
	}
	if len(q.batches) != 1 {
		t.Fatalf("batches: got %d, want 1", len(q.batches))
	}
	if len(q.batches[0]) != 2 {
		t.Fatalf("spans: got %d, want 2", len(q.batches[0]))
	}
	if q.batches[0][0].Name != "span-1" {
		t.Errorf("span[0].name: got %q", q.batches[0][0].Name)
	}
	if q.batches[0][1].Name != "span-2" {
		t.Errorf("span[1].name: got %q", q.batches[0][1].Name)
	}
}

func TestOTLPHandler_JSON_401OnMissingKey(t *testing.T) {
	q := &fakeIngestQueue{}
	v := &fakeValidator{}
	h := handler.NewOTLPHandler(q, v, nil)
	ts := httptest.NewServer(h.Router())
	defer ts.Close()

	req, _ := http.NewRequest("POST", ts.URL+"/v1/traces", bytes.NewReader([]byte(`{"resource_spans": []}`)))
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status: got %d, want %d", resp.StatusCode, http.StatusUnauthorized)
	}
}

func TestOTLPHandler_JSON_401OnInvalidKey(t *testing.T) {
	q := &fakeIngestQueue{}
	v := &fakeValidator{}
	h := handler.NewOTLPHandler(q, v, nil)
	ts := httptest.NewServer(h.Router())
	defer ts.Close()

	req, _ := http.NewRequest("POST", ts.URL+"/v1/traces", bytes.NewReader([]byte(`{"resource_spans": []}`)))
	req.Header.Set("X-API-Key", "invalid_key")
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status: got %d, want %d", resp.StatusCode, http.StatusUnauthorized)
	}
}

func TestOTLPHandler_JSON_503WhenQueueFails(t *testing.T) {
	q := &failingQueue{}
	v := &fakeValidator{}
	h := handler.NewOTLPHandler(q, v, nil)
	ts := httptest.NewServer(h.Router())
	defer ts.Close()

	req, _ := http.NewRequest("POST", ts.URL+"/v1/traces", bytes.NewReader([]byte(`{
		"resource_spans": [{
			"scope_spans": [{"spans": [{"trace_id":"0102030405060708090a0b0c0d0e0f10","span_id":"1111111111111111","name":"test"}]}]
		}]
	}`)))
	req.Header.Set("X-API-Key", "valid_project_key")
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("status: got %d, want %d", resp.StatusCode, http.StatusServiceUnavailable)
	}
}

func TestOTLPHandler_JSON_400OnInvalidJSON(t *testing.T) {
	q := &fakeIngestQueue{}
	v := &fakeValidator{}
	h := handler.NewOTLPHandler(q, v, nil)
	ts := httptest.NewServer(h.Router())
	defer ts.Close()

	req, _ := http.NewRequest("POST", ts.URL+"/v1/traces", bytes.NewReader([]byte("not json")))
	req.Header.Set("X-API-Key", "valid_project_key")
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status: got %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}
}

func TestOTLPHandler_JSON_MethodNotAllowed(t *testing.T) {
	q := &fakeIngestQueue{}
	v := &fakeValidator{}
	h := handler.NewOTLPHandler(q, v, nil)
	ts := httptest.NewServer(h.Router())
	defer ts.Close()

	req, _ := http.NewRequest("GET", ts.URL+"/v1/traces", nil)
	req.Header.Set("X-API-Key", "valid_project_key")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("status: got %d, want %d", resp.StatusCode, http.StatusMethodNotAllowed)
	}
}

func TestOTLPHandler_JSON_ProjectIDAttached(t *testing.T) {
	q := &fakeIngestQueue{}
	v := &fakeValidator{}
	h := handler.NewOTLPHandler(q, v, nil)
	ts := httptest.NewServer(h.Router())
	defer ts.Close()

	req, _ := http.NewRequest("POST", ts.URL+"/v1/traces", bytes.NewReader([]byte(`{
		"resource_spans": [{
			"scope_spans": [{"spans": [{"trace_id":"0102030405060708090a0b0c0d0e0f10","span_id":"1111111111111111"}]}]
		}]
	}`)))
	req.Header.Set("X-API-Key", "valid_project_key")
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	span := q.batches[0][0]
	if span.ProjectID != "proj-1" {
		t.Errorf("project_id: got %q, want %q", span.ProjectID, "proj-1")
	}
}

func TestOTLPHandler_JSON_EmptySpans(t *testing.T) {
	q := &fakeIngestQueue{}
	v := &fakeValidator{}
	h := handler.NewOTLPHandler(q, v, nil)
	ts := httptest.NewServer(h.Router())
	defer ts.Close()

	req, _ := http.NewRequest("POST", ts.URL+"/v1/traces", bytes.NewReader([]byte(`{"resource_spans": []}`)))
	req.Header.Set("X-API-Key", "valid_project_key")
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status: got %d, want %d", resp.StatusCode, http.StatusOK)
	}
}

// --- CombinedRouter Tests ---

func TestCombinedRouter_NativeEndpoint(t *testing.T) {
	q := &fakeIngestQueue{}
	v := &fakeValidator{}
	h := handler.CombinedRouter(q, v, nil)
	ts := httptest.NewServer(h)
	defer ts.Close()

	spans := []*handler.NativeSpan{
		{
			TraceID: "0123456789abcdef0123456789abcdef",
			SpanID:  "0123456789abcdef",
			Name:    "test-span",
		},
	}
	payload, _ := json.Marshal(map[string][]*handler.NativeSpan{"spans": spans})

	req, _ := http.NewRequest("POST", ts.URL+"/api/v1/spans", bytes.NewReader(payload))
	req.Header.Set("X-API-Key", "valid_project_key")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted {
		t.Errorf("status: got %d, want %d", resp.StatusCode, http.StatusAccepted)
	}
}

func TestCombinedRouter_OTLPEndpoint(t *testing.T) {
	q := &fakeIngestQueue{}
	v := &fakeValidator{}
	h := handler.CombinedRouter(q, v, nil)
	ts := httptest.NewServer(h)
	defer ts.Close()

	jsonData := `{
		"resource_spans": [{
			"scope_spans": [{
				"spans": [{"trace_id":"0102030405060708090a0b0c0d0e0f10","span_id":"1111111111111111","name":"otlp-span"}]
			}]
		}]
	}`

	req, _ := http.NewRequest("POST", ts.URL+"/v1/traces", bytes.NewReader([]byte(jsonData)))
	req.Header.Set("X-API-Key", "valid_project_key")
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status: got %d, want %d", resp.StatusCode, http.StatusOK)
	}
	if len(q.batches) != 1 {
		t.Fatalf("batches: got %d, want 1", len(q.batches))
	}
	if q.batches[0][0].Name != "otlp-span" {
		t.Errorf("span name: got %q, want %q", q.batches[0][0].Name, "otlp-span")
	}
}

func TestCombinedRouter_CORS_Preflight(t *testing.T) {
	q := &fakeIngestQueue{}
	v := &fakeValidator{}
	h := handler.CombinedRouter(q, v, []string{"*"})
	ts := httptest.NewServer(h)
	defer ts.Close()

	req, _ := http.NewRequest("OPTIONS", ts.URL+"/api/v1/spans", nil)
	req.Header.Set("Origin", "http://example.com")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("status: got %d, want %d", resp.StatusCode, http.StatusNoContent)
	}
}

func TestCombinedRouter_CORS_OTLPPreflight(t *testing.T) {
	q := &fakeIngestQueue{}
	v := &fakeValidator{}
	h := handler.CombinedRouter(q, v, []string{"*"})
	ts := httptest.NewServer(h)
	defer ts.Close()

	req, _ := http.NewRequest("OPTIONS", ts.URL+"/v1/traces", nil)
	req.Header.Set("Origin", "http://example.com")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("status: got %d, want %d", resp.StatusCode, http.StatusNoContent)
	}
}

// -----------------------------------------------------------------------
// Helpers - fakeIngestQueue, fakeValidator, failingQueue are shared
// with handler_test.go in the same test package.
// -----------------------------------------------------------------------
