package handler

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/marcboeker/go-duckdb/v2"
)

func TestHandleSpansQuery_AuthRequired(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/spans/query", strings.NewReader(`{}`))
	w := httptest.NewRecorder()

	h := &SpanHandler{
	}

	h.HandleSpansQuery(w, req, nil)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestHandleSpansQuery_MissingBody(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/spans/query", nil)
	w := httptest.NewRecorder()

	h := &SpanHandler{
	}

	h.HandleSpansQuery(w, req, &testSessionStore{projectID: "test-proj"})

	if w.Code != http.StatusBadRequest {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandleSpansQuery_MethodNotAllowed(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/spans/query", nil)
	w := httptest.NewRecorder()

	h := &SpanHandler{
	}

	h.HandleSpansQuery(w, req, &testSessionStore{projectID: "test-proj"})

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
	}

	h.HandleSpansQuery(w, req, &testSessionStore{projectID: "test-proj"})

	if w.Code != http.StatusBadRequest {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusBadRequest)
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
	}

	// Make request.
	body := strings.NewReader(`{
		"from": "2025-01-01T00:00:00Z",
		"to": "2025-01-02T00:00:00Z",
		"limit": 50
	}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/spans/query", body)
	w := httptest.NewRecorder()

	h.HandleSpansQuery(w, req, &testSessionStore{projectID: "test-proj"})

	if w.Code != http.StatusOK {
		t.Errorf("status: got %d, want %d\nbody: %s", w.Code, http.StatusOK, w.Body.String())
		return
	}

	// Parse response.
	var resp struct {
		Spans  []map[string]any `json:"spans"`
		Next   string           `json:"next,omitempty"`
		Limit  int              `json:"limit"`
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

// testSessionStore is a test implementation of sessionStore.
type testSessionStore struct {
	projectID string
}

func (s *testSessionStore) ProjectID(r *http.Request) (string, bool) {
	if s.projectID == "" {
		return "", false
	}
	return s.projectID, true
}
