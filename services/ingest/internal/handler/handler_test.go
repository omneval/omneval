package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/zbloss/lantern/internal/domain"
	"github.com/zbloss/lantern/internal/queue"
	"github.com/zbloss/lantern/services/ingest/internal/handler"
)

// fakeIngestQueue stores enqueued spans in-memory for testing.
type fakeIngestQueue struct {
	batches [][]*domain.Span
}

func (f *fakeIngestQueue) Enqueue(_ context.Context, spans []*domain.Span) error {
	f.batches = append(f.batches, spans)
	return nil
}

// fakeValidator is a minimal Validator for testing.
type fakeValidator struct {
	key *domain.APIKey
}

func (f *fakeValidator) Validate(_ context.Context, rawKey string) (*handler.ValidatedKey, error) {
	if rawKey == "valid_project_key" {
		return &handler.ValidatedKey{
			ProjectID: "proj-1",
			Kind:      domain.APIKeyKindProject,
		}, nil
	}
	if rawKey == "valid_service_key" {
		return &handler.ValidatedKey{
			ProjectID:   "proj-2",
			Kind:        domain.APIKeyKindService,
			ServiceName: "my-service",
		}, nil
	}
	return nil, fmt.Errorf("invalid API key")
}

// --- Tests ---

func TestNativeHandler_PostSpans_202OnSuccess(t *testing.T) {
	q := &fakeIngestQueue{}
	v := &fakeValidator{}
	h := handler.NewNativeHandler(q, v, nil, nil)
	ts := httptest.NewServer(h.Router())
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
	if len(q.batches) != 1 {
		t.Fatalf("batches enqueued: got %d, want 1", len(q.batches))
	}
	if len(q.batches[0]) != 1 {
		t.Errorf("spans in batch: got %d, want 1", len(q.batches[0]))
	}
}

func TestNativeHandler_PostSpans_401OnMissingKey(t *testing.T) {
	q := &fakeIngestQueue{}
	v := &fakeValidator{}
	h := handler.NewNativeHandler(q, v, nil, nil)
	ts := httptest.NewServer(h.Router())
	defer ts.Close()

	payload := []byte(`{"spans": [{"trace_id": "0123456789abcdef0123456789abcdef", "span_id": "0123456789abcdef", "name": "test"}]}`)
	req, _ := http.NewRequest("POST", ts.URL+"/api/v1/spans", bytes.NewReader(payload))

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status: got %d, want %d", resp.StatusCode, http.StatusUnauthorized)
	}
}

func TestNativeHandler_PostSpans_401OnInvalidKey(t *testing.T) {
	q := &fakeIngestQueue{}
	v := &fakeValidator{}
	h := handler.NewNativeHandler(q, v, nil, nil)
	ts := httptest.NewServer(h.Router())
	defer ts.Close()

	payload := []byte(`{"spans": [{"trace_id": "0123456789abcdef0123456789abcdef", "span_id": "0123456789abcdef", "name": "test"}]}`)
	req, _ := http.NewRequest("POST", ts.URL+"/api/v1/spans", bytes.NewReader(payload))
	req.Header.Set("X-API-Key", "invalid_key")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status: got %d, want %d", resp.StatusCode, http.StatusUnauthorized)
	}
}

func TestNativeHandler_NormalizesStringInput(t *testing.T) {
	q := &fakeIngestQueue{}
	v := &fakeValidator{}
	h := handler.NewNativeHandler(q, v, nil, nil)
	ts := httptest.NewServer(h.Router())
	defer ts.Close()

	spans := []*handler.NativeSpan{
		{
			TraceID: "0123456789abcdef0123456789abcdef",
			SpanID:  "0123456789abcdef",
			Name:    "test-span",
			Input:   "Hello world",
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
		t.Fatalf("status: got %d, want %d", resp.StatusCode, http.StatusAccepted)
	}

	// Verify normalization
	if len(q.batches) != 1 || len(q.batches[0]) != 1 {
		t.Fatal("expected 1 batch with 1 span")
	}
	span := q.batches[0][0]
	var messages []map[string]any
	if err := json.Unmarshal([]byte(span.Input), &messages); err != nil {
		t.Fatalf("input is not valid JSON array: %v", err)
	}
	if len(messages) != 1 {
		t.Fatalf("messages length: got %d, want 1", len(messages))
	}
	if messages[0]["role"] != "user" {
		t.Errorf("role: got %q, want %q", messages[0]["role"], "user")
	}
	if messages[0]["content"] != "Hello world" {
		t.Errorf("content: got %q, want %q", messages[0]["content"], "Hello world")
	}
}

func TestNativeHandler_NormalizesStringOutput(t *testing.T) {
	q := &fakeIngestQueue{}
	v := &fakeValidator{}
	h := handler.NewNativeHandler(q, v, nil, nil)
	ts := httptest.NewServer(h.Router())
	defer ts.Close()

	spans := []*handler.NativeSpan{
		{
			TraceID: "0123456789abcdef0123456789abcdef",
			SpanID:  "0123456789abcdef",
			Name:    "test-span",
			Output:  "Response text",
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
		t.Fatalf("status: got %d, want %d", resp.StatusCode, http.StatusAccepted)
	}

	if len(q.batches) != 1 || len(q.batches[0]) != 1 {
		t.Fatal("expected 1 batch with 1 span")
	}
	span := q.batches[0][0]
	var messages []map[string]any
	if err := json.Unmarshal([]byte(span.Output), &messages); err != nil {
		t.Fatalf("output is not valid JSON array: %v", err)
	}
	if len(messages) != 1 {
		t.Fatalf("messages length: got %d, want 1", len(messages))
	}
	if messages[0]["role"] != "assistant" {
		t.Errorf("role: got %q, want %q", messages[0]["role"], "assistant")
	}
}

func TestNativeHandler_ValidatesSpanIDFormat(t *testing.T) {
	q := &fakeIngestQueue{}
	v := &fakeValidator{}
	h := handler.NewNativeHandler(q, v, nil, nil)
	ts := httptest.NewServer(h.Router())
	defer ts.Close()

	// span_id must be exactly 8 hex bytes (16 hex chars)
	spans := []*handler.NativeSpan{
		{
			TraceID: "0123456789abcdef0123456789abcdef",
			SpanID:  "short",
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

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status: got %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}
}

func TestNativeHandler_ValidatesTraceIDFormat(t *testing.T) {
	q := &fakeIngestQueue{}
	v := &fakeValidator{}
	h := handler.NewNativeHandler(q, v, nil, nil)
	ts := httptest.NewServer(h.Router())
	defer ts.Close()

	// trace_id must be exactly 16 hex bytes (32 hex chars)
	spans := []*handler.NativeSpan{
		{
			TraceID: "short",
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

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status: got %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}
}

func TestNativeHandler_AttachesServiceNameFromServiceKey(t *testing.T) {
	q := &fakeIngestQueue{}
	v := &fakeValidator{}
	h := handler.NewNativeHandler(q, v, nil, nil)
	ts := httptest.NewServer(h.Router())
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
	req.Header.Set("X-API-Key", "valid_service_key")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("status: got %d, want %d", resp.StatusCode, http.StatusAccepted)
	}

	if len(q.batches) != 1 || len(q.batches[0]) != 1 {
		t.Fatal("expected 1 batch with 1 span")
	}
	if q.batches[0][0].ServiceName != "my-service" {
		t.Errorf("service_name: got %q, want %q", q.batches[0][0].ServiceName, "my-service")
	}
}

func TestNativeHandler_PostSpans_503WhenQueueFails(t *testing.T) {
	q := &failingQueue{}
	v := &fakeValidator{}
	h := handler.NewNativeHandler(q, v, nil, nil)
	ts := httptest.NewServer(h.Router())
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

	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("status: got %d, want %d", resp.StatusCode, http.StatusServiceUnavailable)
	}
}

func TestNativeHandler_EmptySpansArray(t *testing.T) {
	q := &fakeIngestQueue{}
	v := &fakeValidator{}
	h := handler.NewNativeHandler(q, v, nil, nil)
	ts := httptest.NewServer(h.Router())
	defer ts.Close()

	payload := []byte(`{"spans": []}`)

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
	if len(q.batches) != 1 {
		t.Fatalf("batches enqueued: got %d, want 1", len(q.batches))
	}
}

func TestNativeHandler_MultipleSpansInOneRequest(t *testing.T) {
	q := &fakeIngestQueue{}
	v := &fakeValidator{}
	h := handler.NewNativeHandler(q, v, nil, nil)
	ts := httptest.NewServer(h.Router())
	defer ts.Close()

	spans := []*handler.NativeSpan{
		{
			TraceID: "0123456789abcdef0123456789abcdef",
			SpanID:  "0123456789abcdef",
			Name:    "span-1",
		},
		{
			TraceID: "0123456789abcdef0123456789abcdef",
			SpanID:  "abcdef0123456789",
			Name:    "span-2",
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
	if len(q.batches) != 1 {
		t.Fatalf("batches enqueued: got %d, want 1", len(q.batches))
	}
	if len(q.batches[0]) != 2 {
		t.Errorf("spans in batch: got %d, want 2", len(q.batches[0]))
	}
}

func TestNativeHandler_JSONArrayInput(t *testing.T) {
	q := &fakeIngestQueue{}
	v := &fakeValidator{}
	h := handler.NewNativeHandler(q, v, nil, nil)
	ts := httptest.NewServer(h.Router())
	defer ts.Close()

	// Input is already a JSON array - should be preserved as-is
	spans := []*handler.NativeSpan{
		{
			TraceID: "0123456789abcdef0123456789abcdef",
			SpanID:  "0123456789abcdef",
			Name:    "test-span",
			Input:   `[{"role":"user","content":"Hi"},{"role":"assistant","content":"Hello"}]`,
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
		t.Fatalf("status: got %d, want %d", resp.StatusCode, http.StatusAccepted)
	}

	if len(q.batches) != 1 || len(q.batches[0]) != 1 {
		t.Fatal("expected 1 batch with 1 span")
	}
	// The JSON string input should be kept as-is (wrapped in JSON)
	// Actually, per the acceptance criteria, plain strings should be normalized.
	// If it's already valid JSON, it should be used directly.
	span := q.batches[0][0]
	if span.Input != `[{"role":"user","content":"Hi"},{"role":"assistant","content":"Hello"}]` {
		t.Errorf("input: got %q, want %q", span.Input, `[{"role":"user","content":"Hi"},{"role":"assistant","content":"Hello"}]`)
	}
}

func TestNativeHandler_SetProjectIDFromKey(t *testing.T) {
	q := &fakeIngestQueue{}
	v := &fakeValidator{}
	h := handler.NewNativeHandler(q, v, nil, nil)
	ts := httptest.NewServer(h.Router())
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
		t.Fatalf("status: got %d, want %d", resp.StatusCode, http.StatusAccepted)
	}

	if len(q.batches) != 1 || len(q.batches[0]) != 1 {
		t.Fatal("expected 1 batch with 1 span")
	}
	if q.batches[0][0].ProjectID != "proj-1" {
		t.Errorf("project_id: got %q, want %q", q.batches[0][0].ProjectID, "proj-1")
	}
}

func TestNativeHandler_SetProjectIDFromServiceKey(t *testing.T) {
	q := &fakeIngestQueue{}
	v := &fakeValidator{}
	h := handler.NewNativeHandler(q, v, nil, nil)
	ts := httptest.NewServer(h.Router())
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
	req.Header.Set("X-API-Key", "valid_service_key")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("status: got %d, want %d", resp.StatusCode, http.StatusAccepted)
	}

	if len(q.batches) != 1 || len(q.batches[0]) != 1 {
		t.Fatal("expected 1 batch with 1 span")
	}
	if q.batches[0][0].ProjectID != "proj-2" {
		t.Errorf("project_id: got %q, want %q", q.batches[0][0].ProjectID, "proj-2")
	}
}

// failingQueue always returns an error to simulate Redis failure.
type failingQueue struct{}

func (f *failingQueue) Enqueue(_ context.Context, _ []*domain.Span) error {
	return queue.ErrQueueUnreachable
}

// --- CORS Tests ---

func TestNativeHandler_CORS_PreflightReturns204(t *testing.T) {
	q := &fakeIngestQueue{}
	v := &fakeValidator{}
	h := handler.NewNativeHandler(q, v, []string{"*"}, nil)
	ts := httptest.NewServer(h.Router())
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

func TestNativeHandler_CORS_PreflightSetsAllowOrigin(t *testing.T) {
	q := &fakeIngestQueue{}
	v := &fakeValidator{}
	h := handler.NewNativeHandler(q, v, []string{"*"}, nil)
	ts := httptest.NewServer(h.Router())
	defer ts.Close()

	req, _ := http.NewRequest("OPTIONS", ts.URL+"/api/v1/spans", nil)
	req.Header.Set("Origin", "http://example.com")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "*" {
		t.Errorf("Access-Control-Allow-Origin: got %q, want %q", got, "*")
	}
}

func TestNativeHandler_CORS_PostWithWildcardOrigin(t *testing.T) {
	q := &fakeIngestQueue{}
	v := &fakeValidator{}
	h := handler.NewNativeHandler(q, v, []string{"*"}, nil)
	ts := httptest.NewServer(h.Router())
	defer ts.Close()

	spans := []*handler.NativeSpan{
		{TraceID: "0123456789abcdef0123456789abcdef", SpanID: "0123456789abcdef", Name: "test"},
	}
	payload, _ := json.Marshal(map[string][]*handler.NativeSpan{"spans": spans})

	req, _ := http.NewRequest("POST", ts.URL+"/api/v1/spans", bytes.NewReader(payload))
	req.Header.Set("X-API-Key", "valid_project_key")
	req.Header.Set("Origin", "http://example.com")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted {
		t.Errorf("status: got %d, want %d", resp.StatusCode, http.StatusAccepted)
	}
	if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "*" {
		t.Errorf("Access-Control-Allow-Origin: got %q, want %q", got, "*")
	}
}

func TestNativeHandler_CORS_PostWithSpecificOrigin(t *testing.T) {
	q := &fakeIngestQueue{}
	v := &fakeValidator{}
	h := handler.NewNativeHandler(q, v, []string{"http://example.com"}, nil)
	ts := httptest.NewServer(h.Router())
	defer ts.Close()

	spans := []*handler.NativeSpan{
		{TraceID: "0123456789abcdef0123456789abcdef", SpanID: "0123456789abcdef", Name: "test"},
	}
	payload, _ := json.Marshal(map[string][]*handler.NativeSpan{"spans": spans})

	req, _ := http.NewRequest("POST", ts.URL+"/api/v1/spans", bytes.NewReader(payload))
	req.Header.Set("X-API-Key", "valid_project_key")
	req.Header.Set("Origin", "http://example.com")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted {
		t.Errorf("status: got %d, want %d", resp.StatusCode, http.StatusAccepted)
	}
	if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "http://example.com" {
		t.Errorf("Access-Control-Allow-Origin: got %q, want %q", got, "http://example.com")
	}
}

func TestNativeHandler_CORS_PostWithUnmatchedOrigin(t *testing.T) {
	q := &fakeIngestQueue{}
	v := &fakeValidator{}
	h := handler.NewNativeHandler(q, v, []string{"http://example.com"}, nil)
	ts := httptest.NewServer(h.Router())
	defer ts.Close()

	spans := []*handler.NativeSpan{
		{TraceID: "0123456789abcdef0123456789abcdef", SpanID: "0123456789abcdef", Name: "test"},
	}
	payload, _ := json.Marshal(map[string][]*handler.NativeSpan{"spans": spans})

	req, _ := http.NewRequest("POST", ts.URL+"/api/v1/spans", bytes.NewReader(payload))
	req.Header.Set("X-API-Key", "valid_project_key")
	req.Header.Set("Origin", "http://unknown.com")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted {
		t.Errorf("status: got %d, want %d", resp.StatusCode, http.StatusAccepted)
	}
	if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("Access-Control-Allow-Origin: got %q, want empty (unmatched origin)", got)
	}
}

func TestNativeHandler_CORS_WithoutOriginHeader(t *testing.T) {
	q := &fakeIngestQueue{}
	v := &fakeValidator{}
	h := handler.NewNativeHandler(q, v, []string{"*"}, nil)
	ts := httptest.NewServer(h.Router())
	defer ts.Close()

	spans := []*handler.NativeSpan{
		{TraceID: "0123456789abcdef0123456789abcdef", SpanID: "0123456789abcdef", Name: "test"},
	}
	payload, _ := json.Marshal(map[string][]*handler.NativeSpan{"spans": spans})

	req, _ := http.NewRequest("POST", ts.URL+"/api/v1/spans", bytes.NewReader(payload))
	req.Header.Set("X-API-Key", "valid_project_key")
	// No Origin header

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted {
		t.Errorf("status: got %d, want %d", resp.StatusCode, http.StatusAccepted)
	}
	if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("Access-Control-Allow-Origin: got %q, want empty (no Origin header)", got)
	}
}
