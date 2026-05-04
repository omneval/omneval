package handler

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/zbloss/lantern/internal/domain"
	"github.com/zbloss/lantern/services/query/internal/query"
)

// SpanHandler handles POST /api/v1/spans/query (paginated span list)
// and GET /api/v1/traces/:traceId (single-trace waterfall detail).
type SpanHandler struct {
	DB           *sql.DB
	SessionStore sessionStore
}

// sessionStore abstracts session lookup for project ID extraction.
type sessionStore interface {
	ProjectID(r *http.Request) (string, bool)
}

// HandleSpansQuery handles POST /api/v1/spans/query.
// It extracts project_id from the authenticated session, builds the SQL query
// with keyset cursor pagination, executes the hot+cold UNION, and returns
// a paginated span list with a next cursor.
func (h *SpanHandler) HandleSpansQuery(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req query.SpanQueryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}

	// Extract project_id from the authenticated session.
	projectID, ok := h.SessionStore.ProjectID(r)
	if !ok || projectID == "" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	// Build the query — projectID is injected from the session, never from client input.
	q, err := query.NewSpanQuery(projectID, req, nil, "")
	if err != nil {
		http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
		return
	}

	sqlStr, args, err := q.SQL()
	if err != nil {
		http.Error(w, "query compilation error", http.StatusInternalServerError)
		return
	}

	rows, err := h.DB.Query(sqlStr, args...)
	if err != nil {
		http.Error(w, "query execution error: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	// Scan rows into domain spans.
	spanRows, err := scanAllRows(rows)
	if err != nil {
		http.Error(w, "scan error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	spans, err := query.ScanRows(spanRows)
	if err != nil {
		http.Error(w, "row scan error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Compute next cursor using the effective limit from the query.
	// This handles the case where req.Limit was 0 (unset) and the
	// query defaulted to DefaultLimit.
	next := query.NextCursor(spans, q.EffectiveLimit())

	resp := query.SpanResponse{
		Spans: spans,
		Next:  next,
		Limit: q.EffectiveLimit(),
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		http.Error(w, "encode error", http.StatusInternalServerError)
		return
	}
}

// HandleTraceDetail handles GET /api/v1/traces/:traceId.
// Returns the span tree for a single trace.
func (h *SpanHandler) HandleTraceDetail(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	traceID := r.PathValue("traceId")
	if traceID == "" {
		http.Error(w, "missing trace ID", http.StatusBadRequest)
		return
	}

	// Extract project_id from the authenticated session.
	projectID, ok := h.SessionStore.ProjectID(r)
	if !ok || projectID == "" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	// Build the span query for this trace.
	req := query.SpanQueryRequest{
		From:  time.Time{},
		To:    time.Date(9999, 12, 31, 23, 59, 59, 0, time.UTC),
		Filters: []query.SpanQueryFilter{
			{Field: "trace_id", Op: "eq", Value: traceID},
		},
		Limit: 10000,
	}

	q, err := query.NewSpanQuery(projectID, req, nil, "")
	if err != nil {
		http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
		return
	}

	sqlStr, args, err := q.SQL()
	if err != nil {
		http.Error(w, "query compilation error", http.StatusInternalServerError)
		return
	}

	rows, err := h.DB.Query(sqlStr, args...)
	if err != nil {
		http.Error(w, "query execution error: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	spanRows, err := scanAllRows(rows)
	if err != nil {
		http.Error(w, "scan error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	spans, err := query.ScanRows(spanRows)
	if err != nil {
		http.Error(w, "row scan error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Build the trace tree.
	trace := buildTraceTree(spans)

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(trace); err != nil {
		http.Error(w, "encode error", http.StatusInternalServerError)
		return
	}
}

// scanAllRows scans all database rows into [][]any, handling the column
// types and scanning loop in one step.
func scanAllRows(rows *sql.Rows) ([][]any, error) {
	var result [][]any
	for rows.Next() {
		cols, err := rows.ColumnTypes()
		if err != nil {
			return nil, fmt.Errorf("column types: %w", err)
		}
		values := make([]any, len(cols))
		valuePtrs := make([]any, len(cols))
		for i := range values {
			valuePtrs[i] = &values[i]
		}
		if err := rows.Scan(valuePtrs...); err != nil {
			return nil, fmt.Errorf("scan row: %w", err)
		}
		result = append(result, values)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows iteration: %w", err)
	}
	return result, nil
}

// buildTraceTree groups spans by trace_id and links parent-child relationships.
func buildTraceTree(spans []*domain.Span) domain.Trace {
	if len(spans) == 0 {
		return domain.Trace{}
	}

	// Sort by start_time for deterministic tree building.
	// Find the root span (no parent).
	var root *domain.Span
	trace := domain.Trace{
		TraceID: spans[0].TraceID,
	}

	// Build a map of span_id -> span for parent lookup.
	spanMap := make(map[string]*domain.Span, len(spans))
	for i := range spans {
		spanMap[spans[i].SpanID] = spans[i]
		trace.ProjectID = spans[i].ProjectID
	}

	for i := range spans {
		s := spans[i]
		if s.ParentID == "" {
			root = s
		}
	}

	if root == nil && len(spans) > 0 {
		// No span has an empty parent — use the first one as root.
		root = spans[0]
	}

	trace.RootSpan = root
	trace.Spans = spans
	return trace
}
