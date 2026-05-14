package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/zbloss/lantern/internal/domain"
	"github.com/zbloss/lantern/internal/metadata"
)

// PromptHandler handles prompt registry endpoints:
//   GET    /api/v1/prompts                       — list all prompt names with latest versions and labels
//   POST   /api/v1/prompts                       — create an immutable prompt version
//   GET    /api/v1/prompts/:name                 — resolve by ?version=N or ?label=<label>
//   GET    /api/v1/prompts/:name/versions        — list all versions for a prompt
//   PUT    /api/v1/prompts/:name/labels/:label   — reassign a label

type PromptHandler struct {
	Store        metadata.Store
	Cache        *PromptCache
	SessionStore SessionStore
}

// HandleCreatePrompt handles POST /api/v1/prompts.
// Creates an immutable prompt version. Re-posting the same version returns 409.
func (h *PromptHandler) HandleCreatePrompt(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract project_id from the authenticated session.
	var projectID string
	var ok bool
	if h.SessionStore != nil {
		projectID, ok = h.SessionStore.ProjectID(r)
	}
	if !ok || projectID == "" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var req struct {
		Name        string  `json:"name"`
		Version     int64   `json:"version"`
		Template    string  `json:"template"`
		Model       string  `json:"model"`
		Temperature float64 `json:"temperature"`
		MaxTokens   int     `json:"max_tokens"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}
	if req.Name == "" || req.Version <= 0 {
		http.Error(w, "name and version are required", http.StatusBadRequest)
		return
	}

	// Check if version already exists (idempotency).
	_, err := h.Store.GetPromptVersion(r.Context(), projectID, req.Name, req.Version)
	if err == nil {
		http.Error(w, "version already exists", http.StatusConflict)
		return
	}
	if !errors.Is(err, metadata.ErrNotFound) {
		http.Error(w, "store error", http.StatusInternalServerError)
		return
	}

	pv := &domain.PromptVersion{
		VersionID: uuid.New().String(),
		ProjectID: projectID,
		Name:      req.Name,
		Version:   req.Version,
		Template:  req.Template,
		ModelConfig: domain.PromptModelConfig{
			Model:       req.Model,
			Temperature: req.Temperature,
			MaxTokens:   req.MaxTokens,
		},
		CreatedAt: time.Now().UTC(),
	}

	if err := h.Store.CreatePromptVersion(r.Context(), pv); err != nil {
		// Duplicate key is also a conflict (409), not a generic error.
		errStr := err.Error()
		if strings.Contains(errStr, "UNIQUE") || strings.Contains(errStr, "duplicate") {
			http.Error(w, "version already exists", http.StatusConflict)
			return
		}
		slog.Error("query: create prompt version", "project_id", projectID, "name", pv.Name, "err", err)
		http.Error(w, "store error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(pv)
}

// HandleGetPrompt handles GET /api/v1/prompts/:name.
// Resolves by ?version=N (unbounded cache) or ?label=<label> (30s TTL cache).
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

	if versionQuery == "" && labelQuery == "" {
		http.Error(w, "provide ?version=N or ?label=<label>", http.StatusBadRequest)
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
		pv, getErr = h.Cache.GetVersion(r.Context(), r.URL.Query().Get("project_id"), name, v)
	} else if labelQuery != "" {
		pv, getErr = h.Cache.GetLabel(r.Context(), r.URL.Query().Get("project_id"), name, labelQuery)
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
	json.NewEncoder(w).Encode(pv)
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

	names, err := h.Store.ListPromptNames(r.Context(), projectID)
	if err != nil {
		slog.Error("query: list prompt names", "project_id", projectID, "err", err)
		http.Error(w, "store error", http.StatusInternalServerError)
		return
	}

	result := make([]promptListItem, 0, len(names))
	for _, name := range names {
		// Get all versions for this prompt to find the latest.
		versions, err := h.Store.ListPromptVersions(r.Context(), projectID, name)
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
			pv, err := h.Store.GetPromptByLabel(r.Context(), projectID, name, label)
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
	var projectID string
	if h.SessionStore != nil {
		projectID, _ = h.SessionStore.ProjectID(r)
	}
	if projectID == "" {
		projectID = r.URL.Query().Get("project_id")
	}
	_, err := h.Store.GetPromptVersion(r.Context(), projectID, name, req.Version)
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

	if setErr := h.Store.SetPromptLabel(r.Context(), pl); setErr != nil {
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

// ---- In-Process Caches ----

// cacheEntry holds a cached prompt version with an optional expiration time.
// A zero ExpiresAt means the entry never expires (used for the version cache).
type cacheEntry struct {
	PromptVersion *domain.PromptVersion
	ExpiresAt     time.Time
}

// PromptCache provides in-process caching for prompt lookups:
//   - Version cache: unbounded (no eviction)
//   - Label cache: 30-second TTL expiry
type PromptCache struct {
	mu           sync.RWMutex
	Store        metadata.Store
	versionCache map[string]*cacheEntry
	labelCache   map[string]*cacheEntry
}

// NewPromptCache creates a new PromptCache backed by the given Store.
func NewPromptCache(store metadata.Store) *PromptCache {
	return &PromptCache{
		Store:        store,
		versionCache: make(map[string]*cacheEntry),
		labelCache:   make(map[string]*cacheEntry),
	}
}

// versionCacheKey builds the cache key for a prompt version lookup.
func versionCacheKey(projectID, name string, version int64) string {
	return projectID + "|" + name + "|" + strconv.FormatInt(version, 10)
}

// labelCacheKey builds the cache key for a prompt label lookup.
func labelCacheKey(projectID, name, label string) string {
	return projectID + "|" + name + "|" + label
}

// GetVersion retrieves a prompt version from the cache or the store.
// The version cache never evicts (unbounded) in Phase 1.
func (c *PromptCache) GetVersion(ctx context.Context, projectID, name string, version int64) (*domain.PromptVersion, error) {
	key := versionCacheKey(projectID, name, version)

	c.mu.RLock()
	if entry, ok := c.versionCache[key]; ok {
		c.mu.RUnlock()
		return entry.PromptVersion, nil
	}
	c.mu.RUnlock()

	// Cache miss — fetch from store.
	pv, err := c.Store.GetPromptVersion(ctx, projectID, name, version)
	if err != nil {
		return nil, err
	}

	c.mu.Lock()
	// Double-check after acquiring write lock.
	if entry, ok := c.versionCache[key]; ok {
		c.mu.Unlock()
		return entry.PromptVersion, nil
	}
	c.versionCache[key] = &cacheEntry{PromptVersion: pv}
	c.mu.Unlock()

	return pv, nil
}

// GetLabel retrieves a prompt version resolved by label from the cache or the store.
// The label cache expires after 30 seconds.
func (c *PromptCache) GetLabel(ctx context.Context, projectID, name, label string) (*domain.PromptVersion, error) {
	key := labelCacheKey(projectID, name, label)

	c.mu.RLock()
	if entry, ok := c.labelCache[key]; ok {
		if time.Now().Before(entry.ExpiresAt) {
			pv := entry.PromptVersion
			c.mu.RUnlock()
			return pv, nil
		}
		// Expired — fall through to store.
	}
	c.mu.RUnlock()

	// Cache miss or expired — fetch from store.
	pv, err := c.Store.GetPromptByLabel(ctx, projectID, name, label)
	if err != nil {
		return nil, err
	}

	c.mu.Lock()
	// Double-check after acquiring write lock.
	if entry, ok := c.labelCache[key]; ok && time.Now().Before(entry.ExpiresAt) {
		pv = entry.PromptVersion
	} else {
		c.labelCache[key] = &cacheEntry{
			PromptVersion: pv,
			ExpiresAt:     time.Now().Add(30 * time.Second),
		}
	}
	c.mu.Unlock()

	return pv, nil
}

// InvalidateLabel removes a label cache entry so the next lookup hits the store.
// Called after label reassignment.
func (c *PromptCache) InvalidateLabel(projectID, name, label string) {
	key := labelCacheKey(projectID, name, label)
	c.mu.Lock()
	delete(c.labelCache, key)
	c.mu.Unlock()
}

// HandleListPromptVersions handles GET /api/v1/prompts/:name/versions
// and returns all versions for a prompt name (uses metadata store).
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

	projectID := r.URL.Query().Get("project_id")
	if projectID == "" {
		http.Error(w, "project_id is required", http.StatusBadRequest)
		return
	}

	versions, err := h.Store.ListPromptVersions(r.Context(), projectID, name)
	if err != nil {
		slog.Error("query: list prompt versions", "project_id", projectID, "name", name, "err", err)
		http.Error(w, "store error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"name":     name,
		"versions": versions,
		"count":    len(versions),
	})
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
