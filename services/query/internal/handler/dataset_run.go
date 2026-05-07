package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/zbloss/lantern/internal/domain"
	"github.com/zbloss/lantern/internal/judge"
	"github.com/zbloss/lantern/internal/metadata"
)

// DatasetRunHandler handles dataset run endpoints:
//
//	POST   /api/v1/datasets/:id/runs                          — create and run evaluation
//	GET    /api/v1/datasets/:id/runs                          — list run history
//	GET    /api/v1/datasets/:id/runs/:runId                   — get run with per-item scores
//	GET    /api/v1/datasets/:id/runs/:runId/status            — get run status (polling)

type DatasetRunHandler struct {
	Store        metadata.Store
	SessionStore SessionStore
	JudgeClient  judge.LLMClient
	Cache        *PromptCache
}

// RunDatasetRequest is the body accepted by POST /api/v1/datasets/:id/runs.
type RunDatasetRequest struct {
	EvalRuleID string `json:"eval_rule_id"`
}

// datasetRunListItem represents a dataset run in the list endpoint response.
type datasetRunListItem struct {
	RunID      string  `json:"run_id"`
	EvalRuleID string  `json:"eval_rule_id"`
	Status     string  `json:"status"`
	ItemCount  int     `json:"item_count"`
	MeanScore  float64 `json:"mean_score,omitempty"`
	CreatedAt  string  `json:"created_at"`
}

// ListDatasetRunsResponse is returned by GET /api/v1/datasets/:id/runs.
type ListDatasetRunsResponse struct {
	Runs []datasetRunListItem `json:"runs"`
}

// getRunResponse is returned by GET /api/v1/datasets/:id/runs/:runId.
type getRunResponse struct {
	RunID      string                `json:"run_id"`
	DatasetID  string                `json:"dataset_id"`
	EvalRuleID string                `json:"eval_rule_id"`
	Status     string                `json:"status"`
	CreatedAt  string                `json:"created_at"`
	Items      []runItemResponse     `json:"items"`
}

// runItemResponse represents a single scored item in a run detail response.
type runItemResponse struct {
	ItemID         string  `json:"item_id"`
	Input          string  `json:"input"`
	ExpectedOutput string  `json:"expected_output"`
	Score          float64 `json:"score"`
	Reasoning      string  `json:"reasoning"`
}

// ---- HTTP Handlers ----

// HandleRun handles POST /api/v1/datasets/:id/runs.
// It creates a DatasetRun, then synchronously scores each item by calling
// the judge LLM with the eval rule's prompt template.
func (h *DatasetRunHandler) HandleRun(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	projectID, ok := extractProjectID(h.SessionStore, r)
	if !ok || projectID == "" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	datasetID := r.PathValue("id")
	if datasetID == "" {
		datasetID = extractDatasetID(r.URL.Path)
	}
	if datasetID == "" {
		http.Error(w, "dataset ID is required", http.StatusBadRequest)
		return
	}

	// Verify dataset exists and belongs to the project.
	ds, err := h.Store.GetDataset(r.Context(), datasetID)
	if err != nil {
		if errors.Is(err, metadata.ErrNotFound) {
			http.Error(w, "dataset not found", http.StatusNotFound)
			return
		}
		http.Error(w, "store error", http.StatusInternalServerError)
		return
	}
	if ds.ProjectID != projectID {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	var req RunDatasetRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}
	if req.EvalRuleID == "" {
		http.Error(w, "eval_rule_id is required", http.StatusBadRequest)
		return
	}

	// Verify the eval rule exists.
	evalRule, err := h.Store.GetEvalRule(r.Context(), req.EvalRuleID)
	if err != nil {
		if errors.Is(err, metadata.ErrNotFound) {
			http.Error(w, "eval rule not found", http.StatusNotFound)
			return
		}
		http.Error(w, "store error", http.StatusInternalServerError)
		return
	}

	// Verify eval rule belongs to the same project.
	if evalRule.ProjectID != projectID {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	// Check judge LLM is configured.
	if h.JudgeClient == nil {
		http.Error(w, "judge LLM not configured", http.StatusBadRequest)
		return
	}

	// Get dataset items.
	items, err := h.Store.ListDatasetItems(r.Context(), datasetID)
	if err != nil {
		http.Error(w, "store error", http.StatusInternalServerError)
		return
	}
	if len(items) == 0 {
		http.Error(w, "dataset has no items", http.StatusBadRequest)
		return
	}

	// Resolve the prompt version for the eval rule.
	pv, err := h.Cache.GetVersion(r.Context(), projectID, evalRule.PromptName, evalRule.PromptVersion)
	if err != nil {
		http.Error(w, "prompt not found", http.StatusBadRequest)
		return
	}

	// Create the run record with "pending" status.
	run := &domain.DatasetRun{
		RunID:         uuid.New().String(),
		DatasetID:     datasetID,
		EvalRuleID:    evalRule.RuleID,
		PromptVersion: pv.Version,
		Status:        domain.DatasetRunStatusRunning,
		CreatedAt:     time.Now().UTC(),
	}
	if err := h.Store.CreateDatasetRun(r.Context(), run); err != nil {
		http.Error(w, "store error", http.StatusInternalServerError)
		return
	}

	// Score each item synchronously.
	var firstErr error
	for _, item := range items {
		if scoreErr := h.scoreItem(r.Context(), run, item, evalRule, pv); scoreErr != nil {
			if firstErr == nil {
				firstErr = scoreErr
			}
			// Continue scoring remaining items.
		}
	}

	// Update run status.
	run.Status = domain.DatasetRunStatusComplete
	if firstErr != nil {
		run.Status = domain.DatasetRunStatusError
	}
	_ = h.Store.UpdateDatasetRun(r.Context(), run) // best-effort

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]string{
		"run_id": run.RunID,
		"status": run.Status,
	})
}

// scoreItem scores a single dataset item by rendering the eval rule's prompt
// template and calling the judge LLM.
func (h *DatasetRunHandler) scoreItem(
	ctx context.Context,
	run *domain.DatasetRun,
	item *domain.DatasetItem,
	evalRule *domain.EvalRule,
	pv *domain.PromptVersion,
) error {
	// Build variables from item data.
	variables := map[string]string{
		"input":            item.Input,
		"expected_output":  item.ExpectedOutput,
		"source_span_id":   item.SourceSpanID,
		"eval_rule_id":     evalRule.RuleID,
		"dataset_id":       item.DatasetID,
		"dataset_run_id":   run.RunID,
	}

	// Interpolate the prompt template.
	interpolated, missing := judge.Interpolate(pv.Template, variables)
	if len(missing) > 0 {
		return &judgeError{msg: "missing prompt variables: " + strings.Join(missing, ", ")}
	}

	// Build chat messages for the judge LLM.
	messages := judge.BuildJudgeMessages(interpolated)

	// Call the judge LLM.
	resp, err := h.JudgeClient.Chat(ctx, judge.ChatRequest{
		Model:       evalRule.JudgeModel,
		Messages:    messages,
		Temperature: 0.0, // deterministic scoring
	})
	if err != nil {
		return &judgeError{msg: err.Error()}
	}

	// Parse the response for score and reasoning.
	content := ""
	if len(resp.Choices) > 0 {
		content = resp.Choices[0].Message.Content
	}

	score, reasoning := parseJudgeResponse(content)

	// Create the run item record.
	runItem := &domain.DatasetRunItem{
		RunItemID: uuid.New().String(),
		RunID:     run.RunID,
		ItemID:    item.ItemID,
		Score:     score,
		Reasoning: reasoning,
		CreatedAt: time.Now().UTC(),
	}

	if err := h.Store.CreateDatasetRunItem(ctx, runItem); err != nil {
		return &judgeError{msg: "store error: " + err.Error()}
	}

	return nil
}

// judgeError wraps an error for internal use.
type judgeError struct {
	msg string
}

func (e *judgeError) Error() string { return e.msg }

// parseJudgeResponse extracts a numeric score and reasoning from the LLM response.
// The judge prompt should produce output like:
//
//	Score: 8.5
//	Reasoning: The output is accurate but could be more detailed.
//
// Falls back to 0.0 score and the full content as reasoning if parsing fails.
func parseJudgeResponse(content string) (float64, string) {
	score := 0.0
	reasoning := content

	lines := strings.Split(content, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		lower := strings.ToLower(line)
		if strings.HasPrefix(lower, "score:") || strings.HasPrefix(lower, "score ") {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				if _, err := fmt.Sscanf(strings.TrimSpace(parts[1]), "%f", &score); err == nil {
					reasoning = strings.TrimSpace(line)
					break
				}
			}
		}
	}

	return score, reasoning
}

// HandleListRuns handles GET /api/v1/datasets/:id/runs.
func (h *DatasetRunHandler) HandleListRuns(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	projectID, ok := extractProjectID(h.SessionStore, r)
	if !ok || projectID == "" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	datasetID := r.PathValue("id")
	if datasetID == "" {
		datasetID = extractDatasetID(r.URL.Path)
	}
	if datasetID == "" {
		http.Error(w, "dataset ID is required", http.StatusBadRequest)
		return
	}

	// Verify dataset exists and belongs to the project.
	ds, err := h.Store.GetDataset(r.Context(), datasetID)
	if err != nil {
		if errors.Is(err, metadata.ErrNotFound) {
			http.Error(w, "dataset not found", http.StatusNotFound)
			return
		}
		http.Error(w, "store error", http.StatusInternalServerError)
		return
	}
	if ds.ProjectID != projectID {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	runs, err := h.Store.ListDatasetRuns(r.Context(), datasetID)
	if err != nil {
		http.Error(w, "store error", http.StatusInternalServerError)
		return
	}

	result := make([]datasetRunListItem, 0, len(runs))
	for _, run := range runs {
		items, _ := h.Store.ListDatasetRunItems(r.Context(), run.RunID)
		itemCount := len(items)
		meanScore := 0.0
		if itemCount > 0 {
			var total float64
			for _, item := range items {
				total += item.Score
			}
			meanScore = total / float64(itemCount)
		}
		result = append(result, datasetRunListItem{
			RunID:      run.RunID,
			EvalRuleID: run.EvalRuleID,
			Status:     run.Status,
			ItemCount:  itemCount,
			MeanScore:  meanScore,
			CreatedAt:  run.CreatedAt.Format(time.RFC3339),
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(ListDatasetRunsResponse{Runs: result})
}

// HandleGetRun handles GET /api/v1/datasets/:id/runs/:runId.
func (h *DatasetRunHandler) HandleGetRun(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	projectID, ok := extractProjectID(h.SessionStore, r)
	if !ok || projectID == "" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	datasetID := r.PathValue("id")
	if datasetID == "" {
		datasetID = extractDatasetID(r.URL.Path)
	}
	runID := r.PathValue("runId")
	if runID == "" {
		// Fallback for tests that don't use ServeMux pattern matching.
		runID = extractRunID(r.URL.Path)
	}
	if runID == "" {
		http.Error(w, "run ID is required", http.StatusBadRequest)
		return
	}

	// Verify dataset exists and belongs to the project.
	ds, err := h.Store.GetDataset(r.Context(), datasetID)
	if err != nil {
		if errors.Is(err, metadata.ErrNotFound) {
			http.Error(w, "dataset not found", http.StatusNotFound)
			return
		}
		http.Error(w, "store error", http.StatusInternalServerError)
		return
	}
	if ds.ProjectID != projectID {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	// Get the run.
	run, err := h.Store.GetDatasetRun(r.Context(), runID)
	if err != nil {
		if errors.Is(err, metadata.ErrNotFound) {
			http.Error(w, "run not found", http.StatusNotFound)
			return
		}
		http.Error(w, "store error", http.StatusInternalServerError)
		return
	}

	// Verify the run belongs to the dataset.
	if run.DatasetID != datasetID {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	// Get run items.
	runItems, err := h.Store.ListDatasetRunItems(r.Context(), runID)
	if err != nil {
		http.Error(w, "store error", http.StatusInternalServerError)
		return
	}

	// Build per-item responses with item details.
	// First, get all dataset items for this dataset to look up input/expected_output.
	datasetItems, err := h.Store.ListDatasetItems(r.Context(), datasetID)
	if err != nil {
		http.Error(w, "store error", http.StatusInternalServerError)
		return
	}
	itemMap := make(map[string]*domain.DatasetItem)
	for _, di := range datasetItems {
		itemMap[di.ItemID] = di
	}

	items := make([]runItemResponse, 0, len(runItems))
	for _, ri := range runItems {
		di := itemMap[ri.ItemID]
		items = append(items, runItemResponse{
			ItemID:         ri.ItemID,
			Input:          di.Input,
			ExpectedOutput: di.ExpectedOutput,
			Score:          ri.Score,
			Reasoning:      ri.Reasoning,
		})
	}

	resp := getRunResponse{
		RunID:      run.RunID,
		DatasetID:  run.DatasetID,
		EvalRuleID: run.EvalRuleID,
		Status:     run.Status,
		CreatedAt:  run.CreatedAt.Format(time.RFC3339),
		Items:      items,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// HandleGetRunStatus handles GET /api/v1/datasets/:id/runs/:runId/status.
// It returns just the run status for polling.
func (h *DatasetRunHandler) HandleGetRunStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	projectID, ok := extractProjectID(h.SessionStore, r)
	if !ok || projectID == "" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	datasetID := r.PathValue("id")
	if datasetID == "" {
		datasetID = extractDatasetID(r.URL.Path)
	}
	runID := r.PathValue("runId")
	if runID == "" {
		runID = extractRunID(r.URL.Path)
	}
	if runID == "" {
		http.Error(w, "run ID is required", http.StatusBadRequest)
		return
	}

	// Verify dataset exists and belongs to the project.
	ds, err := h.Store.GetDataset(r.Context(), datasetID)
	if err != nil {
		if errors.Is(err, metadata.ErrNotFound) {
			http.Error(w, "dataset not found", http.StatusNotFound)
			return
		}
		http.Error(w, "store error", http.StatusInternalServerError)
		return
	}
	if ds.ProjectID != projectID {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	// Get the run.
	run, err := h.Store.GetDatasetRun(r.Context(), runID)
	if err != nil {
		if errors.Is(err, metadata.ErrNotFound) {
			http.Error(w, "run not found", http.StatusNotFound)
			return
		}
		http.Error(w, "store error", http.StatusInternalServerError)
		return
	}

	if run.DatasetID != datasetID {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"run_id": run.RunID,
		"status": run.Status,
	})
}

// ---- Helpers ----

// extractRunID extracts the run ID from a URL path like
// "/api/v1/datasets/abc123/runs/xyz789" or "/api/v1/datasets/abc123/runs/xyz789/status".
func extractRunID(path string) string {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	// Expected: api, v1, datasets, <datasetId>, runs, <runId>...
	for i, p := range parts {
		if p == "runs" && i+1 < len(parts) {
			return parts[i+1]
		}
	}
	return ""
}
