package handler

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/omneval/omneval/internal/domain"
	"github.com/omneval/omneval/internal/metadata"
	"github.com/omneval/omneval/services/query/internal/auth"
)

func TestAdminHandler_TracesCount(t *testing.T) {
	db := setupTestDB(t)
	handler := &AdminHandler{DB: db, Store: newFakeAdminStore(), SessionStore: &FakeSessionStore{projectID: "proj-1"}}

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
	handler := &AdminHandler{DB: db, Store: newFakeAdminStore(), SessionStore: &FakeSessionStore{projectID: "proj-1"}}

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
	handler := &AdminHandler{DB: db, Store: newFakeAdminStore(), SessionStore: &FakeSessionStore{projectID: "proj-1"}}

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
	handler := &AdminHandler{DB: db, Store: newFakeAdminStore(), SessionStore: &FakeSessionStore{projectID: "proj-1"}}

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
	handler := &AdminHandler{DB: db, Store: newFakeAdminStore(), SessionStore: &FakeSessionStore{projectID: "proj-1"}}

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
	handler := &AdminHandler{DB: db, Store: newFakeAdminStore(), SessionStore: &FakeSessionStore{projectID: "proj-1"}}

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
	handler := &AdminHandler{DB: db, Store: newFakeAdminStore(), SessionStore: &FakeSessionStore{projectID: "proj-1"}}

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
	handler := &AdminHandler{DB: db, Store: newFakeAdminStore(), SessionStore: &FakeSessionStore{projectID: "proj-1"}}

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
	handler := &AdminHandler{DB: db, Store: newFakeAdminStore(), SessionStore: &FakeSessionStore{projectID: "proj-1"}}

	req := httptest.NewRequest("DELETE", "/api/v1/admin/api-keys/", nil)
	req = withAdminContext(req, "admin@test.com")

	w := httptest.NewRecorder()
	handler.HandleAdminAPIKeyDelete(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

// TestAdminHandler_APIKeysList_ReturnsAllOrgKeys verifies that the admin
// API keys endpoint returns all keys across all projects by querying the
// metadata store — not DuckDB.
func TestAdminHandler_APIKeysList_ReturnsAllOrgKeys(t *testing.T) {
	db := setupTestDB(t)

	store := newFakeAdminStore()
	now := time.Now().UTC()
	store.keys = []*domain.APIKey{
		{KeyID: "key-1", ProjectID: "proj-1", Kind: domain.APIKeyKindProject, CreatedAt: now},
		{KeyID: "key-2", ProjectID: "proj-1", Kind: domain.APIKeyKindService, ServiceName: "agent", CreatedAt: now},
		{KeyID: "key-3", ProjectID: "proj-2", Kind: domain.APIKeyKindProject, CreatedAt: now},
	}

	handler := &AdminHandler{
		DB: db,
		Store: store,
		SessionStore: &FakeSessionStore{
			userProjects: []*domain.Project{
				{ProjectID: "proj-1"},
				{ProjectID: "proj-2"},
			},
		},
	}

	req := httptest.NewRequest("GET", "/api/v1/admin/api-keys", nil)
	req = withAdminContext(req, "admin@test.com")

	w := httptest.NewRecorder()
	handler.HandleAdminAPIKeysList(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var keys []map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &keys); err != nil {
		t.Fatalf("decode response: %v (body: %q)", err, w.Body.String())
	}

	if len(keys) != 3 {
		t.Errorf("expected 3 keys across all projects, got %d: %v", len(keys), keys)
	}
}

// TestAdminHandler_APIKeysList_EmptyWhenNoKeys verifies the admin endpoint
// returns an empty array (not null/error) when there are no keys.
func TestAdminHandler_APIKeysList_EmptyWhenNoKeys(t *testing.T) {
	db := setupTestDB(t)

	store := newFakeAdminStore()
	// No keys inserted.

	handler := &AdminHandler{
		DB: db,
		Store: store,
		SessionStore: &FakeSessionStore{
			userProjects: []*domain.Project{
				{ProjectID: "proj-1"},
			},
		},
	}

	req := httptest.NewRequest("GET", "/api/v1/admin/api-keys", nil)
	req = withAdminContext(req, "admin@test.com")

	w := httptest.NewRecorder()
	handler.HandleAdminAPIKeysList(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var keys []map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &keys); err != nil {
		t.Fatalf("decode response: %v (body: %q)", err, w.Body.String())
	}

	if len(keys) != 0 {
		t.Errorf("expected 0 keys, got %d", len(keys))
	}
}

// fakeAdminStore is a minimal metadata.Store fake for AdminHandler tests.
// It supports ListAPIKeys and RevokeAPIKey; all other methods are stubs.
type fakeAdminStore struct {
	keys []*domain.APIKey
}

func newFakeAdminStore() *fakeAdminStore {
	return &fakeAdminStore{}
}

func (f *fakeAdminStore) ListAPIKeys(_ context.Context, projectID string) ([]*domain.APIKey, error) {
	var result []*domain.APIKey
	for _, k := range f.keys {
		if k.ProjectID == projectID {
			result = append(result, k)
		}
	}
	return result, nil
}

func (f *fakeAdminStore) RevokeAPIKey(_ context.Context, keyID string) error {
	for _, k := range f.keys {
		if k.KeyID == keyID {
			now := time.Now().UTC()
			k.RevokedAt = &now
			return nil
		}
	}
	return metadata.ErrNotFound
}

// ---- metadata.Store stubs (not exercised by these tests) ----

func (f *fakeAdminStore) CreateOrganization(_ context.Context, _ *domain.Organization) error {
	return nil
}
func (f *fakeAdminStore) GetOrganization(_ context.Context, _ string) (*domain.Organization, error) {
	return nil, metadata.ErrNotFound
}
func (f *fakeAdminStore) CreateProject(_ context.Context, _ *domain.Project) error { return nil }
func (f *fakeAdminStore) GetProject(_ context.Context, _ string) (*domain.Project, error) {
	return nil, metadata.ErrNotFound
}
func (f *fakeAdminStore) ListProjects(_ context.Context, _ string) ([]*domain.Project, error) {
	return nil, nil
}
func (f *fakeAdminStore) CreateUser(_ context.Context, _ *domain.User) error { return nil }
func (f *fakeAdminStore) GetUserByEmail(_ context.Context, _ string) (*domain.User, error) {
	return nil, metadata.ErrNotFound
}
func (f *fakeAdminStore) GetUserByID(_ context.Context, _ string) (*domain.User, error) {
	return nil, metadata.ErrNotFound
}
func (f *fakeAdminStore) CountUsers(_ context.Context) (int, error) { return 0, nil }
func (f *fakeAdminStore) UpdateUserPassword(_ context.Context, _, _ string) error { return nil }
func (f *fakeAdminStore) UpdateUserResetToken(_ context.Context, _, _ string, _ time.Time) error {
	return nil
}
func (f *fakeAdminStore) GetUserByResetToken(_ context.Context, _ string) (*domain.User, error) {
	return nil, metadata.ErrNotFound
}
func (f *fakeAdminStore) ListUsers(_ context.Context, _ string) ([]*domain.User, error) {
	return nil, nil
}
func (f *fakeAdminStore) CheckPassword(_, _ string) error { return nil }
func (f *fakeAdminStore) CreateSession(_ context.Context, _ *domain.Session) error { return nil }
func (f *fakeAdminStore) GetSession(_ context.Context, _ string) (*domain.Session, error) {
	return nil, metadata.ErrNotFound
}
func (f *fakeAdminStore) DeleteSession(_ context.Context, _ string) error { return nil }
func (f *fakeAdminStore) CreateAPIKey(_ context.Context, _ *domain.APIKey) error { return nil }
func (f *fakeAdminStore) GetAPIKeyByHash(_ context.Context, _ string) (*domain.APIKey, error) {
	return nil, metadata.ErrNotFound
}
func (f *fakeAdminStore) CreatePromptVersion(_ context.Context, _ *domain.PromptVersion) error {
	return nil
}
func (f *fakeAdminStore) GetPromptVersion(_ context.Context, _, _ string, _ int64) (*domain.PromptVersion, error) {
	return nil, metadata.ErrNotFound
}
func (f *fakeAdminStore) GetPromptByLabel(_ context.Context, _, _, _ string) (*domain.PromptVersion, error) {
	return nil, metadata.ErrNotFound
}
func (f *fakeAdminStore) ListPromptNames(_ context.Context, _ string) ([]string, error) {
	return nil, nil
}
func (f *fakeAdminStore) ListPromptVersions(_ context.Context, _, _ string) ([]*domain.PromptVersion, error) {
	return nil, nil
}
func (f *fakeAdminStore) SetPromptLabel(_ context.Context, _ *domain.PromptLabel) error { return nil }
func (f *fakeAdminStore) CreateEvalRule(_ context.Context, _ *domain.EvalRule) error    { return nil }
func (f *fakeAdminStore) GetEvalRule(_ context.Context, _ string) (*domain.EvalRule, error) {
	return nil, metadata.ErrNotFound
}
func (f *fakeAdminStore) ListEvalRules(_ context.Context, _ string) ([]*domain.EvalRule, error) {
	return nil, nil
}
func (f *fakeAdminStore) UpdateEvalRule(_ context.Context, _ *domain.EvalRule) error { return nil }
func (f *fakeAdminStore) DeleteEvalRule(_ context.Context, _ string) error           { return nil }
func (f *fakeAdminStore) CreateDataset(_ context.Context, _ *domain.Dataset) error   { return nil }
func (f *fakeAdminStore) ListDatasets(_ context.Context, _ string) ([]*domain.Dataset, error) {
	return nil, nil
}
func (f *fakeAdminStore) GetDataset(_ context.Context, _ string) (*domain.Dataset, error) {
	return nil, metadata.ErrNotFound
}
func (f *fakeAdminStore) DeleteDataset(_ context.Context, _ string) error { return nil }
func (f *fakeAdminStore) CreateDatasetItem(_ context.Context, _ *domain.DatasetItem) error {
	return nil
}
func (f *fakeAdminStore) ListDatasetItems(_ context.Context, _ string) ([]*domain.DatasetItem, error) {
	return nil, nil
}
func (f *fakeAdminStore) ListDatasetItemsPaginated(_ context.Context, _, _ string, _ int) ([]*domain.DatasetItem, string, error) {
	return nil, "", nil
}
func (f *fakeAdminStore) CreateDatasetRun(_ context.Context, _ *domain.DatasetRun) error {
	return nil
}
func (f *fakeAdminStore) GetDatasetRun(_ context.Context, _ string) (*domain.DatasetRun, error) {
	return nil, metadata.ErrNotFound
}
func (f *fakeAdminStore) UpdateDatasetRun(_ context.Context, _ *domain.DatasetRun) error {
	return nil
}
func (f *fakeAdminStore) ListDatasetRuns(_ context.Context, _ string) ([]*domain.DatasetRun, error) {
	return nil, nil
}
func (f *fakeAdminStore) CreateDatasetRunItem(_ context.Context, _ *domain.DatasetRunItem) error {
	return nil
}
func (f *fakeAdminStore) GetDatasetRunItem(_ context.Context, _ string) (*domain.DatasetRunItem, error) {
	return nil, metadata.ErrNotFound
}
func (f *fakeAdminStore) UpdateDatasetRunItem(_ context.Context, _ *domain.DatasetRunItem) error {
	return nil
}
func (f *fakeAdminStore) ListDatasetRunItems(_ context.Context, _ string) ([]*domain.DatasetRunItem, error) {
	return nil, nil
}
func (f *fakeAdminStore) Migrate(_ context.Context) error { return nil }
func (f *fakeAdminStore) Close() error                    { return nil }

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
	tmpDir, err := os.MkdirTemp("", "omneval-test-*")
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
			parent_id        VARCHAR,
			conversation_id  VARCHAR,
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
