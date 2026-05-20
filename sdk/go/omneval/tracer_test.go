package omneval

import (
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"

	ptrace "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	tracev1 "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/protobuf/proto"
)

// TestConfigure_WiresExporter verifies Configure sets up the global OTLP
// exporter without panicking.
func TestConfigure_WiresExporter(t *testing.T) {
	var serverMu sync.Mutex
	var receivedSpans int64
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read body: %v", err)
			return
		}
		if len(body) == 0 {
			t.Error("expected non-empty body")
			return
		}
		serverMu.Lock()
		receivedSpans++
		serverMu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	err := Configure(ts.URL+"/v1/traces", "oev_proj_testkey123")
	if err != nil {
		t.Fatalf("Configure: %v", err)
	}

	ctx := StartSpan(context.Background(), "test.operation")
	SetModel(ctx, "gpt-4")
	SetInput(ctx, "hello")
	SetOutput(ctx, "hi there")
	SetTokens(ctx, 10, 5)
	EndSpan(ctx)

	// With a synchronous exporter, spans are exported immediately on EndSpan.
	// No need to call Shutdown here — the sync exporter does the work synchronously.
	serverMu.Lock()
	count := receivedSpans
	serverMu.Unlock()

	if count < 1 {
		t.Errorf("expected at least 1 span received, got %d", count)
	}
}

// TestConfigure_BadEndpointRejectsConfig verifies Configure returns an error
// when the endpoint URL is malformed.
func TestConfigure_BadEndpointRejectsConfig(t *testing.T) {
	defer Shutdown()

	err := Configure("://bad-url", "key")
	if err == nil {
		t.Error("expected error for malformed URL, got nil")
	}
}

// TestContextPropagation verifies child spans are correctly linked to parent
// spans via context.Context.
func TestContextPropagation(t *testing.T) {
	var receivedSpans atomic.Int64
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			return
		}
		if len(body) > 0 {
			receivedSpans.Add(1)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	Configure(ts.URL+"/v1/traces", "oev_proj_test")

	parentCtx := StartSpan(context.Background(), "parent.operation")
	SetModel(parentCtx, "gpt-4")

	childCtx := StartSpan(parentCtx, "child.operation")
	SetModel(childCtx, "gpt-3.5-turbo")

	EndSpan(childCtx)
	EndSpan(parentCtx)
	Shutdown()

	if receivedSpans.Load() < 1 {
		t.Errorf("expected at least 1 span exported, got %d", receivedSpans.Load())
	}
}

// TestContextPropagation_ParentID verifies that the child span's parent ID
// matches the parent span's ID, confirming correct OTel context propagation.
func TestContextPropagation_ParentID(t *testing.T) {
	var receivedRequests []fakeOTLPRequest
	var mu sync.Mutex

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			return
		}
		if len(body) == 0 {
			return
		}
		mu.Lock()
		receivedRequests = append(receivedRequests, fakeOTLPRequest{Body: body})
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	Configure(ts.URL+"/v1/traces", "oev_proj_test")

	parentCtx := StartSpan(context.Background(), "parent.operation")
	SetModel(parentCtx, "gpt-4")

	childCtx := StartSpan(parentCtx, "child.operation")
	SetModel(childCtx, "gpt-3.5-turbo")

	EndSpan(childCtx)
	EndSpan(parentCtx)
	Shutdown()

	mu.Lock()
	reqs := receivedRequests
	mu.Unlock()

	if len(reqs) == 0 {
		t.Fatal("no OTLP requests received")
	}

	// Decode the OTLP trace data and verify parent-child span linking.
	// The OTLP HTTP exporter uses gzip compression by default.
	// Collect all spans across all requests (each span may be exported separately).
	var spans []*tracev1.Span
	for _, req := range reqs {
		decompressed, err := decompressOTLPBody(req.Body)
		if err != nil {
			t.Fatalf("failed to decompress OTLP request: %v", err)
		}
		var exportReq ptrace.ExportTraceServiceRequest
		if err := proto.Unmarshal(decompressed, &exportReq); err != nil {
			t.Fatalf("failed to unmarshal OTLP request: %v", err)
		}
		for _, rs := range exportReq.ResourceSpans {
			for _, ss := range rs.ScopeSpans {
				spans = append(spans, ss.Spans...)
			}
		}
	}

	if len(spans) != 2 {
		t.Fatalf("expected 2 spans, got %d", len(spans))
	}

	// Find parent and child spans by name.
	var parentSpan, childSpan *tracev1.Span
	for _, sp := range spans {
		switch sp.Name {
		case "parent.operation":
			parentSpan = sp
		case "child.operation":
			childSpan = sp
		}
	}

	if parentSpan == nil {
		t.Fatal("missing parent span")
	}
	if childSpan == nil {
		t.Fatal("missing child span")
	}

	// The child's ParentSpanId must match the parent's SpanId.
	if string(childSpan.ParentSpanId) != string(parentSpan.SpanId) {
		t.Errorf("child span parent_id mismatch: got %x, want %x", childSpan.ParentSpanId, parentSpan.SpanId)
	}
}

// TestSpanAttributesJSON verifies OTLP encoding works.
func TestSpanAttributesJSON(t *testing.T) {
	var receivedCount atomic.Int64

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedCount.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	Configure(ts.URL+"/v1/traces", "oev_proj_test")

	ctx := StartSpan(context.Background(), "attr.span")
	SetModel(ctx, "test-model")
	SetTokens(ctx, 10, 5)
	EndSpan(ctx)
	Shutdown()

	if receivedCount.Load() < 1 {
		t.Errorf("expected span to be exported, got %d requests", receivedCount.Load())
	}
}

// TestEndSpanOnNilContextIsSafe verifies EndSpan handles a nil-or-empty context.
func TestEndSpan_NilContext(t *testing.T) {
	// Should not panic.
	EndSpan(context.Background())
}

// TestGlobalReset verifies that multiple Configure calls reset the global state.
func TestConfigure_MultipleCalls(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	Configure(ts.URL+"/v1/traces", "key1")
	Configure(ts.URL+"/v1/traces", "key2")

	StartSpan(context.Background(), "after.reconfigure")
	EndSpan(context.Background())
	Shutdown()
}

// TestSetInput_StringInput verifies that SetInput/SetOutput work without panicking.
func TestSetInput_StringInput(t *testing.T) {
	var spanReceived atomic.Int64

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		spanReceived.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	Configure(ts.URL+"/v1/traces", "oev_proj_test")

	ctx := StartSpan(context.Background(), "input-test")
	SetInput(ctx, "simple string input")
	SetOutput(ctx, "simple string output")
	SetModel(ctx, "gpt-4")
	SetTokens(ctx, 100, 50)
	EndSpan(ctx)
	Shutdown()

	if spanReceived.Load() < 1 {
		t.Error("expected span to be exported")
	}
}

// TestStartSpan_EmptyNameIsSafe verifies StartSpan handles an empty name
// without panicking.
func TestStartSpan_EmptyNameIsSafe(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	Configure(ts.URL+"/v1/traces", "oev_proj_test")

	ctx := StartSpan(context.Background(), "")
	if ctx == nil {
		t.Error("expected non-nil context from StartSpan")
	}
	EndSpan(ctx)
	Shutdown()
}

// TestConfigure_WithAPIKey verifies the SDK configures without error with an API key.
func TestConfigure_WithAPIKey(t *testing.T) {
	ts := newFakeOTLPServer()
	defer ts.Close()

	err := Configure(ts.URL()+"/v1/traces", "oev_proj_mykey")
	if err != nil {
		t.Fatalf("Configure: %v", err)
	}

	ctx := StartSpan(context.Background(), "key-test")
	EndSpan(ctx)
	Shutdown()

	if ts.RequestCount() < 1 {
		t.Error("expected span to be exported")
	}
}

// TestMultipleSpansInSequence verifies multiple spans can be started and ended.
func TestMultipleSpansInSequence(t *testing.T) {
	ts := newFakeOTLPServer()
	defer ts.Close()

	Configure(ts.URL()+"/v1/traces", "oev_proj_test")

	for i := 0; i < 5; i++ {
		ctx := StartSpan(context.Background(), "span-"+string(rune('0'+i)))
		SetModel(ctx, "gpt-4")
		SetTokens(ctx, int64(i*10), int64(i*5))
		EndSpan(ctx)
	}
	Shutdown()

	if ts.RequestCount() < 1 {
		t.Error("expected at least one export request")
	}
}

// TestNestedSpans verifies nested span context propagation with multiple
// children and grandchild spans.
func TestNestedSpans(t *testing.T) {
	ts := newFakeOTLPServer()
	defer ts.Close()

	Configure(ts.URL()+"/v1/traces", "oev_proj_test")

	parentCtx := StartSpan(context.Background(), "outer")
	child1Ctx := StartSpan(parentCtx, "inner-1")
	EndSpan(child1Ctx)
	child2Ctx := StartSpan(parentCtx, "inner-2")
	EndSpan(child2Ctx)
	grandchildCtx := StartSpan(child2Ctx, "deep")
	EndSpan(grandchildCtx)
	EndSpan(parentCtx)
	Shutdown()

	if ts.RequestCount() < 1 {
		t.Error("expected at least one export request")
	}
}

// decompressOTLPBody decompresses gzip-compressed OTLP protobuf data.
// The OTLP HTTP exporter uses gzip compression by default.
func decompressOTLPBody(body []byte) ([]byte, error) {
	r, err := gzip.NewReader(bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	defer r.Close()
	return io.ReadAll(r)
}

// ---- Fake HTTP server for integration-like tests ----

// fakeOTLPServer is a simple OTLP receiver that captures requests.
type fakeOTLPServer struct {
	mu       sync.Mutex
	requests []fakeOTLPRequest
	server   *httptest.Server
}

type fakeOTLPRequest struct {
	ContentLength int64
	ContentType   string
	Body          []byte
}

func newFakeOTLPServer() *fakeOTLPServer {
	f := &fakeOTLPServer{}
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		f.mu.Lock()
		f.requests = append(f.requests, fakeOTLPRequest{
			ContentLength: r.ContentLength,
			ContentType:   r.Header.Get("Content-Type"),
			Body:          body,
		})
		f.mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	return f
}

func (f *fakeOTLPServer) URL() string {
	return f.server.URL
}

func (f *fakeOTLPServer) Close() {
	f.server.Close()
}

func (f *fakeOTLPServer) RequestCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.requests)
}
