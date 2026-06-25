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

	"github.com/omneval/omneval/internal/auth"
	"github.com/omneval/omneval/internal/handlers"
)

// --- Native Handler Tests ---

func TestNativeHandler_AcceptsXAPIKey(t *testing.T) {
	q := &fakeIngestQueue{}
	v := &fakeValidator{}
	h := handlers.NewNativeHandler(q, v, nil)
	ts := httptest.NewServer(h.Router())
	defer ts.Close()

	body := map[string]any{
		"spans": []map[string]any{
			{
				"span_id":  "0102030405060708",
				"trace_id": "0102030405060708090a0b0c0d0e0f10",
				"name":     "test-span",
			},
		},
	}
	jsonBody, _ := json.Marshal(body)

	rq, _ := http.NewRequest("POST", ts.URL+"/api/v1/spans", bytes.NewReader(jsonBody))
	rq.Header.Set("X-API-Key", "valid_project_key")
	rq.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(rq)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted {
		t.Errorf("status: got %d, want %d", resp.StatusCode, http.StatusAccepted)
	}
	if len(q.batches) != 1 || len(q.batches[0]) != 1 {
		t.Errorf("expected 1 batch with 1 span, got %d batches", len(q.batches))
	}
}

func TestNativeHandler_AcceptsBearerAuth(t *testing.T) {
	q := &fakeIngestQueue{}
	v := &fakeValidator{}
	h := handlers.NewNativeHandler(q, v, nil)
	ts := httptest.NewServer(h.Router())
	defer ts.Close()

	body := map[string]any{
		"spans": []map[string]any{
			{
				"span_id":  "0102030405060708",
				"trace_id": "0102030405060708090a0b0c0d0e0f10",
				"name":     "test-span",
			},
		},
	}
	jsonBody, _ := json.Marshal(body)

	rq, _ := http.NewRequest("POST", ts.URL+"/api/v1/spans", bytes.NewReader(jsonBody))
	rq.Header.Set("Authorization", "Bearer valid_project_key")
	rq.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(rq)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted {
		t.Errorf("status: got %d, want %d", resp.StatusCode, http.StatusAccepted)
	}
	if len(q.batches) != 1 || len(q.batches[0]) != 1 {
		t.Errorf("expected 1 batch with 1 span, got %d batches", len(q.batches))
	}
}

func TestNativeHandler_XAPIKeyPrecedence(t *testing.T) {
	q := &fakeIngestQueue{}
	trackedKey := ""
	v := &trackingValidator{
		validate: func(_ context.Context, rawKey string) (*auth.ValidatedKey, error) {
			trackedKey = rawKey
			if rawKey == "valid_project_key" {
				return &auth.ValidatedKey{ProjectID: "proj-1"}, nil
			}
			return nil, fmt.Errorf("invalid API key")
		},
	}
	h := handlers.NewNativeHandler(q, v, nil)
	ts := httptest.NewServer(h.Router())
	defer ts.Close()

	body := map[string]any{
		"spans": []map[string]any{
			{
				"span_id":  "0102030405060708",
				"trace_id": "0102030405060708090a0b0c0d0e0f10",
				"name":     "test-span",
			},
		},
	}
	jsonBody, _ := json.Marshal(body)

	rq, _ := http.NewRequest("POST", ts.URL+"/api/v1/spans", bytes.NewReader(jsonBody))
	rq.Header.Set("X-API-Key", "valid_project_key")
	rq.Header.Set("Authorization", "Bearer some_other_key")
	rq.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(rq)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("status: got %d, want %d", resp.StatusCode, http.StatusAccepted)
	}
	if trackedKey != "valid_project_key" {
		t.Errorf("X-API-Key should take precedence: got key %q", trackedKey)
	}
}

func TestNativeHandler_RejectsMalformedAuthorization(t *testing.T) {
	q := &fakeIngestQueue{}
	v := &fakeValidator{}
	h := handlers.NewNativeHandler(q, v, nil)
	ts := httptest.NewServer(h.Router())
	defer ts.Close()

	body := map[string]any{
		"spans": []map[string]any{},
	}
	jsonBody, _ := json.Marshal(body)

	rq, _ := http.NewRequest("POST", ts.URL+"/api/v1/spans", bytes.NewReader(jsonBody))
	rq.Header.Set("Authorization", "Basic YWJj")
	rq.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(rq)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status: got %d, want %d", resp.StatusCode, http.StatusUnauthorized)
	}
}

func TestNativeHandler_401OnMissingKey(t *testing.T) {
	q := &fakeIngestQueue{}
	v := &fakeValidator{}
	h := handlers.NewNativeHandler(q, v, nil)
	ts := httptest.NewServer(h.Router())
	defer ts.Close()

	body := map[string]any{
		"spans": []map[string]any{},
	}
	jsonBody, _ := json.Marshal(body)

	rq, _ := http.NewRequest("POST", ts.URL+"/api/v1/spans", bytes.NewReader(jsonBody))
	rq.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(rq)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status: got %d, want %d", resp.StatusCode, http.StatusUnauthorized)
	}
}

func TestNativeHandler_RejectsInvalidKey(t *testing.T) {
	q := &fakeIngestQueue{}
	v := &fakeValidator{}
	h := handlers.NewNativeHandler(q, v, nil)
	ts := httptest.NewServer(h.Router())
	defer ts.Close()

	body := map[string]any{
		"spans": []map[string]any{},
	}
	jsonBody, _ := json.Marshal(body)

	rq, _ := http.NewRequest("POST", ts.URL+"/api/v1/spans", bytes.NewReader(jsonBody))
	rq.Header.Set("X-API-Key", "invalid_key")
	rq.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(rq)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status: got %d, want %d", resp.StatusCode, http.StatusUnauthorized)
	}
}

func TestNativeHandler_AcceptsConversationID(t *testing.T) {
	q := &fakeIngestQueue{}
	v := &fakeValidator{}
	h := handlers.NewNativeHandler(q, v, nil)
	ts := httptest.NewServer(h.Router())
	defer ts.Close()

	body := map[string]any{
		"spans": []map[string]any{
			{
				"span_id":         "0102030405060708",
				"trace_id":        "0102030405060708090a0b0c0d0e0f10",
				"conversation_id": "conv-abc-123",
				"name":            "test-span",
			},
		},
	}
	jsonBody, _ := json.Marshal(body)

	rq, _ := http.NewRequest("POST", ts.URL+"/api/v1/spans", bytes.NewReader(jsonBody))
	rq.Header.Set("X-API-Key", "valid_project_key")
	rq.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(rq)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted {
		t.Errorf("status: got %d, want %d", resp.StatusCode, http.StatusAccepted)
	}
	if len(q.batches) != 1 || len(q.batches[0]) != 1 {
		t.Fatalf("expected 1 batch with 1 span, got %d batches", len(q.batches))
	}
	if q.batches[0][0].ConversationID != "conv-abc-123" {
		t.Errorf("conversation_id: got %q, want %q", q.batches[0][0].ConversationID, "conv-abc-123")
	}
}

func TestNativeHandler_ValidatesSpanFields(t *testing.T) {
	q := &fakeIngestQueue{}
	v := &fakeValidator{}
	h := handlers.NewNativeHandler(q, v, nil)
	ts := httptest.NewServer(h.Router())
	defer ts.Close()

	body := map[string]any{
		"spans": []map[string]any{
			{
				"span_id":  "short",
				"trace_id": "0102030405060708090a0b0c0d0e0f10",
				"name":     "test-span",
			},
		},
	}
	jsonBody, _ := json.Marshal(body)

	rq, _ := http.NewRequest("POST", ts.URL+"/api/v1/spans", bytes.NewReader(jsonBody))
	rq.Header.Set("X-API-Key", "valid_project_key")
	rq.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(rq)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status: got %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}
}

// TestIngestAdapter_Translate_proves that the unified IngestAdapter interface
// Translate method accepts a *http.Request, extracts and validates the API
// key, parses the body, and returns normalised domain.Spans.
func TestIngestAdapter_Translate(t *testing.T) {
	q := &fakeIngestQueue{}
	v := &fakeValidator{}
	h := handlers.NewNativeHandler(q, v, nil)

	body := map[string]any{
		"spans": []map[string]any{
			{
				"span_id":  "0102030405060708",
				"trace_id": "0102030405060708090a0b0c0d0e0f10",
				"name":     "my-span",
				"model":    "gpt-4",
				"input":    "hello",
				"output":   "hi back",
			},
		},
	}
	jsonBody, _ := json.Marshal(body)

	req, _ := http.NewRequest("POST", "/api/v1/spans", bytes.NewReader(jsonBody))
	req.Header.Set("X-API-Key", "valid_project_key")

	spans, err := h.Translate(req.Context(), req)
	if err != nil {
		t.Fatalf("Translate failed: %v", err)
	}

	if len(spans) != 1 {
		t.Fatalf("spans: got %d, want 1", len(spans))
	}
	if spans[0].Name != "my-span" {
		t.Errorf("span name: got %q, want %q", spans[0].Name, "my-span")
	}
	if spans[0].ProjectID != "proj-1" {
		t.Errorf("project_id: got %q, want %q", spans[0].ProjectID, "proj-1")
	}
}

// TestIngestAdapter_Translate_HTTPRequest proves Translate accepts a full
// *http.Request, extracts and validates the API key, parses the body, and
// returns a batch of normalised domain.Spans.
func TestIngestAdapter_Translate_HTTPRequest(t *testing.T) {
	q := &fakeIngestQueue{}
	v := &fakeValidator{}
	h := handlers.NewNativeHandler(q, v, nil)

	body := map[string]any{
		"spans": []map[string]any{
			{
				"span_id":  "0102030405060708",
				"trace_id": "0102030405060708090a0b0c0d0e0f10",
				"name":     "translate-span",
				"model":    "gpt-4",
			},
		},
	}
	jsonBody, _ := json.Marshal(body)

	req, err := http.NewRequest("POST", "/api/v1/spans", bytes.NewReader(jsonBody))
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	req.Header.Set("X-API-Key", "valid_project_key")
	req.Header.Set("Content-Type", "application/json")

	spans, err := h.Translate(req.Context(), req)
	if err != nil {
		t.Fatalf("Translate failed: %v", err)
	}

	if len(spans) != 1 {
		t.Fatalf("spans returned: got %d, want 1", len(spans))
	}
	if spans[0].Name != "translate-span" {
		t.Errorf("span name: got %q, want %q", spans[0].Name, "translate-span")
	}
	if spans[0].ProjectID != "proj-1" {
		t.Errorf("project_id: got %q, want %q", spans[0].ProjectID, "proj-1")
	}
}

// TestIngestAdapter_Translate_HTTPRequest_AuthError proves Translate returns
// an error when the API key is missing or invalid.
func TestIngestAdapter_Translate_HTTPRequest_AuthError(t *testing.T) {
	q := &fakeIngestQueue{}
	v := &fakeValidator{}
	h := handlers.NewNativeHandler(q, v, nil)

	body := map[string]any{
		"spans": []map[string]any{
			{
				"span_id":  "0102030405060708",
				"trace_id": "0102030405060708090a0b0c0d0e0f10",
				"name":     "auth-span",
			},
		},
	}
	jsonBody, _ := json.Marshal(body)

	// Missing key
	req, _ := http.NewRequest("POST", "/api/v1/spans", bytes.NewReader(jsonBody))
	_, err := h.Translate(req.Context(), req)
	if err == nil {
		t.Fatal("expected error for missing API key, got nil")
	}

	// Invalid key
	req2, _ := http.NewRequest("POST", "/api/v1/spans", bytes.NewReader(jsonBody))
	req2.Header.Set("X-API-Key", "bad_key")
	_, err = h.Translate(req2.Context(), req2)
	if err == nil {
		t.Fatal("expected error for invalid API key, got nil")
	}
}

// TestIngestAdapter_Translate_HTTPRequest_EmptyBatch proves Translate returns
// an error when the spans array is empty.
func TestIngestAdapter_Translate_HTTPRequest_EmptyBatch(t *testing.T) {
	q := &fakeIngestQueue{}
	v := &fakeValidator{}
	h := handlers.NewNativeHandler(q, v, nil)

	body := map[string]any{
		"spans": []map[string]any{},
	}
	jsonBody, _ := json.Marshal(body)

	req, _ := http.NewRequest("POST", "/api/v1/spans", bytes.NewReader(jsonBody))
	req.Header.Set("X-API-Key", "valid_project_key")

	_, err := h.Translate(req.Context(), req)
	if err == nil {
		t.Fatal("expected error for empty spans array, got nil")
	}
}

// TestIngestAdapter_Translate_HTTPRequest_BadRequest proves Translate returns
// an error when the request body is malformed JSON.
func TestIngestAdapter_Translate_HTTPRequest_BadRequest(t *testing.T) {
	q := &fakeIngestQueue{}
	v := &fakeValidator{}
	h := handlers.NewNativeHandler(q, v, nil)

	req, _ := http.NewRequest("POST", "/api/v1/spans", strings.NewReader("{bad json"))
	req.Header.Set("X-API-Key", "valid_project_key")
	req.Header.Set("Content-Type", "application/json")

	_, err := h.Translate(req.Context(), req)
	if err == nil {
		t.Fatal("expected error for malformed JSON, got nil")
	}
}

// TestIngestAdapter_Translate_HTTPRequest_MultipleSpans proves Translate
// handles a batch of multiple spans and validates normalisation across all.
func TestIngestAdapter_Translate_HTTPRequest_MultipleSpans(t *testing.T) {
	q := &fakeIngestQueue{}
	v := &fakeValidator{}
	h := handlers.NewNativeHandler(q, v, nil)

	body := map[string]any{
		"spans": []map[string]any{
			{"span_id": "0102030405060708", "trace_id": "0102030405060708090a0b0c0d0e0f10", "name": "span-a", "model": "gpt-4"},
			{"span_id": "0203040506070809", "trace_id": "0102030405060708090a0b0c0d0e0f10", "name": "span-b", "model": "gpt-3.5"},
		},
	}
	jsonBody, _ := json.Marshal(body)

	req, _ := http.NewRequest("POST", "/api/v1/spans", bytes.NewReader(jsonBody))
	req.Header.Set("X-API-Key", "valid_project_key")

	spans, err := h.Translate(req.Context(), req)
	if err != nil {
		t.Fatalf("Translate failed: %v", err)
	}

	if len(spans) != 2 {
		t.Fatalf("spans returned: got %d, want 2", len(spans))
	}
	if spans[0].Name != "span-a" {
		t.Errorf("first span name: got %q, want %q", spans[0].Name, "span-a")
	}
	if spans[1].Name != "span-b" {
		t.Errorf("second span name: got %q, want %q", spans[1].Name, "span-b")
	}
}

// TestIngestAdapter_Interface ensures NativeHandler satisfies IngestAdapter.
var _ handlers.IngestAdapter = (*handlers.NativeHandler)(nil)

// TestIngestAdapter_Route_proves the Route method returns an http.Handler
// that serves POST /api/v1/spans.
func TestIngestAdapter_Route(t *testing.T) {
	q := &fakeIngestQueue{}
	v := &fakeValidator{}
	h := handlers.NewNativeHandler(q, v, nil)

	handler := h.Route()
	ts := httptest.NewServer(handler)
	defer ts.Close()

	body := map[string]any{
		"spans": []map[string]any{
			{
				"span_id":  "0102030405060708",
				"trace_id": "0102030405060708090a0b0c0d0e0f10",
				"name":     "route-span",
			},
		},
	}
	jsonBody, _ := json.Marshal(body)

	rq, _ := http.NewRequest("POST", ts.URL+"/api/v1/spans", bytes.NewReader(jsonBody))
	rq.Header.Set("X-API-Key", "valid_project_key")
	rq.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(rq)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted {
		t.Errorf("status: got %d, want %d", resp.StatusCode, http.StatusAccepted)
	}
}

// TestIngestAdapter_Route_CORS_proves the route path respects CORS middleware.
func TestIngestAdapter_Route_CORS(t *testing.T) {
	q := &fakeIngestQueue{}
	v := &fakeValidator{}
	h := handlers.NewNativeHandler(q, v, []string{"http://localhost:3000"})

	handler := h.Route()
	ts := httptest.NewServer(handler)
	defer ts.Close()

	// OPTIONS preflight
	rq, _ := http.NewRequest("OPTIONS", ts.URL+"/api/v1/spans", nil)
	rq.Header.Set("Origin", "http://localhost:3000")
	resp, err := http.DefaultClient.Do(rq)
	if err != nil {
		t.Fatalf("OPTIONS request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("OPTIONS status: got %d, want %d", resp.StatusCode, http.StatusNoContent)
	}
	if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "http://localhost:3000" {
		t.Errorf("CORS Allow-Origin: got %q, want %q", got, "http://localhost:3000")
	}
}
