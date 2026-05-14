package handler

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/marcboeker/go-duckdb/v2"
	"github.com/zbloss/lantern/internal/domain"
)

const spansTableDDL = `
	CREATE TABLE spans (
		span_id        VARCHAR NOT NULL,
		trace_id       VARCHAR NOT NULL,
		parent_id      VARCHAR,
		project_id     VARCHAR NOT NULL,
		service_name   VARCHAR,
		name           VARCHAR,
		kind           VARCHAR,
		start_time     TIMESTAMPTZ NOT NULL,
		end_time       TIMESTAMPTZ,
		model          VARCHAR,
		input          JSON,
		output         JSON,
		input_tokens   BIGINT,
		output_tokens  BIGINT,
		cost_usd       DOUBLE,
		prompt_name    VARCHAR,
		prompt_version BIGINT,
		status_code    VARCHAR,
		status_message VARCHAR,
		attributes     JSON,
		PRIMARY KEY (trace_id, span_id)
	);
`

const scoresTableDDL = `
	CREATE TABLE scores (
		score_id       VARCHAR      NOT NULL PRIMARY KEY,
		span_id        VARCHAR      NOT NULL,
		trace_id       VARCHAR      NOT NULL,
		project_id     VARCHAR      NOT NULL,
		eval_name      VARCHAR,
		value          DOUBLE,
		reasoning      VARCHAR,
		judge_model    VARCHAR,
		prompt_name    VARCHAR,
		prompt_version BIGINT,
		created_at     TIMESTAMPTZ  NOT NULL
	);
`

func TestHandleSpansQuery_AuthRequired(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/spans/query", strings.NewReader(`{}`))
	w := httptest.NewRecorder()

	h := &SpanHandler{
		SessionStore: &FakeSessionStore{},
	}

	h.HandleSpansQuery(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestHandleSpansQuery_MissingBody(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/spans/query", nil)
	w := httptest.NewRecorder()

	h := &SpanHandler{
		SessionStore: &FakeSessionStore{projectID: "test-proj"},
	}

	h.HandleSpansQuery(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandleSpansQuery_MethodNotAllowed(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/spans/query", nil)
	w := httptest.NewRecorder()

	h := &SpanHandler{
		SessionStore: &FakeSessionStore{projectID: "test-proj"},
	}

	h.HandleSpansQuery(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusMethodNotAllowed)
	}
}

func TestHandleSpansQuery_InvalidCursor(t *testing.T) {
	body := strings.NewReader(`{
		"from": "2025-01-01T00:00:00Z",
		"to": "2025-01-02T00:00:00Z",
		"cursor": "invalid!!!"
	}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/spans/query", body)
	w := httptest.NewRecorder()

	h := &SpanHandler{
		SessionStore: &FakeSessionStore{projectID: "test-proj"},
	}

	h.HandleSpansQuery(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandleSpansQuery_FieldDecodingErrors(t *testing.T) {
	tests := []struct {
		name          string
		body          string
		wantErrorCode string
	}{
		{
			name:          "invalid from timestamp",
			body:          `{"from": "not-a-timestamp"}`,
			wantErrorCode: "invalid 'from' field: expected RFC 3339 timestamp",
		},
		{
			name:          "invalid to timestamp",
			body:          `{"to": 12345}`,
			wantErrorCode: "invalid 'to' field: expected RFC 3339 timestamp",
		},
		{
			name:          "cursor as number",
			body:          `{"cursor": 42}`,
			wantErrorCode: "invalid 'cursor' field: expected string",
		},
		{
			name:          "limit as string",
			body:          `{"limit": "fast"}`,
			wantErrorCode: "invalid 'limit' field: expected number",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			body := strings.NewReader(tc.body)
			req := httptest.NewRequest(http.MethodPost, "/api/v1/spans/query", body)
			w := httptest.NewRecorder()

			h := &SpanHandler{
				SessionStore: &FakeSessionStore{projectID: "test-proj"},
			}

			h.HandleSpansQuery(w, req)

			if w.Code != http.StatusBadRequest {
				t.Errorf("status: got %d, want %d", w.Code, http.StatusBadRequest)
				return
			}

			contentType := w.Header().Get("Content-Type")
			if contentType != "application/json" {
				t.Errorf("Content-Type: got %q, want %q", contentType, "application/json")
				return
			}

			var resp map[string]string
			if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
				t.Fatalf("decode response: %v (raw: %q)", err, w.Body.String())
			}

			if resp["error"] != tc.wantErrorCode {
				t.Errorf("error: got %q, want %q", resp["error"], tc.wantErrorCode)
			}
		})
	}
}

func TestHandleSpansQuery_WithDatabase(t *testing.T) {
	// Create a temp DuckDB file.
	tmpDir, err := os.MkdirTemp("", "lantern-handler-test")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)
	tmpPath := tmpDir + "/test.duckdb"

	db, err := sql.Open("duckdb", tmpPath)
	if err != nil {
		t.Fatalf("open duckdb: %v", err)
	}
	defer db.Close()

	// Create the spans table.
	if _, err := db.ExecContext(context.Background(), `
		CREATE TABLE spans (
			span_id        VARCHAR NOT NULL,
			trace_id       VARCHAR NOT NULL,
			parent_id      VARCHAR,
			project_id     VARCHAR NOT NULL,
			service_name   VARCHAR,
			name           VARCHAR,
			kind           VARCHAR,
			start_time     TIMESTAMPTZ NOT NULL,
			end_time       TIMESTAMPTZ,
			model          VARCHAR,
			input          JSON,
			output         JSON,
			input_tokens   BIGINT,
			output_tokens  BIGINT,
			cost_usd       DOUBLE,
			prompt_name    VARCHAR,
			prompt_version BIGINT,
			status_code    VARCHAR,
			status_message VARCHAR,
			attributes     JSON,
			PRIMARY KEY (trace_id, span_id)
		);
	`); err != nil {
		t.Fatalf("create table: %v", err)
	}

	// Insert a test span.
	baseTime := time.Date(2025, 1, 1, 10, 0, 0, 0, time.UTC)
	if _, err := db.ExecContext(context.Background(),
		`INSERT INTO spans (span_id, trace_id, project_id, model, start_time, end_time) VALUES (?, ?, ?, ?, ?, ?)`,
		"span-001", "trace-abc", "test-proj", "gpt-4",
		baseTime, baseTime.Add(10*time.Second)); err != nil {
		t.Fatalf("insert span: %v", err)
	}

	// Create handler with real DB.
	h := &SpanHandler{
		DB:           db,
		SessionStore: &FakeSessionStore{projectID: "test-proj"},
	}

	// Make request.
	body := strings.NewReader(`{
		"from": "2025-01-01T00:00:00Z",
		"to": "2025-01-02T00:00:00Z",
		"limit": 50
	}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/spans/query", body)
	w := httptest.NewRecorder()

	h.HandleSpansQuery(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status: got %d, want %d\nbody: %s", w.Code, http.StatusOK, w.Body.String())
		return
	}

	// Parse response.
	var resp struct {
		Spans []map[string]any `json:"spans"`
		Next  string           `json:"next,omitempty"`
		Limit int              `json:"limit"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if len(resp.Spans) != 1 {
		t.Errorf("spans: got %d, want 1", len(resp.Spans))
	}

	if resp.Limit != 50 {
		t.Errorf("limit: got %d, want 50", resp.Limit)
	}

	// Should not have a next cursor since we only have 1 span and limit is 50.
	if resp.Next != "" {
		t.Errorf("next: got %q, want empty", resp.Next)
	}
}

// serveTraceDetail is a test helper that routes a request through ServeMux
// so that path parameters are properly resolved.
func serveTraceDetail(h *SpanHandler, method, url string, body io.Reader) *httptest.ResponseRecorder {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/traces/{traceId}", h.HandleTraceDetail)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(method, url, body)
	mux.ServeHTTP(w, req)
	return w
}

func TestHandleTraceDetail_MissingTraceID(t *testing.T) {
	// When path doesn't match /api/v1/traces/{traceId}, ServeMux returns 404.
	w := serveTraceDetail(&SpanHandler{
		SessionStore: &FakeSessionStore{projectID: "test-proj"},
	}, http.MethodGet, "/api/v1/traces/", nil)

	if w.Code != http.StatusNotFound {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestHandleTraceDetail_MethodNotAllowed(t *testing.T) {
	w := serveTraceDetail(&SpanHandler{
		SessionStore: &FakeSessionStore{projectID: "test-proj"},
	}, http.MethodPost, "/api/v1/traces/abc123", strings.NewReader(`{}`))

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusMethodNotAllowed)
	}
}

// callHandlerDirect invokes the handler directly (bypassing ServeMux)
// so we can test the handler's own error paths (method, missing ID, etc.).
func callHandlerDirect(h *SpanHandler, method, url string, body io.Reader) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(method, url, body)
	h.HandleTraceDetail(w, req)
	return w
}

func TestHandleTraceDetail_HandlerReturnsJSON(t *testing.T) {
	// Test that the handler itself (not ServeMux) returns JSON for error cases.
	h := &SpanHandler{
		SessionStore: &FakeSessionStore{projectID: "test-proj"},
	}

	tests := []struct {
		method    string
		wantCode  int
		wantError string
	}{
		{method: http.MethodPost, wantCode: http.StatusMethodNotAllowed, wantError: "method not allowed"},
	}

	for _, tc := range tests {
		w := callHandlerDirect(h, tc.method, "/api/v1/traces/abc123", strings.NewReader(`{}`))

		if w.Code != tc.wantCode {
			t.Errorf("%s /api/v1/traces/abc123: status got %d, want %d", tc.method, w.Code, tc.wantCode)
			continue
		}

		contentType := w.Header().Get("Content-Type")
		if contentType != "application/json" {
			t.Errorf("%s: Content-Type got %q, want %q", tc.method, contentType, "application/json")
			continue
		}

		var resp map[string]string
		if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
			t.Fatalf("%s: decode response: %v (raw: %q)", tc.method, err, w.Body.String())
		}
		if resp["error"] != tc.wantError {
			t.Errorf("%s: error got %q, want %q", tc.method, resp["error"], tc.wantError)
		}
	}
}

func TestHandleTraceDetail_NotFound(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "lantern-trace-test")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)
	tmpPath := tmpDir + "/test.duckdb"

	db, err := sql.Open("duckdb", tmpPath)
	if err != nil {
		t.Fatalf("open duckdb: %v", err)
	}
	defer db.Close()

	// Create spans table.
	if _, err := db.ExecContext(context.Background(), spansTableDDL); err != nil {
		t.Fatalf("create table: %v", err)
	}

	h := &SpanHandler{
		DB:           db,
		SessionStore: &FakeSessionStore{projectID: "test-proj"},
	}

	w := serveTraceDetail(h, http.MethodGet, "/api/v1/traces/nonexistent", nil)

	if w.Code != http.StatusNotFound {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusNotFound)
	}

	// Verify JSON content type.
	contentType := w.Header().Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("Content-Type: got %q, want %q", contentType, "application/json")
	}

	// Verify JSON body shape: {"error": "trace not found"}.
	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response body: %v (raw: %q)", err, w.Body.String())
	}
	if resp["error"] != "trace not found" {
		t.Errorf("error body: got %q, want %q", resp["error"], "trace not found")
	}
}

func TestHandleTraceDetail_SingleSpan(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "lantern-trace-test")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)
	tmpPath := tmpDir + "/test.duckdb"

	db, err := sql.Open("duckdb", tmpPath)
	if err != nil {
		t.Fatalf("open duckdb: %v", err)
	}
	defer db.Close()

	if _, err := db.ExecContext(context.Background(), spansTableDDL); err != nil {
		t.Fatalf("create table: %v", err)
	}

	baseTime := time.Date(2025, 1, 1, 10, 0, 0, 0, time.UTC)
	if _, err := db.ExecContext(context.Background(),
		`INSERT INTO spans (span_id, trace_id, parent_id, project_id, name, model, start_time, end_time, input, output, input_tokens, output_tokens, cost_usd, status_code) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"span-001", "trace-abc", "", "test-proj", "root span", "gpt-4",
		baseTime, baseTime.Add(10*time.Second), `[{"role":"user","content":"hello"}]`, `[{"role":"assistant","content":"hi"}]`, 100, 50, 0.001, "OK"); err != nil {
		t.Fatalf("insert span: %v", err)
	}

	h := &SpanHandler{
		DB:           db,
		SessionStore: &FakeSessionStore{projectID: "test-proj"},
	}

	w := serveTraceDetail(h, http.MethodGet, "/api/v1/traces/trace-abc", nil)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want %d\nbody: %s", w.Code, http.StatusOK, w.Body.String())
	}

	var trace struct {
		TraceID   string            `json:"trace_id"`
		ProjectID string            `json:"project_id"`
		RootSpan  json.RawMessage   `json:"root_span"`
		Spans     []json.RawMessage `json:"spans"`
	}
	if err := json.NewDecoder(w.Body).Decode(&trace); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if trace.TraceID != "trace-abc" {
		t.Errorf("trace_id: got %q, want %q", trace.TraceID, "trace-abc")
	}
	if trace.ProjectID != "test-proj" {
		t.Errorf("project_id: got %q, want %q", trace.ProjectID, "test-proj")
	}
	if len(trace.Spans) != 1 {
		t.Errorf("spans count: got %d, want 1", len(trace.Spans))
	}
}

func TestHandleTraceDetail_MultiLevelTree(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "lantern-trace-test")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)
	tmpPath := tmpDir + "/test.duckdb"

	db, err := sql.Open("duckdb", tmpPath)
	if err != nil {
		t.Fatalf("open duckdb: %v", err)
	}
	defer db.Close()

	if _, err := db.ExecContext(context.Background(), spansTableDDL); err != nil {
		t.Fatalf("create table: %v", err)
	}

	baseTime := time.Date(2025, 1, 1, 10, 0, 0, 0, time.UTC)

	// Insert a 3-level tree: root -> child1 -> grandchild
	_, err = db.ExecContext(context.Background(),
		`INSERT INTO spans (span_id, trace_id, parent_id, project_id, name, start_time, end_time) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		"span-root", "trace-xyz", "", "test-proj", "root", baseTime, baseTime.Add(100*time.Millisecond))
	if err != nil {
		t.Fatalf("insert root: %v", err)
	}
	_, err = db.ExecContext(context.Background(),
		`INSERT INTO spans (span_id, trace_id, parent_id, project_id, name, start_time, end_time) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		"span-child", "trace-xyz", "span-root", "test-proj", "child", baseTime.Add(10*time.Millisecond), baseTime.Add(50*time.Millisecond))
	if err != nil {
		t.Fatalf("insert child: %v", err)
	}
	_, err = db.ExecContext(context.Background(),
		`INSERT INTO spans (span_id, trace_id, parent_id, project_id, name, start_time, end_time) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		"span-grandchild", "trace-xyz", "span-child", "test-proj", "grandchild", baseTime.Add(20*time.Millisecond), baseTime.Add(40*time.Millisecond))
	if err != nil {
		t.Fatalf("insert grandchild: %v", err)
	}

	h := &SpanHandler{
		DB:           db,
		SessionStore: &FakeSessionStore{projectID: "test-proj"},
	}

	w := serveTraceDetail(h, http.MethodGet, "/api/v1/traces/trace-xyz", nil)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want %d\nbody: %s", w.Code, http.StatusOK, w.Body.String())
	}

	var trace struct {
		TraceID  string      `json:"trace_id"`
		RootSpan *treeSpan   `json:"root_span"`
		Spans    []*treeSpan `json:"spans"`
	}
	if err := json.NewDecoder(w.Body).Decode(&trace); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if trace.TraceID != "trace-xyz" {
		t.Errorf("trace_id: got %q, want %q", trace.TraceID, "trace-xyz")
	}

	// Verify root span has exactly one child.
	if trace.RootSpan == nil {
		t.Fatal("root_span is nil")
	}
	if len(trace.RootSpan.Children) != 1 {
		t.Errorf("root.children: got %d, want 1", len(trace.RootSpan.Children))
	}

	// Verify grandchild is child of child.
	child := trace.RootSpan.Children[0]
	if len(child.Children) != 1 {
		t.Errorf("child.children: got %d, want 1", len(child.Children))
	}
	if len(child.Children) > 0 && child.Children[0].Name != "grandchild" {
		t.Errorf("grandchild.name: got %q, want %q", child.Children[0].Name, "grandchild")
	}
}

func TestHandleTraceDetail_SiblingChildren(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "lantern-trace-test")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)
	tmpPath := tmpDir + "/test.duckdb"

	db, err := sql.Open("duckdb", tmpPath)
	if err != nil {
		t.Fatalf("open duckdb: %v", err)
	}
	defer db.Close()

	if _, err := db.ExecContext(context.Background(), spansTableDDL); err != nil {
		t.Fatalf("create table: %v", err)
	}

	baseTime := time.Date(2025, 1, 1, 10, 0, 0, 0, time.UTC)

	// Root with two children.
	_, err = db.ExecContext(context.Background(),
		`INSERT INTO spans (span_id, trace_id, parent_id, project_id, name, start_time, end_time) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		"span-root", "trace-sib", "", "test-proj", "root", baseTime, baseTime.Add(100*time.Millisecond))
	if err != nil {
		t.Fatalf("insert root: %v", err)
	}
	_, err = db.ExecContext(context.Background(),
		`INSERT INTO spans (span_id, trace_id, parent_id, project_id, name, start_time, end_time) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		"span-a", "trace-sib", "span-root", "test-proj", "child-a", baseTime.Add(10*time.Millisecond), baseTime.Add(50*time.Millisecond))
	if err != nil {
		t.Fatalf("insert child-a: %v", err)
	}
	_, err = db.ExecContext(context.Background(),
		`INSERT INTO spans (span_id, trace_id, parent_id, project_id, name, start_time, end_time) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		"span-b", "trace-sib", "span-root", "test-proj", "child-b", baseTime.Add(20*time.Millisecond), baseTime.Add(60*time.Millisecond))
	if err != nil {
		t.Fatalf("insert child-b: %v", err)
	}

	h := &SpanHandler{
		DB:           db,
		SessionStore: &FakeSessionStore{projectID: "test-proj"},
	}

	w := serveTraceDetail(h, http.MethodGet, "/api/v1/traces/trace-sib", nil)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want %d\nbody: %s", w.Code, http.StatusOK, w.Body.String())
	}

	var trace struct {
		RootSpan *treeSpan `json:"root_span"`
	}
	if err := json.NewDecoder(w.Body).Decode(&trace); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if trace.RootSpan == nil {
		t.Fatal("root_span is nil")
	}
	if len(trace.RootSpan.Children) != 2 {
		t.Errorf("root.children: got %d, want 2", len(trace.RootSpan.Children))
	}

	// Both children should be present.
	childNames := make(map[string]bool)
	for _, c := range trace.RootSpan.Children {
		childNames[c.Name] = true
	}
	if !childNames["child-a"] || !childNames["child-b"] {
		t.Errorf("expected both child-a and child-b, got %v", childNames)
	}
}

func TestHandleTraceDetail_NoParentFallback(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "lantern-trace-test")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)
	tmpPath := tmpDir + "/test.duckdb"

	db, err := sql.Open("duckdb", tmpPath)
	if err != nil {
		t.Fatalf("open duckdb: %v", err)
	}
	defer db.Close()

	if _, err := db.ExecContext(context.Background(), spansTableDDL); err != nil {
		t.Fatalf("create table: %v", err)
	}

	baseTime := time.Date(2025, 1, 1, 10, 0, 0, 0, time.UTC)

	// All spans have non-empty parent_id — no root. The first span (by start_time) becomes root.
	_, err = db.ExecContext(context.Background(),
		`INSERT INTO spans (span_id, trace_id, parent_id, project_id, name, start_time, end_time) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		"span-a", "trace-noroot", "span-b", "test-proj", "span-a", baseTime, baseTime.Add(50*time.Millisecond))
	if err != nil {
		t.Fatalf("insert span-a: %v", err)
	}
	_, err = db.ExecContext(context.Background(),
		`INSERT INTO spans (span_id, trace_id, parent_id, project_id, name, start_time, end_time) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		"span-b", "trace-noroot", "", "test-proj", "span-b", baseTime.Add(10*time.Millisecond), baseTime.Add(100*time.Millisecond))
	if err != nil {
		t.Fatalf("insert span-b: %v", err)
	}

	h := &SpanHandler{
		DB:           db,
		SessionStore: &FakeSessionStore{projectID: "test-proj"},
	}

	w := serveTraceDetail(h, http.MethodGet, "/api/v1/traces/trace-noroot", nil)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want %d\nbody: %s", w.Code, http.StatusOK, w.Body.String())
	}

	var trace struct {
		RootSpan *treeSpan `json:"root_span"`
	}
	if err := json.NewDecoder(w.Body).Decode(&trace); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	// Should still return a trace with root span (span-b which has empty parent_id).
	if trace.RootSpan == nil {
		t.Fatal("root_span is nil")
	}
	if trace.RootSpan.SpanID != "span-b" {
		t.Errorf("root span_id: got %q, want %q", trace.RootSpan.SpanID, "span-b")
	}
}

func TestHandleTraceDetail_ScoresAttached(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "lantern-trace-test")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)
	tmpPath := tmpDir + "/test.duckdb"

	db, err := sql.Open("duckdb", tmpPath)
	if err != nil {
		t.Fatalf("open duckdb: %v", err)
	}
	defer db.Close()

	if _, err := db.ExecContext(context.Background(), spansTableDDL); err != nil {
		t.Fatalf("create table: %v", err)
	}
	if _, err := db.ExecContext(context.Background(), scoresTableDDL); err != nil {
		t.Fatalf("create scores table: %v", err)
	}

	baseTime := time.Date(2025, 1, 1, 10, 0, 0, 0, time.UTC)

	_, err = db.ExecContext(context.Background(),
		`INSERT INTO spans (span_id, trace_id, parent_id, project_id, name, model, start_time, end_time) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		"span-001", "trace-scores", "", "test-proj", "llm-call", "gpt-4", baseTime, baseTime.Add(5*time.Second))
	if err != nil {
		t.Fatalf("insert span: %v", err)
	}
	_, err = db.ExecContext(context.Background(),
		`INSERT INTO scores (score_id, span_id, trace_id, project_id, eval_name, value, reasoning, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		"score-001", "span-001", "trace-scores", "test-proj", "helpfulness", 0.9, "Very helpful response", baseTime)
	if err != nil {
		t.Fatalf("insert score: %v", err)
	}

	h := &SpanHandler{
		DB:           db,
		SessionStore: &FakeSessionStore{projectID: "test-proj"},
	}

	w := serveTraceDetail(h, http.MethodGet, "/api/v1/traces/trace-scores", nil)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want %d\nbody: %s", w.Code, http.StatusOK, w.Body.String())
	}

	var trace struct {
		RootSpan *treeSpan `json:"root_span"`
	}
	if err := json.NewDecoder(w.Body).Decode(&trace); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if trace.RootSpan == nil {
		t.Fatal("root_span is nil")
	}
	if len(trace.RootSpan.Scores) != 1 {
		t.Errorf("root.scores: got %d, want 1", len(trace.RootSpan.Scores))
	} else {
		if trace.RootSpan.Scores[0].EvalName != "helpfulness" {
			t.Errorf("score eval_name: got %q, want %q", trace.RootSpan.Scores[0].EvalName, "helpfulness")
		}
		if trace.RootSpan.Scores[0].Value != 0.9 {
			t.Errorf("score value: got %f, want 0.9", trace.RootSpan.Scores[0].Value)
		}
	}
}

func TestHandleTraceDetail_ProjectIsolation(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "lantern-trace-test")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)
	tmpPath := tmpDir + "/test.duckdb"

	db, err := sql.Open("duckdb", tmpPath)
	if err != nil {
		t.Fatalf("open duckdb: %v", err)
	}
	defer db.Close()

	if _, err := db.ExecContext(context.Background(), spansTableDDL); err != nil {
		t.Fatalf("create table: %v", err)
	}

	baseTime := time.Date(2025, 1, 1, 10, 0, 0, 0, time.UTC)

	// Insert spans for different projects with the same trace_id.
	_, err = db.ExecContext(context.Background(),
		`INSERT INTO spans (span_id, trace_id, parent_id, project_id, name, start_time, end_time) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		"span-001", "trace-shared", "", "proj-a", "span-a", baseTime, baseTime.Add(5*time.Second))
	if err != nil {
		t.Fatalf("insert span-a: %v", err)
	}
	_, err = db.ExecContext(context.Background(),
		`INSERT INTO spans (span_id, trace_id, parent_id, project_id, name, start_time, end_time) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		"span-002", "trace-shared", "", "proj-b", "span-b", baseTime.Add(10*time.Millisecond), baseTime.Add(5*time.Second))
	if err != nil {
		t.Fatalf("insert span-b: %v", err)
	}

	// Query as proj-a — should only see proj-a's span.
	h := &SpanHandler{
		DB:           db,
		SessionStore: &FakeSessionStore{projectID: "proj-a"},
	}

	w := serveTraceDetail(h, http.MethodGet, "/api/v1/traces/trace-shared", nil)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want %d\nbody: %s", w.Code, http.StatusOK, w.Body.String())
	}

	var trace struct {
		ProjectID string     `json:"project_id"`
		Spans     []treeSpan `json:"spans"`
	}
	if err := json.NewDecoder(w.Body).Decode(&trace); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if trace.ProjectID != "proj-a" {
		t.Errorf("project_id: got %q, want %q", trace.ProjectID, "proj-a")
	}
	// Only the span from proj-a should be returned.
	if len(trace.Spans) != 1 {
		t.Errorf("spans count: got %d, want 1", len(trace.Spans))
	}
}

func TestBuildTraceTree_EmptySpans(t *testing.T) {
	trace := buildTraceTree([]*domain.Span{}, nil, "", "")
	if trace.TraceID != "" {
		t.Errorf("trace_id: got %q, want empty", trace.TraceID)
	}
	if trace.RootSpan != nil {
		t.Error("root_span should be nil for empty spans")
	}
}

func TestBuildTraceTree_SingleSpan(t *testing.T) {
	baseTime := time.Date(2025, 1, 1, 10, 0, 0, 0, time.UTC)
	spans := []*domain.Span{
		{SpanID: "span-001", TraceID: "trace-1", ParentID: "", Name: "root", StartTime: baseTime},
	}

	trace := buildTraceTree(spans, nil, "", "")

	if trace.TraceID != "trace-1" {
		t.Errorf("trace_id: got %q, want %q", trace.TraceID, "trace-1")
	}
	if trace.RootSpan == nil {
		t.Fatal("root_span is nil")
	}
	if trace.RootSpan.SpanID != "span-001" {
		t.Errorf("root span_id: got %q, want %q", trace.RootSpan.SpanID, "span-001")
	}
	if len(trace.RootSpan.Children) != 0 {
		t.Errorf("root.children: got %d, want 0", len(trace.RootSpan.Children))
	}
}

func TestBuildTraceTree_NestedChildren(t *testing.T) {
	baseTime := time.Date(2025, 1, 1, 10, 0, 0, 0, time.UTC)
	spans := []*domain.Span{
		{SpanID: "root", TraceID: "trace-1", ParentID: "", Name: "root", StartTime: baseTime},
		{SpanID: "child1", TraceID: "trace-1", ParentID: "root", Name: "child1", StartTime: baseTime.Add(time.Second)},
		{SpanID: "child2", TraceID: "trace-1", ParentID: "root", Name: "child2", StartTime: baseTime.Add(2 * time.Second)},
		{SpanID: "grandchild", TraceID: "trace-1", ParentID: "child1", Name: "grandchild", StartTime: baseTime.Add(3 * time.Second)},
	}

	trace := buildTraceTree(spans, nil, "", "")

	if trace.RootSpan == nil {
		t.Fatal("root_span is nil")
	}
	if trace.RootSpan.SpanID != "root" {
		t.Errorf("root span_id: got %q, want %q", trace.RootSpan.SpanID, "root")
	}
	if len(trace.RootSpan.Children) != 2 {
		t.Errorf("root.children: got %d, want 2", len(trace.RootSpan.Children))
	}

	// Find child1 and verify grandchild is its child.
	var child1 *domain.Span
	for _, c := range trace.RootSpan.Children {
		if c.SpanID == "child1" {
			child1 = c
			break
		}
	}
	if child1 == nil {
		t.Fatal("child1 not found in root.children")
	}
	if len(child1.Children) != 1 {
		t.Errorf("child1.children: got %d, want 1", len(child1.Children))
	}
	if len(child1.Children) > 0 && child1.Children[0].SpanID != "grandchild" {
		t.Errorf("grandchild span_id: got %q, want %q", child1.Children[0].SpanID, "grandchild")
	}

	// Verify child2 has no children.
	var child2 *domain.Span
	for _, c := range trace.RootSpan.Children {
		if c.SpanID == "child2" {
			child2 = c
			break
		}
	}
	if child2 == nil {
		t.Fatal("child2 not found in root.children")
	}
	if len(child2.Children) != 0 {
		t.Errorf("child2.children: got %d, want 0", len(child2.Children))
	}
}

func TestBuildTraceTree_AllMissingParents(t *testing.T) {
	// All spans have non-empty parent_id pointing to spans outside the set.
	baseTime := time.Date(2025, 1, 1, 10, 0, 0, 0, time.UTC)
	spans := []*domain.Span{
		{SpanID: "span-a", TraceID: "trace-1", ParentID: "missing-1", Name: "a", StartTime: baseTime},
		{SpanID: "span-b", TraceID: "trace-1", ParentID: "missing-2", Name: "b", StartTime: baseTime.Add(time.Second)},
	}

	trace := buildTraceTree(spans, nil, "", "")

	// With no valid root, the first span should be used as root.
	if trace.RootSpan == nil {
		t.Fatal("root_span is nil")
	}
	if trace.RootSpan.SpanID != "span-a" {
		t.Errorf("root span_id: got %q, want %q", trace.RootSpan.SpanID, "span-a")
	}
}

// treeSpan is a flattened version of domain.Span for JSON unmarshaling in tests.
type treeSpan struct {
	SpanID   string      `json:"span_id"`
	Name     string      `json:"name"`
	Children []treeSpan  `json:"children"`
	Scores   []spanScore `json:"scores"`
}

type spanScore struct {
	EvalName  string  `json:"eval_name"`
	Value     float64 `json:"value"`
	Reasoning string  `json:"reasoning"`
}

func TestHandleSpansQuery_FiltersAsObject(t *testing.T) {
	body := strings.NewReader(`{"filters": {"model": "gpt-4o"}}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/spans/query", body)
	w := httptest.NewRecorder()

	h := &SpanHandler{
		SessionStore: &FakeSessionStore{projectID: "test-proj"},
	}

	h.HandleSpansQuery(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusBadRequest)
	}

	contentType := w.Header().Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("Content-Type: got %q, want %q", contentType, "application/json")
	}

	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v (raw: %q)", err, w.Body.String())
	}

	errorMsg := resp["error"]
	if errorMsg == "" {
		t.Fatal("expected error message in response body")
	}
	if !strings.Contains(errorMsg, "filters must be an array") {
		t.Errorf("error should mention filters must be an array, got: %q", errorMsg)
	}
}

func TestHandleSpansQuery_UnknownField(t *testing.T) {
	body := strings.NewReader(`{
		"from": "2025-01-01T00:00:00Z",
		"to": "2025-01-02T00:00:00Z",
		"filters": [{"field": "nonexistent_field", "op": "eq", "value": "x"}]
	}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/spans/query", body)
	w := httptest.NewRecorder()

	h := &SpanHandler{
		SessionStore: &FakeSessionStore{projectID: "test-proj"},
	}

	h.HandleSpansQuery(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusBadRequest)
	}

	contentType := w.Header().Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("Content-Type: got %q, want %q", contentType, "application/json")
	}

	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v (raw: %q)", err, w.Body.String())
	}

	errorMsg := resp["error"]
	if errorMsg == "" {
		t.Fatal("expected error message in response body")
	}
	if !strings.Contains(errorMsg, "accepted fields") {
		t.Errorf("error should mention accepted fields, got: %q", errorMsg)
	}
	if !strings.Contains(errorMsg, "nonexistent_field") {
		t.Errorf("error should mention the unknown field name, got: %q", errorMsg)
	}
}

func TestHandleSpansQuery_UnknownOp(t *testing.T) {
	body := strings.NewReader(`{
		"from": "2025-01-01T00:00:00Z",
		"to": "2025-01-02T00:00:00Z",
		"filters": [{"field": "model", "op": "regex", "value": "gpt.*"}]
	}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/spans/query", body)
	w := httptest.NewRecorder()

	h := &SpanHandler{
		SessionStore: &FakeSessionStore{projectID: "test-proj"},
	}

	h.HandleSpansQuery(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusBadRequest)
	}

	contentType := w.Header().Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("Content-Type: got %q, want %q", contentType, "application/json")
	}

	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v (raw: %q)", err, w.Body.String())
	}

	errorMsg := resp["error"]
	if errorMsg == "" {
		t.Fatal("expected error message in response body")
	}
	if !strings.Contains(errorMsg, "accepted operators") {
		t.Errorf("error should mention accepted operators, got: %q", errorMsg)
	}
	if !strings.Contains(errorMsg, "regex") {
		t.Errorf("error should mention the unknown operator, got: %q", errorMsg)
	}
}

func TestHandleSpansQuery_FiltersAsNumber(t *testing.T) {
	body := strings.NewReader(`{"filters": 42}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/spans/query", body)
	w := httptest.NewRecorder()

	h := &SpanHandler{
		SessionStore: &FakeSessionStore{projectID: "test-proj"},
	}

	h.HandleSpansQuery(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusBadRequest)
	}

	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v (raw: %q)", err, w.Body.String())
	}
	if !strings.Contains(resp["error"], "filters must be an array") {
		t.Errorf("error should mention filters must be an array, got: %q", resp["error"])
	}
}

func TestHandleSpansQuery_FiltersAsString(t *testing.T) {
	body := strings.NewReader(`{"filters": "model eq gpt-4"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/spans/query", body)
	w := httptest.NewRecorder()

	h := &SpanHandler{
		SessionStore: &FakeSessionStore{projectID: "test-proj"},
	}

	h.HandleSpansQuery(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusBadRequest)
	}

	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v (raw: %q)", err, w.Body.String())
	}
	if !strings.Contains(resp["error"], "filters must be an array") {
		t.Errorf("error should mention filters must be an array, got: %q", resp["error"])
	}
}

// FakeSessionStore is a test fake implementing SessionStore.
type FakeSessionStore struct {
	projectID string
}

func (f *FakeSessionStore) ProjectID(r *http.Request) (string, bool) {
	if f.projectID == "" {
		return "", false
	}
	return f.projectID, true
}

func (f *FakeSessionStore) ListProjects(r *http.Request) ([]*domain.Project, error) {
	if f.projectID == "" {
		return nil, fmt.Errorf("unauthenticated")
	}
	return []*domain.Project{{ProjectID: f.projectID}}, nil
}
