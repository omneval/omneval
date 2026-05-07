package playground

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/zbloss/lantern/internal/domain"
	"github.com/zbloss/lantern/internal/judge"
	"github.com/zbloss/lantern/services/query/internal/handler"
)

// PlaygroundHandler handles POST /api/v1/playground/run.
type PlaygroundHandler struct {
	Cache        *handler.PromptCache
	LLMClient    LLMClient
	SessionStore handler.SessionStore
}

// HandleRun handles POST /api/v1/playground/run.
// It resolves a prompt, renders variable interpolation, calls the LLM, and returns the result.
func (h *PlaygroundHandler) HandleRun(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req Request
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}

	if req.PromptName == "" {
		http.Error(w, "prompt_name is required", http.StatusBadRequest)
		return
	}

	// Extract project_id from the authenticated session.
	var projectID string
	var ok bool
	if h.SessionStore != nil {
		projectID, ok = h.SessionStore.ProjectID(r)
	}
	if !ok || projectID == "" {
		projectID = r.URL.Query().Get("project_id")
	}
	if projectID == "" {
		http.Error(w, "project_id is required", http.StatusBadRequest)
		return
	}

	// Resolve the prompt version.
	pv, err := h.resolvePrompt(projectID, req.PromptName, req.Version, req.Label)
	if err != nil {
		if errors.Is(err, errProvideVersionOrLabel) {
			http.Error(w, "provide version or label", http.StatusBadRequest)
			return
		}
		// Prompt not found — return 400 as specified.
		http.Error(w, "prompt not found", http.StatusBadRequest)
		return
	}

	// Build the model config from prompt, with optional overrides.
	model := pv.ModelConfig.Model
	temperature := pv.ModelConfig.Temperature
	maxTokens := pv.ModelConfig.MaxTokens

	if req.ModelOverride != nil && *req.ModelOverride != "" {
		model = *req.ModelOverride
	}
	if req.TemperatureOverride != nil && *req.TemperatureOverride > 0 {
		temperature = *req.TemperatureOverride
	}

	// Interpolate the template.
	interpolated, missing := judge.Interpolate(pv.Template, req.Variables)
	if len(missing) > 0 {
		http.Error(w, "missing required variables: "+strings.Join(missing, ", "), http.StatusBadRequest)
		return
	}

	// Check LLM client is configured.
	if h.LLMClient == nil {
		http.Error(w, "playground LLM not configured", http.StatusBadRequest)
		return
	}

	// Build chat messages.
	messages := judge.BuildJudgeMessages(interpolated)

	// Call the LLM.
	start := time.Now()
	resp, err := h.LLMClient.Chat(r.Context(), judge.ChatRequest{
		Model:       model,
		Messages:    messages,
		Temperature: temperature,
		MaxTokens:   maxTokens,
	})
	durationMs := time.Since(start).Milliseconds()
	if err != nil {
		// 422 for upstream LLM errors.
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnprocessableEntity)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	// Build the response.
	output := ""
	if len(resp.Choices) > 0 {
		output = resp.Choices[0].Message.Content
	}

	playgroundResp := Response{
		Output:       output,
		Model:        model,
		InputTokens:  resp.Usage.PromptTokens,
		OutputTokens: resp.Usage.CompletionTokens,
		DurationMs:   durationMs,
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(playgroundResp); err != nil {
		http.Error(w, "encode error", http.StatusInternalServerError)
		return
	}
}

var errProvideVersionOrLabel = errors.New("provide version or label")

// resolvePrompt wraps the prompt cache lookup.
func (h *PlaygroundHandler) resolvePrompt(
	projectID string,
	name string,
	version *int64,
	label *string,
) (*domain.PromptVersion, error) {
	if version != nil && *version > 0 {
		pv, err := h.Cache.GetVersion(nil, projectID, name, *version)
		if err != nil {
			return nil, err
		}
		return pv, nil
	}
	if label != nil && *label != "" {
		pv, err := h.Cache.GetLabel(nil, projectID, name, *label)
		if err != nil {
			return nil, err
		}
		return pv, nil
	}
	return nil, errProvideVersionOrLabel
}
