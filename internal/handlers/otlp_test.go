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
	"time"

	coltracev1 "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	commonv1 "go.opentelemetry.io/proto/otlp/common/v1"
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

// --- Bearer Auth Integration Tests ---

func TestOTLPHandler_AcceptsBearerAuth(t *testing.T) {
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
	rq.Header.Set("Authorization", "Bearer valid_project_key")
	rq.Header.Set("Content-Type", "application/x-protobuf")

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

func TestOTLPHandler_XAPIKeyPrecedenceOverBearerAuth(t *testing.T) {
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
	rq.Header.Set("Authorization", "Bearer some_other_key")
	rq.Header.Set("Content-Type", "application/x-protobuf")

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

func TestOTLPHandler_RejectsMalformedAuthorization(t *testing.T) {
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
	rq.Header.Set("Authorization", "Basic YWJj")
	rq.Header.Set("Content-Type", "application/x-protobuf")

	resp, err := http.DefaultClient.Do(rq)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status: got %d, want %d (malformed Authorization should be treated as no auth)", resp.StatusCode, http.StatusUnauthorized)
	}
}

// trackingValidator is a test validator that records which key was validated.
type trackingValidator struct {
	validate func(ctx context.Context, rawKey string) (*auth.ValidatedKey, error)
}

func (v *trackingValidator) Validate(ctx context.Context, rawKey string) (*auth.ValidatedKey, error) {
	return v.validate(ctx, rawKey)
}

// postOTLPRequest sends req to h via the given content type and returns the
// spans enqueued in q. It fails the test if the request is not accepted.
func postOTLPRequest(t *testing.T, h *handlers.OTLPHandler, q *fakeIngestQueue, req *coltracev1.ExportTraceServiceRequest, contentType string) []*domain.Span {
	t.Helper()

	ts := httptest.NewServer(h.Router())
	defer ts.Close()

	var body []byte
	var err error
	switch contentType {
	case "application/json":
		body, err = protojson.Marshal(req)
	default:
		body, err = proto.Marshal(req)
	}
	if err != nil {
		t.Fatalf("marshal OTLP request: %v", err)
	}

	rq, _ := http.NewRequest("POST", ts.URL+"/v1/traces", bytes.NewReader(body))
	rq.Header.Set("X-API-Key", "valid_project_key")
	rq.Header.Set("Content-Type", contentType)

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
		t.Fatalf("expected 1 batch, got %d", len(q.batches))
	}
	return q.batches[0]
}

// buildOTLPRequestWithStatus creates an OTLP request with a single span
// carrying the given proto Status (which may be nil for "no status set").
func buildOTLPRequestWithStatus(status *tracev1.Status) *coltracev1.ExportTraceServiceRequest {
	return &coltracev1.ExportTraceServiceRequest{
		ResourceSpans: []*tracev1.ResourceSpans{
			{
				ScopeSpans: []*tracev1.ScopeSpans{
					{
						Spans: []*tracev1.Span{
							{
								TraceId: []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10},
								SpanId:  []byte{0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18},
								Name:    "status-span",
								Status:  status,
							},
						},
					},
				},
			},
		},
	}
}

// TestOTLPHandler_StatusCodeOK_Protobuf covers issue #135: an OTLP span with
// proto Status{Code: STATUS_CODE_OK, Message: "all good"} sent as protobuf
// should translate to domain.Span.StatusCode == "OK" with the message carried
// through to StatusMessage.
func TestOTLPHandler_StatusCodeOK_Protobuf(t *testing.T) {
	q := &fakeIngestQueue{}
	v := &fakeValidator{}
	h := handlers.NewOTLPHandler(q, v)

	req := buildOTLPRequestWithStatus(&tracev1.Status{
		Code:    tracev1.Status_STATUS_CODE_OK,
		Message: "all good",
	})

	spans := postOTLPRequest(t, h, q, req, "application/x-protobuf")
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}
	if got := spans[0].StatusCode; got != "OK" {
		t.Errorf("status_code: got %q, want %q", got, "OK")
	}
	if got := spans[0].StatusMessage; got != "all good" {
		t.Errorf("status_message: got %q, want %q", got, "all good")
	}
}

// TestOTLPHandler_StatusCodeError_Protobuf covers STATUS_CODE_ERROR mapping
// to domain.Span.StatusCode == "ERROR".
func TestOTLPHandler_StatusCodeError_Protobuf(t *testing.T) {
	q := &fakeIngestQueue{}
	v := &fakeValidator{}
	h := handlers.NewOTLPHandler(q, v)

	req := buildOTLPRequestWithStatus(&tracev1.Status{
		Code:    tracev1.Status_STATUS_CODE_ERROR,
		Message: "boom",
	})

	spans := postOTLPRequest(t, h, q, req, "application/x-protobuf")
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}
	if got := spans[0].StatusCode; got != "ERROR" {
		t.Errorf("status_code: got %q, want %q", got, "ERROR")
	}
	if got := spans[0].StatusMessage; got != "boom" {
		t.Errorf("status_message: got %q, want %q", got, "boom")
	}
}

// TestOTLPHandler_StatusCodeExplicitUnset_Protobuf covers an OTLP span that
// explicitly sets Status{Code: STATUS_CODE_UNSET} (as opposed to omitting the
// Status field entirely). This should translate to the literal "UNSET" so it
// is consistent with the stored values used by the status_code filter (#139).
func TestOTLPHandler_StatusCodeExplicitUnset_Protobuf(t *testing.T) {
	q := &fakeIngestQueue{}
	v := &fakeValidator{}
	h := handlers.NewOTLPHandler(q, v)

	req := buildOTLPRequestWithStatus(&tracev1.Status{
		Code: tracev1.Status_STATUS_CODE_UNSET,
	})

	spans := postOTLPRequest(t, h, q, req, "application/x-protobuf")
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}
	if got := spans[0].StatusCode; got != "UNSET" {
		t.Errorf("status_code: got %q, want %q", got, "UNSET")
	}
}

// TestOTLPHandler_StatusCodeAbsent_Protobuf covers an OTLP span with no
// Status field at all (the common case for SDKs that never set status).
// status_code should remain empty (stored as NULL/'' in the Lake, matched by
// the "UNSET" filter per #139).
func TestOTLPHandler_StatusCodeAbsent_Protobuf(t *testing.T) {
	q := &fakeIngestQueue{}
	v := &fakeValidator{}
	h := handlers.NewOTLPHandler(q, v)

	req := buildOTLPRequestWithStatus(nil)

	spans := postOTLPRequest(t, h, q, req, "application/x-protobuf")
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}
	if got := spans[0].StatusCode; got != "" {
		t.Errorf("status_code: got %q, want empty", got)
	}
	if got := spans[0].StatusMessage; got != "" {
		t.Errorf("status_message: got %q, want empty", got)
	}
}

// TestOTLPHandler_StatusCodeOK_JSON covers the JSON (protojson) content-type
// path: STATUS_CODE_OK should still map to "OK".
func TestOTLPHandler_StatusCodeOK_JSON(t *testing.T) {
	q := &fakeIngestQueue{}
	v := &fakeValidator{}
	h := handlers.NewOTLPHandler(q, v)

	req := buildOTLPRequestWithStatus(&tracev1.Status{
		Code:    tracev1.Status_STATUS_CODE_OK,
		Message: "fine",
	})

	spans := postOTLPRequest(t, h, q, req, "application/json")
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}
	if got := spans[0].StatusCode; got != "OK" {
		t.Errorf("status_code: got %q, want %q", got, "OK")
	}
	if got := spans[0].StatusMessage; got != "fine" {
		t.Errorf("status_message: got %q, want %q", got, "fine")
	}
}

// TestOTLPHandler_StatusCodeError_JSON covers the JSON (protojson) content-type
// path: STATUS_CODE_ERROR should still map to "ERROR".
func TestOTLPHandler_StatusCodeError_JSON(t *testing.T) {
	q := &fakeIngestQueue{}
	v := &fakeValidator{}
	h := handlers.NewOTLPHandler(q, v)

	req := buildOTLPRequestWithStatus(&tracev1.Status{
		Code:    tracev1.Status_STATUS_CODE_ERROR,
		Message: "broke",
	})

	spans := postOTLPRequest(t, h, q, req, "application/json")
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}
	if got := spans[0].StatusCode; got != "ERROR" {
		t.Errorf("status_code: got %q, want %q", got, "ERROR")
	}
	if got := spans[0].StatusMessage; got != "broke" {
		t.Errorf("status_message: got %q, want %q", got, "broke")
	}
}

// buildMinimalOTLPRequest creates an OTLP request with a single span.
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

// --- Diagnostic tests for Issue #279 ---

// TestSpanEventsNotRead_DiagnosticIssue279 confirms the end-to-end behavior:
// an OTLP request carrying only response-metadata attributes (no prompt/
// completion content) is ingested by the handler and normalized to a domain
// span with empty Input and Output.  This mirrors the shape of confirmed live
// litellm.completion spans from issue #279 where content lives in Span Events,
// which the pipeline never reads.
func TestSpanEventsNotRead_DiagnosticIssue279(t *testing.T) {
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
								TraceId:   []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10},
								SpanId:    []byte{0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18},
								Name:      "litellm.completion",
								StartTimeUnixNano: uint64(time.Now().UnixNano()),
								EndTimeUnixNano:   uint64(time.Now().UnixNano()),
								Attributes: []*commonv1.KeyValue{
									// Only response metadata — no gen_ai.prompt.* or
									// gen_ai.completion.* keys.  Real production content
									// is only in Span Events, which the pipeline does not
									// read at all.
									{Key: "gen_ai.response.id", Value: &commonv1.AnyValue{Value: &commonv1.AnyValue_StringValue{StringValue: "chatcmpl-abc123"}}},
									{Key: "gen_ai.request.model", Value: &commonv1.AnyValue{Value: &commonv1.AnyValue_StringValue{StringValue: "gpt-4o"}}},
									{Key: "gen_ai.system", Value: &commonv1.AnyValue{Value: &commonv1.AnyValue_StringValue{StringValue: "openai"}}},
									{Key: "lmnr.span.ids_path", Value: &commonv1.AnyValue{Value: &commonv1.AnyValue_StringValue{StringValue: "/spans"}}},
									{Key: "lmnr.span.sdk_version", Value: &commonv1.AnyValue{Value: &commonv1.AnyValue_StringValue{StringValue: "0.7.52"}}},
									{Key: "lmnr.span.type", Value: &commonv1.AnyValue{Value: &commonv1.AnyValue_StringValue{StringValue: "LLM"}}},
								},
								// NOTE: Events are empty. A real litellm/Laminar
								// span carries prompt/completion content here as
								// Span Events, but Omneval's pipeline does not read
								// Span Events at all.
							},
						},
					},
				},
			},
		},
	}

	spans := postOTLPRequest(t, h, q, req, "application/x-protobuf")
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}

	// Key diagnostic: attributes-only response metadata produces empty Input
	// and Output.  When gen_ai.input.messages / gen_ai.output.messages are
	// absent from Attributes, content may have gone to Span Events (OpenAI
	// instrumentor path), which the pipeline has never captured.
	if !strings.Contains(spans[0].Input, `"role":"user"`) || !strings.Contains(spans[0].Input, `"content":""`) {
		t.Errorf("input: got %q (expected normalized empty message — gen_ai.input.messages not in Attributes)", spans[0].Input)
	}
	if !strings.Contains(spans[0].Output, `"role":"assistant"`) || !strings.Contains(spans[0].Output, `"content":""`) {
		t.Errorf("output: got %q (expected normalized empty message — gen_ai.output.messages not in Attributes)", spans[0].Output)
	}
}

// TestSpanEventsCaptured_Ingestion tests that the OTLP handler correctly parses
// Span Events and populates Input/Output in the domain model.  This covers
// the Laminar/LiteLLM instrumentation path where content arrives as Span
// Events rather than Span Attributes.
func TestSpanEventsCaptured_Ingestion(t *testing.T) {
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
								TraceId:   []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10},
								SpanId:    []byte{0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18},
								Name:      "litellm.completion",
								StartTimeUnixNano: uint64(time.Now().UnixNano()),
								EndTimeUnixNano:   uint64(time.Now().UnixNano()),
								Attributes: []*commonv1.KeyValue{
									{Key: "gen_ai.response.id", Value: &commonv1.AnyValue{Value: &commonv1.AnyValue_StringValue{StringValue: "chatcmpl-abc123"}}},
									{Key: "gen_ai.request.model", Value: &commonv1.AnyValue{Value: &commonv1.AnyValue_StringValue{StringValue: "gpt-4o"}}},
									{Key: "gen_ai.system", Value: &commonv1.AnyValue{Value: &commonv1.AnyValue_StringValue{StringValue: "openai"}}},
								},
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

	spans := postOTLPRequest(t, h, q, req, "application/x-protobuf")
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}

	s := spans[0]

	// Input should contain the user message from Span Events.
	if !strings.Contains(s.Input, "user") {
		t.Errorf("input should contain 'user', got: %q", s.Input)
	}
	if !strings.Contains(s.Input, "Hello, world!") {
		t.Errorf("input should contain 'Hello, world!', got: %q", s.Input)
	}

	// Output should contain the assistant message from Span Events.
	if !strings.Contains(s.Output, "assistant") {
		t.Errorf("output should contain 'assistant', got: %q", s.Output)
	}
	if !strings.Contains(s.Output, "Hi there! How can I help?") {
		t.Errorf("output should contain 'Hi there! How can I help?', got: %q", s.Output)
	}
}

// TestSpanEventsCapture_DiagnosticIssue279 proves that Omneval's pipeline does
// NOT read Span Events from incoming OTLP spans.  The test constructs an OTLP
// request with prompt content embedded in Span Events (the exact shape that
// Laminar's LitellmInstrumentor sets on the wire: events named
// "gen_ai.prompt.message" and "gen_ai.completion.message" with role/content
// attributes).  The handler ingests the request but the normalizer produces
// empty Input/Output because convertToResourceSpans only reads
// s.GetAttributes(), never s.GetEvents().
//
// Source: lmnr/opentelemetry_lib/litellm/__init__.py uses set_span_attribute
// (NOT add_event) — however the OTel wire format can also carry content as
// Span Events when the OpenAI instrumentor path is taken (see
// lmnr/opentelemetry_lib/opentelemetry/instrumentation/openai/shared/
// chat_wrappers.py:407, 483, 507: span.add_event(name="llm.content.completion.chunk")).
// Regardless of which path is active, Span Events are silently dropped.
func TestSpanEventsCapture_DiagnosticIssue279(t *testing.T) {
	q := &fakeIngestQueue{}
	v := &fakeValidator{}
	h := handlers.NewOTLPHandler(q, v)

	// Build an OTLP request where prompt/completion content is placed in
	// Span Events (mimicking the wire format that Laminar emits when
	// content is captured as events).
	req := &coltracev1.ExportTraceServiceRequest{
		ResourceSpans: []*tracev1.ResourceSpans{
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
									// Only response metadata — no prompt/completion
									// content in Attributes.
									{Key: "gen_ai.response.id", Value: &commonv1.AnyValue{Value: &commonv1.AnyValue_StringValue{StringValue: "chatcmpl-abc123"}}},
									{Key: "gen_ai.request.model", Value: &commonv1.AnyValue{Value: &commonv1.AnyValue_StringValue{StringValue: "gpt-4o"}}},
									{Key: "gen_ai.system", Value: &commonv1.AnyValue{Value: &commonv1.AnyValue_StringValue{StringValue: "openai"}}},
									{Key: "gen_ai.usage.total_tokens", Value: &commonv1.AnyValue{Value: &commonv1.AnyValue_IntValue{IntValue: 150}}},
								},
								// Content is ONLY in Span Events — this is the shape
								// Laminar emits on the wire via span.add_event().
								Events: []*tracev1.Span_Event{
									{
										Name: "gen_ai.prompt.message",
										TimeUnixNano: uint64(time.Now().UnixNano()),
										Attributes: []*commonv1.KeyValue{
											{Key: "gen_ai.prompt.message.role", Value: &commonv1.AnyValue{Value: &commonv1.AnyValue_StringValue{StringValue: "user"}}},
											{Key: "gen_ai.prompt.message.content", Value: &commonv1.AnyValue{Value: &commonv1.AnyValue_StringValue{StringValue: "Hello, world!"}}},
										},
									},
									{
										Name: "gen_ai.completion.message",
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

	spans := postOTLPRequest(t, h, q, req, "application/x-protobuf")
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}

	// ISSUE #279 FIXED: Span Events with prompt/completion content are now
	// parsed by convertToResourceSpans and routed into Input/Output via
	// buildMessagesFromEvents in toRawMap.
	if !strings.Contains(spans[0].Input, `"role":"user"`) || !strings.Contains(spans[0].Input, `"content":"Hello, world!"`) {
		t.Errorf("input: got %q (expected user message captured from Span Events)", spans[0].Input)
	}
	if !strings.Contains(spans[0].Output, `"role":"assistant"`) || !strings.Contains(spans[0].Output, `"content":"Hi there! How can I help?"`) {
		t.Errorf("output: got %q (expected assistant message captured from Span Events)", spans[0].Output)
	}
}
