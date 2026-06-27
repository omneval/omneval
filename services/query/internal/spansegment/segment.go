// Package spansegment provides the Span segment — a deep module for span query
// domain logic. The Segment interface is the seam between the Router dispatcher
// and span-specific route registration.
package spansegment

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/omneval/omneval/internal/auth"
	"github.com/omneval/omneval/internal/domain"
	"github.com/omneval/omneval/services/query/internal/dsl"
	"github.com/omneval/omneval/services/query/internal/metrics"
	"github.com/omneval/omneval/services/query/internal/query"
	"github.com/omneval/omneval/services/query/internal/querybuild"
	"github.com/omneval/omneval/services/query/internal/routes"
)

// Re-export shared route types so consumers referencing spansegment.* continue to work.
type (
	AuthPolicy    = routes.AuthPolicy
	AuthRoute     = routes.AuthRoute
	DBHandle      = routes.DBHandle
	SessionStore  = routes.SessionStore
)

const (
	AuthPolicyPublic     = routes.AuthPolicyPublic
	AuthPolicySession    = routes.AuthPolicySession
	AuthPolicyAPIKeyOrSession = routes.AuthPolicyAPIKeyOrSession
	AuthPolicyAdmin      = routes.AuthPolicyAdmin
)

// Segment is the interface for a domain-specific HTTP segment.
// Each segment owns its handler types and returns the routes it
// serves via Routes(). The Router uses this seam to register
// routes without importing domain-specific handler types.
type Segment interface {
	Routes() []AuthRoute
}

// --- SpanHandler ---

// SpanHandler handles POST /api/v1/spans/query (paginated span list),
// GET /api/v1/traces/:traceId (single-trace waterfall detail),
// and GET /api/v1/projects (project list for the UI project switcher).
type SpanHandler struct {
	SessionStore    SessionStore
	ProjectResolver auth.ProjectResolver
	Metrics         *metrics.QueryMetrics
	// Lake is the DuckDB handle attached read-only to the Lake. All span
	// reads compile against lake.spans (ADR-0004). It is also used for
	// trace tree score loading in buildTraceTree / withScores.
	Lake DBHandle
	// QueryBuilder encapsulates the full DSL → SQL → execute → scan pipeline
	// for span and analytics queries. It owns the Lake connection, query
	// compilation, row scanning, and cursor computation.
	QueryBuilder *querybuild.QueryBuilder
}

// Routes returns the span-related API routes as AuthRoute entries with
// AuthPolicySession so the Router can use them for policy-based auth dispatch.
func (h *SpanHandler) Routes() []AuthRoute {
	return []AuthRoute{
		{Method: http.MethodGet, Path: "/api/v1/projects", Handler: http.HandlerFunc(h.HandleProjects), Policy: AuthPolicySession},
		{Method: http.MethodPost, Path: "/api/v1/spans/query", Handler: http.HandlerFunc(h.HandleSpansQuery), Policy: AuthPolicySession},
		{Method: http.MethodPost, Path: "/api/v1/spans/batch", Handler: http.HandlerFunc(h.HandleSpansBatch), Policy: AuthPolicySession},
		{Method: http.MethodPost, Path: "/api/v1/analytics/spans", Handler: http.HandlerFunc(h.HandleAnalyticsSpans), Policy: AuthPolicySession},
		{Method: http.MethodGet, Path: "/api/v1/traces/{traceId}", Handler: http.HandlerFunc(h.HandleTraceDetail), Policy: AuthPolicySession},
	}
}

// writeJSONError writes a JSON error response with the given status code.
func writeJSONError(w http.ResponseWriter, message string, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": message})
}

// --- HandleSpansQuery ---

// HandleSpansQuery handles POST /api/v1/spans/query.
func (h *SpanHandler) HandleSpansQuery(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	defer func() {
		if h.Metrics != nil {
			h.Metrics.RecordRequestDuration("/api/v1/spans/query", time.Since(start).Seconds())
		}
	}()

	if r.Method != http.MethodPost {
		writeJSONError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Decode into a raw map first to validate filters is an array.
	var rawBody map[string]json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&rawBody); err != nil {
		writeJSONError(w, "invalid JSON body", http.StatusBadRequest)
		return
	}

	// Validate that filters (if present) is a JSON array.
	if rawFilters, ok := rawBody["filters"]; ok {
		var filtersKind []json.RawMessage
		if err := json.Unmarshal(rawFilters, &filtersKind); err != nil {
			writeJSONError(w, "filters must be an array of {field, op, value} objects", http.StatusBadRequest)
			return
		}
	}

	// Now decode into the typed struct.
	var req query.SpanQueryRequest
	if rawFrom, ok := rawBody["from"]; ok {
		if err := json.Unmarshal(rawFrom, &req.From); err != nil {
			writeJSONError(w, "invalid 'from' field: expected RFC 3339 timestamp", http.StatusBadRequest)
			return
		}
	}
	if rawTo, ok := rawBody["to"]; ok {
		if err := json.Unmarshal(rawTo, &req.To); err != nil {
			writeJSONError(w, "invalid 'to' field: expected RFC 3339 timestamp", http.StatusBadRequest)
			return
		}
	}
	if rawFilters, ok := rawBody["filters"]; ok {
		if err := json.Unmarshal(rawFilters, &req.Filters); err != nil {
			writeJSONError(w, "filters must be an array of {field, op, value} objects", http.StatusBadRequest)
			return
		}
	}
	if rawCursor, ok := rawBody["cursor"]; ok {
		if err := json.Unmarshal(rawCursor, &req.Cursor); err != nil {
			writeJSONError(w, "invalid 'cursor' field: expected string", http.StatusBadRequest)
			return
		}
	}
	if rawLimit, ok := rawBody["limit"]; ok {
		if err := json.Unmarshal(rawLimit, &req.Limit); err != nil {
			writeJSONError(w, "invalid 'limit' field: expected number", http.StatusBadRequest)
			return
		}
	}

	// Resolve project_id: honor an explicit project_id in the body (the UI
	// project switcher) after an org-membership check, else the session
	// default.
	var bodyProjectID string
	if rawPID, ok := rawBody["project_id"]; ok {
		if err := json.Unmarshal(rawPID, &bodyProjectID); err != nil {
			writeJSONError(w, "invalid 'project_id' field: expected string", http.StatusBadRequest)
			return
		}
	}
	if bodyProjectID != "" {
		r = auth.WithExplicitProjectID(r, bodyProjectID)
	}
	resolver := h.resolveProjectID()
	projectID, ok := auth.ProjectIDWithErrorWithResolver(w, r, resolver)
	if !ok {
		return
	}

	// Delegate to the QueryBuilder for the full pipeline.
	if h.QueryBuilder == nil {
		writeJSONError(w, "query builder not configured", http.StatusServiceUnavailable)
		return
	}
	dslQuery := &dsl.Query{
		From:      req.From,
		To:        req.To,
		Limit:     req.Limit,
		Cursor:    req.Cursor,
		ProjectID: projectID,
		QueryType: dsl.QueryTypeSpan,
	}
	for _, f := range req.Filters {
		dslQuery.Filters = append(dslQuery.Filters, dsl.Filter{
			Field: f.Field,
			Op:    dsl.FilterOp(f.Op),
			Value: f.Value,
		})
	}

	result, err := h.QueryBuilder.Execute(r.Context(), dslQuery)
	if err != nil {
		if querybuild.IsValidationError(err) {
			writeJSONError(w, err.Error(), http.StatusBadRequest)
		} else {
			writeJSONError(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}

	resp := &query.SpanResponse{
		Spans: result.Spans,
		Next:  result.Next,
		Limit: result.Limit,
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		writeJSONError(w, "encode error", http.StatusInternalServerError)
		return
	}
}

// --- HandleTraceDetail ---

// HandleTraceDetail handles GET /api/v1/traces/{traceId}.
func (h *SpanHandler) HandleTraceDetail(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	defer func() {
		if h.Metrics != nil {
			h.Metrics.RecordRequestDuration("/api/v1/traces/{traceId}", time.Since(start).Seconds())
		}
	}()

	if r.Method != http.MethodGet {
		writeJSONError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	traceID := r.PathValue("traceId")
	if traceID == "" {
		writeJSONError(w, "missing trace ID", http.StatusBadRequest)
		return
	}

	// Resolve project_id: honor an explicit ?project_id= (the UI project
	// switcher) after an org-membership check, else the session default.
	if pid := r.URL.Query().Get("project_id"); pid != "" {
		r = auth.WithExplicitProjectID(r, pid)
	}
	resolver := h.resolveProjectID()
	projectID, ok := auth.ProjectIDWithErrorWithResolver(w, r, resolver)
	if !ok {
		return
	}

	// Query all spans for this trace in this project.
	spans, err := h.querySpansForTrace(projectID, traceID)
	if err != nil {
		writeJSONError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	// Return 404 if no spans found.
	if len(spans) == 0 {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": "trace not found"})
		return
	}

	// Build the trace tree (includes score loading from lake.scores).
	trace := buildTraceTree(spans, h.Lake, traceID, projectID, "lake.scores")

	// Compute trace-level rollups (sum across all spans) so the UI header
	// pill reflects total usage rather than just the root span's own
	// (typically zero) values. See #137.
	var totalInputTokens, totalOutputTokens int64
	var totalCostUSD float64
	for _, s := range trace.Spans {
		totalInputTokens += s.InputTokens
		totalOutputTokens += s.OutputTokens
		totalCostUSD += s.CostUSD
	}

	resp := domain.TraceResponse{
		TraceID:           trace.TraceID,
		ProjectID:         trace.ProjectID,
		RootSpan:          trace.RootSpan,
		Spans:             trace.Spans,
		TotalInputTokens:  totalInputTokens,
		TotalOutputTokens: totalOutputTokens,
		TotalCostUSD:      totalCostUSD,
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		writeJSONError(w, "encode error", http.StatusInternalServerError)
		return
	}
}

// --- querySpansForTrace ---

// querySpansForTrace fetches all spans for a given trace_id and project_id
// using LakeTraceSpansSQL with dedupe on (trace_id, span_id) (per ADR-0004).
func (h *SpanHandler) querySpansForTrace(projectID, traceID string) ([]*domain.Span, error) {
	req := query.SpanQueryRequest{
		From: time.Now().Add(-90 * 24 * time.Hour),
		To:   time.Now(),
		Filters: []query.SpanQueryFilter{
			{Field: "trace_id", Op: "eq", Value: traceID},
		},
	}

	q, err := query.NewSpanQuery(projectID, req)
	if err != nil {
		return nil, fmt.Errorf("build span query: %w", err)
	}

	sqlStr, args := q.LakeTraceSpansSQL(traceID)
	rows, err := h.Lake.Query(sqlStr, args...)
	if err != nil {
		return nil, fmt.Errorf("execute lake trace spans query: %w", err)
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

// --- scanAllRows ---

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

// --- HandleProjects ---

// HandleProjects handles GET /api/v1/projects.
func (h *SpanHandler) HandleProjects(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSONError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	projects, err := h.SessionStore.ListProjects(r)
	if err != nil {
		writeJSONError(w, "error listing projects", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(projects); err != nil {
		writeJSONError(w, "encode error", http.StatusInternalServerError)
		return
	}
}

// --- HandleAnalyticsSpans ---

// HandleAnalyticsSpans handles POST /api/v1/analytics/spans.
func (h *SpanHandler) HandleAnalyticsSpans(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req dsl.Query
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, "invalid JSON body", http.StatusBadRequest)
		return
	}

	if req.ProjectID != "" {
		r = auth.WithExplicitProjectID(r, req.ProjectID)
	}
	resolver := h.resolveProjectID()
	projectID, ok := auth.ProjectIDWithErrorWithResolver(w, r, resolver)
	if !ok {
		return
	}

	if h.QueryBuilder == nil {
		writeJSONError(w, "query builder not configured", http.StatusServiceUnavailable)
		return
	}
	req.ProjectID = projectID
	req.QueryType = dsl.QueryTypeAnalytics

	result, err := h.QueryBuilder.Execute(r.Context(), &req)
	if err != nil {
		if querybuild.IsValidationError(err) {
			writeJSONError(w, err.Error(), http.StatusBadRequest)
		} else {
			writeJSONError(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}

	resp := AnalyticsResponse{
		Rows: result.Rows,
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		writeJSONError(w, "encode error", http.StatusInternalServerError)
		return
	}
}

// AnalyticsResponse is the JSON body returned by POST /api/v1/analytics/spans.
type AnalyticsResponse struct {
	Rows []map[string]any `json:"rows"`
}

// --- HandleSpansBatch ---

// HandleSpansBatch handles POST /api/v1/spans/batch.
func (h *SpanHandler) HandleSpansBatch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req BatchSpansRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, "invalid JSON body", http.StatusBadRequest)
		return
	}

	if len(req.SpanIDs) == 0 {
		writeJSONError(w, "span_ids array is required", http.StatusBadRequest)
		return
	}

	resolver := h.resolveProjectID()
	projectID, ok := auth.ProjectIDWithErrorWithResolver(w, r, resolver)
	if !ok {
		return
	}

	placeholders := make([]string, len(req.SpanIDs))
	args := make([]any, len(req.SpanIDs))
	for i, id := range req.SpanIDs {
		placeholders[i] = "?"
		args[i] = id
	}
	args = append(args, projectID)
	query := fmt.Sprintf("SELECT span_id, name, CAST(input AS VARCHAR), CAST(output AS VARCHAR) FROM lake.spans WHERE span_id IN (%s) AND project_id = ?", strings.Join(placeholders, ", "))

	rows, err := h.Lake.QueryContext(r.Context(), query, args...)
	if err != nil {
		writeJSONError(w, "query error: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var batchSpans []BatchSpanResponse
	for rows.Next() {
		var spanID, name string
		var inputJSON, outputJSON *string
		if err := rows.Scan(&spanID, &name, &inputJSON, &outputJSON); err != nil {
			writeJSONError(w, "scan error: "+err.Error(), http.StatusInternalServerError)
			return
		}

		inputStr := ""
		if inputJSON != nil {
			inputStr = *inputJSON
		}
		outputStr := ""
		if outputJSON != nil {
			outputStr = *outputJSON
		}

		batchSpans = append(batchSpans, BatchSpanResponse{
			SpanID: spanID,
			Name:   name,
			Input:  inputStr,
			Output: outputStr,
		})
	}

	if err := rows.Err(); err != nil {
		writeJSONError(w, "row error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(BatchSpansResponse{Spans: batchSpans})
}

// BatchSpansRequest is the JSON body for POST /api/v1/spans/batch.
type BatchSpansRequest struct {
	SpanIDs []string `json:"span_ids"`
}

// BatchSpanResponse is a single span in the batch response.
type BatchSpanResponse struct {
	SpanID string `json:"span_id"`
	Name   string `json:"name"`
	Input  string `json:"input"`
	Output string `json:"output"`
}

// BatchSpansResponse is the JSON body for POST /api/v1/spans/batch.
type BatchSpansResponse struct {
	Spans []BatchSpanResponse `json:"spans"`
}

// --- buildTraceTree / withScores ---

// buildTraceTree groups spans by trace_id and links parent-child relationships.
func buildTraceTree(spans []*domain.Span, db DBHandle, traceID, projectID, scoresTable string) domain.Trace {
	if len(spans) == 0 {
		return domain.Trace{}
	}

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

	for _, s := range spans {
		if s.ParentID != "" {
			if parent, ok := spanMap[s.ParentID]; ok {
				parent.Children = append(parent.Children, s)
			}
		}
	}

	if root == nil && len(spans) > 0 {
		root = spans[0]
	}

	trace.RootSpan = root
	trace.Spans = spans

	if db != nil && traceID != "" && projectID != "" {
		trace.Spans = withScores(db, trace.Spans, traceID, projectID, scoresTable)
	}

	return trace
}

// withScores loads scores for the given spans and attaches them inline.
func withScores(db DBHandle, spans []*domain.Span, traceID, projectID, scoresTable string) []*domain.Span {
	rows, err := db.QueryContext(context.Background(),
		"SELECT score_id, span_id, trace_id, project_id, eval_name, value, reasoning, judge_model, "+
			"prompt_name, prompt_version, created_at FROM "+scoresTable+
			" WHERE trace_id = ? AND project_id = ?",
		traceID, projectID,
	)
	if err != nil {
		return spans
	}
	defer rows.Close()

	scoresBySpan := make(map[string][]domain.Score)
	for rows.Next() {
		var s domain.Score
		var createdAtStr string
		var judgeModelStr *string
		var promptNameStr *string
		var promptVer *int64
		if err := rows.Scan(&s.ScoreID, &s.SpanID, &s.TraceID, &s.ProjectID, &s.EvalName, &s.Value, &s.Reasoning, &judgeModelStr, &promptNameStr, &promptVer, &createdAtStr); err != nil {
			continue
		}
		if judgeModelStr != nil {
			s.JudgeModel = *judgeModelStr
		}
		if promptNameStr != nil {
			s.PromptName = *promptNameStr
		}
		if promptVer != nil {
			s.PromptVersion = *promptVer
		}
		if createdAtStr != "" {
			t, err := time.Parse(time.RFC3339, createdAtStr)
			if err == nil {
				s.CreatedAt = t
			}
		}
		scoresBySpan[s.SpanID] = append(scoresBySpan[s.SpanID], s)
	}

	for _, span := range spans {
		span.Scores = scoresBySpan[span.SpanID]
	}

	return spans
}

// --- resolveProjectID ---

// resolveProjectID returns a ProjectResolver that chains h.ProjectResolver
// (if non-nil) with a fallback to h.SessionStore.ProjectID.
func (h *SpanHandler) resolveProjectID() auth.ProjectResolver {
	if h.ProjectResolver == nil && h.SessionStore != nil {
		return auth.NewSessionStoreResolver(h.SessionStore)
	}
	return h.ProjectResolver
}