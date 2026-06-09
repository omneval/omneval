package handler

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/omneval/omneval/services/query/internal/auth"
)

// ConversationHandler handles conversation-related endpoints:
//   - GET /api/v1/conversations
//   - GET /api/v1/conversations/:conversationId
type ConversationHandler struct {
	DB           DBHandle
	SessionStore SessionStore
}

// ConversationListItem represents a single conversation in the paginated
// list response, with aggregate metadata.
type ConversationListItem struct {
	ConversationID    string  `json:"conversation_id"`
	ProjectID         string  `json:"project_id"`
	ServiceName       string  `json:"service_name"`
	TraceCount        int     `json:"trace_count"`
	SpanCount         int     `json:"span_count"`
	StartTime         string  `json:"start_time"`
	EndTime           string  `json:"end_time"`
	TotalCostUSD      float64 `json:"total_cost_usd"`
	TotalInputTokens  int64   `json:"total_input_tokens"`
	TotalOutputTokens int64   `json:"total_output_tokens"`
}

// ConversationListResponse is the response for GET /api/v1/conversations.
type ConversationListResponse struct {
	Conversations []ConversationListItem `json:"conversations"`
	Next          *string                `json:"next,omitempty"`
	Limit         int                    `json:"limit"`
}

// ConversationTraceItem represents a single trace within a conversation detail
// response, with root span metadata.
type ConversationTraceItem struct {
	TraceID      string  `json:"trace_id"`
	RootSpanName string  `json:"root_span_name"`
	RootSpanKind string  `json:"root_span_kind"`
	StartTime    string  `json:"start_time"`
	EndTime      string  `json:"end_time"`
	SpanCount    int     `json:"span_count"`
	CostUSD      float64 `json:"cost_usd"`
	InputTokens  int64   `json:"input_tokens"`
	OutputTokens int64   `json:"output_tokens"`
	Model        string  `json:"model"`
}

// ConversationDetailResponse is the response for
// GET /api/v1/conversations/:conversationId.
type ConversationDetailResponse struct {
	ConversationID string                  `json:"conversation_id"`
	Traces         []ConversationTraceItem `json:"traces"`
}

// resolveProjectID validates that a project is available from the session store
// and returns the project ID. It writes an appropriate HTTP error if validation
// fails and returns an empty string.
func (h *ConversationHandler) resolveProjectID(w http.ResponseWriter, r *http.Request) string {
	projectID, ok := h.SessionStore.ProjectID(r)
	if !ok || projectID == "" {
		if auth.CurrentUserFromContext(r) != nil {
			writeJSONError(w, "no project found — create a project first via POST /api/v1/projects", http.StatusBadRequest)
		} else {
			writeJSONError(w, "unauthorized", http.StatusUnauthorized)
		}
		return ""
	}
	return projectID
}

// HandleListConversations returns paginated conversation list with aggregate
// metadata, ordered by most recent start_time descending.
// Supports keyset pagination via from/to/limit/cursor query parameters.
func (h *ConversationHandler) HandleListConversations(w http.ResponseWriter, r *http.Request) {
	projectID := h.resolveProjectID(w, r)
	if projectID == "" {
		return
	}

	// Parse pagination params.
	limit := 50
	if l, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && l > 0 {
		limit = l
	}
	cursor := r.URL.Query().Get("cursor")

	// Build query.
	query := `
		SELECT
			conversation_id,
			project_id,
			COALESCE(MAX(service_name), '') AS service_name,
			COUNT(DISTINCT trace_id) AS trace_count,
			COUNT(*) AS span_count,
			CAST(MIN(start_time) AS VARCHAR) AS start_time,
			COALESCE(CAST(MAX(end_time) AS VARCHAR), '') AS end_time,
			ROUND(COALESCE(SUM(cost_usd), 0), 6) AS total_cost_usd,
			COALESCE(SUM(input_tokens), 0) AS total_input_tokens,
			COALESCE(SUM(output_tokens), 0) AS total_output_tokens
		FROM spans
		WHERE project_id = ? AND conversation_id IS NOT NULL AND conversation_id != ''
	`
	args := []any{projectID}

	// Apply keyset pagination using cursor (conversation_id boundary).
	if cursor != "" {
		query += " AND conversation_id < ?"
		args = append(args, cursor)
	}

	query += `
		GROUP BY conversation_id, project_id
		ORDER BY end_time DESC, conversation_id DESC
		LIMIT ?
	`
	args = append(args, limit+1) // fetch one extra to determine if there's a next page.

	rows, err := h.DB.Query(query, args...)
	if err != nil {
		writeJSONError(w, "query execution error: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	conversations := make([]ConversationListItem, 0)
	for rows.Next() {
		var c ConversationListItem
		if err := rows.Scan(&c.ConversationID, &c.ProjectID, &c.ServiceName, &c.TraceCount, &c.SpanCount, &c.StartTime, &c.EndTime, &c.TotalCostUSD, &c.TotalInputTokens, &c.TotalOutputTokens); err != nil {
			writeJSONError(w, "scan error: "+err.Error(), http.StatusInternalServerError)
			return
		}
		conversations = append(conversations, c)
	}

	// Determine if there's a next page.
	var next *string
	if len(conversations) > limit {
		conversations = conversations[:limit]
		lastCursor := conversations[len(conversations)-1].ConversationID
		next = &lastCursor
	}

	resp := ConversationListResponse{
		Conversations: conversations,
		Next:          next,
		Limit:         limit,
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		writeJSONError(w, "encode error", http.StatusInternalServerError)
		return
	}
}

// HandleConversationDetail returns ordered trace list with root span metadata
// for a single conversation.
func (h *ConversationHandler) HandleConversationDetail(w http.ResponseWriter, r *http.Request) {
	projectID := h.resolveProjectID(w, r)
	if projectID == "" {
		return
	}

	conversationID := r.PathValue("conversationId")
	if conversationID == "" {
		writeJSONError(w, "missing conversation ID", http.StatusBadRequest)
		return
	}

	// Get distinct traces ordered by start_time, with root span info.
	rows, err := h.DB.Query(`
		SELECT
			trace_id,
			CAST(MIN(start_time) AS VARCHAR) AS start_time,
			COALESCE(CAST(MAX(end_time) AS VARCHAR), '') AS end_time,
			COUNT(*) AS span_count,
			ROUND(COALESCE(SUM(cost_usd), 0), 6) AS cost_usd,
			COALESCE(SUM(input_tokens), 0) AS input_tokens,
			COALESCE(SUM(output_tokens), 0) AS output_tokens,
			(SELECT s2.name FROM spans s2
			 WHERE s2.trace_id = s.trace_id AND (s2.parent_id IS NULL OR s2.parent_id = '')
			 ORDER BY s2.start_time ASC
			 LIMIT 1
			) AS root_span_name,
			(SELECT s2.kind FROM spans s2
			 WHERE s2.trace_id = s.trace_id AND (s2.parent_id IS NULL OR s2.parent_id = '')
			 ORDER BY s2.start_time ASC
			 LIMIT 1
			) AS root_span_kind,
			(SELECT s2.model FROM spans s2
			 WHERE s2.trace_id = s.trace_id AND s2.model != ''
			 ORDER BY s2.start_time ASC
			 LIMIT 1
			) AS model
		FROM spans s
		WHERE project_id = ? AND conversation_id = ?
		GROUP BY trace_id
		ORDER BY start_time ASC
	`, projectID, conversationID)
	if err != nil {
		writeJSONError(w, "query execution error: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	traces := make([]ConversationTraceItem, 0)
	for rows.Next() {
		var t ConversationTraceItem
		if err := rows.Scan(&t.TraceID, &t.StartTime, &t.EndTime, &t.SpanCount, &t.CostUSD, &t.InputTokens, &t.OutputTokens, &t.RootSpanName, &t.RootSpanKind, &t.Model); err != nil {
			writeJSONError(w, "scan error: "+err.Error(), http.StatusInternalServerError)
			return
		}
		if t.RootSpanName == "" {
			t.RootSpanName = "unknown"
		}
		if t.RootSpanKind == "" {
			t.RootSpanKind = "internal"
		}
		traces = append(traces, t)
	}

	if len(traces) == 0 {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": "conversation not found"})
		return
	}

	resp := ConversationDetailResponse{
		ConversationID: conversationID,
		Traces:         traces,
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		writeJSONError(w, "encode error", http.StatusInternalServerError)
		return
	}
}
