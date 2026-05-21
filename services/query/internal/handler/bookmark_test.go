package handler

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	_ "github.com/marcboeker/go-duckdb/v2"
)

const bookmarkTestSchema = `
	CREATE TABLE spans (
		span_id        VARCHAR      NOT NULL,
		trace_id       VARCHAR      NOT NULL,
		parent_id      VARCHAR,
		project_id     VARCHAR      NOT NULL,
		service_name   VARCHAR,
		name           VARCHAR,
		kind           VARCHAR,
		start_time     TIMESTAMPTZ  NOT NULL,
		end_time       TIMESTAMPTZ,
		model          VARCHAR,
		input          JSON,
		output         JSON,
		input_tokens   BIGINT,
		output_tokens  BIGINT,
		cost_usd       DOUBLE,
		prompt_name    VARCHAR,
		prompt_version BIGINT,
		status_code    VARCHAR,
		status_message VARCHAR,
		attributes     JSON,
		PRIMARY KEY (trace_id, span_id)
	);

	CREATE TABLE bookmarks (
		trace_id       VARCHAR      NOT NULL,
		project_id     VARCHAR      NOT NULL,
		created_at     TIMESTAMPTZ  NOT NULL,
		PRIMARY KEY (trace_id, project_id)
	);
`

func newBookmarkMux(t *testing.T, projectID string) (*http.ServeMux, *sql.DB) {
	t.Helper()
	db, err := sql.Open("duckdb", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if _, err := db.Exec(bookmarkTestSchema); err != nil {
		t.Fatalf("create schema: %v", err)
	}

	mux := http.NewServeMux()
	bh := &BookmarkHandler{
		DB:           db,
		SessionStore: &FakeSessionStore{projectID: projectID},
	}
	mux.HandleFunc("POST /api/v1/traces/{traceId}/bookmark", bh.HandleBookmark)

	return mux, db
}

func TestBookmarkHandler_ToggleBookmark(t *testing.T) {
	mux, db := newBookmarkMux(t, "test-proj")

	// Insert a test span so the trace exists.
	if _, err := db.Exec(
		`INSERT INTO spans (span_id, trace_id, project_id, name, kind, start_time) VALUES (?, ?, ?, ?, ?, ?)`,
		"span-1", "trace-1", "test-proj", "test", "generation", "2024-01-01T00:00:00Z",
	); err != nil {
		t.Fatalf("insert span: %v", err)
	}

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

		// Verify bookmark exists in DB.
		var count int
		if err := db.QueryRow("SELECT COUNT(*) FROM bookmarks WHERE trace_id = ? AND project_id = ?", "trace-1", "test-proj").Scan(&count); err != nil {
			t.Fatalf("query bookmarks: %v", err)
		}
		if count != 1 {
			t.Errorf("bookmark count: got %d, want %d", count, 1)
		}
	})

	t.Run("unbookmark a trace", func(t *testing.T) {
		body, _ := json.Marshal(map[string]bool{"bookmarked": false})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/traces/trace-1/bookmark", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("status: got %d, want %d", w.Code, http.StatusOK)
		}

		// Verify bookmark was removed.
		var count int
		if err := db.QueryRow("SELECT COUNT(*) FROM bookmarks WHERE trace_id = ? AND project_id = ?", "trace-1", "test-proj").Scan(&count); err != nil {
			t.Fatalf("query bookmarks: %v", err)
		}
		if count != 0 {
			t.Errorf("bookmark count after unbookmark: got %d, want %d", count, 0)
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
	mux, db := newBookmarkMux(t, "test-proj")

	// Insert a test span.
	if _, err := db.Exec(
		`INSERT INTO spans (span_id, trace_id, project_id, name, kind, start_time) VALUES (?, ?, ?, ?, ?, ?)`,
		"span-toggle", "trace-toggle", "test-proj", "test", "generation", "2024-01-01T00:00:00Z",
	); err != nil {
		t.Fatalf("insert span: %v", err)
	}

	// First POST with no body — should bookmark (insert).
	req := httptest.NewRequest(http.MethodPost, "/api/v1/traces/trace-toggle/bookmark", http.NoBody)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("first toggle status: got %d, want %d\nbody: %s", w.Code, http.StatusOK, w.Body.String())
	}

	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM bookmarks WHERE trace_id = ? AND project_id = ?", "trace-toggle", "test-proj").Scan(&count); err != nil {
		t.Fatalf("query bookmarks after first toggle: %v", err)
	}
	if count != 1 {
		t.Errorf("after first toggle bookmark count: got %d, want 1", count)
	}

	// Second POST with no body — should unbookmark (delete).
	req = httptest.NewRequest(http.MethodPost, "/api/v1/traces/trace-toggle/bookmark", http.NoBody)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("second toggle status: got %d, want %d\nbody: %s", w.Code, http.StatusOK, w.Body.String())
	}

	if err := db.QueryRow("SELECT COUNT(*) FROM bookmarks WHERE trace_id = ? AND project_id = ?", "trace-toggle", "test-proj").Scan(&count); err != nil {
		t.Fatalf("query bookmarks after second toggle: %v", err)
	}
	if count != 0 {
		t.Errorf("after second toggle bookmark count: got %d, want 0", count)
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
