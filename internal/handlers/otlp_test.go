package handlers_test

import (
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	coltracev1 "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	tracev1 "go.opentelemetry.io/proto/otlp/trace/v1"

	"github.com/omneval/omneval/internal/auth"
	"github.com/omneval/omneval/internal/domain"
	"github.com/omneval/omneval/internal/handlers"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
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
type fakeValidator struct{}

func (f *fakeValidator) Validate(_ context.Context, rawKey string) (*auth.ValidatedKey, error) {
	if rawKey == "valid_project_key" {
		return &auth.ValidatedKey{
			ProjectID: "proj-1",
		}, nil
	}
	return nil, fmt.Errorf("invalid API key")
}

// --- Tests ---

func TestOTLPHandler_AcceptsBatch(t *testing.T) {
	q := &fakeIngestQueue{}
	v := &fakeValidator{}
	h := handlers.NewOTLPHandler(q, v)
	ts := httptest.NewServer(h.Router())
	defer ts.Close()

	// Build a minimal OTLP request with 2 spans
	req := &coltracev1.ExportTraceServiceRequest{
		ResourceSpans: []*tracev1.ResourceSpans{
			{
				ScopeSpans: []*tracev1.ScopeSpans{
					{
						Spans: []*tracev1.Span{
							{
								TraceId: []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10},
								SpanId:  []byte{0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18},
								Name:    "test-otel-span",
							},
							{
								TraceId: []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10},
								SpanId:  []byte{0x19, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e, 0x1f, 0x20},
								Name:    "test-otel-span-2",
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

	req2, _ := http.NewRequest("POST", ts.URL+"/v1/traces", bytes.NewReader(body))
	req2.Header.Set("X-API-Key", "valid_project_key")
	req2.Header.Set("Content-Type", "application/x-protobuf")

	resp, err := http.DefaultClient.Do(req2)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("status: got %d, want %d", resp.StatusCode, http.StatusAccepted)
	}
	if len(q.batches) != 1 || len(q.batches[0]) != 2 {
		t.Errorf("expected 1 batch with 2 spans, got %d batches", len(q.batches))
	}
}

func TestOTLPHandler_401OnMissingKey(t *testing.T) {
	q := &fakeIngestQueue{}
	v := &fakeValidator{}
	h := handlers.NewOTLPHandler(q, v)
	ts := httptest.NewServer(h.Router())
	defer ts.Close()

	req := &coltracev1.ExportTraceServiceRequest{}
	body, _ := proto.Marshal(req)

	req2, _ := http.NewRequest("POST", ts.URL+"/v1/traces", bytes.NewReader(body))
	req2.Header.Set("Content-Type", "application/x-protobuf")

	resp, err := http.DefaultClient.Do(req2)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status: got %d, want %d", resp.StatusCode, http.StatusUnauthorized)
	}
}

func TestOTLPHandler_405OnWrongMethod(t *testing.T) {
	q := &fakeIngestQueue{}
	v := &fakeValidator{}
	h := handlers.NewOTLPHandler(q, v)
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

func TestOTLPHandler_400OnUnsupportedContentType(t *testing.T) {
	q := &fakeIngestQueue{}
	v := &fakeValidator{}
	h := handlers.NewOTLPHandler(q, v)
	ts := httptest.NewServer(h.Router())
	defer ts.Close()

	req, _ := http.NewRequest("POST", ts.URL+"/v1/traces", strings.NewReader("not protobuf"))
	req.Header.Set("X-API-Key", "valid_project_key")
	req.Header.Set("Content-Type", "text/plain")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status: got %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}
}

func TestOTLPHandler_400OnInvalidProtobuf(t *testing.T) {
	q := &fakeIngestQueue{}
	v := &fakeValidator{}
	h := handlers.NewOTLPHandler(q, v)
	ts := httptest.NewServer(h.Router())
	defer ts.Close()

	req, _ := http.NewRequest("POST", ts.URL+"/v1/traces", strings.NewReader("garbage"))
	req.Header.Set("X-API-Key", "valid_project_key")
	req.Header.Set("Content-Type", "application/x-protobuf")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status: got %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}
}

func TestOTLPHandler_400OnInvalidJSON(t *testing.T) {
	q := &fakeIngestQueue{}
	v := &fakeValidator{}
	h := handlers.NewOTLPHandler(q, v)
	ts := httptest.NewServer(h.Router())
	defer ts.Close()

	req, _ := http.NewRequest("POST", ts.URL+"/v1/traces", strings.NewReader("{invalid"))
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

// --- Logging Tests ---

func TestOTLPHandler_LogsAcceptedBatch(t *testing.T) {
	buf := &strings.Builder{}
	logger := slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelInfo}))
	orig := slog.Default()
	slog.SetDefault(logger)
	defer slog.SetDefault(orig)

	q := &fakeIngestQueue{}
	v := &fakeValidator{}
	h := handlers.NewOTLPHandler(q, v)
	ts := httptest.NewServer(h.Router())
	defer ts.Close()

	req := buildMinimalOTLPRequest()
	body, err := proto.Marshal(req)
	if err != nil {
		t.Fatalf("marshal OTLP request: %v", err)
	}

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

	if !strings.Contains(buf.String(), "accepted spans") {
		t.Errorf("expected log with 'accepted spans', got:\n%s", buf.String())
	}
	if !strings.Contains(buf.String(), "project_id=") {
		t.Errorf("expected log with 'project_id=', got:\n%s", buf.String())
	}
	if !strings.Contains(buf.String(), "span_count=") {
		t.Errorf("expected log with 'span_count=', got:\n%s", buf.String())
	}
}

func TestOTLPHandler_LogsEnqueueFailure(t *testing.T) {
	buf := &strings.Builder{}
	logger := slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelError}))
	orig := slog.Default()
	slog.SetDefault(logger)
	defer slog.SetDefault(orig)

	q := &failingIngestQueue{}
	v := &fakeValidator{}
	h := handlers.NewOTLPHandler(q, v)
	ts := httptest.NewServer(h.Router())
	defer ts.Close()

	req := buildMinimalOTLPRequest()
	body, err := proto.Marshal(req)
	if err != nil {
		t.Fatalf("marshal OTLP request: %v", err)
	}

	rq, _ := http.NewRequest("POST", ts.URL+"/v1/traces", bytes.NewReader(body))
	rq.Header.Set("X-API-Key", "valid_project_key")
	rq.Header.Set("Content-Type", "application/x-protobuf")

	resp, err := http.DefaultClient.Do(rq)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status: got %d, want %d", resp.StatusCode, http.StatusServiceUnavailable)
	}

	if !strings.Contains(buf.String(), "enqueue failed") {
		t.Errorf("expected log with 'enqueue failed', got:\n%s", buf.String())
	}
}

// failingIngestQueue always returns an error for enqueue operations.
type failingIngestQueue struct{}

func (f *failingIngestQueue) Enqueue(_ context.Context, _ []*domain.Span) error {
	return fmt.Errorf("queue full")
}

// compressGzip compresses data with gzip and returns it in a bytes.Buffer.
func compressGzip(t *testing.T, data []byte) *bytes.Buffer {
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	if _, err := gw.Write(data); err != nil {
		t.Fatalf("gzip write: %v", err)
	}
	if err := gw.Close(); err != nil {
		t.Fatalf("gzip close: %v", err)
	}
	return &buf
}

// --- Gzip Decompression Tests ---

func TestOTLPHandler_AcceptsGzipCompressedProtobuf(t *testing.T) {
	q := &fakeIngestQueue{}
	v := &fakeValidator{}
	h := handlers.NewOTLPHandler(q, v)
	ts := httptest.NewServer(h.Router())
	defer ts.Close()

	req := buildMinimalOTLPRequest()
	body, err := proto.Marshal(req)
	if err != nil {
		t.Fatalf("marshal OTLP request: %v", err)
	}

	rq, _ := http.NewRequest("POST", ts.URL+"/v1/traces", compressGzip(t, body))
	rq.Header.Set("X-API-Key", "valid_project_key")
	rq.Header.Set("Content-Type", "application/x-protobuf")
	rq.Header.Set("Content-Encoding", "gzip")

	resp, err := http.DefaultClient.Do(rq)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted {
		bodyBytes, _ := io.ReadAll(resp.Body)
		t.Fatalf("status: got %d, want %d, body: %s", resp.StatusCode, http.StatusAccepted, string(bodyBytes))
	}
	if len(q.batches) != 1 || len(q.batches[0]) != 1 {
		t.Errorf("expected 1 batch with 1 span, got %d batches", len(q.batches))
	}
}

func TestOTLPHandler_GzipCompressedJSON(t *testing.T) {
	q := &fakeIngestQueue{}
	v := &fakeValidator{}
	h := handlers.NewOTLPHandler(q, v)
	ts := httptest.NewServer(h.Router())
	defer ts.Close()

	// Build a minimal OTLP request and serialize to JSON
	req := buildMinimalOTLPRequest()
	jsonBytes, err := protojson.Marshal(req)
	if err != nil {
		t.Fatalf("protojson marshal: %v", err)
	}

	rq, _ := http.NewRequest("POST", ts.URL+"/v1/traces", compressGzip(t, jsonBytes))
	rq.Header.Set("X-API-Key", "valid_project_key")
	rq.Header.Set("Content-Type", "application/json")
	rq.Header.Set("Content-Encoding", "gzip")

	resp, err := http.DefaultClient.Do(rq)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted {
		bodyBytes, _ := io.ReadAll(resp.Body)
		t.Fatalf("status: got %d, want %d, body: %s", resp.StatusCode, http.StatusAccepted, string(bodyBytes))
	}
	if len(q.batches) != 1 {
		t.Errorf("expected 1 batch, got %d", len(q.batches))
	}
}

func TestOTLPHandler_GzipInvalidData_Returns400(t *testing.T) {
	q := &fakeIngestQueue{}
	v := &fakeValidator{}
	h := handlers.NewOTLPHandler(q, v)
	ts := httptest.NewServer(h.Router())
	defer ts.Close()

	// Send random data with gzip content-encoding
	data := []byte("not-gzip-data")
	rq, _ := http.NewRequest("POST", ts.URL+"/v1/traces", bytes.NewReader(data))
	rq.Header.Set("X-API-Key", "valid_project_key")
	rq.Header.Set("Content-Type", "application/x-protobuf")
	rq.Header.Set("Content-Encoding", "gzip")

	resp, err := http.DefaultClient.Do(rq)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status: got %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}
}

// buildMinimalOTLPRequest creates an OTLP request with a gen_ai model attribute
// so the translate function produces domain spans.
func buildMinimalOTLPRequest() *coltracev1.ExportTraceServiceRequest {
	return &coltracev1.ExportTraceServiceRequest{
		ResourceSpans: []*tracev1.ResourceSpans{
			{
				ScopeSpans: []*tracev1.ScopeSpans{
					{
						Spans: []*tracev1.Span{
							{
								TraceId: []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10},
								SpanId:  []byte{0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18},
								Name:    "test-otel-span",
							},
						},
					},
				},
			},
		},
	}
}
