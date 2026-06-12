package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/omneval/omneval/internal/fake"
)

func newBookmarkMux(t *testing.T, projectID string) (*http.ServeMux, *fake.FakeMetadataStore) {
	t.Helper()
	store := fake.NewFakeMetadataStore()

	mux := http.NewServeMux()
	bh := &BookmarkHandler{
		Store:        store,
		SessionStore: &FakeSessionStore{projectID: projectID},
	}
	mux.HandleFunc("POST /api/v1/traces/{traceId}/bookmark", bh.HandleBookmark)

	return mux, store
}

func TestBookmarkHandler_ToggleBookmark(t *testing.T) {
	mux, store := newBookmarkMux(t, "test-proj")

	t.Run("bookmark a trace", func(t *testing.T) {
		body, _ := json.Marshal(map[string]bool{"bookmarked": true})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/traces/trace-1/bookmark", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("status: got %d, want %d\nbody: %s", w.Code, http.StatusOK, w.Body.String())
		}

		var resp map[string]bool
		if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if !resp["bookmarked"] {
			t.Error("expected bookmarked=true in response")
		}

		// Verify bookmark exists in store.
		bookmarked, err := store.IsBookmarked(req.Context(), "test-proj", "trace-1")
		if err != nil {
			t.Fatalf("isBookmarked: %v", err)
		}
		if !bookmarked {
			t.Error("expected trace to be bookmarked")
		}
	})

	t.Run("unbookmark a trace", func(t *testing.T) {
		body, _ := json.Marshal(map[string]bool{"bookmarked": false})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/traces/trace-1/bookmark", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("status: got %d, want %d\nbody: %s", w.Code, http.StatusOK, w.Body.String())
		}

		// Verify bookmark was removed.
		bookmarked, err := store.IsBookmarked(req.Context(), "test-proj", "trace-1")
		if err != nil {
			t.Fatalf("isBookmarked: %v", err)
		}
		if bookmarked {
			t.Error("expected trace to not be bookmarked")
		}
	})

	t.Run("method not allowed", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/traces/trace-1/bookmark", nil)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)

		if w.Code != http.StatusMethodNotAllowed {
			t.Errorf("status: got %d, want %d", w.Code, http.StatusMethodNotAllowed)
		}
	})

	t.Run("invalid body", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/traces/trace-1/bookmark", bytes.NewReader([]byte("not json")))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("status: got %d, want %d", w.Code, http.StatusBadRequest)
		}
	})
}

// TestBookmarkHandler_EmptyBodyToggles verifies that POST with no body
// treats the endpoint as a pure toggle: first call bookmarks, second unbookmarks.
func TestBookmarkHandler_EmptyBodyToggles(t *testing.T) {
	mux, store := newBookmarkMux(t, "test-proj")

	// First POST with no body — should bookmark (insert).
	req := httptest.NewRequest(http.MethodPost, "/api/v1/traces/trace-toggle/bookmark", http.NoBody)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("first toggle status: got %d, want %d\nbody: %s", w.Code, http.StatusOK, w.Body.String())
	}

	bookmarked, err := store.IsBookmarked(req.Context(), "test-proj", "trace-toggle")
	if err != nil {
		t.Fatalf("isBookmarked after first toggle: %v", err)
	}
	if !bookmarked {
		t.Error("after first toggle: expected trace to be bookmarked")
	}

	// Second POST with no body — should unbookmark (delete).
	req = httptest.NewRequest(http.MethodPost, "/api/v1/traces/trace-toggle/bookmark", http.NoBody)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("second toggle status: got %d, want %d\nbody: %s", w.Code, http.StatusOK, w.Body.String())
	}

	bookmarked, err = store.IsBookmarked(req.Context(), "test-proj", "trace-toggle")
	if err != nil {
		t.Fatalf("isBookmarked after second toggle: %v", err)
	}
	if bookmarked {
		t.Error("after second toggle: expected trace to not be bookmarked")
	}
}

func TestBookmarkHandler_AuthRequired(t *testing.T) {
	mux, _ := newBookmarkMux(t, "") // empty project ID

	body, _ := json.Marshal(map[string]bool{"bookmarked": true})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/traces/trace-1/bookmark", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusUnauthorized)
	}
}
