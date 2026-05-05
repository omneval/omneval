package handler

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/zbloss/lantern/internal/domain"
	"github.com/zbloss/lantern/internal/idgen"
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
	trace := buildTraceTree(spans, h.DB, traceID, projectID)

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
func buildTraceTree(spans []*domain.Span, db *sql.DB, traceID string, projectID string) domain.Trace {
	if len(spans) == 0 {
		return domain.Trace{}
	}

	// Sort by start_time for deterministic tree building.
	// Find the root span (no parent).
	var root *domain.Span
	trace := domain.Trace{
		TraceID: spans[0].TraceID,
	}

	// Collect project_id from spans.
	for i := range spans {
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

	// Load scores for all spans in this trace.
	if db != nil && traceID != "" && projectID != "" {
		trace.Spans = withScores(db, trace.Spans, traceID, projectID)
	}

	return trace
}

// withScores loads scores for the given spans and attaches them inline.
func withScores(db *sql.DB, spans []*domain.Span, traceID, projectID string) []*domain.Span {
	rows, err := db.QueryContext(context.Background(),
		`SELECT span_id, eval_name, value, reasoning, judge_model,
		        prompt_name, prompt_version, created_at
		 FROM scores WHERE trace_id = ? AND project_id = ?`,
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
		var promptNameStr *string
		var promptVer *int64
		if err := rows.Scan(&s.SpanID, &s.EvalName, &s.Value, &s.Reasoning, &s.JudgeModel, &promptNameStr, &promptVer, &createdAtStr); err != nil {
			continue
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

// ScoreHandler handles POST /api/v1/scores — the public-facing endpoint
// that allows manual score writes from the UI or API consumers.
type ScoreHandler struct {
	DB *sql.DB
}

// NewScoreHandler creates a new ScoreHandler.
func NewScoreHandler(db *sql.DB) http.Handler {
	h := &ScoreHandler{DB: db}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/scores", h.HandleScores)
	return mux
}

// HandleScores writes a score to DuckDB.
func (h *ScoreHandler) HandleScores(w http.ResponseWriter, r *http.Request) {
	var req domain.ScoreRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}

	if req.SpanID == "" || req.TraceID == "" || req.ProjectID == "" {
		http.Error(w, "span_id, trace_id, and project_id are required", http.StatusBadRequest)
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
		http.Error(w, fmt.Sprintf("write score: %v", err), http.StatusInternalServerError)
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


