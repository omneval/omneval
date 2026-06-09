package handler

import (
	"encoding/json"
	"net/http"

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
	ConversationID string  `json:"conversation_id"`
	TraceCount     int     `json:"trace_count"`
	SpanCount      int     `json:"span_count"`
	TotalCostUSD   float64 `json:"total_cost_usd"`
	FirstSeen      string  `json:"first_seen"`
	LastSeen       string  `json:"last_seen"`
}

// ConversationListResponse is the response for GET /api/v1/conversations.
type ConversationListResponse struct {
	Conversations []ConversationListItem `json:"conversations"`
}

// ConversationTraceItem represents a single trace within a conversation detail
// response, with root span metadata.
type ConversationTraceItem struct {
	TraceID      string `json:"trace_id"`
	RootSpanName string `json:"root_span_name"`
	StartTime    string `json:"start_time"`
}

// ConversationDetailResponse is the response for
// GET /api/v1/conversations/:conversationId.
type ConversationDetailResponse struct {
	ConversationID string                `json:"conversation_id"`
	Traces         []ConversationTraceItem `json:"traces"`
}

// HandleListConversations returns paginated conversation list with aggregate
// metadata, ordered by most recent start_time descending.
func (h *ConversationHandler) HandleListConversations(w http.ResponseWriter, r *http.Request) {
	projectID, ok := h.SessionStore.ProjectID(r)
	if !ok || projectID == "" {
		if auth.CurrentUserFromContext(r) != nil {
			writeJSONError(w, "no project found — create a project first via POST /api/v1/projects", http.StatusBadRequest)
		} else {
			writeJSONError(w, "unauthorized", http.StatusUnauthorized)
		}
		return
	}

	rows, err := h.DB.Query(`
		SELECT
			conversation_id,
			COUNT(DISTINCT trace_id) AS trace_count,
			COUNT(*) AS span_count,
			ROUND(COALESCE(SUM(cost_usd), 0), 6) AS total_cost_usd,
			MIN(start_time) AS first_seen,
			MAX(start_time) AS last_seen
		FROM spans
		WHERE project_id = ? AND conversation_id IS NOT NULL AND conversation_id != ''
		GROUP BY conversation_id
		ORDER BY last_seen DESC
	`, projectID)
	if err != nil {
		writeJSONError(w, "query execution error: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var conversations []ConversationListItem
	for rows.Next() {
		var c ConversationListItem
		if err := rows.Scan(&c.ConversationID, &c.TraceCount, &c.SpanCount, &c.TotalCostUSD, &c.FirstSeen, &c.LastSeen); err != nil {
			writeJSONError(w, "scan error: "+err.Error(), http.StatusInternalServerError)
			return
		}
		conversations = append(conversations, c)
	}

	resp := ConversationListResponse{
		Conversations: conversations,
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
	projectID, ok := h.SessionStore.ProjectID(r)
	if !ok || projectID == "" {
		if auth.CurrentUserFromContext(r) != nil {
			writeJSONError(w, "no project found — create a project first via POST /api/v1/projects", http.StatusBadRequest)
		} else {
			writeJSONError(w, "unauthorized", http.StatusUnauthorized)
		}
		return
	}

	conversationID := r.PathValue("conversationId")
	if conversationID == "" {
		writeJSONError(w, "missing conversation ID", http.StatusBadRequest)
		return
	}

	// Verify the conversation exists.
	var exists int
	err := h.DB.QueryRow(`
		SELECT COUNT(*) FROM spans
		WHERE project_id = ? AND conversation_id = ?
	`, projectID, conversationID).Scan(&exists)
	if err != nil {
		writeJSONError(w, "query execution error: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if exists == 0 {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": "conversation not found"})
		return
	}

	// Get distinct traces ordered by start_time, with root span info.
	rows, err := h.DB.Query(`
		SELECT
			trace_id,
			MIN(start_time) AS start_time,
			(SELECT s2.name FROM spans s2
			 WHERE s2.trace_id = s.trace_id AND s2.parent_id = ''
			 ORDER BY s2.start_time ASC
			 LIMIT 1
			) AS root_span_name
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

	var traces []ConversationTraceItem
	for rows.Next() {
		var t ConversationTraceItem
		if err := rows.Scan(&t.TraceID, &t.StartTime, &t.RootSpanName); err != nil {
			writeJSONError(w, "scan error: "+err.Error(), http.StatusInternalServerError)
			return
		}
		if t.RootSpanName == "" {
			t.RootSpanName = "unknown"
		}
		traces = append(traces, t)
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
