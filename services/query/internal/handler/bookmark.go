package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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

	// Parse optional JSON body. An empty body (EOF) is treated as a pure toggle
	// request — the handler queries current state and flips it. An explicit
	// {"bookmarked": true/false} body sets the desired state directly.
	var req struct {
		Bookmarked *bool `json:"bookmarked"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}

	projectID, ok := h.SessionStore.ProjectID(r)
	if !ok || projectID == "" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	// Determine desired bookmark state.
	var wantBookmarked bool
	if req.Bookmarked != nil {
		// Explicit value supplied in body.
		wantBookmarked = *req.Bookmarked
	} else {
		// No body → toggle: check current state.
		currently, err := h.isBookmarked(r.Context(), projectID, traceID)
		if err != nil {
			http.Error(w, fmt.Sprintf("bookmark check error: %v", err), http.StatusInternalServerError)
			return
		}
		wantBookmarked = !currently
	}

	if wantBookmarked {
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
	json.NewEncoder(w).Encode(map[string]bool{"bookmarked": wantBookmarked})
}

// isBookmarked reports whether a bookmark record exists for the given trace_id and project_id.
func (h *BookmarkHandler) isBookmarked(ctx context.Context, projectID, traceID string) (bool, error) {
	rows, err := h.DB.QueryContext(ctx, `
		SELECT COUNT(*) FROM bookmarks WHERE trace_id = ? AND project_id = ?
	`, traceID, projectID)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	var count int
	if rows.Next() {
		if err := rows.Scan(&count); err != nil {
			return false, err
		}
	}
	return count > 0, nil
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
