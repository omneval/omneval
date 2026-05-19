package handler

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/omneval/omneval/services/query/internal/auth"
)

// AdminHandler handles admin-only API endpoints:
// - GET  /api/v1/admin/api-keys       — list all API keys across all projects
// - DEL  /api/v1/admin/api-keys/:id   — revoke an API key
// - DEL  /api/v1/admin/projects/:id   — delete a project and its data
// - GET  /api/v1/admin/traces/:projId/count — count traces for a project
// - DEL  /api/v1/admin/traces/:projId — delete all traces for a project
type AdminHandler struct {
	DB           *sql.DB
	SessionStore SessionStore
}

// HandleAdminAPIKeysList handles GET /api/v1/admin/api-keys.
// Returns all API keys across all projects.
func (h *AdminHandler) HandleAdminAPIKeysList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSONError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if !auth.IsAdminUser(r) {
		writeJSONError(w, "forbidden: admin access required", http.StatusForbidden)
		return
	}

	projects, err := h.SessionStore.ListProjects(r)
	if err != nil {
		writeJSONError(w, "failed to list projects", http.StatusInternalServerError)
		return
	}

	var allKeys []map[string]any
	for _, p := range projects {
		keys := fetchProjectAPIKeys(h.DB, p.ProjectID)
		allKeys = append(allKeys, keys...)
	}

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

	projects, err := h.SessionStore.ListProjects(r)
	if err != nil {
		writeJSONError(w, "failed to list projects", http.StatusInternalServerError)
		return
	}

	revoked := false
	for _, p := range projects {
		if err := revokeKey(h.DB, p.ProjectID, keyID); err == nil {
			revoked = true
		}
	}

	if !revoked {
		writeJSONError(w, "key not found", http.StatusNotFound)
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
// Deletes all spans (traces) for a project.
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

	_, err := h.DB.Exec(`DELETE FROM spans WHERE project_id = ?`, projectID)
	if err != nil {
		writeJSONError(w, "failed to delete traces", http.StatusInternalServerError)
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

	// Delete all spans for this project.
	_, err := h.DB.Exec(`DELETE FROM spans WHERE project_id = ?`, projectID)
	if err != nil {
		writeJSONError(w, "failed to delete traces", http.StatusInternalServerError)
		return
	}

	// Revoke all API keys for this project.
	_, err = h.DB.Exec(
		`UPDATE api_keys SET revoked_at = CURRENT_TIMESTAMP
		 WHERE project_id = ? AND revoked_at IS NULL`,
		projectID,
	)
	if err != nil {
		writeJSONError(w, "failed to revoke keys", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// RegisterAdminRoutes registers the admin endpoints on the given mux.
// These routes should be protected by session auth + admin check.
func (h *AdminHandler) RegisterAdminRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/admin/api-keys", h.HandleAdminAPIKeysList)
	mux.HandleFunc("DELETE /api/v1/admin/api-keys/", h.HandleAdminAPIKeyDelete)
	mux.HandleFunc("GET /api/v1/admin/traces/", h.HandleAdminTracesCount)
	mux.HandleFunc("DELETE /api/v1/admin/traces/", h.HandleAdminTracesDelete)
	mux.HandleFunc("DELETE /api/v1/admin/projects/", h.HandleAdminProjectsDelete)
}

// fetchProjectAPIKeys queries the api_keys table for a given project.
func fetchProjectAPIKeys(db *sql.DB, projectID string) []map[string]any {
	rows, err := db.Query(`
		SELECT key_id, kind, service_name, created_at, revoked_at
		FROM api_keys
		WHERE project_id = ?
		ORDER BY created_at DESC
	`, projectID)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var keys []map[string]any
	for rows.Next() {
		var keyID, kind, createdAt string
		var serviceName, revokedAt *string
		if err := rows.Scan(&keyID, &kind, &serviceName, &createdAt, &revokedAt); err != nil {
			continue
		}
		key := map[string]any{
			"key_id":     keyID,
			"kind":       kind,
			"created_at": createdAt,
		}
		if serviceName != nil {
			key["service_name"] = *serviceName
		}
		if revokedAt != nil {
			key["revoked_at"] = *revokedAt
		}
		keys = append(keys, key)
	}
	return keys
}

// revokeKey marks an API key as revoked in a specific project.
func revokeKey(db *sql.DB, projectID, keyID string) error {
	_, err := db.Exec(
		`UPDATE api_keys SET revoked_at = CURRENT_TIMESTAMP
		 WHERE project_id = ? AND key_id = ? AND revoked_at IS NULL`,
		projectID, keyID,
	)
	return err
}
