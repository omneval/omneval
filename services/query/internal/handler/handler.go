package handler

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/omneval/omneval/internal/domain"
	"github.com/omneval/omneval/internal/idgen"
	"github.com/omneval/omneval/internal/metadata"
	"github.com/omneval/omneval/services/query/internal/auth"
	"github.com/omneval/omneval/services/query/internal/dsl"
	"github.com/omneval/omneval/services/query/internal/metrics"
	"github.com/omneval/omneval/services/query/internal/query"
)

// SpanHandler handles POST /api/v1/spans/query (paginated span list),
// GET /api/v1/traces/:traceId (single-trace waterfall detail),
// and GET /api/v1/projects (project list for the UI project switcher).
type SpanHandler struct {
	DB           DBHandle
	SessionStore SessionStore
	Metrics      *metrics.QueryMetrics
	// Lake is the DuckDB handle attached read-only to the Lake.
	// When non-nil, all span reads compile against lake.spans instead
	// of the S3 snapshot path (ADR-0004).
	Lake DBHandle
	// Meta resolves bookmarked trace IDs for "bookmarked" filters —
	// bookmarks live in the Metadata Store, not DuckDB (ADR-0004).
	Meta metadata.Store
}

// SessionStore abstracts session lookup for project ID extraction.
type SessionStore interface {
	ProjectID(r *http.Request) (string, bool)
	ListProjects(r *http.Request) ([]*domain.Project, error)
}

// resolveProjectID determines the project_id a request should query.
//
// When explicitID is non-empty (the UI project switcher includes it in the
// request body / query string), it is honored after verifying it belongs to
// the authenticated user's org. When it is empty, we fall back to the
// session-derived default, which returns the org's first project.
//
// All UI-facing read endpoints (span list, trace detail, analytics) share this
// helper so the Traces page, Trace Detail, and Dashboard always resolve to the
// same project. Previously the span list and trace detail used only the session
// default while the Dashboard honored the body project_id, so selecting a
// non-default project in the switcher showed data on the Dashboard but an empty
// Traces page.
//
// On failure it writes the appropriate HTTP error and returns ok=false; callers
// should return immediately.
func (h *SpanHandler) resolveProjectID(w http.ResponseWriter, r *http.Request, explicitID string) (string, bool) {
	if explicitID == "" {
		projectID, ok := h.SessionStore.ProjectID(r)
		if !ok || projectID == "" {
			if auth.CurrentUserFromContext(r) != nil {
				writeJSONError(w, "no project found — create a project first via POST /api/v1/projects", http.StatusBadRequest)
			} else {
				writeJSONError(w, "unauthorized", http.StatusUnauthorized)
			}
			return "", false
		}
		return projectID, true
	}

	// An explicit project_id must belong to the authenticated user's org.
	if auth.CurrentUserFromContext(r) == nil {
		writeJSONError(w, "unauthorized", http.StatusUnauthorized)
		return "", false
	}
	userProjects, err := h.SessionStore.ListProjects(r)
	if err != nil {
		writeJSONError(w, "unauthorized", http.StatusUnauthorized)
		return "", false
	}
	for _, p := range userProjects {
		if p.ProjectID == explicitID {
			return explicitID, true
		}
	}
	writeJSONError(w, "project_id not found in user's organizations", http.StatusForbidden)
	return "", false
}

// HandleSpansQuery handles POST /api/v1/spans/query.
// It extracts project_id from the authenticated session, builds the SQL query
// with keyset cursor pagination, executes the hot+cold UNION, and returns
// a paginated span list with a next cursor.
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
	// default. Mirrors the analytics endpoint so the Traces page and Dashboard
	// always query the same project.
	var bodyProjectID string
	if rawPID, ok := rawBody["project_id"]; ok {
		if err := json.Unmarshal(rawPID, &bodyProjectID); err != nil {
			writeJSONError(w, "invalid 'project_id' field: expected string", http.StatusBadRequest)
			return
		}
	}
	projectID, ok := h.resolveProjectID(w, r, bodyProjectID)
	if !ok {
		return
	}

	// Build the query — projectID is validated server-side against the user's
	// org; the raw client value is never trusted directly.
	q, err := query.NewSpanQuery(projectID, req, nil, "")
	if err != nil {
		writeJSONError(w, "bad request: "+err.Error(), http.StatusBadRequest)
		return
	}

	// "bookmarked" filters need the project's starred trace IDs from the
	// Metadata Store before SQL compilation.
	if q.NeedsBookmarks() && h.Meta != nil {
		ids, err := h.Meta.ListBookmarkedTraceIDs(r.Context(), projectID)
		if err != nil {
			writeJSONError(w, "bookmark lookup error: "+err.Error(), http.StatusInternalServerError)
			return
		}
		q.SetBookmarkedTraceIDs(ids)
	}

	sqlStr, args, err := h.compileSpanQuery(q, false)
	if err != nil {
		writeJSONError(w, "query compilation error", http.StatusInternalServerError)
		return
	}

	dbHandle := h.selectDBForSpans(false)
	rows, err := dbHandle.Query(sqlStr, args...)
	if err != nil {
		writeJSONError(w, "query execution error: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	// Scan rows into domain spans.
	spanRows, err := scanAllRows(rows)
	if err != nil {
		writeJSONError(w, "scan error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	spans, err := query.ScanRows(spanRows)
	if err != nil {
		writeJSONError(w, "row scan error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Compute next cursor using the effective limit from the query.
	// This handles the case where req.Limit was 0 (unset) and the
	// query defaulted to DefaultLimit.
	// The SQL fetches limit+1 rows; NextCursor determines whether more
	// pages exist based on whether we got more than limit results.
	next := query.NextCursor(spans, q.EffectiveLimit())

	// Truncate to the requested page size — the extra row was only used to
	// detect whether a next page exists and must not be returned to callers.
	if len(spans) > q.EffectiveLimit() {
		spans = spans[:q.EffectiveLimit()]
	}

	resp := query.SpanResponse{
		Spans: spans,
		Next:  next,
		Limit: q.EffectiveLimit(),
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		writeJSONError(w, "encode error", http.StatusInternalServerError)
		return
	}
}

// HandleTraceDetail handles GET /api/v1/traces/{traceId}.
// Returns the span tree for a single trace with inline scores.
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
	projectID, ok := h.resolveProjectID(w, r, r.URL.Query().Get("project_id"))
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

	// Build the trace tree (includes score loading).
	// When Lake is available, read scores from lake.scores; otherwise
	// fall back to the snapshot scores table.
	scoresDB := h.DB
	scoresTable := "scores"
	if h.Lake != nil {
		scoresDB = h.Lake
		scoresTable = "lake.scores"
	}
	trace := buildTraceTree(spans, scoresDB, traceID, projectID, scoresTable)

	resp := domain.TraceResponse{
		TraceID:   trace.TraceID,
		ProjectID: trace.ProjectID,
		RootSpan:  trace.RootSpan,
		Spans:     trace.Spans,
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		writeJSONError(w, "encode error", http.StatusInternalServerError)
		return
	}
}

// querySpansForTrace fetches all spans for a given trace_id and project_id.
// When Lake is available, it uses LakeTraceSpansSQL with dedupe (per ADR-0004).
func (h *SpanHandler) querySpansForTrace(projectID, traceID string) ([]*domain.Span, error) {
	req := query.SpanQueryRequest{
		From: time.Time{},
		To:   time.Date(9999, 12, 31, 23, 59, 59, 0, time.UTC),
		Filters: []query.SpanQueryFilter{
			{Field: "trace_id", Op: "eq", Value: traceID},
		},
		Limit: 10000,
	}

	q, err := query.NewSpanQuery(projectID, req, nil, "")
	if err != nil {
		return nil, fmt.Errorf("build span query: %w", err)
	}

	// When Lake is available, use LakeTraceSpansSQL with dedupe on (trace_id, span_id).
	if h.Lake != nil {
		sqlStr, args := q.LakeTraceSpansSQL(traceID, true)
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

// HandleProjects handles GET /api/v1/projects.
// Returns the list of projects for the authenticated user's organization.
// Used by the frontend project switcher dropdown.
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

// HandleAnalyticsSpans handles POST /api/v1/analytics/spans.
// Accepts a structured Query (filters, aggregations, group_by, order_by, limit),
// compiles it to parameterized DuckDB SQL via the DSL compiler, executes
// the hot+cold UNION, and returns the aggregated rows.
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

	// Determine project_id: prefer the one from the request body (the UI
	// project switcher), validated against the user's org; otherwise fall back
	// to the session default. Shared with the span list / trace detail
	// endpoints so all read paths resolve to the same project.
	projectID, ok := h.resolveProjectID(w, r, req.ProjectID)
	if !ok {
		return
	}

	// Validate and apply default time range (last 30 days when omitted).
	if err := req.Validate(); err != nil {
		writeJSONError(w, "bad request: "+err.Error(), http.StatusBadRequest)
		return
	}

	// Compile the query — all fields are validated against allowlists.
	// When Lake is available, compile against the single Lake table set.
	var sqlStr string
	var args []any
	var err error
	if h.Lake != nil {
		sqlStr, args, err = dsl.CompileLake(projectID, req)
	} else {
		sqlStr, args, err = dsl.Compile(projectID, req)
	}
	if err != nil {
		writeJSONError(w, "query compilation error: "+err.Error(), http.StatusBadRequest)
		return
	}

	dbHandle := h.selectDBForAnalytics()
	rows, err := dbHandle.Query(sqlStr, args...)
	if err != nil {
		writeJSONError(w, "query execution error: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	// Scan rows into column headers + [][]any.
	cols, err := rows.ColumnTypes()
	if err != nil {
		writeJSONError(w, "scan column types: "+err.Error(), http.StatusInternalServerError)
		return
	}

	var result []map[string]any
	for rows.Next() {
		values := make([]any, len(cols))
		valuePtrs := make([]any, len(cols))
		for i := range values {
			valuePtrs[i] = &values[i]
		}
		if err := rows.Scan(valuePtrs...); err != nil {
			writeJSONError(w, "scan row: "+err.Error(), http.StatusInternalServerError)
			return
		}

		row := make(map[string]any, len(cols))
		for i, col := range cols {
			row[col.Name()] = values[i]
		}
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		writeJSONError(w, "row iteration: "+err.Error(), http.StatusInternalServerError)
		return
	}

	resp := AnalyticsResponse{
		Rows: result,
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

// buildTraceTree groups spans by trace_id and links parent-child relationships.
// It sets Span.Children for proper waterfall rendering.
func buildTraceTree(spans []*domain.Span, db DBHandle, traceID, projectID, scoresTable string) domain.Trace {
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

	// Load scores for all spans in this trace.
	if db != nil && traceID != "" && projectID != "" {
		trace.Spans = withScores(db, trace.Spans, traceID, projectID, scoresTable)
	}

	return trace
}

// withScores loads scores for the given spans and attaches them inline.
// scoresTable should be "scores" for snapshot DB or "lake.scores" for Lake.
func withScores(db DBHandle, spans []*domain.Span, traceID, projectID, scoresTable string) []*domain.Span {
	rows, err := db.QueryContext(context.Background(),
		"SELECT span_id, eval_name, value, reasoning, judge_model, "+
			"prompt_name, prompt_version, created_at FROM "+scoresTable+
			" WHERE trace_id = ? AND project_id = ?",
		traceID, projectID,
	)
	if err != nil {
		return spans // non-fatal, continue without scores
	}
	defer rows.Close()

	// Map span_id -> scores.
	scoresBySpan := make(map[string][]domain.Score)
	for rows.Next() {
		var s domain.Score
		var createdAtStr string
		var judgeModelStr *string
		var promptNameStr *string
		var promptVer *int64
		if err := rows.Scan(&s.SpanID, &s.EvalName, &s.Value, &s.Reasoning, &judgeModelStr, &promptNameStr, &promptVer, &createdAtStr); err != nil {
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
		// Parse the timestamp if available.
		if createdAtStr != "" {
			t, err := time.Parse(time.RFC3339, createdAtStr)
			if err == nil {
				s.CreatedAt = t
			}
		}
		scoresBySpan[s.SpanID] = append(scoresBySpan[s.SpanID], s)
	}

	// Attach scores to spans.
	for _, span := range spans {
		span.Scores = scoresBySpan[span.SpanID]
	}

	return spans
}

// writeJSONError writes a JSON error response with the given status code.
// It sets Content-Type to application/json and writes {"error": message}.
func writeJSONError(w http.ResponseWriter, message string, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": message})
}

// compileSpanQuery compiles a SpanQuery to SQL.
// When Lake is available and dedupe is false (span list), it uses LakeSQL.
// When Lake is available and dedupe is true (trace detail), it uses LakeSQL with dedupe.
func (h *SpanHandler) compileSpanQuery(q *query.SpanQuery, dedupe bool) (string, []any, error) {
	if h.Lake != nil {
		return q.LakeSQL(dedupe)
	}
	return q.SQL()
}

// selectDBForSpans returns the database handle to use for span reads.
// When Lake is available, it returns the Lake handle; otherwise the snapshot DB.
func (h *SpanHandler) selectDBForSpans(_ bool) DBHandle {
	if h.Lake != nil {
		return h.Lake
	}
	return h.DB
}

// selectDBForAnalytics returns the database handle to use for analytics reads.
// When Lake is available, it returns the Lake handle; otherwise the snapshot DB.
func (h *SpanHandler) selectDBForAnalytics() DBHandle {
	if h.Lake != nil {
		return h.Lake
	}
	return h.DB
}

// ScoreHandler handles POST /api/v1/scores — the public-facing endpoint
// that allows manual score writes from the UI or API consumers.
type ScoreHandler struct {
	DB DBHandle
}

// NewScoreHandler creates a new ScoreHandler.
func NewScoreHandler(db DBHandle) http.Handler {
	h := &ScoreHandler{DB: db}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/scores", h.HandleScores)
	return mux
}

// HandleScores writes a score to DuckDB.
func (h *ScoreHandler) HandleScores(w http.ResponseWriter, r *http.Request) {
	var req domain.ScoreRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, "invalid JSON", http.StatusBadRequest)
		return
	}

	if req.SpanID == "" || req.TraceID == "" || req.ProjectID == "" {
		writeJSONError(w, "span_id, trace_id, and project_id are required", http.StatusBadRequest)
		return
	}

	scoreID := idgen.Generate()
	score := &domain.Score{
		ScoreID:       scoreID,
		SpanID:        req.SpanID,
		TraceID:       req.TraceID,
		ProjectID:     req.ProjectID,
		EvalName:      req.EvalName,
		Value:         req.Value,
		Reasoning:     req.Reasoning,
		JudgeModel:    req.JudgeModel,
		PromptName:    req.PromptName,
		PromptVersion: req.PromptVersion,
		CreatedAt:     time.Now(),
	}

	if err := h.writeScore(r.Context(), score); err != nil {
		writeJSONError(w, "write score: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]string{"score_id": scoreID})
}

// writeScore writes a score to DuckDB.
func (h *ScoreHandler) writeScore(ctx context.Context, score *domain.Score) error {
	_, err := h.DB.ExecContext(ctx, `
		INSERT INTO scores (
			score_id, span_id, trace_id, project_id,
			eval_name, value, reasoning, judge_model,
			prompt_name, prompt_version, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		score.ScoreID,
		score.SpanID,
		score.TraceID,
		score.ProjectID,
		score.EvalName,
		score.Value,
		score.Reasoning,
		score.JudgeModel,
		score.PromptName,
		score.PromptVersion,
		score.CreatedAt,
	)
	return err
}
