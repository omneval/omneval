package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/omneval/omneval/internal/auth"
	"github.com/omneval/omneval/internal/domain"
	"github.com/omneval/omneval/internal/metadata"
)

// EvalRuleHandler handles eval rule CRUD endpoints:
//
//	POST   /api/v1/eval-rules          — create an eval rule
//	GET    /api/v1/eval-rules          — list all eval rules for the project
//	DELETE /api/v1/eval-rules/:id      — delete an eval rule by ID
//	POST   /api/v1/eval-rules/preview  — preview matching spans for a filter
type EvalRuleHandler struct {
	DB            DBHandle
	EvalRuleStore metadata.EvalRuleStore
	PromptStore   metadata.PromptStore
	SessionStore  SessionStore
	ProjectResolver auth.ProjectResolver
	// DefaultJudgeModel is the model used when the request omits judge_model.
	// Wired from cfg.Eval.LLMModel at startup. Falls back to "gpt-4o-mini" when empty.
	DefaultJudgeModel string
}

// resolveProjectID returns a ProjectResolver that chains h.ProjectResolver
// (if non-nil) with a fallback to h.SessionStore.ProjectID.  When both are
// nil it returns nil so callers still get the 401.
func (h *EvalRuleHandler) resolveProjectID() auth.ProjectResolver {
	if h.ProjectResolver == nil && h.SessionStore != nil {
		return auth.NewSessionStoreResolver(h.SessionStore)
	}
	return h.ProjectResolver
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
	SpanID    string    `json:"span_id"`
	TraceID   string    `json:"trace_id"`
	Name      string    `json:"name"`
	Kind      string    `json:"kind"`
	Model     string    `json:"model"`
	StartTime time.Time `json:"start_time"`
	CostUSD   float64   `json:"cost_usd"`
}

// PreviewEvalRulesResponse is returned by POST /api/v1/eval-rules/preview.
type PreviewEvalRulesResponse struct {
	Spans         []*PreviewSpan `json:"spans"`
	MatchCount24h int64          `json:"match_count_24h"`
}

// ---- HTTP Handlers ----

// HandleCreate handles POST /api/v1/eval-rules.
func (h *EvalRuleHandler) HandleCreate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	resolver := h.resolveProjectID()
	projectID, ok := auth.ProjectIDWithErrorWithResolver(w, r, resolver)
	if !ok {
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
		if h.DefaultJudgeModel != "" {
			req.JudgeModel = h.DefaultJudgeModel
		} else {
			req.JudgeModel = "gpt-4o-mini"
		}
	}

	// Validate the full recursive filter structure.
	if err := req.Filter.Validate(); err != nil {
		http.Error(w, "invalid filter: "+err.Error(), http.StatusBadRequest)
		return
	}

	// Validate that the judge prompt exists in the registry.
	if req.PromptName != "" && h.PromptStore != nil {
		// Check by version first if provided (>= 1).
		if req.PromptVersion > 0 {
			if _, err := h.PromptStore.GetPromptVersion(r.Context(), projectID, req.PromptName, req.PromptVersion); err != nil {
				http.Error(w, "prompt '"+req.PromptName+"' version "+fmt.Sprintf("%d", req.PromptVersion)+" not found", http.StatusBadRequest)
				return
			}
		}
		// Validate the prompt name exists (at least one version).
		if versions, err := h.PromptStore.ListPromptVersions(r.Context(), projectID, req.PromptName); err != nil {
			http.Error(w, "prompt '"+req.PromptName+"' not found", http.StatusBadRequest)
			return
		} else if len(versions) == 0 {
			http.Error(w, "prompt '"+req.PromptName+"' not found", http.StatusBadRequest)
			return
		}
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

	if err := h.EvalRuleStore.CreateEvalRule(r.Context(), rule); err != nil {
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

	resolver := h.resolveProjectID()
	projectID, ok := auth.ProjectIDWithErrorWithResolver(w, r, resolver)
	if !ok {
		return
	}

	rules, err := h.EvalRuleStore.ListEvalRules(r.Context(), projectID)
	if err != nil {
		http.Error(w, "store error", http.StatusInternalServerError)
		return
	}

	if rules == nil {
		rules = []*domain.EvalRule{}
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

	resolver := h.resolveProjectID()
	projectID, ok := auth.ProjectIDWithErrorWithResolver(w, r, resolver)
	if !ok {
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
	existing, err := h.EvalRuleStore.GetEvalRule(r.Context(), ruleID)
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

	if err := h.EvalRuleStore.DeleteEvalRule(r.Context(), ruleID); err != nil {
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

	resolver := h.resolveProjectID()
	projectID, ok := auth.ProjectIDWithErrorWithResolver(w, r, resolver)
	if !ok {
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
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(PreviewEvalRulesResponse{})
		return
	}

	now := time.Now().UTC()

	// Query spans from the last hour, then evaluate each against the filter.
	matchingSpans, err := h.querySpans(r.Context(), projectID, now.Add(-1*time.Hour), 50)
	if err != nil {
		http.Error(w, "query execution error", http.StatusInternalServerError)
		return
	}

	var matched []*PreviewSpan
	for _, s := range matchingSpans {
		if req.Filter.Matches(spanFromPreview(s)) {
			matched = append(matched, s)
		}
	}

	// Count total matches in the last 24 hours.
	matchCount24h := h.countSpansInPeriod(r.Context(), projectID, now.Add(-24*time.Hour), req.Filter)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(PreviewEvalRulesResponse{
		Spans:         matched,
		MatchCount24h: matchCount24h,
	})
}

// querySpans returns up to maxSpans matching spans for the project within
// the time window, ordered by start time descending.
func (h *EvalRuleHandler) querySpans(ctx context.Context, projectID string, since time.Time, maxSpans int) ([]*PreviewSpan, error) {
	var spans []*PreviewSpan
	rows, err := h.DB.QueryContext(ctx,
		`SELECT span_id, trace_id, name, kind, model, start_time, cost_usd
		 FROM spans
		 WHERE project_id = ? AND start_time >= ?
		 ORDER BY start_time DESC, span_id ASC
		 LIMIT ?`,
		projectID, since, maxSpans,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var s PreviewSpan
		if err := rows.Scan(&s.SpanID, &s.TraceID, &s.Name, &s.Kind, &s.Model, &s.StartTime, &s.CostUSD); err != nil {
			return nil, err
		}
		spans = append(spans, &s)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return spans, nil
}

// countSpansInPeriod counts spans matching the filter within the given
// time window.
func (h *EvalRuleHandler) countSpansInPeriod(ctx context.Context, projectID string, since time.Time, filter domain.EvalFilter) int64 {
	rows, err := h.DB.QueryContext(ctx,
		`SELECT span_id, trace_id, name, kind, model, start_time, cost_usd
		 FROM spans
		 WHERE project_id = ? AND start_time >= ?`,
		projectID, since,
	)
	if err != nil {
		return 0
	}
	defer rows.Close()

	var count int64
	for rows.Next() {
		var s PreviewSpan
		if err := rows.Scan(&s.SpanID, &s.TraceID, &s.Name, &s.Kind, &s.Model, &s.StartTime, &s.CostUSD); err != nil {
			break
		}
		if filter.Matches(spanFromPreview(&s)) {
			count++
		}
	}
	_ = rows.Err() // ignore iteration error — best-effort count
	return count
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
// Recursively checks AND/OR/NOT sub-filters.
func hasFilterConditions(f domain.EvalFilter) bool {
	leafCondition := f.Kind != nil ||
		f.Model != nil ||
		f.ServiceName != nil ||
		f.PromptName != nil ||
		f.StatusCode != nil ||
		f.MinCostUSD != nil ||
		f.MaxCostUSD != nil ||
		f.MinDurationMS != nil ||
		f.MaxDurationMS != nil ||
		len(f.AttributesMatch) > 0

	if leafCondition {
		return true
	}

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
	if f.Not != nil && hasFilterConditions(*f.Not) {
		return true
	}
	return false
}

// Routes returns the eval-rule-related API routes as AuthRoute entries with
// AuthPolicyAPIKeyOrSession so the Router can use them for policy-based auth
// dispatch.
func (h *EvalRuleHandler) Routes() []AuthRoute {
	return []AuthRoute{
		{Method: http.MethodPost, Path: "/api/v1/eval-rules", Handler: http.HandlerFunc(h.HandleCreate), Policy: AuthPolicyAPIKeyOrSession},
		{Method: http.MethodGet, Path: "/api/v1/eval-rules", Handler: http.HandlerFunc(h.HandleList), Policy: AuthPolicyAPIKeyOrSession},
		{Method: http.MethodDelete, Path: "/api/v1/eval-rules/{ruleId}", Handler: http.HandlerFunc(h.HandleDelete), Policy: AuthPolicyAPIKeyOrSession},
		{Method: http.MethodPost, Path: "/api/v1/eval-rules/preview", Handler: http.HandlerFunc(h.HandlePreview), Policy: AuthPolicyAPIKeyOrSession},
	}
}
