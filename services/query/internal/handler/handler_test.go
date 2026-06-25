package handler

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/duckdb/duckdb-go/v2"
	"github.com/omneval/omneval/internal/domain"
	_ "github.com/omneval/omneval/internal/duckdbfix"
	"github.com/omneval/omneval/internal/laketest"
	"github.com/omneval/omneval/services/query/internal/auth"
	"github.com/omneval/omneval/services/query/internal/cursor"
	"github.com/omneval/omneval/services/query/internal/query"
	"github.com/omneval/omneval/services/query/internal/querybuild"
)

const spansTableDDL = `
	CREATE TABLE spans (
		span_id         VARCHAR NOT NULL,
		trace_id        VARCHAR NOT NULL,
		parent_id       VARCHAR,
		conversation_id VARCHAR,
		project_id      VARCHAR NOT NULL,
		service_name    VARCHAR,
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
	CREATE SCHEMA IF NOT EXISTS lake;
	CREATE VIEW lake.spans AS SELECT * FROM main.spans;
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
	CREATE SCHEMA IF NOT EXISTS lake;
	CREATE VIEW lake.scores AS SELECT * FROM main.scores;
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
		QueryBuilder: &querybuild.QueryBuilder{},
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
	tmpDir, err := os.MkdirTemp("", "omneval-handler-test")
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
			span_id         VARCHAR NOT NULL,
			trace_id        VARCHAR NOT NULL,
			parent_id       VARCHAR,
			conversation_id VARCHAR,
			project_id      VARCHAR NOT NULL,
			service_name    VARCHAR,
			name            VARCHAR,
			kind            VARCHAR,
			start_time      TIMESTAMPTZ NOT NULL,
			end_time        TIMESTAMPTZ,
			model           VARCHAR,
			input           JSON,
			output          JSON,
			input_tokens    BIGINT,
			output_tokens   BIGINT,
			cost_usd        DOUBLE,
			prompt_name     VARCHAR,
			prompt_version  BIGINT,
			status_code     VARCHAR,
			status_message  VARCHAR,
			attributes      JSON,
			PRIMARY KEY (trace_id, span_id)
		);
	`); err != nil {
		t.Fatalf("create table: %v", err)
	}

	// SpanHandler reads lake.spans (ADR-0004); create a "lake" schema with a
	// view over the spans table to stand in for the Lake.
	if _, err := db.ExecContext(context.Background(),
		`CREATE SCHEMA lake; CREATE VIEW lake.spans AS SELECT * FROM main.spans;`); err != nil {
		t.Fatalf("create lake schema: %v", err)
	}

	// Insert a test span.
	baseTime := time.Date(2025, 1, 1, 10, 0, 0, 0, time.UTC)
	if _, err := db.ExecContext(context.Background(),
		`INSERT INTO spans (span_id, trace_id, project_id, model, start_time, end_time) VALUES (?, ?, ?, ?, ?, ?)`,
		"span-001", "trace-abc", "test-proj", "gpt-4",
		baseTime, baseTime.Add(10*time.Second)); err != nil {
		t.Fatalf("insert span: %v", err)
	}

	// Create handler with real DB and QueryBuilder.
	h := &SpanHandler{
		Lake:         db,
		SessionStore: &FakeSessionStore{projectID: "test-proj"},
		QueryBuilder: &querybuild.QueryBuilder{Lake: db},
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

// TestHandleSpansQuery_StatusCodeFilter covers the Traces page "Status Code"
// filter (issue #139): given spans whose status_code is already populated as
// "OK", "ERROR", "UNSET", or left unset (NULL), filtering by status_code
// should return exactly the matching subset — and filtering by "UNSET"
// should also match the NULL row, since pre-#135 data never populates
// status_code at all.
func TestHandleSpansQuery_StatusCodeFilter(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "omneval-handler-test")
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
	insert := func(spanID, traceID string, statusCode any) {
		if _, err := db.ExecContext(context.Background(),
			`INSERT INTO spans (span_id, trace_id, project_id, model, start_time, end_time, status_code) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			spanID, traceID, "test-proj", "gpt-4", baseTime, baseTime.Add(10*time.Second), statusCode); err != nil {
			t.Fatalf("insert span %s: %v", spanID, err)
		}
	}

	insert("span-ok", "trace-ok", "OK")
	insert("span-error", "trace-error", "ERROR")
	insert("span-unset", "trace-unset", "UNSET")
	insert("span-null", "trace-null", nil)

	h := &SpanHandler{
		Lake:         db,
		SessionStore: &FakeSessionStore{projectID: "test-proj"},
		QueryBuilder: &querybuild.QueryBuilder{Lake: db},
	}

	query := func(t *testing.T, filterValue []string) []string {
		t.Helper()
		valueJSON, err := json.Marshal(filterValue)
		if err != nil {
			t.Fatalf("marshal filter value: %v", err)
		}
		reqBody := fmt.Sprintf(`{
			"from": "2025-01-01T00:00:00Z",
			"to": "2025-01-02T00:00:00Z",
			"filters": [{"field": "status_code", "op": "in", "value": %s}]
		}`, valueJSON)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/spans/query", strings.NewReader(reqBody))
		w := httptest.NewRecorder()
		h.HandleSpansQuery(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("status: got %d, want %d\nbody: %s", w.Code, http.StatusOK, w.Body.String())
		}

		var resp struct {
			Spans []struct {
				TraceID string `json:"trace_id"`
			} `json:"spans"`
		}
		if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
			t.Fatalf("decode response: %v", err)
		}

		traceIDs := make([]string, 0, len(resp.Spans))
		for _, s := range resp.Spans {
			traceIDs = append(traceIDs, s.TraceID)
		}
		return traceIDs
	}

	t.Run("OK", func(t *testing.T) {
		got := query(t, []string{"OK"})
		if len(got) != 1 || got[0] != "trace-ok" {
			t.Errorf("status_code=OK: got %v, want [trace-ok]", got)
		}
	})

	t.Run("ERROR", func(t *testing.T) {
		got := query(t, []string{"ERROR"})
		if len(got) != 1 || got[0] != "trace-error" {
			t.Errorf("status_code=ERROR: got %v, want [trace-error]", got)
		}
	})

	t.Run("OK_and_ERROR", func(t *testing.T) {
		got := query(t, []string{"OK", "ERROR"})
		want := map[string]bool{"trace-ok": true, "trace-error": true}
		if len(got) != 2 {
			t.Fatalf("status_code=OK,ERROR: got %v, want 2 traces", got)
		}
		for _, id := range got {
			if !want[id] {
				t.Errorf("status_code=OK,ERROR: unexpected trace %q in %v", id, got)
			}
		}
	})

	t.Run("UNSET_matches_literal_and_null", func(t *testing.T) {
		got := query(t, []string{"UNSET"})
		want := map[string]bool{"trace-unset": true, "trace-null": true}
		if len(got) != 2 {
			t.Fatalf("status_code=UNSET: got %v, want 2 traces (trace-unset, trace-null)", got)
		}
		for _, id := range got {
			if !want[id] {
				t.Errorf("status_code=UNSET: unexpected trace %q in %v", id, got)
			}
		}
	})
}

func TestHandleSpansQuery_NoTimeRange_DefaultsTo30Days(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "omneval-handler-test")
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

	// Insert a span within the last 30 days.
	now := time.Now()
	recentSpan := now.Add(-7 * 24 * time.Hour) // 7 days ago
	if _, err := db.ExecContext(context.Background(),
		`INSERT INTO spans (span_id, trace_id, project_id, model, start_time, end_time) VALUES (?, ?, ?, ?, ?, ?)`,
		"span-recent", "trace-recent", "test-proj", "gpt-4",
		recentSpan, recentSpan.Add(10*time.Second)); err != nil {
		t.Fatalf("insert recent span: %v", err)
	}

	// Insert a span older than 30 days — should NOT be returned.
	oldSpan := now.Add(-60 * 24 * time.Hour)
	if _, err := db.ExecContext(context.Background(),
		`INSERT INTO spans (span_id, trace_id, project_id, model, start_time, end_time) VALUES (?, ?, ?, ?, ?, ?)`,
		"span-old", "trace-old", "test-proj", "gpt-3.5",
		oldSpan, oldSpan.Add(10*time.Second)); err != nil {
		t.Fatalf("insert old span: %v", err)
	}

	h := &SpanHandler{
		Lake:         db,
		SessionStore: &FakeSessionStore{projectID: "test-proj"},
		QueryBuilder: &querybuild.QueryBuilder{Lake: db},
	}

	// Request with no time range — should default to last 30 days.
	body := strings.NewReader(`{"limit": 50}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/spans/query", body)
	w := httptest.NewRecorder()

	h.HandleSpansQuery(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want %d\nbody: %s", w.Code, http.StatusOK, w.Body.String())
	}

	var resp struct {
		Spans []map[string]any `json:"spans"`
		Limit int              `json:"limit"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	// Should only return the recent span (within 30 days).
	if len(resp.Spans) != 1 {
		t.Errorf("spans count: got %d, want 1 (only recent span within 30 days)", len(resp.Spans))
	}

	// The returned span should be the recent one.
	if len(resp.Spans) > 0 {
		if spanID, ok := resp.Spans[0]["span_id"].(string); !ok || spanID != "span-recent" {
			t.Errorf("expected span-recent, got %v", resp.Spans[0]["span_id"])
		}
	}

	if resp.Limit != 50 {
		t.Errorf("limit: got %d, want 50", resp.Limit)
	}
}

func TestHandleAnalyticsSpans_NoTimeRange_DefaultsTo30Days(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "omneval-analytics-test")
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

	// Insert a span within the last 30 days.
	now := time.Now()
	recentSpan := now.Add(-7 * 24 * time.Hour)
	if _, err := db.ExecContext(context.Background(),
		`INSERT INTO spans (span_id, trace_id, project_id, model, start_time, end_time) VALUES (?, ?, ?, ?, ?, ?)`,
		"span-recent", "trace-recent", "test-proj", "gpt-4",
		recentSpan, recentSpan.Add(10*time.Second)); err != nil {
		t.Fatalf("insert recent span: %v", err)
	}

	// Insert a span older than 30 days — should NOT be returned.
	oldSpan := now.Add(-60 * 24 * time.Hour)
	if _, err := db.ExecContext(context.Background(),
		`INSERT INTO spans (span_id, trace_id, project_id, model, start_time, end_time) VALUES (?, ?, ?, ?, ?, ?)`,
		"span-old", "trace-old", "test-proj", "gpt-3.5",
		oldSpan, oldSpan.Add(10*time.Second)); err != nil {
		t.Fatalf("insert old span: %v", err)
	}

	h := &SpanHandler{
		Lake:           db,
		SessionStore: &FakeSessionStore{projectID: "test-proj"},
		QueryBuilder: &querybuild.QueryBuilder{Lake: db},
	}

	// Request with no time range — should default to last 30 days.
	body := strings.NewReader(`{
		"group_by": [{"field": "model"}],
		"aggregations": [{"function": "count", "field": "*", "alias": "count"}]
	}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/analytics/spans", body)
	w := httptest.NewRecorder()

	h.HandleAnalyticsSpans(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want %d\nbody: %s", w.Code, http.StatusOK, w.Body.String())
	}

	var resp struct {
		Rows []map[string]any `json:"rows"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	// Should only return gpt-4 (the recent span within 30 days).
	if len(resp.Rows) != 1 {
		t.Errorf("rows count: got %d, want 1", len(resp.Rows))
	}
	if len(resp.Rows) > 0 {
		if model, ok := resp.Rows[0]["model"].(string); !ok || model != "gpt-4" {
			t.Errorf("expected model=gpt-4, got %v", resp.Rows[0]["model"])
		}
	}
}

func TestHandleAnalyticsSpans_FromAfterTo_Returns400(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "omneval-analytics-test")
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

	h := &SpanHandler{
		Lake:           db,
		SessionStore: &FakeSessionStore{projectID: "test-proj"},
		QueryBuilder:   &querybuild.QueryBuilder{Lake: db},
	}

	body := strings.NewReader(`{
		"from": "2025-06-01T00:00:00Z",
		"to": "2025-01-01T00:00:00Z",
		"aggregations": [{"function": "count", "field": "*", "alias": "count"}]
	}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/analytics/spans", body)
	w := httptest.NewRecorder()

	h.HandleAnalyticsSpans(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusBadRequest)
	}

	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if !strings.Contains(resp["error"], "from must not be after to") {
		t.Errorf("error should mention 'from must not be after to', got: %q", resp["error"])
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
	tmpDir, err := os.MkdirTemp("", "omneval-trace-test")
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
		Lake:           db,
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
	tmpDir, err := os.MkdirTemp("", "omneval-trace-test")
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

	baseTime := time.Now().Add(-2 * time.Hour)
	if _, err := db.ExecContext(context.Background(),
		`INSERT INTO spans (span_id, trace_id, parent_id, project_id, name, model, start_time, end_time, input, output, input_tokens, output_tokens, cost_usd, status_code) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"span-001", "trace-abc", "", "test-proj", "root span", "gpt-4",
		baseTime, baseTime.Add(10*time.Second), `[{"role":"user","content":"hello"}]`, `[{"role":"assistant","content":"hi"}]`, 100, 50, 0.001, "OK"); err != nil {
		t.Fatalf("insert span: %v", err)
	}

	h := &SpanHandler{
		Lake:           db,
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
	tmpDir, err := os.MkdirTemp("", "omneval-trace-test")
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

	baseTime := time.Now().Add(-2 * time.Hour)

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
		Lake:           db,
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

// TestHandleTraceDetail_TokenRollup verifies that the trace detail response
// includes trace-level rollups (SUM input_tokens, SUM output_tokens, SUM
// cost_usd) across all spans in the trace, not just the root span's own
// (typically zero) values. See issue #137.
func TestHandleTraceDetail_TokenRollup(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "omneval-trace-test")
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

	baseTime := time.Now().Add(-2 * time.Hour)

	// Root span: an orchestration span with no tokens/cost of its own.
	_, err = db.ExecContext(context.Background(),
		`INSERT INTO spans (span_id, trace_id, parent_id, project_id, name, kind, start_time, end_time, input_tokens, output_tokens, cost_usd) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"span-root", "trace-tokens", "", "test-proj", "conversation", "chain", baseTime, baseTime.Add(200*time.Millisecond), 0, 0, 0.0)
	if err != nil {
		t.Fatalf("insert root: %v", err)
	}

	// Two descendant LLM spans carrying the actual token usage.
	_, err = db.ExecContext(context.Background(),
		`INSERT INTO spans (span_id, trace_id, parent_id, project_id, name, kind, model, start_time, end_time, input_tokens, output_tokens, cost_usd) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"span-llm1", "trace-tokens", "span-root", "test-proj", "litellm.completion", "llm", "gpt-4", baseTime.Add(10*time.Millisecond), baseTime.Add(80*time.Millisecond), 30000, 10000, 0.5)
	if err != nil {
		t.Fatalf("insert llm span 1: %v", err)
	}
	_, err = db.ExecContext(context.Background(),
		`INSERT INTO spans (span_id, trace_id, parent_id, project_id, name, kind, model, start_time, end_time, input_tokens, output_tokens, cost_usd) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"span-llm2", "trace-tokens", "span-root", "test-proj", "litellm.completion", "llm", "gpt-4", baseTime.Add(90*time.Millisecond), baseTime.Add(180*time.Millisecond), 5000, 1795, 0.1)
	if err != nil {
		t.Fatalf("insert llm span 2: %v", err)
	}

	h := &SpanHandler{
		Lake:         db,
		SessionStore: &FakeSessionStore{projectID: "test-proj"},
	}

	w := serveTraceDetail(h, http.MethodGet, "/api/v1/traces/trace-tokens", nil)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want %d\nbody: %s", w.Code, http.StatusOK, w.Body.String())
	}

	var trace struct {
		TraceID           string  `json:"trace_id"`
		TotalInputTokens  int64   `json:"total_input_tokens"`
		TotalOutputTokens int64   `json:"total_output_tokens"`
		TotalCostUSD      float64 `json:"total_cost_usd"`
	}
	if err := json.NewDecoder(w.Body).Decode(&trace); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	wantInput := int64(30000 + 5000)
	wantOutput := int64(10000 + 1795)
	wantCost := 0.5 + 0.1

	if trace.TotalInputTokens != wantInput {
		t.Errorf("total_input_tokens: got %d, want %d", trace.TotalInputTokens, wantInput)
	}
	if trace.TotalOutputTokens != wantOutput {
		t.Errorf("total_output_tokens: got %d, want %d", trace.TotalOutputTokens, wantOutput)
	}
	if math.Abs(trace.TotalCostUSD-wantCost) > 1e-9 {
		t.Errorf("total_cost_usd: got %v, want %v", trace.TotalCostUSD, wantCost)
	}
}

func TestHandleTraceDetail_SiblingChildren(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "omneval-trace-test")
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

	baseTime := time.Now().Add(-2 * time.Hour)

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
		Lake:           db,
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
	tmpDir, err := os.MkdirTemp("", "omneval-trace-test")
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

	baseTime := time.Now().Add(-2 * time.Hour)

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
		Lake:           db,
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
	tmpDir, err := os.MkdirTemp("", "omneval-trace-test")
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

	baseTime := time.Now().Add(-2 * time.Hour)

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
		Lake:           db,
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
		if trace.RootSpan.Scores[0].ScoreID != "score-001" {
			t.Errorf("score score_id: got %q, want %q", trace.RootSpan.Scores[0].ScoreID, "score-001")
		}
		if trace.RootSpan.Scores[0].TraceID != "trace-scores" {
			t.Errorf("score trace_id: got %q, want %q", trace.RootSpan.Scores[0].TraceID, "trace-scores")
		}
		if trace.RootSpan.Scores[0].ProjectID != "test-proj" {
			t.Errorf("score project_id: got %q, want %q", trace.RootSpan.Scores[0].ProjectID, "test-proj")
		}
	}
}

func TestHandleTraceDetail_ProjectIsolation(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "omneval-trace-test")
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

	baseTime := time.Now().Add(-2 * time.Hour)

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
		Lake:           db,
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

// TestHandleTraceDetail_ExplicitProjectID tests that HandleTraceDetail
// honours an explicit ?project_id= query parameter that differs from the
// session's default project — the fix for issue #188 (bug 2).
func TestHandleTraceDetail_ExplicitProjectID(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "omneval-trace-explicit-pid")
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

	// Insert spans: only proj-b has a span for "trace-b-other".
	// proj-a's default session has NO span with this trace_id.
	// Use a recent timestamp so the span falls within LakeTraceSpansSQL's
	// default 90-day window.
	now := time.Now().UTC()
	_, err = db.ExecContext(context.Background(),
		`INSERT INTO spans (span_id, trace_id, parent_id, project_id, name, start_time, end_time) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		"span-b1", "trace-b-other", "", "proj-b", "span-b", now, now.Add(5*time.Second))
	if err != nil {
		t.Fatalf("insert span-b: %v", err)
	}

	// The user has both proj-a and proj-b (e.g. multi-project org).
	// proj-a is the session default, but the UI explicitly requests proj-b.
	h := &SpanHandler{
		Lake: db,
		SessionStore: &FakeSessionStore{
			projectID: "proj-a",
			userProjects: []*domain.Project{
				{ProjectID: "proj-a", Name: "Default Project"},
				{ProjectID: "proj-b", Name: "Other Project"},
			},
		},
	}

	w := serveTraceDetail(h, http.MethodGet, "/api/v1/traces/trace-b-other?project_id=proj-b", nil)

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

	if trace.ProjectID != "proj-b" {
		t.Errorf("project_id: got %q, want %q", trace.ProjectID, "proj-b")
	}
	if len(trace.Spans) != 1 {
		t.Errorf("spans count: got %d, want 1", len(trace.Spans))
	}
}

func TestBuildTraceTree_EmptySpans(t *testing.T) {
	trace := buildTraceTree([]*domain.Span{}, nil, "", "", "scores")
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

	trace := buildTraceTree(spans, nil, "", "", "scores")

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

	trace := buildTraceTree(spans, nil, "", "", "scores")

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

	trace := buildTraceTree(spans, nil, "", "", "scores")

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
	ScoreID   string  `json:"score_id"`
	TraceID   string  `json:"trace_id"`
	ProjectID string  `json:"project_id"`
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
		QueryBuilder: &querybuild.QueryBuilder{},
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
		QueryBuilder: &querybuild.QueryBuilder{},
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

// ── Analytics Endpoint Tests ──────────────────────────────────────

func TestHandleAnalyticsSpans_AuthRequired(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/analytics/spans", strings.NewReader(`{}`))
	w := httptest.NewRecorder()

	h := &SpanHandler{
		SessionStore: &FakeSessionStore{},
	}

	h.HandleAnalyticsSpans(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestHandleAnalyticsSpans_MethodNotAllowed(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/analytics/spans", nil)
	w := httptest.NewRecorder()

	h := &SpanHandler{
		SessionStore: &FakeSessionStore{projectID: "test-proj"},
	}

	h.HandleAnalyticsSpans(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusMethodNotAllowed)
	}
}

func TestHandleAnalyticsSpans_WithDatabase_ProjectFromSession(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "omneval-analytics-test")
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
		`INSERT INTO spans (span_id, trace_id, project_id, model, start_time, end_time, input_tokens, output_tokens, cost_usd) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"span-001", "trace-abc", "test-proj", "gpt-4",
		baseTime, baseTime.Add(10*time.Second), 100, 50, 0.002); err != nil {
		t.Fatalf("insert span: %v", err)
	}

	h := &SpanHandler{
		Lake:           db,
		SessionStore: &FakeSessionStore{projectID: "test-proj"},
		QueryBuilder: &querybuild.QueryBuilder{Lake: db},
	}

	body := strings.NewReader(`{
		"from": "2025-01-01T00:00:00Z",
		"to": "2025-01-02T00:00:00Z",
		"group_by": [{"field": "model"}],
		"aggregations": [{"function": "count", "field": "*", "alias": "count"}]
	}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/analytics/spans", body)
	w := httptest.NewRecorder()

	h.HandleAnalyticsSpans(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want %d. body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	var resp struct {
		Rows []map[string]any `json:"rows"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if len(resp.Rows) == 0 {
		t.Fatal("expected rows in analytics response")
	}

	// Verify the model group is present.
	found := false
	for _, row := range resp.Rows {
		if name, ok := row["model"].(string); ok && name == "gpt-4" {
			found = true
			count := row["count"]
			_ = count
			break
		}
	}
	if !found {
		t.Errorf("expected row with model=gpt-4, got rows: %v", resp.Rows)
	}
}

func TestHandleAnalyticsSpans_ProjectIDOverride(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "omneval-analytics-test")
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
	// Insert spans for TWO different projects.
	if _, err := db.ExecContext(context.Background(),
		`INSERT INTO spans (span_id, trace_id, project_id, model, start_time, end_time) VALUES (?, ?, ?, ?, ?, ?)`,
		"span-a", "trace-a", "proj-a", "gpt-4", baseTime, baseTime.Add(10*time.Second)); err != nil {
		t.Fatalf("insert span-a: %v", err)
	}
	if _, err := db.ExecContext(context.Background(),
		`INSERT INTO spans (span_id, trace_id, project_id, model, start_time, end_time) VALUES (?, ?, ?, ?, ?, ?)`,
		"span-b", "trace-b", "proj-b", "claude-3", baseTime, baseTime.Add(5*time.Second)); err != nil {
		t.Fatalf("insert span-b: %v", err)
	}

	userProjects := []*domain.Project{
		{ProjectID: "proj-a"},
		{ProjectID: "proj-b"},
	}

	// Session returns proj-a, but the request overrides to proj-b.
	h := &SpanHandler{
		Lake:         db,
		SessionStore: &FakeSessionStore{
			projectID:    "proj-a",
			userProjects: userProjects,
		},
		QueryBuilder: &querybuild.QueryBuilder{Lake: db},
	}

	body := strings.NewReader(`{
		"from": "2025-01-01T00:00:00Z",
		"to": "2025-01-02T00:00:00Z",
		"project_id": "proj-b",
		"group_by": [{"field": "model"}],
		"aggregations": [{"function": "count", "field": "*", "alias": "count"}]
	}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/analytics/spans", body)
	// Add the current user to the request context (auth middleware normally does this).
	req = req.WithContext(context.WithValue(req.Context(), auth.UserIDContextKey, &auth.CurrentUser{UserID: "user-1"}))
	w := httptest.NewRecorder()

	h.HandleAnalyticsSpans(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want %d. body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	var resp struct {
		Rows []map[string]any `json:"rows"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if len(resp.Rows) == 0 {
		t.Fatal("expected rows when querying proj-b")
	}

	// Should only contain claude-3 (proj-b), not gpt-4 (proj-a).
	for _, row := range resp.Rows {
		if name, ok := row["model"].(string); ok {
			if name != "claude-3" {
				t.Errorf("expected model=claude-3 for proj-b, got model=%s", name)
			}
		}
	}
}

func TestHandleAnalyticsSpans_ProjectIDForbidden(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "omneval-analytics-test")
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

	h := &SpanHandler{
		Lake:         db,
		SessionStore: &FakeSessionStore{
			projectID: "proj-a",
			userProjects: []*domain.Project{
				{ProjectID: "proj-a"},
			},
		},
		QueryBuilder: &querybuild.QueryBuilder{Lake: db},
	}

	body := strings.NewReader(`{
		"from": "2025-01-01T00:00:00Z",
		"to": "2025-01-02T00:00:00Z",
		"project_id": "proj-unknown"
	}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/analytics/spans", body)
	req = req.WithContext(context.WithValue(req.Context(), auth.UserIDContextKey, &auth.CurrentUser{UserID: "user-1"}))
	w := httptest.NewRecorder()

	h.HandleAnalyticsSpans(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusForbidden)
	}
}

// ── Issue #15: contains filter and time_bucket group-by ──────────

func TestHandleSpansQuery_ContainsFilter(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "omneval-spans-query-test")
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
	// Insert a span whose name contains "qa-".
	if _, err := db.ExecContext(context.Background(),
		`INSERT INTO spans (span_id, trace_id, project_id, name, start_time, end_time) VALUES (?, ?, ?, ?, ?, ?)`,
		"span-qa-001", "trace-abc", "test-proj", "qa-test-span", baseTime, baseTime.Add(10*time.Second)); err != nil {
		t.Fatalf("insert qa span: %v", err)
	}
	// Insert a span whose name does NOT contain "qa-".
	if _, err := db.ExecContext(context.Background(),
		`INSERT INTO spans (span_id, trace_id, project_id, name, start_time, end_time) VALUES (?, ?, ?, ?, ?, ?)`,
		"span-prod-001", "trace-def", "test-proj", "prod-span", baseTime.Add(11*time.Second), baseTime.Add(21*time.Second)); err != nil {
		t.Fatalf("insert prod span: %v", err)
	}

	h := &SpanHandler{
		Lake:         db,
		SessionStore: &FakeSessionStore{projectID: "test-proj"},
		QueryBuilder: &querybuild.QueryBuilder{Lake: db},
	}

	body := strings.NewReader(`{
		"from": "2025-01-01T00:00:00Z",
		"to": "2025-01-02T00:00:00Z",
		"filters": [{"field": "name", "op": "contains", "value": "qa-"}],
		"limit": 50
	}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/spans/query", body)
	w := httptest.NewRecorder()

	h.HandleSpansQuery(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want %d\nbody: %s", w.Code, http.StatusOK, w.Body.String())
	}

	var resp struct {
		Spans []map[string]any `json:"spans"`
		Limit int              `json:"limit"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if len(resp.Spans) != 1 {
		t.Errorf("spans count: got %d, want 1 (only qa- span)", len(resp.Spans))
	}
	if len(resp.Spans) > 0 {
		name := resp.Spans[0]["name"]
		if !strings.Contains(fmt.Sprint(name), "qa-") {
			t.Errorf("expected name containing 'qa-', got %v", name)
		}
	}
	if resp.Limit != 50 {
		t.Errorf("limit: got %d, want 50", resp.Limit)
	}
}

func TestHandleAnalyticsSpans_TimeBucketHour(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "omneval-analytics-test")
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
	// Insert 3 spans in the same hour bucket.
	for i := 0; i < 3; i++ {
		if _, err := db.ExecContext(context.Background(),
			`INSERT INTO spans (span_id, trace_id, project_id, model, start_time, end_time) VALUES (?, ?, ?, ?, ?, ?)`,
			fmt.Sprintf("span-%d", i), fmt.Sprintf("trace-%d", i), "test-proj", "gpt-4",
			baseTime.Add(time.Duration(i)*5*time.Minute), baseTime.Add(time.Duration(i)*5*time.Minute).Add(time.Minute)); err != nil {
			t.Fatalf("insert span %d: %v", i, err)
		}
	}
	// Insert 2 spans in the next hour bucket.
	for i := 0; i < 2; i++ {
		nextHour := baseTime.Add(1 * time.Hour)
		if _, err := db.ExecContext(context.Background(),
			`INSERT INTO spans (span_id, trace_id, project_id, model, start_time, end_time) VALUES (?, ?, ?, ?, ?, ?)`,
			fmt.Sprintf("span-next-%d", i), fmt.Sprintf("trace-next-%d", i), "test-proj", "claude-3",
			nextHour.Add(time.Duration(i)*5*time.Minute), nextHour.Add(time.Duration(i)*5*time.Minute).Add(time.Minute)); err != nil {
			t.Fatalf("insert next-hour span %d: %v", i, err)
		}
	}

	h := &SpanHandler{
		Lake:         db,
		SessionStore: &FakeSessionStore{projectID: "test-proj"},
		QueryBuilder: &querybuild.QueryBuilder{Lake: db},
	}

	body := strings.NewReader(`{
		"from": "2025-01-01T00:00:00Z",
		"to": "2025-01-02T00:00:00Z",
		"group_by": [{"field": "time_bucket", "interval": "1h"}],
		"aggregations": [{"function": "count", "field": "*", "alias": "count"}]
	}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/analytics/spans", body)
	w := httptest.NewRecorder()

	h.HandleAnalyticsSpans(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want %d\nbody: %s", w.Code, http.StatusOK, w.Body.String())
	}

	var resp struct {
		Rows []map[string]any `json:"rows"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	// Should have exactly 2 rows (2 hour buckets).
	if len(resp.Rows) != 2 {
		t.Errorf("rows count: got %d, want 2", len(resp.Rows))
	}

	// Verify counts: one bucket has 3, the other has 2.
	counts := make(map[string]int)
	for _, row := range resp.Rows {
		// start_time is a string timestamp.
		ts, _ := row["start_time"].(string)
		// count could be string or int64 depending on DuckDB's JSON handling.
		var count int
		switch v := row["count"].(type) {
		case float64:
			count = int(v)
		case int64:
			count = int(v)
		case string:
			fmt.Sscanf(v, "%d", &count)
		}
		counts[ts] = count
	}
	// Verify we have exactly 3 in one bucket and 2 in another.
	found3, found2 := false, false
	for _, c := range counts {
		if c == 3 {
			found3 = true
		}
		if c == 2 {
			found2 = true
		}
	}
	if !found3 || !found2 {
		t.Errorf("expected bucket counts with 3 and 2, got: %v", counts)
	}
}

func TestHandleAnalyticsSpans_TimeBucketInvalidInterval(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "omneval-analytics-test")
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

	h := &SpanHandler{
		Lake:           db,
		SessionStore: &FakeSessionStore{projectID: "test-proj"},
		QueryBuilder:   &querybuild.QueryBuilder{Lake: db},
	}

	body := strings.NewReader(`{
		"from": "2025-01-01T00:00:00Z",
		"to": "2025-01-02T00:00:00Z",
		"group_by": [{"field": "time_bucket", "interval": "1y"}],
		"aggregations": [{"function": "count", "field": "*", "alias": "count"}]
	}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/analytics/spans", body)
	w := httptest.NewRecorder()

	h.HandleAnalyticsSpans(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusBadRequest)
	}

	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v (raw: %q)", err, w.Body.String())
	}
	if !strings.Contains(resp["error"], "time_bucket") {
		t.Errorf("error should mention time_bucket, got: %q", resp["error"])
	}
}

// TestHandleSpansQuery_ProjectIDOverride verifies the span list honors an
// explicit project_id in the request body (the UI project switcher) rather than
// always using the session default. This is the fix for the Traces page showing
// "No traces" while the Dashboard showed data: the session default returns the
// org's first project, but the switcher may select a different one.
func TestHandleSpansQuery_ProjectIDOverride(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "omneval-spans-override-test")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	db, err := sql.Open("duckdb", tmpDir+"/test.duckdb")
	if err != nil {
		t.Fatalf("open duckdb: %v", err)
	}
	defer db.Close()

	if _, err := db.ExecContext(context.Background(), spansTableDDL); err != nil {
		t.Fatalf("create table: %v", err)
	}

	baseTime := time.Date(2025, 1, 1, 10, 0, 0, 0, time.UTC)
	if _, err := db.ExecContext(context.Background(),
		`INSERT INTO spans (span_id, trace_id, project_id, model, start_time, end_time) VALUES (?, ?, ?, ?, ?, ?)`,
		"span-a", "trace-a", "proj-a", "gpt-4", baseTime, baseTime.Add(10*time.Second)); err != nil {
		t.Fatalf("insert span-a: %v", err)
	}
	if _, err := db.ExecContext(context.Background(),
		`INSERT INTO spans (span_id, trace_id, project_id, model, start_time, end_time) VALUES (?, ?, ?, ?, ?, ?)`,
		"span-b", "trace-b", "proj-b", "claude-3", baseTime, baseTime.Add(5*time.Second)); err != nil {
		t.Fatalf("insert span-b: %v", err)
	}

	// Session default is proj-a, but the request selects proj-b.
	h := &SpanHandler{
		Lake:         db,
		SessionStore: &FakeSessionStore{
			projectID:    "proj-a",
			userProjects: []*domain.Project{{ProjectID: "proj-a"}, {ProjectID: "proj-b"}},
		},
		QueryBuilder: &querybuild.QueryBuilder{Lake: db},
	}

	body := strings.NewReader(`{
		"from": "2025-01-01T00:00:00Z",
		"to": "2025-01-02T00:00:00Z",
		"project_id": "proj-b",
		"limit": 50
	}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/spans/query", body)
	req = req.WithContext(context.WithValue(req.Context(), auth.UserIDContextKey, &auth.CurrentUser{UserID: "user-1"}))
	w := httptest.NewRecorder()

	h.HandleSpansQuery(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want %d. body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	var resp struct {
		Spans []map[string]any `json:"spans"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(resp.Spans) != 1 {
		t.Fatalf("spans: got %d, want 1 (only proj-b)", len(resp.Spans))
	}
	if got := resp.Spans[0]["span_id"]; got != "span-b" {
		t.Errorf("span_id: got %v, want span-b (proj-b)", got)
	}
}

// TestHandleSpansQuery_ProjectIDForbidden verifies a body project_id outside the
// user's org is rejected with 403 rather than silently queried.
func TestHandleSpansQuery_ProjectIDForbidden(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "omneval-spans-forbidden-test")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	db, err := sql.Open("duckdb", tmpDir+"/test.duckdb")
	if err != nil {
		t.Fatalf("open duckdb: %v", err)
	}
	defer db.Close()
	if _, err := db.ExecContext(context.Background(), spansTableDDL); err != nil {
		t.Fatalf("create table: %v", err)
	}

	h := &SpanHandler{
		Lake:         db,
		SessionStore: &FakeSessionStore{
			projectID:    "proj-a",
			userProjects: []*domain.Project{{ProjectID: "proj-a"}},
		},
		QueryBuilder: &querybuild.QueryBuilder{Lake: db},
	}

	body := strings.NewReader(`{"project_id": "proj-unknown", "limit": 50}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/spans/query", body)
	req = req.WithContext(context.WithValue(req.Context(), auth.UserIDContextKey, &auth.CurrentUser{UserID: "user-1"}))
	w := httptest.NewRecorder()

	h.HandleSpansQuery(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("status: got %d, want %d. body: %s", w.Code, http.StatusForbidden, w.Body.String())
	}
}

// FakeSessionStore is a test fake implementing SessionStore.
type FakeSessionStore struct {
	projectID    string
	userProjects []*domain.Project
}

func (f *FakeSessionStore) ProjectID(r *http.Request) (string, bool) {
	if f.projectID == "" && len(f.userProjects) == 0 {
		return "", false
	}
	if f.projectID != "" {
		return f.projectID, true
	}
	if len(f.userProjects) > 0 {
		return f.userProjects[0].ProjectID, true
	}
	return "", false
}

// AuthorizeProject is a minimal ProjectAuthorizer implementation for tests.
func (f *FakeSessionStore) AuthorizeProject(_ *http.Request, projectID string) bool {
	if f.projectID == projectID {
		return true
	}
	for _, p := range f.userProjects {
		if p.ProjectID == projectID {
			return true
		}
	}
	return false
}

func (f *FakeSessionStore) ListProjects(r *http.Request) ([]*domain.Project, error) {
	if len(f.userProjects) == 0 {
		if f.projectID != "" {
			return []*domain.Project{{ProjectID: f.projectID}}, nil
		}
		return nil, fmt.Errorf("unauthenticated")
	}
	return f.userProjects, nil
}

// SessionStore methods — minimal stubs so FakeSessionStore implements metadata.SessionStore.

func (f *FakeSessionStore) CreateSession(ctx context.Context, session *domain.Session) error {
	return nil
}

func (f *FakeSessionStore) GetSession(ctx context.Context, sessionID string) (*domain.Session, error) {
	return nil, nil
}

func (f *FakeSessionStore) DeleteSession(ctx context.Context, sessionID string) error {
	return nil
}

func TestHandleProjects_ReturnsSnakeCaseJSON(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/projects", nil)
	w := httptest.NewRecorder()

	h := &SpanHandler{
		SessionStore: &FakeSessionStore{projectID: "test-proj-123"},
	}

	h.HandleProjects(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusOK)
		return
	}

	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("content-type: got %q, want %q", ct, "application/json")
	}

	// Parse the JSON response and verify snake_case keys exist.
	var projects []map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &projects); err != nil {
		t.Fatalf("invalid JSON response: %v", err)
	}

	if len(projects) == 0 {
		t.Fatal("expected at least one project in response")
	}

	p := projects[0]

	// Verify snake_case keys are present.
	for _, key := range []string{"project_id", "org_id", "name", "created_at"} {
		if _, ok := p[key]; !ok {
			t.Errorf("response missing snake_case key %q", key)
		}
	}

	// Verify PascalCase keys are NOT present.
	for _, key := range []string{"ProjectID", "OrgID", "Name", "CreatedAt"} {
		if _, ok := p[key]; ok {
			t.Errorf("response should not contain PascalCase key %q", key)
		}
	}

	// Verify the project_id value is correct.
	if pid, ok := p["project_id"].(string); !ok || pid != "test-proj-123" {
		t.Errorf("project_id: got %v, want %q", p["project_id"], "test-proj-123")
	}
}

// ── Issue #28: Authenticated endpoints return 400 (not 401) when no project ─

// TestHandleSpansQuery_AuthenticatedButNoProject verifies that an authenticated
// user whose org has no projects receives 400 (not 401).
func TestHandleSpansQuery_AuthenticatedButNoProject(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/spans/query", strings.NewReader(`{}`))
	// Inject a current user into context (simulates passing session middleware).
	req = req.WithContext(context.WithValue(req.Context(), auth.UserIDContextKey, &auth.CurrentUser{UserID: "user-1"}))
	w := httptest.NewRecorder()

	h := &SpanHandler{
		// FakeSessionStore with no projectID returns ("", false).
		SessionStore: &FakeSessionStore{},
	}

	h.HandleSpansQuery(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status: got %d, want %d (authenticated user with no project should be 400, not 401)", w.Code, http.StatusBadRequest)
	}

	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v (raw: %q)", err, w.Body.String())
	}
	if !strings.Contains(resp["error"], "no project found") {
		t.Errorf("error message should contain 'no project found', got: %q", resp["error"])
	}
}

// TestHandleSpansQuery_UnauthenticatedStill401 verifies that a request with
// no session at all (no CurrentUser in context) still gets 401.
func TestHandleSpansQuery_UnauthenticatedStill401(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/spans/query", strings.NewReader(`{}`))
	// No user in context.
	w := httptest.NewRecorder()

	h := &SpanHandler{
		SessionStore: &FakeSessionStore{},
	}

	h.HandleSpansQuery(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status: got %d, want %d (unauthenticated should be 401)", w.Code, http.StatusUnauthorized)
	}
}

// TestHandleTraceDetail_AuthenticatedButNoProject verifies that an authenticated
// user whose org has no projects receives 400 (not 401) from trace detail.
func TestHandleTraceDetail_AuthenticatedButNoProject(t *testing.T) {
	mux := http.NewServeMux()
	h := &SpanHandler{
		SessionStore: &FakeSessionStore{},
	}
	mux.HandleFunc("GET /api/v1/traces/{traceId}", h.HandleTraceDetail)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/traces/trace-abc", nil)
	req = req.WithContext(context.WithValue(req.Context(), auth.UserIDContextKey, &auth.CurrentUser{UserID: "user-1"}))
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status: got %d, want %d (authenticated user with no project should be 400, not 401)", w.Code, http.StatusBadRequest)
	}

	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v (raw: %q)", err, w.Body.String())
	}
	if !strings.Contains(resp["error"], "no project found") {
		t.Errorf("error message should contain 'no project found', got: %q", resp["error"])
	}
}

// TestHandleAnalyticsSpans_AuthenticatedButNoProject verifies that an authenticated
// user whose org has no projects receives 400 (not 401) from analytics spans.
func TestHandleAnalyticsSpans_AuthenticatedButNoProject(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/analytics/spans", strings.NewReader(`{}`))
	req = req.WithContext(context.WithValue(req.Context(), auth.UserIDContextKey, &auth.CurrentUser{UserID: "user-1"}))
	w := httptest.NewRecorder()

	h := &SpanHandler{
		SessionStore: &FakeSessionStore{},
	}

	h.HandleAnalyticsSpans(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status: got %d, want %d (authenticated user with no project should be 400, not 401)", w.Code, http.StatusBadRequest)
	}

	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v (raw: %q)", err, w.Body.String())
	}
	if !strings.Contains(resp["error"], "no project found") {
		t.Errorf("error message should contain 'no project found', got: %q", resp["error"])
	}
}

// ── Issue #85: Trace detail dedupes duplicate span rows ──

// TestHandleTraceDetail_DedupeOnLake proves that when the Lake path is used,
// ingesting the same span twice results in only one row in the waterfall
// (per ADR-0004 Batch Ledger residual-duplicate policy).
func TestHandleTraceDetail_DedupeOnLake(t *testing.T) {
	ctx := context.Background()

	// Set up a real Lake with partitioned spans table.
	lk := laketest.NewLocal(t)

	// Insert the same span twice (simulating Batch Ledger residual duplicates).
	baseTime := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	span := &domain.Span{
		SpanID:      "span-001",
		TraceID:     "trace-dedupe",
		ProjectID:   "proj-dedupe",
		ServiceName: "svc",
		Name:        "llm-call",
		Kind:        domain.SpanKind("llm"),
		StartTime:   baseTime,
		EndTime:     baseTime.Add(time.Second),
		Model:       "gpt-4o",
	}
	if err := lk.InsertSpans(ctx, []*domain.Span{span}); err != nil {
		t.Fatalf("insert span (1st): %v", err)
	}
	// Insert the same span again (duplicate).
	if err := lk.InsertSpans(ctx, []*domain.Span{span}); err != nil {
		t.Fatalf("insert span (2nd): %v", err)
	}

	// Verify raw Lake has 2 rows.
	var rawCount int
	if err := lk.DB().QueryRowContext(ctx, "SELECT count(*) FROM lake.spans WHERE span_id = ?", "span-001").Scan(&rawCount); err != nil {
		t.Fatalf("count raw spans: %v", err)
	}
	if rawCount != 2 {
		t.Fatalf("raw Lake should have 2 duplicate rows, got %d", rawCount)
	}

	// Set up handler with Lake attached.
	h := &SpanHandler{
		Lake:         lk.DB(),
		SessionStore: &FakeSessionStore{projectID: "proj-dedupe"},
	}

	// Query the trace detail via LakeTraceSpansSQL with dedupe=true.
	spans, err := h.querySpansForTrace("proj-dedupe", "trace-dedupe")
	if err != nil {
		t.Fatalf("querySpansForTrace: %v", err)
	}

	// After dedup, only 1 row should appear in the waterfall.
	if len(spans) != 1 {
		t.Fatalf("trace detail span count: got %d, want 1 (dedupe should remove duplicate)", len(spans))
	}

	// Verify the returned span has the correct ID.
	if spans[0].SpanID != "span-001" {
		t.Errorf("span_id: got %q, want %q", spans[0].SpanID, "span-001")
	}
}

// TestHandleTraceDetail_LimitedSpans verifies that LakeTraceSpansSQL applies
// LIMIT so a trace with > limit spans only returns limit rows from the
// executed query (not just the SQL string).
func TestHandleTraceDetail_LimitedSpans(t *testing.T) {
	ctx := context.Background()
	lk := laketest.NewLocal(t)

	baseTime := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	numSpans := 150
	limit := 100

	// Insert 150 spans for the same trace.
	spans := make([]*domain.Span, 0, numSpans)
	for i := 0; i < numSpans; i++ {
		spans = append(spans, &domain.Span{
			SpanID:      fmt.Sprintf("span-%03d", i),
			TraceID:     "trace-limited",
			ProjectID:   "proj-limited",
			ServiceName: "svc",
			Name:        fmt.Sprintf("span-%d", i),
			Kind:        domain.SpanKind("default"),
			StartTime:   baseTime.Add(time.Duration(i) * time.Millisecond),
			EndTime:     baseTime.Add(time.Duration(i) * time.Millisecond).Add(time.Millisecond),
		})
	}
	if err := lk.InsertSpans(ctx, spans); err != nil {
		t.Fatalf("insert spans: %v", err)
	}

	// The handler uses LakeTraceSpansSQL which caps at MaxTraceSpansLimit (10000),
	// so 150 spans should all return. Test with a smaller limit by constructing
	// a direct SQL query with a LIMIT.
	q, err := query.NewSpanQuery("proj-limited", query.SpanQueryRequest{
		From:  time.Time{},
		To:    time.Date(9999, 12, 31, 23, 59, 59, 0, time.UTC),
		Filters: []query.SpanQueryFilter{
			{Field: "trace_id", Op: "eq", Value: "trace-limited"},
		},
		Limit: limit,
	})
	if err != nil {
		t.Fatalf("NewSpanQuery: %v", err)
	}

	sqlStr, args := q.LakeTraceSpansSQL("trace-limited")
	rows, err := lk.DB().Query(sqlStr, args...)
	if err != nil {
		t.Fatalf("execute lake trace spans query: %v", err)
	}
	defer rows.Close()

	var count int
	for rows.Next() {
		count++
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows iteration: %v", err)
	}

	// With limit=100 and 150 spans, only 100 rows should be returned.
	if count != limit {
		t.Errorf("span count with limit %d: got %d, want %d", limit, count, limit)
	}
}

// TestLakeSQL_Integration verifies the traces-list query (LakeSQL) through
// the handler endpoint with real data — i.e. it exercises the full query path
// end-to-end against an in-process Lake, not just the SQL string.
func TestLakeSQL_Integration(t *testing.T) {
	ctx := context.Background()
	lk := laketest.NewLocal(t)

	// Insert two traces with known span counts and costs.
	// Trace "A": 2 spans, cost 0.05 + 0.10 = 0.15
	// Trace "B": 3 spans, cost 0.20 + 0.30 + 0.40 = 0.90
	baseTime := time.Date(2025, 6, 1, 12, 0, 0, 0, time.UTC)
	spans := []*domain.Span{
		{SpanID: "a-1", TraceID: "trace-a", ProjectID: "proj-int", ServiceName: "svc", Name: "s1", Kind: domain.SpanKind("llm"), StartTime: baseTime, CostUSD: 0.05, InputTokens: 100, OutputTokens: 50, StatusCode: "ok"},
		{SpanID: "a-2", TraceID: "trace-a", ProjectID: "proj-int", ServiceName: "svc", Name: "s2", Kind: domain.SpanKind("llm"), ParentID: "a-1", StartTime: baseTime.Add(time.Second), CostUSD: 0.10, InputTokens: 200, OutputTokens: 100, StatusCode: "ok"},
		{SpanID: "b-1", TraceID: "trace-b", ProjectID: "proj-int", ServiceName: "svc", Name: "s3", Kind: domain.SpanKind("llm"), StartTime: baseTime.Add(2 * time.Second), CostUSD: 0.20, InputTokens: 50, OutputTokens: 25, StatusCode: "error"},
		{SpanID: "b-2", TraceID: "trace-b", ProjectID: "proj-int", ServiceName: "svc", Name: "s4", Kind: domain.SpanKind("tool"), ParentID: "b-1", StartTime: baseTime.Add(3 * time.Second), CostUSD: 0.30, InputTokens: 10, OutputTokens: 5, StatusCode: "ok"},
		{SpanID: "b-3", TraceID: "trace-b", ProjectID: "proj-int", ServiceName: "svc", Name: "s5", Kind: domain.SpanKind("llm"), ParentID: "b-1", StartTime: baseTime.Add(4 * time.Second), CostUSD: 0.40, InputTokens: 30, OutputTokens: 15, StatusCode: "ok"},
	}
	if err := lk.InsertSpans(ctx, spans); err != nil {
		t.Fatalf("insert spans: %v", err)
	}

	// Build the traces-list query via the production querybuild.ExecuteSpan
	// path — the two-phase PhaseOneSQL/MainSQL query (issue #229) — rather
	// than a removed single-shot LakeSQL().
	qb := &querybuild.QueryBuilder{Lake: lk}

	resp, err := qb.ExecuteSpan(ctx, query.SpanQueryRequest{
		From:  baseTime.Add(-time.Hour),
		To:    baseTime.Add(time.Hour),
		Limit: 10,
	}, "proj-int")
	if err != nil {
		t.Fatalf("ExecuteSpan: %v", err)
	}

	// We should get exactly 2 rows — one per trace.
	if len(resp.Spans) != 2 {
		t.Fatalf("expected 2 trace rows, got %d", len(resp.Spans))
	}

	// Build a map keyed by trace_id for easy lookup.
	traceMap := make(map[string]int64)
	for _, s := range resp.Spans {
		traceMap[s.TraceID] = s.SpanCount
	}

	// Verify rollup: trace-a has 2 spans.
	if spanCount, ok := traceMap["trace-a"]; !ok {
		t.Fatal("missing trace-a in results")
	} else if spanCount != 2 {
		t.Errorf("trace-a span_count: got %d, want 2", spanCount)
	}

	// Verify rollup: trace-b has 3 spans.
	if spanCount, ok := traceMap["trace-b"]; !ok {
		t.Fatal("missing trace-b in results")
	} else if spanCount != 3 {
		t.Errorf("trace-b span_count: got %d, want 3", spanCount)
	}

	// Verify keyset pagination: add a third trace with a later start_time
	// and verify it appears after trace-b when querying with limit=1.
	spans2 := []*domain.Span{
		{SpanID: "c-1", TraceID: "trace-c", ProjectID: "proj-int", ServiceName: "svc", Name: "s6", Kind: domain.SpanKind("llm"), StartTime: baseTime.Add(10 * time.Second), CostUSD: 0.50, InputTokens: 60, OutputTokens: 30, StatusCode: "ok"},
	}
	if err := lk.InsertSpans(ctx, spans2); err != nil {
		t.Fatalf("insert extra span: %v", err)
	}

	resp2, err := qb.ExecuteSpan(ctx, query.SpanQueryRequest{
		From:  baseTime.Add(-time.Hour),
		To:    baseTime.Add(time.Hour),
		Limit: 1,
	}, "proj-int")
	if err != nil {
		t.Fatalf("ExecuteSpan (keyset): %v", err)
	}

	// The most recent trace should be "trace-c" (start_time at +10s).
	if len(resp2.Spans) == 0 || resp2.Spans[0].TraceID != "trace-c" {
		t.Errorf("first trace with limit=1: got %v, want trace-c", resp2.Spans)
	}

	// Now verify that using keyset pagination from trace-c returns trace-b.
	resp3, err := qb.ExecuteSpan(ctx, query.SpanQueryRequest{
		From:   baseTime.Add(-time.Hour),
		To:     baseTime.Add(time.Hour),
		Limit:  1,
		Cursor: cursor.Encode(cursor.Cursor{StartTime: baseTime.Add(10 * time.Second), SpanID: "c-1"}),
	}, "proj-int")
	if err != nil {
		t.Fatalf("ExecuteSpan with cursor: %v", err)
	}

	// After trace-c, the next trace should be "trace-b" (start_time at +2s).
	if len(resp3.Spans) == 0 || resp3.Spans[0].TraceID != "trace-b" {
		t.Errorf("next trace after trace-c: got %v, want trace-b", resp3.Spans)
	}
}

// ── HandleSpansBatch tests ──────────────────────────────────────────

func TestHandleSpansBatch_MissingBody(t *testing.T) {
	h := &SpanHandler{
		SessionStore: &FakeSessionStore{projectID: "test-proj"},
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/spans/batch", nil)
	w := httptest.NewRecorder()
	h.HandleSpansBatch(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandleSpansBatch_EmptySpanIDs(t *testing.T) {
	h := &SpanHandler{
		SessionStore: &FakeSessionStore{projectID: "test-proj"},
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/spans/batch", strings.NewReader(`{"span_ids": []}`))
	w := httptest.NewRecorder()
	h.HandleSpansBatch(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandleSpansBatch_MethodNotAllowed(t *testing.T) {
	h := &SpanHandler{
		SessionStore: &FakeSessionStore{projectID: "test-proj"},
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/spans/batch", nil)
	w := httptest.NewRecorder()
	h.HandleSpansBatch(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusMethodNotAllowed)
	}
}

func TestHandleSpansBatch_AuthRequired(t *testing.T) {
	h := &SpanHandler{
		SessionStore: &FakeSessionStore{},
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/spans/batch", strings.NewReader(`{"span_ids": ["span-1"]}`))
	w := httptest.NewRecorder()
	h.HandleSpansBatch(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestHandleSpansBatch_Success(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "omneval-spans-batch-test")
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

	// Insert test spans with NULL input/output.
	// Columns: span_id, trace_id, parent_id, conversation_id, project_id,
	//          service_name, name, kind, start_time, end_time, model,
	//          input, output, input_tokens, output_tokens, cost_usd,
	//          prompt_name, prompt_version, status_code, status_message, attributes
	if _, err := db.ExecContext(context.Background(),
		`INSERT INTO spans VALUES ('span-1', 'trace-a', NULL, NULL, 'test-proj', 'svc', `+
			`'span-one', 'llm', '2025-01-01T00:00:00Z', '2025-01-01T00:00:01Z', `+
			`'gpt-4', NULL, NULL, 10, 20, 0.5, NULL, 0, 'ok', '', NULL)`,
	); err != nil {
		t.Fatalf("insert span-1: %v", err)
	}

	// Insert a span with actual input/output JSON strings.
	inputJSON := `{"key":"value1"}`
	outputJSON := `{"key":"value2"}`
	if _, err := db.ExecContext(context.Background(),
		`INSERT INTO spans VALUES ('span-2', 'trace-a', 'span-1', NULL, 'test-proj', 'svc', `+
			`'span-two', 'tool', '2025-01-01T00:00:02Z', '2025-01-01T00:00:03Z', `+
			`'gpt-4', ?, ?, 5, 15, 0.3, NULL, 0, 'ok', '', NULL)`,
		inputJSON, outputJSON,
	); err != nil {
		t.Fatalf("insert span-2: %v", err)
	}

	h := &SpanHandler{
		Lake:         db,
		SessionStore: &FakeSessionStore{projectID: "test-proj"},
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/spans/batch",
		strings.NewReader(`{"span_ids": ["span-1", "span-2", "span-nonexistent"]}`))
	w := httptest.NewRecorder()
	h.HandleSpansBatch(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want %d\nbody: %s", w.Code, http.StatusOK, w.Body.String())
	}

	var resp struct {
		Spans []struct {
			SpanID string `json:"span_id"`
			Name   string `json:"name"`
			Input  string `json:"input"`
			Output string `json:"output"`
		} `json:"spans"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	// Should have 2 spans (nonexistent span is skipped).
	if len(resp.Spans) != 2 {
		t.Errorf("spans count: got %d, want 2\nbody: %s", len(resp.Spans), w.Body.String())
	}

	// Verify data integrity.
	spanMap := make(map[string]struct{ name, input, output string })
	for _, s := range resp.Spans {
		spanMap[s.SpanID] = struct{ name, input, output string }{s.Name, s.Input, s.Output}
	}

	if s, ok := spanMap["span-1"]; !ok {
		t.Error("span-1 not found in response")
	} else if s.name != "span-one" {
		t.Errorf("span-1 name: got %q, want %q", s.name, "span-one")
	}
	if s, ok := spanMap["span-2"]; !ok {
		t.Error("span-2 not found in response")
	} else if s.input == "" || s.output == "" {
		t.Errorf("span-2 input/output should not be empty: input=%q output=%q", s.input, s.output)
	}
}

// TestHandleSpansBatch_ScopesByProjectID is a regression test for #267:
// the handler resolved the caller's project ID but never used it to filter
// the span_id IN (...) query, letting any authenticated session pull
// input/output for spans belonging to a different project.
func TestHandleSpansBatch_ScopesByProjectID(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "omneval-spans-batch-scope-test")
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

	// span-a belongs to project A, span-b belongs to project B.
	secretInput := `{"key":"project-b-secret-input"}`
	secretOutput := `{"key":"project-b-secret-output"}`
	if _, err := db.ExecContext(context.Background(),
		`INSERT INTO spans VALUES ('span-a', 'trace-a', NULL, NULL, 'project-a', 'svc', `+
			`'span-a-name', 'llm', '2025-01-01T00:00:00Z', '2025-01-01T00:00:01Z', `+
			`'gpt-4', '{"key":"a"}', '{"key":"a"}', 10, 20, 0.5, NULL, 0, 'ok', '', NULL)`,
	); err != nil {
		t.Fatalf("insert span-a: %v", err)
	}
	if _, err := db.ExecContext(context.Background(),
		`INSERT INTO spans VALUES ('span-b', 'trace-b', NULL, NULL, 'project-b', 'svc', `+
			`'span-b-name', 'llm', '2025-01-01T00:00:00Z', '2025-01-01T00:00:01Z', `+
			`'gpt-4', ?, ?, 10, 20, 0.5, NULL, 0, 'ok', '', NULL)`,
		secretInput, secretOutput,
	); err != nil {
		t.Fatalf("insert span-b: %v", err)
	}

	// Authenticated for project A, but requests both span-a and project B's span-b.
	h := &SpanHandler{
		Lake:         db,
		SessionStore: &FakeSessionStore{projectID: "project-a"},
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/spans/batch",
		strings.NewReader(`{"span_ids": ["span-a", "span-b"]}`))
	w := httptest.NewRecorder()
	h.HandleSpansBatch(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want %d\nbody: %s", w.Code, http.StatusOK, w.Body.String())
	}

	var resp struct {
		Spans []struct {
			SpanID string `json:"span_id"`
			Input  string `json:"input"`
			Output string `json:"output"`
		} `json:"spans"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	for _, s := range resp.Spans {
		if s.SpanID == "span-b" {
			t.Fatalf("cross-tenant leak: project A's session received project B's span-b: input=%q output=%q", s.Input, s.Output)
		}
	}
	if len(resp.Spans) != 1 || resp.Spans[0].SpanID != "span-a" {
		t.Fatalf("expected only span-a scoped to project-a, got %+v", resp.Spans)
	}
}
