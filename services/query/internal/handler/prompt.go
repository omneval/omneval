package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/omneval/omneval/internal/auth"
	"github.com/omneval/omneval/internal/domain"
	"github.com/omneval/omneval/internal/metadata"
)

// PromptHandler handles prompt registry endpoints:
//   GET    /api/v1/prompts                       — list all prompt names with latest versions and labels
//   POST   /api/v1/prompts                       — create an immutable prompt version
//   GET    /api/v1/prompts/:name                 — resolve by ?version=N or ?label=<label>
//   GET    /api/v1/prompts/:name/versions        — list all versions for a prompt
//   PUT    /api/v1/prompts/:name/labels/:label   — reassign a label

type PromptHandler struct {
	PromptStore  metadata.PromptStore
	Cache        *PromptCache
	SessionStore SessionStore
	Validator    auth.Validator
}



// HandleCreatePrompt handles POST /api/v1/prompts.
// Creates an immutable prompt version. Re-posting the same version returns 409.
func (h *PromptHandler) HandleCreatePrompt(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	projectID, ok := auth.ProjectIDWithError(w, r)
	if !ok {
		return
	}

	var req struct {
		Name        string  `json:"name"`
		Version     int64   `json:"version"`
		Template    string  `json:"template"`
		Model       string  `json:"model"`
		Temperature float64 `json:"temperature"`
		MaxTokens   int     `json:"max_tokens"`
		Label       string  `json:"label"` // optional: assign a label at creation time
		ModelConfig *struct {
			Model       string  `json:"model"`
			Temperature float64 `json:"temperature"`
			MaxTokens   int     `json:"max_tokens"`
		} `json:"model_config"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}
	if req.Name == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}

	// Auto-increment version when not provided.
	if req.Version <= 0 {
		versions, listErr := h.PromptStore.ListPromptVersions(r.Context(), projectID, req.Name)
		if listErr != nil {
			slog.Error("query: list prompt versions for auto-increment", "project_id", projectID, "name", req.Name, "err", listErr)
			http.Error(w, "store error", http.StatusInternalServerError)
			return
		}
		var maxVersion int64
		for _, v := range versions {
			if v.Version > maxVersion {
				maxVersion = v.Version
			}
		}
		req.Version = maxVersion + 1
	}

	// Check if version already exists (idempotency).
	_, err := h.PromptStore.GetPromptVersion(r.Context(), projectID, req.Name, req.Version)
	if err == nil {
		http.Error(w, "version already exists", http.StatusConflict)
		return
	}
	if !errors.Is(err, metadata.ErrNotFound) {
		http.Error(w, "store error", http.StatusInternalServerError)
		return
	}

	// Resolve model config: nested model_config takes precedence over flat fields.
	modelCfg := domain.PromptModelConfig{
		Model:       req.Model,
		Temperature: req.Temperature,
		MaxTokens:   req.MaxTokens,
	}
	if req.ModelConfig != nil {
		modelCfg.Model = req.ModelConfig.Model
		modelCfg.Temperature = req.ModelConfig.Temperature
		modelCfg.MaxTokens = req.ModelConfig.MaxTokens
	}

	pv := &domain.PromptVersion{
		VersionID:   uuid.New().String(),
		ProjectID:   projectID,
		Name:        req.Name,
		Version:     req.Version,
		Template:    req.Template,
		ModelConfig: modelCfg,
		CreatedAt:   time.Now().UTC(),
	}

	if err := h.PromptStore.CreatePromptVersion(r.Context(), pv); err != nil {
		// Duplicate key is also a conflict (409), not a generic error.
		errStr := err.Error()
		if strings.Contains(errStr, "UNIQUE") || strings.Contains(errStr, "duplicate") {
			// Even if the label param was provided, the version conflict takes
			// precedence — return 409 without creating a stale label.
			http.Error(w, "version already exists", http.StatusConflict)
			return
		}
		slog.Error("query: create prompt version", "project_id", projectID, "name", pv.Name, "err", err)
		http.Error(w, "store error", http.StatusInternalServerError)
		return
	}

	if req.Label != "" {
		pl := &domain.PromptLabel{
			ProjectID: projectID,
			Name:      req.Name,
			Label:     req.Label,
			Version:   pv.Version,
			UpdatedAt: time.Now().UTC(),
		}
		if labelErr := h.PromptStore.SetPromptLabel(r.Context(), pl); labelErr != nil {
			slog.Warn("query: set initial prompt label", "name", pv.Name, "label", req.Label, "err", labelErr)
		}
		h.Cache.InvalidateLabel(projectID, req.Name, req.Label)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(pv.ToJSON())
}

// HandleGetPrompt handles GET /api/v1/prompts/:name.
// Resolves by ?version=N (unbounded cache), ?label=<label> (30s TTL cache),
// or defaults to the latest (highest) version when no params are provided.
func (h *PromptHandler) HandleGetPrompt(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	name := r.PathValue("name")
	if name == "" {
		name = extractPromptName(r.URL.Path)
	}
	if name == "" {
		http.Error(w, "prompt name is required", http.StatusBadRequest)
		return
	}

	versionQuery := r.URL.Query().Get("version")
	labelQuery := r.URL.Query().Get("label")

	projectID, ok := auth.ProjectIDWithError(w, r)
	if !ok {
		return
	}

	var pv *domain.PromptVersion
	var getErr error

	if versionQuery != "" {
		var v int64
		fmt.Sscanf(versionQuery, "%d", &v)
		if v <= 0 {
			http.Error(w, "invalid version", http.StatusBadRequest)
			return
		}
		pv, getErr = h.Cache.GetVersion(r.Context(), projectID, name, v)
	} else if labelQuery != "" {
		pv, getErr = h.Cache.GetLabel(r.Context(), projectID, name, labelQuery)
	} else {
		pv, getErr = h.getLatestVersion(r.Context(), name, projectID)
	}

	if getErr != nil {
		if errors.Is(getErr, metadata.ErrNotFound) {
			http.Error(w, "prompt not found", http.StatusNotFound)
			return
		}
		slog.Error("query: get prompt", "name", name, "err", getErr)
		http.Error(w, "store error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(pv.ToJSON())
}

// getLatestVersion returns the prompt version with the highest version number
// for the given prompt name and project.
func (h *PromptHandler) getLatestVersion(ctx context.Context, name, projectID string) (*domain.PromptVersion, error) {
	if projectID == "" {
		return nil, metadata.ErrNotFound
	}

	versions, err := h.PromptStore.ListPromptVersions(ctx, projectID, name)
	if err != nil {
		return nil, err
	}
	if len(versions) == 0 {
		return nil, metadata.ErrNotFound
	}

	var latest *domain.PromptVersion
	for _, v := range versions {
		if latest == nil || v.Version > latest.Version {
			latest = v
		}
	}
	return latest, nil
}

// promptListItem represents a prompt name with its latest version and active labels.
type promptListItem struct {
	Name          string           `json:"name"`
	LatestVersion int64            `json:"latest_version"`
	Labels        map[string]int64 `json:"labels"`
}

// HandleListPrompts handles GET /api/v1/prompts.
// Returns all prompt names for the authenticated project with their latest
// version and active labels (production, staging, dev).
func (h *PromptHandler) HandleListPrompts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	projectID, ok := auth.ProjectIDWithError(w, r)
	if !ok {
		return
	}

	names, err := h.PromptStore.ListPromptNames(r.Context(), projectID)
	if err != nil {
		slog.Error("query: list prompt names", "project_id", projectID, "err", err)
		http.Error(w, "store error", http.StatusInternalServerError)
		return
	}

	result := make([]promptListItem, 0, len(names))
	for _, name := range names {
		// Get all versions for this prompt to find the latest.
		versions, err := h.PromptStore.ListPromptVersions(r.Context(), projectID, name)
		if err != nil {
			slog.Warn("query: list prompt versions", "project_id", projectID, "name", name, "err", err)
			continue
		}

		var latestVersion int64
		for _, v := range versions {
			if v.Version > latestVersion {
				latestVersion = v.Version
			}
		}

		// Get active labels for this prompt.
		labels := make(map[string]int64)
		for _, label := range []string{"production", "staging", "dev"} {
			pv, err := h.PromptStore.GetPromptByLabel(r.Context(), projectID, name, label)
			if err == nil {
				labels[label] = pv.Version
			}
		}

		result = append(result, promptListItem{
			Name:          name,
			LatestVersion: latestVersion,
			Labels:        labels,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

// HandleSetLabel handles PUT /api/v1/prompts/:name/labels/:label.
// Reassigns a label to a different version.
func (h *PromptHandler) HandleSetLabel(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	name := r.PathValue("name")
	label := r.PathValue("label")

	// Fallback for tests that don't use ServeMux pattern matching.
	if name == "" || label == "" {
		parts := extractPromptLabelPath(r.URL.Path)
		if len(parts) == 2 {
			if name == "" {
				name = parts[0]
			}
			if label == "" {
				label = parts[1]
			}
		}
	}
	if name == "" || label == "" {
		http.Error(w, "name and label are required", http.StatusBadRequest)
		return
	}

	var req struct {
		Version int64 `json:"version"`
	}
	if decodeErr := json.NewDecoder(r.Body).Decode(&req); decodeErr != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}
	if req.Version <= 0 {
		http.Error(w, "version is required", http.StatusBadRequest)
		return
	}

	// Validate the version exists.
	projectID, ok := auth.ProjectIDWithError(w, r)
	if !ok {
		return
	}
	_, err := h.PromptStore.GetPromptVersion(r.Context(), projectID, name, req.Version)
	if err != nil {
		if errors.Is(err, metadata.ErrNotFound) {
			http.Error(w, "prompt version not found", http.StatusNotFound)
			return
		}
		http.Error(w, "store error", http.StatusInternalServerError)
		return
	}

	pl := &domain.PromptLabel{
		ProjectID: projectID,
		Name:      name,
		Label:     label,
		Version:   req.Version,
	}

	if setErr := h.PromptStore.SetPromptLabel(r.Context(), pl); setErr != nil {
		slog.Error("query: set prompt label", "project_id", projectID, "name", name, "label", label, "err", setErr)
		http.Error(w, "store error", http.StatusInternalServerError)
		return
	}

	// Invalidate the label cache so the next lookup reflects the new assignment.
	h.Cache.InvalidateLabel(projectID, name, label)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"name":    name,
		"label":   label,
		"version": req.Version,
	})
}

// HandleListPromptVersions handles GET /api/v1/prompts/:name/versions
// and returns all versions for a prompt name (uses metadata store).
// Infers project_id from session context; falls back to query param.
func (h *PromptHandler) HandleListPromptVersions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	name := r.PathValue("name")
	if name == "" {
		name = extractPromptName(r.URL.Path)
	}
	if name == "" {
		http.Error(w, "prompt name is required", http.StatusBadRequest)
		return
	}

	projectID, ok := auth.ProjectIDWithError(w, r)
	if !ok {
		return
	}

	versions, err := h.PromptStore.ListPromptVersions(r.Context(), projectID, name)
	if err != nil {
		slog.Error("query: list prompt versions", "project_id", projectID, "name", name, "err", err)
		http.Error(w, "store error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(versionsToJSON(versions))
}

// versionsToJSON converts []*domain.PromptVersion to []domain.PromptVersionJSON
// so that model/temperature/max_tokens appear as flat top-level fields.
func versionsToJSON(versions []*domain.PromptVersion) []domain.PromptVersionJSON {
	result := make([]domain.PromptVersionJSON, len(versions))
	for i, v := range versions {
		result[i] = v.ToJSON()
	}
	return result
}

// extractPromptName extracts the prompt name from a URL path like
// "/api/v1/prompts/greeting" or "/api/v1/prompts/greeting/versions".
func extractPromptName(path string) string {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	// Expected: api, v1, prompts, <name>...
	for i, p := range parts {
		if p == "prompts" && i+1 < len(parts) {
			return parts[i+1]
		}
	}
	return ""
}

// extractPromptLabelPath extracts (name, label) from a URL path like
// "/api/v1/prompts/greeting/labels/production".
func extractPromptLabelPath(path string) []string {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	// Expected: api, v1, prompts, <name>, labels, <label>
	for i := range parts {
		if parts[i] == "prompts" && i+3 < len(parts) && parts[i+2] == "labels" {
			return []string{parts[i+1], parts[i+3]}
		}
	}
	return nil
}
