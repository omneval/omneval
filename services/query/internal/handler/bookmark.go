package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// BookmarkHandler handles POST /api/v1/traces/{traceId}/bookmark.
// It toggles bookmark state by inserting or deleting from the bookmarks table.
type BookmarkHandler struct {
	DB           DBHandle
	SessionStore SessionStore
}

// HandleBookmark handles POST /api/v1/traces/{traceId}/bookmark.
// It toggles the bookmark flag on/off for the given trace_id.
func (h *BookmarkHandler) HandleBookmark(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	traceID := r.PathValue("traceId")
	if traceID == "" {
		http.Error(w, "missing trace ID", http.StatusBadRequest)
		return
	}

	var req struct {
		Bookmarked bool `json:"bookmarked"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}

	projectID, ok := h.SessionStore.ProjectID(r)
	if !ok || projectID == "" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	if req.Bookmarked {
		if err := h.bookmark(r.Context(), projectID, traceID); err != nil {
			http.Error(w, fmt.Sprintf("bookmark error: %v", err), http.StatusInternalServerError)
			return
		}
	} else {
		if err := h.unbookmark(r.Context(), projectID, traceID); err != nil {
			http.Error(w, fmt.Sprintf("unbookmark error: %v", err), http.StatusInternalServerError)
			return
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"bookmarked": req.Bookmarked})
}

// bookmark inserts a bookmark record for the given trace_id and project_id.
func (h *BookmarkHandler) bookmark(ctx context.Context, projectID, traceID string) error {
	_, err := h.DB.ExecContext(ctx, `
		INSERT OR REPLACE INTO bookmarks (trace_id, project_id, created_at)
		VALUES (?, ?, ?)
	`, traceID, projectID, time.Now())
	return err
}

// unbookmark removes a bookmark record for the given trace_id and project_id.
func (h *BookmarkHandler) unbookmark(ctx context.Context, projectID, traceID string) error {
	_, err := h.DB.ExecContext(ctx, `
		DELETE FROM bookmarks WHERE trace_id = ? AND project_id = ?
	`, traceID, projectID)
	return err
}
