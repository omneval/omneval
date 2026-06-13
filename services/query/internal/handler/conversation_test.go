package handler

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	_ "github.com/duckdb/duckdb-go/v2"
)

const conversationTestSchema = `
	CREATE TABLE spans (
		span_id          VARCHAR      NOT NULL,
		trace_id         VARCHAR      NOT NULL,
		parent_id        VARCHAR,
		conversation_id  VARCHAR,
		project_id       VARCHAR      NOT NULL,
		service_name     VARCHAR,
		name             VARCHAR,
		kind             VARCHAR,
		start_time       TIMESTAMPTZ  NOT NULL,
		end_time         TIMESTAMPTZ,
		model            VARCHAR,
		input            JSON,
		output           JSON,
		input_tokens     BIGINT,
		output_tokens    BIGINT,
		cost_usd         DOUBLE,
		prompt_name      VARCHAR,
		prompt_version   BIGINT,
		status_code      VARCHAR,
		status_message   VARCHAR,
		attributes       JSON,
		PRIMARY KEY (trace_id, span_id)
	);
`

func newConversationMux(t *testing.T, projectID string) (*http.ServeMux, *sql.DB) {
	t.Helper()
	db, err := sql.Open("duckdb", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if _, err := db.Exec(conversationTestSchema); err != nil {
		t.Fatalf("create schema: %v", err)
	}

	mux := http.NewServeMux()
	ch := &ConversationHandler{
		DB:           db,
		SessionStore: &FakeSessionStore{projectID: projectID},
	}
	mux.HandleFunc("GET /api/v1/conversations", ch.HandleListConversations)
	mux.HandleFunc("GET /api/v1/conversations/{conversationId}", ch.HandleConversationDetail)

	return mux, db
}

// seedConversations inserts spans belonging to two conversations across
// three distinct traces.
func seedConversations(t *testing.T, db *sql.DB) {
	t.Helper()
	// Conversation A: 2 traces (4 spans total)
	_, err := db.Exec(`INSERT INTO spans (span_id, trace_id, parent_id, conversation_id, project_id, name, kind, start_time, model, cost_usd) VALUES
		('s-a1', 't-a1', '', 'conv-A', 'test-proj', 'root', 'llm', '2024-01-01T00:00:00Z', 'gpt-4', 0.10),
		('s-a2', 't-a1', 's-a1', 'conv-A', 'test-proj', 'child', 'tool', '2024-01-01T00:00:01Z', '', 0),
		('s-a3', 't-a2', '', 'conv-A', 'test-proj', 'root2', 'llm', '2024-01-01T01:00:00Z', 'gpt-4', 0.20),
		('s-a4', 't-a2', 's-a3', 'conv-A', 'test-proj', 'child2', 'tool', '2024-01-01T01:00:01Z', '', 0)
	`)
	if err != nil {
		t.Fatalf("seed conv A: %v", err)
	}

	// Conversation B: 1 trace (1 span)
	_, err = db.Exec(`INSERT INTO spans (span_id, trace_id, parent_id, conversation_id, project_id, name, kind, start_time, model, cost_usd) VALUES
		('s-b1', 't-b1', '', 'conv-B', 'test-proj', 'only', 'llm', '2024-01-02T00:00:00Z', 'claude-3', 0.30)
	`)
	if err != nil {
		t.Fatalf("seed conv B: %v", err)
	}

	// Orphan span with no conversation_id
	_, err = db.Exec(`INSERT INTO spans (span_id, trace_id, parent_id, conversation_id, project_id, name, kind, start_time, model, cost_usd) VALUES
		('s-orphan', 't-orphan', '', NULL, 'test-proj', 'orphan', 'llm', '2024-01-03T00:00:00Z', 'gemini', 0.05)
	`)
	if err != nil {
		t.Fatalf("seed orphan: %v", err)
	}
}

func TestConversationHandler_ListConversations(t *testing.T) {
	mux, db := newConversationMux(t, "test-proj")
	seedConversations(t, db)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/conversations", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want %d; body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	var resp ConversationListResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	// Should return 2 conversations (orphan spans excluded).
	if len(resp.Conversations) != 2 {
		t.Fatalf("conversations count: got %d, want 2", len(resp.Conversations))
	}

	// First conversation should be conv-B (most recent start_time).
	if resp.Conversations[0].ConversationID != "conv-B" {
		t.Errorf("first conv: got %q, want %q", resp.Conversations[0].ConversationID, "conv-B")
	}

	// Verify aggregate metadata for conv-A.
	for _, c := range resp.Conversations {
		if c.ConversationID == "conv-A" {
			if c.TraceCount != 2 {
				t.Errorf("conv-A trace_count: got %d, want 2", c.TraceCount)
			}
			if c.SpanCount != 4 {
				t.Errorf("conv-A span_count: got %d, want 4", c.SpanCount)
			}
			if c.TotalCostUSD != 0.30 {
				t.Errorf("conv-A total_cost_usd: got %f, want 0.30", c.TotalCostUSD)
			}
		}
	}
}

func TestConversationHandler_Detail(t *testing.T) {
	mux, db := newConversationMux(t, "test-proj")
	seedConversations(t, db)

	// Fetch conv-A detail — should return traces ordered by start_time.
	req := httptest.NewRequest(http.MethodGet, "/api/v1/conversations/conv-A", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want %d; body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	var resp ConversationDetailResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if resp.ConversationID != "conv-A" {
		t.Errorf("conversation_id: got %q, want %q", resp.ConversationID, "conv-A")
	}

	// Should have 2 traces ordered chronologically.
	if len(resp.Traces) != 2 {
		t.Fatalf("traces count: got %d, want 2", len(resp.Traces))
	}

	if resp.Traces[0].TraceID != "t-a1" {
		t.Errorf("first trace: got %q, want %q", resp.Traces[0].TraceID, "t-a1")
	}
	if resp.Traces[1].TraceID != "t-a2" {
		t.Errorf("second trace: got %q, want %q", resp.Traces[1].TraceID, "t-a2")
	}

	// Each trace should have root span info.
	if resp.Traces[0].RootSpanName != "root" {
		t.Errorf("first root_span_name: got %q, want %q", resp.Traces[0].RootSpanName, "root")
	}
}

func TestConversationHandler_DetailNotFound(t *testing.T) {
	mux, _ := newConversationMux(t, "test-proj")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/conversations/nonexistent", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestConversationHandler_AuthRequired(t *testing.T) {
	mux, _ := newConversationMux(t, "") // no project ID = no auth

	req := httptest.NewRequest(http.MethodGet, "/api/v1/conversations", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusUnauthorized)
	}
}
