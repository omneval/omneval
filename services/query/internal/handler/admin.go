package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/omneval/omneval/internal/domain"
	"github.com/omneval/omneval/internal/lake"
	"github.com/omneval/omneval/internal/metadata"
	"github.com/omneval/omneval/internal/queue"
	"github.com/omneval/omneval/services/query/internal/auth"
)

// ingestQueueLLEN is the minimal interface for reading a Redis list length.
// It allows injecting a real Redis client or a mock in tests.
type ingestQueueLLEN interface {
	LLen(ctx context.Context, key string) (int64, error)
}

// adminAPIKeyInfo is the JSON shape returned by the admin API keys list endpoint.
// It extends the per-project APIKeyInfo shape with a project_id field so admins
// can see which project each key belongs to.
type adminAPIKeyInfo struct {
	KeyID       string            `json:"key_id"`
	ProjectID   string            `json:"project_id"`
	Kind        domain.APIKeyKind `json:"kind"`
	ServiceName string            `json:"service_name,omitempty"`
	Name        string            `json:"name,omitempty"`
	CreatedAt   time.Time         `json:"created_at"`
	RevokedAt   *time.Time        `json:"revoked_at,omitempty"`
}

// AdminHandler handles admin-only API endpoints:
// - GET  /api/v1/admin/api-keys       — list all API keys across all projects
// - DEL  /api/v1/admin/api-keys/:id   — revoke an API key
// - DEL  /api/v1/admin/projects/:id   — delete a project and its data
// - GET  /api/v1/admin/traces/:projId/count — count traces for a project
// - DEL  /api/v1/admin/traces/:projId — delete all traces for a project
// - GET  /api/v1/admin/ops              — read-only ops metrics (e.g. queue depth)
type AdminHandler struct {
	DB            DBHandle
	APIKeyStore   metadata.APIKeyStore
	BookmarkStore metadata.BookmarkStore
	ProjectStore  metadata.ProjectStore
	SessionStore  metadata.SessionStore

	// IngestQueueDB provides a read-only view of the ingest queue (e.g.
	// Redis LLEN). Nil when the Query API has no Redis connectivity.
	IngestQueueDB ingestQueueLLEN

	// LakeRW is a read-write Lake attachment used for durable admin deletes
	// (ADR-0004 / #91). Always set in Lake mode (the only mode now).
	LakeRW *lake.Lake
}

// HandleAdminAPIKeysList handles GET /api/v1/admin/api-keys.
// Returns all API keys across all projects by querying the metadata store.
func (h *AdminHandler) HandleAdminAPIKeysList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSONError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if !auth.IsAdminUser(r) {
		writeJSONError(w, "forbidden: admin access required", http.StatusForbidden)
		return
	}

	projects, err := h.ProjectStore.ListProjects(r.Context(), "")
	if err != nil {
		writeJSONError(w, "failed to list projects", http.StatusInternalServerError)
		return
	}

	var allKeys []adminAPIKeyInfo
	for _, p := range projects {
		keys, err := h.APIKeyStore.ListAPIKeys(r.Context(), p.ProjectID)
		if err != nil {
			continue // skip projects that fail rather than aborting the whole response
		}
		for _, k := range keys {
			entry := adminAPIKeyInfo{
				KeyID:       k.KeyID,
				ProjectID:   k.ProjectID,
				Kind:        k.Kind,
				ServiceName: k.ServiceName,
				Name:        auth.DisplayName(k),
				CreatedAt:   k.CreatedAt,
				RevokedAt:   k.RevokedAt,
			}
			allKeys = append(allKeys, entry)
		}
	}

	if allKeys == nil {
		allKeys = []adminAPIKeyInfo{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(allKeys)
}

// HandleAdminAPIKeyDelete handles DELETE /api/v1/admin/api-keys/:id.
// Revokes an API key regardless of which project it belongs to.
func (h *AdminHandler) HandleAdminAPIKeyDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		writeJSONError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if !auth.IsAdminUser(r) {
		writeJSONError(w, "forbidden: admin access required", http.StatusForbidden)
		return
	}

	keyID := strings.TrimPrefix(r.URL.Path, "/api/v1/admin/api-keys/")
	if keyID == "" {
		writeJSONError(w, "missing key ID", http.StatusBadRequest)
		return
	}

	if err := h.APIKeyStore.RevokeAPIKey(r.Context(), keyID); err != nil {
		if err == metadata.ErrNotFound {
			writeJSONError(w, "key not found", http.StatusNotFound)
			return
		}
		writeJSONError(w, "failed to revoke key", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// HandleAdminTracesCount handles GET /api/v1/admin/traces/:projectID/count.
// Returns the total trace count for a project.
func (h *AdminHandler) HandleAdminTracesCount(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSONError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if !auth.IsAdminUser(r) {
		writeJSONError(w, "forbidden: admin access required", http.StatusForbidden)
		return
	}

	projectID := strings.TrimPrefix(r.URL.Path, "/api/v1/admin/traces/")
	// Remove the /count suffix if present.
	projectID = strings.TrimSuffix(projectID, "/count")
	if projectID == "" {
		writeJSONError(w, "missing project ID", http.StatusBadRequest)
		return
	}

	var count int
	err := h.DB.QueryRow(`
		SELECT COUNT(DISTINCT trace_id) FROM spans
		WHERE project_id = ?
	`, projectID).Scan(&count)
	if err != nil {
		count = 0
	}

	json.NewEncoder(w).Encode(map[string]int{"count": count})
}

// HandleAdminTracesDelete handles DELETE /api/v1/admin/traces/:projectID.
// Deletes all spans, scores, and bookmarks (traces) for a project. The
// delete commits through the Catalog (LakeRW, ADR-0004/#91): the rows are
// gone from every reader's next query and do not resurrect.
func (h *AdminHandler) HandleAdminTracesDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		writeJSONError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if !auth.IsAdminUser(r) {
		writeJSONError(w, "forbidden: admin access required", http.StatusForbidden)
		return
	}

	projectID := strings.TrimPrefix(r.URL.Path, "/api/v1/admin/traces/")
	if projectID == "" {
		writeJSONError(w, "missing project ID", http.StatusBadRequest)
		return
	}

	if err := h.LakeRW.DeleteProject(r.Context(), projectID); err != nil {
		writeJSONError(w, "failed to delete traces", http.StatusInternalServerError)
		return
	}

	if err := h.BookmarkStore.RemoveBookmarksForProject(r.Context(), projectID); err != nil {
		writeJSONError(w, "failed to delete bookmarks", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// HandleAdminProjectsDelete handles DELETE /api/v1/admin/projects/:projectID.
// Deletes a project, all its traces, and revokes all its API keys.
func (h *AdminHandler) HandleAdminProjectsDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		writeJSONError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if !auth.IsAdminUser(r) {
		writeJSONError(w, "forbidden: admin access required", http.StatusForbidden)
		return
	}

	projectID := strings.TrimPrefix(r.URL.Path, "/api/v1/admin/projects/")
	if projectID == "" {
		writeJSONError(w, "missing project ID", http.StatusBadRequest)
		return
	}

	// Delete all spans, scores, and bookmarks for this project. The delete
	// commits through the Catalog (LakeRW, ADR-0004/#91).
	if err := h.LakeRW.DeleteProject(r.Context(), projectID); err != nil {
		writeJSONError(w, "failed to delete traces", http.StatusInternalServerError)
		return
	}
	if err := h.BookmarkStore.RemoveBookmarksForProject(r.Context(), projectID); err != nil {
		writeJSONError(w, "failed to delete bookmarks", http.StatusInternalServerError)
		return
	}

	// Revoke all API keys for this project via the metadata store.
	keys, err := h.APIKeyStore.ListAPIKeys(r.Context(), projectID)
	if err != nil {
		writeJSONError(w, "failed to list keys for revocation", http.StatusInternalServerError)
		return
	}
	for _, k := range keys {
		if k.RevokedAt == nil {
			_ = h.APIKeyStore.RevokeAPIKey(r.Context(), k.KeyID) // best-effort; log errors are not fatal
		}
	}

	w.WriteHeader(http.StatusNoContent)
}

// HandleAdminOps handles GET /api/v1/admin/ops.
// Returns read-only ops metrics (e.g. ingest queue depth). This endpoint
// is deliberately kept separate from destructive admin actions (delete).
func (h *AdminHandler) HandleAdminOps(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSONError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if !auth.IsAdminUser(r) {
		writeJSONError(w, "forbidden: admin access required", http.StatusForbidden)
		return
	}

	metrics := make(map[string]int)

	if h.IngestQueueDB != nil {
		depth, err := h.IngestQueueDB.LLen(r.Context(), queue.KeyIngestSpans)
		if err == nil {
			metrics["ingest_queue_depth"] = int(depth)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(metrics)
}

// AdminRoutes returns the admin endpoints as AuthRoute entries with
// AuthPolicyAdmin so the Router can use them for policy-based auth dispatch.
func (h *AdminHandler) AdminRoutes() []AuthRoute {
	return []AuthRoute{
		{Method: http.MethodGet, Path: "/api/v1/admin/api-keys", Handler: http.HandlerFunc(h.HandleAdminAPIKeysList), Policy: AuthPolicyAdmin},
		{Method: http.MethodDelete, Path: "/api/v1/admin/api-keys/", Handler: http.HandlerFunc(h.HandleAdminAPIKeyDelete), Policy: AuthPolicyAdmin},
		{Method: http.MethodGet, Path: "/api/v1/admin/traces/", Handler: http.HandlerFunc(h.HandleAdminTracesCount), Policy: AuthPolicyAdmin},
		{Method: http.MethodDelete, Path: "/api/v1/admin/traces/", Handler: http.HandlerFunc(h.HandleAdminTracesDelete), Policy: AuthPolicyAdmin},
		{Method: http.MethodDelete, Path: "/api/v1/admin/projects/", Handler: http.HandlerFunc(h.HandleAdminProjectsDelete), Policy: AuthPolicyAdmin},
		{Method: http.MethodGet, Path: "/api/v1/admin/ops", Handler: http.HandlerFunc(h.HandleAdminOps), Policy: AuthPolicyAdmin},
	}
}

// RegisterAdminRoutes registers the admin endpoints on the given mux.
// These routes should be protected by session auth + admin check.
func (h *AdminHandler) RegisterAdminRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/admin/api-keys", h.HandleAdminAPIKeysList)
	mux.HandleFunc("DELETE /api/v1/admin/api-keys/", h.HandleAdminAPIKeyDelete)
	mux.HandleFunc("GET /api/v1/admin/traces/", h.HandleAdminTracesCount)
	mux.HandleFunc("DELETE /api/v1/admin/traces/", h.HandleAdminTracesDelete)
	mux.HandleFunc("DELETE /api/v1/admin/projects/", h.HandleAdminProjectsDelete)
	mux.HandleFunc("GET /api/v1/admin/ops", h.HandleAdminOps)
}
