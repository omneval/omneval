package handler

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/zbloss/lantern/internal/domain"
	"github.com/zbloss/lantern/internal/metadata"
)

// extractProjectID extracts the authenticated project ID from the request.
// Returns an empty string and false when no session store is configured.
func extractProjectID(SessionStore SessionStore, r *http.Request) (string, bool) {
	if SessionStore == nil {
		return "", false
	}
	return SessionStore.ProjectID(r)
}

// EvalRuleHandler handles eval rule CRUD endpoints:
//
//	POST   /api/v1/eval-rules          — create an eval rule
//	GET    /api/v1/eval-rules          — list all eval rules for the project
//	DELETE /api/v1/eval-rules/:id      — delete an eval rule by ID
//	POST   /api/v1/eval-rules/preview  — preview matching spans for a filter
type EvalRuleHandler struct {
	DB           *sql.DB
	Store        metadata.Store
	SessionStore SessionStore
}

// ---- Request / Response Types ----

// CreateEvalRuleRequest is the body accepted by POST /api/v1/eval-rules.
type CreateEvalRuleRequest struct {
	Name          string            `json:"name"`
	JudgeModel    string            `json:"judge_model"`
	PromptName    string            `json:"prompt_name"`
	PromptVersion int64             `json:"prompt_version"`
	SampleRate    float64           `json:"sample_rate"`
	Enabled       bool              `json:"enabled"`
	Filter        domain.EvalFilter `json:"filter"`
}

// ListEvalRulesResponse is returned by GET /api/v1/eval-rules.
type ListEvalRulesResponse struct {
	Rules []*domain.EvalRule `json:"rules"`
}

// PreviewEvalRulesRequest is the body accepted by POST /api/v1/eval-rules/preview.
type PreviewEvalRulesRequest struct {
	Filter domain.EvalFilter `json:"filter"`
}

// PreviewSpan is a lightweight span summary returned by the preview endpoint.
type PreviewSpan struct {
	SpanID      string    `json:"span_id"`
	TraceID     string    `json:"trace_id"`
	Name        string    `json:"name"`
	Kind        string    `json:"kind"`
	Model       string    `json:"model"`
	StartTime   time.Time `json:"start_time"`
	CostUSD     float64   `json:"cost_usd"`
}

// PreviewEvalRulesResponse is returned by POST /api/v1/eval-rules/preview.
type PreviewEvalRulesResponse struct {
	Spans          []*PreviewSpan `json:"spans"`
	MatchCount24h  int64          `json:"match_count_24h"`
}

// ---- HTTP Handlers ----

// HandleCreate handles POST /api/v1/eval-rules.
func (h *EvalRuleHandler) HandleCreate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	projectID, ok := extractProjectID(h.SessionStore, r)
	if !ok || projectID == "" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var req CreateEvalRuleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}

	// Validate required fields.
	if req.Name == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}
	if req.JudgeModel == "" {
		http.Error(w, "judge_model is required", http.StatusBadRequest)
		return
	}

	// Validate the full recursive filter structure.
	if err := req.Filter.Validate(); err != nil {
		http.Error(w, "invalid filter: "+err.Error(), http.StatusBadRequest)
		return
	}

	// Validate and default sample_rate.
	sampleRate := req.SampleRate
	if sampleRate < 0.0 || sampleRate > 1.0 {
		http.Error(w, "sample_rate must be between 0.0 and 1.0", http.StatusBadRequest)
		return
	}
	if sampleRate == 0.0 {
		sampleRate = 1.0
	}

	// Default enabled to true (Go bool zero value is false, but we want true).
	enabled := true

	rule := &domain.EvalRule{
		RuleID:        uuid.New().String(),
		ProjectID:     projectID,
		Name:          req.Name,
		JudgeModel:    req.JudgeModel,
		PromptName:    req.PromptName,
		PromptVersion: req.PromptVersion,
		Filter:        req.Filter,
		SampleRate:    sampleRate,
		Enabled:       enabled,
		CreatedAt:     time.Now().UTC(),
	}

	if err := h.Store.CreateEvalRule(r.Context(), rule); err != nil {
		http.Error(w, "store error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(rule)
}

// HandleList handles GET /api/v1/eval-rules.
func (h *EvalRuleHandler) HandleList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	projectID, ok := extractProjectID(h.SessionStore, r)
	if !ok || projectID == "" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	rules, err := h.Store.ListEvalRules(r.Context(), projectID)
	if err != nil {
		http.Error(w, "store error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(ListEvalRulesResponse{Rules: rules})
}

// HandleDelete handles DELETE /api/v1/eval-rules/:id.
func (h *EvalRuleHandler) HandleDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	projectID, ok := extractProjectID(h.SessionStore, r)
	if !ok || projectID == "" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	ruleID := r.PathValue("id")
	if ruleID == "" {
		// Fallback for httptest requests that don't use ServeMux pattern matching.
		parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		if len(parts) >= 3 && parts[0] == "api" && parts[1] == "v1" && parts[2] == "eval-rules" {
			ruleID = parts[3]
		}
	}
	if ruleID == "" {
		http.Error(w, "rule ID is required", http.StatusBadRequest)
		return
	}

	// Verify the rule exists and belongs to the project.
	existing, err := h.Store.GetEvalRule(r.Context(), ruleID)
	if err != nil {
		if errors.Is(err, metadata.ErrNotFound) {
			http.Error(w, "eval rule not found", http.StatusNotFound)
			return
		}
		http.Error(w, "store error", http.StatusInternalServerError)
		return
	}

	if existing.ProjectID != projectID {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	if err := h.Store.DeleteEvalRule(r.Context(), ruleID); err != nil {
		if errors.Is(err, metadata.ErrNotFound) {
			http.Error(w, "eval rule not found", http.StatusNotFound)
			return
		}
		http.Error(w, "store error", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// HandlePreview handles POST /api/v1/eval-rules/preview.
// It evaluates the provided filter against recent span data in DuckDB
// without persisting anything. Returns up to 50 matching spans from the
// last hour plus a total match count for the last 24 hours.
func (h *EvalRuleHandler) HandlePreview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	projectID, ok := extractProjectID(h.SessionStore, r)
	if !ok || projectID == "" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var req PreviewEvalRulesRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}

	// Validate the filter structure.
	if err := req.Filter.Validate(); err != nil {
		http.Error(w, "invalid filter: "+err.Error(), http.StatusBadRequest)
		return
	}

	// Reject empty filters (no conditions = match everything).
	if !hasFilterConditions(req.Filter) {
		http.Error(w, "filter must have at least one condition", http.StatusBadRequest)
		return
	}

	// If no DB connection is available, return empty results.
	if h.DB == nil {
		resp := PreviewEvalRulesResponse{
			Spans:         nil,
			MatchCount24h: 0,
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
		return
	}

	now := time.Now().UTC()
	hourAgo := now.Add(-1 * time.Hour)
	dayAgo := now.Add(-24 * time.Hour)

	// Query spans from the last hour for the project.
	query := fmt.Sprintf(`
		SELECT span_id, trace_id, name, kind, model, start_time, cost_usd
		FROM spans
		WHERE project_id = ?
		  AND start_time >= ?
		ORDER BY start_time DESC, span_id ASC
		LIMIT 50
	`)

	rows, err := h.DB.QueryContext(r.Context(), query, projectID, hourAgo)
	if err != nil {
		http.Error(w, "query execution error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	// Scan into span structs.
	var matchingSpans []*PreviewSpan
	for rows.Next() {
		var s PreviewSpan
		if err := rows.Scan(&s.SpanID, &s.TraceID, &s.Name, &s.Kind, &s.Model, &s.StartTime, &s.CostUSD); err != nil {
			http.Error(w, "scan error", http.StatusInternalServerError)
			return
		}
		matchingSpans = append(matchingSpans, &s)
	}
	if err := rows.Err(); err != nil {
		http.Error(w, "row iteration error", http.StatusInternalServerError)
		return
	}

	// Evaluate each span against the filter in-memory.
	var matched []*PreviewSpan
	for _, s := range matchingSpans {
		if req.Filter.Matches(spanFromPreview(s)) {
			matched = append(matched, s)
		}
	}

	// Count total matches in the last 24 hours.
	var matchCount24h int64
	countQuery := fmt.Sprintf(`
		SELECT span_id, trace_id, name, kind, model, start_time, cost_usd
		FROM spans
		WHERE project_id = ?
		  AND start_time >= ?
	`)

	countRows, err := h.DB.QueryContext(r.Context(), countQuery, projectID, dayAgo)
	if err == nil {
		defer countRows.Close()
		for countRows.Next() {
			var s PreviewSpan
			if err := countRows.Scan(&s.SpanID, &s.TraceID, &s.Name, &s.Kind, &s.Model, &s.StartTime, &s.CostUSD); err != nil {
				break
			}
			if req.Filter.Matches(spanFromPreview(&s)) {
				matchCount24h++
			}
		}
		countRows.Err() // ignore error from iteration
	}

	resp := PreviewEvalRulesResponse{
		Spans:         matched,
		MatchCount24h: matchCount24h,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// spanFromPreview converts a PreviewSpan back to a domain.Span for filter matching.
func spanFromPreview(p *PreviewSpan) *domain.Span {
	return &domain.Span{
		SpanID:    p.SpanID,
		TraceID:   p.TraceID,
		Name:      p.Name,
		Kind:      domain.SpanKind(p.Kind),
		Model:     p.Model,
		StartTime: p.StartTime,
		CostUSD:   p.CostUSD,
	}
}

// hasFilterConditions checks if the filter has at least one leaf condition.
// Recursively checks AND/OR sub-filters.
func hasFilterConditions(f domain.EvalFilter) bool {
	// Check leaf conditions.
	hasLeaf := f.Kind != nil ||
		f.Model != nil ||
		f.ServiceName != nil ||
		f.PromptName != nil ||
		f.StatusCode != nil ||
		f.MinCostUSD != nil ||
		f.MaxCostUSD != nil ||
		f.MinDurationMS != nil ||
		f.MaxDurationMS != nil ||
		len(f.AttributesMatch) > 0

	if hasLeaf {
		return true
	}

	// Check sub-filters.
	for _, sub := range f.And {
		if hasFilterConditions(*sub) {
			return true
		}
	}
	for _, sub := range f.Or {
		if hasFilterConditions(*sub) {
			return true
		}
	}
	if f.Not != nil {
		return hasFilterConditions(*f.Not)
	}

	return false
}
