package handler

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
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
// Returns the span tree for a single trace with inline scores.
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

	// Query all spans for this trace in this project.
	spans, err := h.querySpansForTrace(projectID, traceID)
	if err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	// Return 404 if no spans found.
	if len(spans) == 0 {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	// Load scores for all spans in this trace.
	scoresBySpan, err := h.queryScoresForTrace(projectID, traceID)
	if err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	// Attach scores to spans.
	for _, s := range spans {
		if sc, ok := scoresBySpan[s.SpanID]; ok {
			s.Scores = sc
		}
	}

	// Build the trace tree.
	trace := buildTraceTree(spans)

	resp := domain.TraceResponse{
		TraceID:   trace.TraceID,
		ProjectID: trace.ProjectID,
		RootSpan:  trace.RootSpan,
		Spans:     trace.Spans,
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		http.Error(w, "encode error", http.StatusInternalServerError)
		return
	}
}

// querySpansForTrace fetches all spans for a given trace_id and project_id.
func (h *SpanHandler) querySpansForTrace(projectID, traceID string) ([]*domain.Span, error) {
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
		return nil, fmt.Errorf("build span query: %w", err)
	}

	sqlStr, args, err := q.SQL()
	if err != nil {
		return nil, fmt.Errorf("compile span query: %w", err)
	}

	rows, err := h.DB.Query(sqlStr, args...)
	if err != nil {
		return nil, fmt.Errorf("execute span query: %w", err)
	}
	defer rows.Close()

	spanRows, err := scanAllRows(rows)
	if err != nil {
		return nil, fmt.Errorf("scan span rows: %w", err)
	}

	spans, err := query.ScanRows(spanRows)
	if err != nil {
		return nil, fmt.Errorf("convert span rows: %w", err)
	}

	return spans, nil
}

// queryScoresForTrace fetches all scores for spans in a given trace_id and project_id.
// Returns a map of span_id -> []*domain.SpanScore.
// Gracefully handles missing scores table (returns empty map).
func (h *SpanHandler) queryScoresForTrace(projectID, traceID string) (map[string][]*domain.SpanScore, error) {
	result := make(map[string][]*domain.SpanScore)

	rows, err := h.DB.Query(
		`SELECT span_id, eval_name, value, reasoning FROM scores WHERE trace_id = ? AND project_id = ?`,
		traceID, projectID,
	)
	if err != nil {
		// If the scores table doesn't exist, return empty scores (graceful degradation).
		// DuckDB reports missing tables with a message containing "Table".
		if strings.Contains(err.Error(), "Table") {
			return result, nil
		}
		return nil, fmt.Errorf("query scores: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var spanID string
		var evalName string
		var value float64
		var reasoning *string

		if err := rows.Scan(&spanID, &evalName, &value, &reasoning); err != nil {
			return nil, fmt.Errorf("scan score row: %w", err)
		}

		sc := &domain.SpanScore{
			EvalName:  evalName,
			Value:     value,
			Reasoning: "",
		}
		if reasoning != nil {
			sc.Reasoning = *reasoning
		}

		result[spanID] = append(result[spanID], sc)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate score rows: %w", err)
	}

	return result, nil
}

// scanAllRows scans all database rows into [][]any.
func scanAllRows(rows *sql.Rows) ([][]any, error) {
	cols, err := rows.ColumnTypes()
	if err != nil {
		return nil, fmt.Errorf("column types: %w", err)
	}

	var result [][]any
	for rows.Next() {
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
// It sets Span.Children for proper waterfall rendering.
func buildTraceTree(spans []*domain.Span) domain.Trace {
	if len(spans) == 0 {
		return domain.Trace{}
	}

	// Build a map of span_id -> span for parent lookup.
	spanMap := make(map[string]*domain.Span, len(spans))
	var trace domain.Trace
	var root *domain.Span

	for _, s := range spans {
		spanMap[s.SpanID] = s
		trace.TraceID = s.TraceID
		trace.ProjectID = s.ProjectID
		if s.ParentID == "" {
			root = s
		}
	}

	// Link children to parents.
	for _, s := range spans {
		if s.ParentID != "" {
			if parent, ok := spanMap[s.ParentID]; ok {
				parent.Children = append(parent.Children, s)
			}
		}
	}

	// If no span has an empty parent, use the first span (by start_time order) as root.
	if root == nil && len(spans) > 0 {
		root = spans[0]
	}

	trace.RootSpan = root
	trace.Spans = spans
	return trace
}
