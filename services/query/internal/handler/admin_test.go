package handler

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/zbloss/lantern/services/query/internal/auth"
)

func TestAdminHandler_TracesCount(t *testing.T) {
	db := setupTestDB(t)
	handler := &AdminHandler{DB: db, SessionStore: &FakeSessionStore{projectID: "proj-1"}}

	req := httptest.NewRequest("GET", "/api/v1/admin/traces/proj-1/count", nil)
	req = withAdminContext(req, "admin@test.com")

	w := httptest.NewRecorder()
	handler.HandleAdminTracesCount(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]int
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if _, ok := resp["count"]; ok {
		t.Logf("response has count: %+v", resp)
	}
}

func TestAdminHandler_TracesDelete(t *testing.T) {
	db := setupTestDB(t)
	handler := &AdminHandler{DB: db, SessionStore: &FakeSessionStore{projectID: "proj-1"}}

	req := httptest.NewRequest("DELETE", "/api/v1/admin/traces/proj-1", nil)
	req = withAdminContext(req, "admin@test.com")

	w := httptest.NewRecorder()
	handler.HandleAdminTracesDelete(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("expected 204, got %d: %s", w.Code, w.Body.String())
	}
}

func TestAdminHandler_ProjectsDelete(t *testing.T) {
	db := setupTestDB(t)
	handler := &AdminHandler{DB: db, SessionStore: &FakeSessionStore{projectID: "proj-1"}}

	req := httptest.NewRequest("DELETE", "/api/v1/admin/projects/proj-1", nil)
	req = withAdminContext(req, "admin@test.com")

	w := httptest.NewRecorder()
	handler.HandleAdminProjectsDelete(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("expected 204, got %d: %s", w.Code, w.Body.String())
	}
}

func TestAdminHandler_MethodNotAllowed(t *testing.T) {
	db := setupTestDB(t)
	handler := &AdminHandler{DB: db, SessionStore: &FakeSessionStore{projectID: "proj-1"}}

	req := httptest.NewRequest("POST", "/api/v1/admin/api-keys", nil)
	req = withAdminContext(req, "admin@test.com")

	w := httptest.NewRecorder()
	handler.HandleAdminAPIKeysList(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d: %s", w.Code, w.Body.String())
	}
}

func TestAdminHandler_NonAdminForbidden(t *testing.T) {
	db := setupTestDB(t)
	handler := &AdminHandler{DB: db, SessionStore: &FakeSessionStore{projectID: "proj-1"}}

	req := httptest.NewRequest("GET", "/api/v1/admin/api-keys", nil)
	// Set admin email to "admin@test.com" but user email to "non-admin@test.com"
	ctx := req.Context()
	ctx = context.WithValue(ctx, auth.AdminContextKey, "admin@test.com")
	ctx = context.WithValue(ctx, auth.CurrentUserKey, &auth.CurrentUser{
		UserID: "test-user",
		Email:  "non-admin@test.com",
	})
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	handler.HandleAdminAPIKeysList(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d: %s", w.Code, w.Body.String())
	}
}

func TestAdminHandler_TracesCountEmptyProjectID(t *testing.T) {
	db := setupTestDB(t)
	handler := &AdminHandler{DB: db, SessionStore: &FakeSessionStore{projectID: "proj-1"}}

	req := httptest.NewRequest("GET", "/api/v1/admin/traces//count", nil)
	req = withAdminContext(req, "admin@test.com")

	w := httptest.NewRecorder()
	handler.HandleAdminTracesCount(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestAdminHandler_TracesDeleteEmptyProjectID(t *testing.T) {
	db := setupTestDB(t)
	handler := &AdminHandler{DB: db, SessionStore: &FakeSessionStore{projectID: "proj-1"}}

	req := httptest.NewRequest("DELETE", "/api/v1/admin/traces/", nil)
	req = withAdminContext(req, "admin@test.com")

	w := httptest.NewRecorder()
	handler.HandleAdminTracesDelete(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestAdminHandler_ProjectsDeleteEmptyProjectID(t *testing.T) {
	db := setupTestDB(t)
	handler := &AdminHandler{DB: db, SessionStore: &FakeSessionStore{projectID: "proj-1"}}

	req := httptest.NewRequest("DELETE", "/api/v1/admin/projects/", nil)
	req = withAdminContext(req, "admin@test.com")

	w := httptest.NewRecorder()
	handler.HandleAdminProjectsDelete(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestAdminHandler_KeyDeleteEmptyKeyID(t *testing.T) {
	db := setupTestDB(t)
	handler := &AdminHandler{DB: db, SessionStore: &FakeSessionStore{projectID: "proj-1"}}

	req := httptest.NewRequest("DELETE", "/api/v1/admin/api-keys/", nil)
	req = withAdminContext(req, "admin@test.com")

	w := httptest.NewRecorder()
	handler.HandleAdminAPIKeyDelete(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func withAdminContext(req *http.Request, email string) *http.Request {
	ctx := req.Context()
	ctx = context.WithValue(ctx, auth.AdminContextKey, email)
	ctx = context.WithValue(ctx, auth.CurrentUserKey, &auth.CurrentUser{
		UserID: "test-user",
		Email:  email,
	})
	return req.WithContext(ctx)
}

func setupTestDB(t *testing.T) *sql.DB {
	t.Helper()
	tmpDir, err := os.MkdirTemp("", "lantern-test-*")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(tmpDir) })

	tmpPath := tmpDir + "/test.duckdb"
	db, err := sql.Open("duckdb", tmpPath)
	if err != nil {
		t.Fatalf("open duckdb: %v", err)
	}

	// Create spans table.
	_, err = db.ExecContext(context.Background(), `
		CREATE TABLE spans (
			span_id        VARCHAR NOT NULL,
			trace_id       VARCHAR NOT NULL,
			parent_id      VARCHAR,
			project_id     VARCHAR NOT NULL,
			service_name   VARCHAR,
			name           VARCHAR,
			kind           VARCHAR,
			start_time     TIMESTAMPTZ NOT NULL,
			end_time       TIMESTAMPTZ,
			model          VARCHAR,
			input          JSON,
			output         JSON,
			token_input    INTEGER DEFAULT 0,
			token_output   INTEGER DEFAULT 0,
			cost_usd       DECIMAL(12,8) DEFAULT 0,
			observation    JSON,
			attributes     JSON,
			bookmark       INTEGER DEFAULT 0,
			scores         JSON DEFAULT '[]',
			PRIMARY KEY (trace_id, span_id)
		)`)
	if err != nil {
		t.Fatalf("create spans table: %v", err)
	}

	// Create api_keys table.
	_, err = db.ExecContext(context.Background(), `
		CREATE TABLE api_keys (
			key_id        VARCHAR NOT NULL PRIMARY KEY,
			project_id    VARCHAR NOT NULL,
			kind          VARCHAR NOT NULL,
			service_name  VARCHAR,
			hashed_key    VARCHAR NOT NULL,
			created_at    TIMESTAMPTZ NOT NULL,
			revoked_at    TIMESTAMPTZ
		)`)
	if err != nil {
		t.Fatalf("create api_keys table: %v", err)
	}

	return db
}
