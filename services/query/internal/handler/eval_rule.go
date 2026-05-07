package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/zbloss/lantern/internal/domain"
	"github.com/zbloss/lantern/internal/metadata"
)

// extractProjectID extracts the authenticated project ID from the request.
// Returns an empty string and false when no session store is configured.
func extractProjectID(sessionStore sessionStore, r *http.Request) (string, bool) {
	if sessionStore == nil {
		return "", false
	}
	return sessionStore.ProjectID(r)
}

// EvalRuleHandler handles eval rule CRUD endpoints:
//   POST   /api/v1/eval-rules          — create an eval rule
//   GET    /api/v1/eval-rules          — list all eval rules for the project
//   DELETE /api/v1/eval-rules/:id      — delete an eval rule by ID
type EvalRuleHandler struct {
	Store        metadata.Store
	SessionStore sessionStore
}

// ---- Request / Response Types ----

// CreateEvalRuleRequest is the body accepted by POST /api/v1/eval-rules.
type CreateEvalRuleRequest struct {
	Name        string          `json:"name"`
	JudgeModel  string          `json:"judge_model"`
	PromptName  string          `json:"prompt_name"`
	PromptVersion int64         `json:"prompt_version"`
	SampleRate  float64         `json:"sample_rate"`
	Enabled     bool            `json:"enabled"`
	Filter      domain.EvalFilter `json:"filter"`
}

// ListEvalRulesResponse is returned by GET /api/v1/eval-rules.
type ListEvalRulesResponse struct {
	Rules []*domain.EvalRule `json:"rules"`
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
