package handlers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
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
// translates a NativeSpan into a normalized domain.Span.  This is the deep
// seam the adapter exposes: Translate(ctx, *NativeSpan, *ValidatedKey)
// (*domain.Span, error).
func TestIngestAdapter_Translate(t *testing.T) {
	q := &fakeIngestQueue{}
	v := &fakeValidator{}
	h := handlers.NewNativeHandler(q, v, nil)

	vk := &auth.ValidatedKey{
		ProjectID:   "proj-test",
		ServiceName: "test-service",
	}

	span, err := h.Translate(nil, &handlers.NativeSpan{
		SpanID:  "0102030405060708",
		TraceID: "0102030405060708090a0b0c0d0e0f10",
		Name:    "my-span",
		Model:   "gpt-4",
		Input:   "hello",
		Output:  "hi back",
	}, vk)
	if err != nil {
		t.Fatalf("Translate failed: %v", err)
	}

	if span.Name != "my-span" {
		t.Errorf("span name: got %q, want %q", span.Name, "my-span")
	}
	if span.ProjectID != "proj-test" {
		t.Errorf("project_id: got %q, want %q", span.ProjectID, "proj-test")
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
