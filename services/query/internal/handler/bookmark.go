package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/omneval/omneval/internal/auth"
	"github.com/omneval/omneval/internal/domain"
	"github.com/omneval/omneval/internal/metadata"
)

// BookmarkHandler handles POST /api/v1/traces/{traceId}/bookmark.
// Bookmarks are mutable user state and live in the Metadata Store
// (ADR-0004 moved them out of the hot DuckDB store).
type BookmarkHandler struct {
	BookmarkStore   metadata.BookmarkStore
	SessionStore    SessionStore
	ProjectResolver auth.ProjectResolver
}

// resolveProjectID returns a ProjectResolver that chains h.ProjectResolver
// (if non-nil) with a fallback to h.SessionStore.ProjectID.  When both are
// nil it returns nil so callers still get the 401.
func (h *BookmarkHandler) resolveProjectID() auth.ProjectResolver {
	if h.ProjectResolver == nil && h.SessionStore != nil {
		return auth.NewSessionStoreResolver(h.SessionStore)
	}
	return h.ProjectResolver
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

	// Resolve project ID using the shared resolver (API-key context → session → query param).
	resolver := h.resolveProjectID()
	projectID, ok := auth.ProjectIDWithErrorWithResolver(w, r, resolver)
	if !ok {
		return
	}

	// Determine desired bookmark state.
	var wantBookmarked bool
	if req.Bookmarked != nil {
		// Explicit value supplied in body.
		wantBookmarked = *req.Bookmarked
	} else {
		// No body → toggle: check current state.
		currently, err := h.BookmarkStore.IsBookmarked(r.Context(), projectID, traceID)
		if err != nil {
			http.Error(w, fmt.Sprintf("bookmark check error: %v", err), http.StatusInternalServerError)
			return
		}
		wantBookmarked = !currently
	}

	if wantBookmarked {
		err := h.BookmarkStore.SetBookmark(r.Context(), &domain.Bookmark{
			ProjectID: projectID,
			TraceID:   traceID,
		})
		if err != nil {
			http.Error(w, fmt.Sprintf("bookmark error: %v", err), http.StatusInternalServerError)
			return
		}
	} else {
		if err := h.BookmarkStore.RemoveBookmark(r.Context(), projectID, traceID); err != nil {
			http.Error(w, fmt.Sprintf("unbookmark error: %v", err), http.StatusInternalServerError)
			return
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"bookmarked": wantBookmarked})
}

// Routes returns the bookmark-related API routes as AuthRoute entries with
// AuthPolicySession so the Router can use them for policy-based auth dispatch.
func (h *BookmarkHandler) Routes() []AuthRoute {
	return []AuthRoute{
		{Method: http.MethodPost, Path: "/api/v1/traces/{traceId}/bookmark", Handler: http.HandlerFunc(h.HandleBookmark), Policy: AuthPolicySession},
	}
}
