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
	// X-API-Key should take precedence
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

func TestNativeHandler_ValidatesSpanFields(t *testing.T) {
	q := &fakeIngestQueue{}
	v := &fakeValidator{}
	h := handlers.NewNativeHandler(q, v, nil)
	ts := httptest.NewServer(h.Router())
	defer ts.Close()

	// Invalid span_id length
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
