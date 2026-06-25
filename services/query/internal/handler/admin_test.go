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
	"github.com/omneval/omneval/internal/lake"
	"github.com/omneval/omneval/internal/laketest"
	"github.com/omneval/omneval/internal/metadata"
	"github.com/omneval/omneval/services/query/internal/auth"
	"github.com/redis/go-redis/v9"
)

func TestAdminRoutes(t *testing.T) {
	t.Parallel()

	h := &AdminHandler{}
	routes := h.AdminRoutes()

	// Verify all expected admin routes are present with AuthPolicyAdmin.
	expectedPaths := map[string]AuthPolicy{
		"GET /api/v1/admin/api-keys":     AuthPolicyAdmin,
		"DELETE /api/v1/admin/api-keys/": AuthPolicyAdmin,
		"GET /api/v1/admin/traces/":      AuthPolicyAdmin,
		"DELETE /api/v1/admin/traces/":   AuthPolicyAdmin,
		"DELETE /api/v1/admin/projects/": AuthPolicyAdmin,
		"GET /api/v1/admin/ops":          AuthPolicyAdmin,
	}

	for _, r := range routes {
		key := r.Method + " " + r.Path
		want, ok := expectedPaths[key]
		if !ok {
			t.Errorf("unexpected route: %s", key)
			continue
		}
		if r.Policy != want {
			t.Errorf("route %s policy = %v, want %v", key, r.Policy, want)
		}
	}

	if len(routes) != len(expectedPaths) {
		t.Errorf("got %d routes, want %d", len(routes), len(expectedPaths))
	}
}

func TestAdminHandler_TracesCount(t *testing.T) {
	db := setupTestDB(t)
	handler := &AdminHandler{DB: db, APIKeyStore: newFakeAdminStore(), ProjectStore: newFakeAdminStore(), SessionStore: &FakeSessionStore{projectID: "proj-1"}}

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
	lk := setupTestLake(t)
	store := newFakeAdminStore()
	handler := &AdminHandler{DB: lk.DB(), APIKeyStore: store, BookmarkStore: store, ProjectStore: store, LakeRW: lk, SessionStore: &FakeSessionStore{projectID: "proj-1"}}

	req := httptest.NewRequest("DELETE", "/api/v1/admin/traces/proj-1", nil)
	req = withAdminContext(req, "admin@test.com")

	w := httptest.NewRecorder()
	handler.HandleAdminTracesDelete(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("expected 204, got %d: %s", w.Code, w.Body.String())
	}
}

func TestAdminHandler_ProjectsDelete(t *testing.T) {
	lk := setupTestLake(t)
	store := newFakeAdminStore()
	handler := &AdminHandler{DB: lk.DB(), APIKeyStore: store, BookmarkStore: store, ProjectStore: store, LakeRW: lk, SessionStore: &FakeSessionStore{projectID: "proj-1"}}

	req := httptest.NewRequest("DELETE", "/api/v1/admin/projects/proj-1", nil)
	req = withAdminContext(req, "admin@test.com")

	w := httptest.NewRecorder()
	handler.HandleAdminProjectsDelete(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("expected 204, got %d: %s", w.Code, w.Body.String())
	}
}

// setupTestLake opens a local-catalog Lake and seeds it with one span and
// one score per project for "proj-1" and "proj-2".
func setupTestLake(t *testing.T) *lake.Lake {
	t.Helper()
	lk := laketest.NewLocal(t)

	start := time.Now().UTC()
	for _, projectID := range []string{"proj-1", "proj-2"} {
		span := &domain.Span{
			SpanID:    "span-" + projectID,
			TraceID:   "trace-" + projectID,
			ProjectID: projectID,
			Name:      "llm-call",
			Kind:      domain.SpanKind("llm"),
			StartTime: start,
			EndTime:   start.Add(time.Second),
		}
		if err := lk.InsertSpans(context.Background(), []*domain.Span{span}); err != nil {
			t.Fatalf("insert span: %v", err)
		}
		score := &domain.Score{
			ScoreID: "score-" + projectID, SpanID: span.SpanID, TraceID: span.TraceID,
			ProjectID: projectID, EvalName: "e", Value: 1, CreatedAt: start, SpanStartTime: start,
		}
		if err := lk.InsertScores(context.Background(), []*domain.Score{score}); err != nil {
			t.Fatalf("insert score: %v", err)
		}
	}
	return lk
}

// TestAdminHandler_TracesDelete_Lake proves that deleting a project's traces
// through the Lake path removes its spans, scores, and bookmarks durably and
// leaves other projects untouched (#91).
func TestAdminHandler_TracesDelete_Lake(t *testing.T) {
	lk := setupTestLake(t)
	store := newFakeAdminStore()
	handler := &AdminHandler{DB: lk.DB(), APIKeyStore: store, BookmarkStore: store, ProjectStore: store, LakeRW: lk, SessionStore: &FakeSessionStore{projectID: "proj-1"}}

	req := httptest.NewRequest("DELETE", "/api/v1/admin/traces/proj-1", nil)
	req = withAdminContext(req, "admin@test.com")

	w := httptest.NewRecorder()
	handler.HandleAdminTracesDelete(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", w.Code, w.Body.String())
	}

	var n int
	if err := lk.DB().QueryRowContext(context.Background(),
		"SELECT count(*) FROM lake.spans WHERE project_id = 'proj-1'").Scan(&n); err != nil {
		t.Fatalf("count proj-1 spans: %v", err)
	}
	if n != 0 {
		t.Errorf("proj-1 spans after delete: got %d, want 0", n)
	}
	if err := lk.DB().QueryRowContext(context.Background(),
		"SELECT count(*) FROM lake.scores WHERE project_id = 'proj-1'").Scan(&n); err != nil {
		t.Fatalf("count proj-1 scores: %v", err)
	}
	if n != 0 {
		t.Errorf("proj-1 scores after delete: got %d, want 0", n)
	}

	// proj-2 is untouched.
	if err := lk.DB().QueryRowContext(context.Background(),
		"SELECT count(*) FROM lake.spans WHERE project_id = 'proj-2'").Scan(&n); err != nil {
		t.Fatalf("count proj-2 spans: %v", err)
	}
	if n != 1 {
		t.Errorf("proj-2 spans after delete: got %d, want 1", n)
	}

	if store.removeBookmarksForProjectCalls != 1 {
		t.Errorf("RemoveBookmarksForProject calls: got %d, want 1", store.removeBookmarksForProjectCalls)
	}
}

// TestAdminHandler_TracesCount_Lake proves the trace-count endpoint reflects
// a Lake-path deletion immediately, on the same admin connection (#91).
func TestAdminHandler_TracesCount_Lake(t *testing.T) {
	lk := setupTestLake(t)
	if _, err := lk.DB().ExecContext(context.Background(),
		"CREATE OR REPLACE VIEW spans AS SELECT * FROM lake.spans"); err != nil {
		t.Fatalf("create spans view: %v", err)
	}
	store := newFakeAdminStore()
	handler := &AdminHandler{DB: lk.DB(), APIKeyStore: store, BookmarkStore: store, ProjectStore: store, LakeRW: lk, SessionStore: &FakeSessionStore{projectID: "proj-1"}}

	countProj1 := func() int {
		req := httptest.NewRequest("GET", "/api/v1/admin/traces/proj-1/count", nil)
		req = withAdminContext(req, "admin@test.com")
		w := httptest.NewRecorder()
		handler.HandleAdminTracesCount(w, req)
		var resp map[string]int
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		return resp["count"]
	}

	if got := countProj1(); got != 1 {
		t.Fatalf("count before delete: got %d, want 1", got)
	}

	deleteReq := httptest.NewRequest("DELETE", "/api/v1/admin/traces/proj-1", nil)
	deleteReq = withAdminContext(deleteReq, "admin@test.com")
	w := httptest.NewRecorder()
	handler.HandleAdminTracesDelete(w, deleteReq)
	if w.Code != http.StatusNoContent {
		t.Fatalf("delete: expected 204, got %d: %s", w.Code, w.Body.String())
	}

	if got := countProj1(); got != 0 {
		t.Errorf("count after delete: got %d, want 0", got)
	}
}

func TestAdminHandler_Ops(t *testing.T) {
	t.Parallel()

	db := setupTestDB(t)
	fakeRedis := &fakeRedisLLEN{depth: 42}
	handler := &AdminHandler{
		DB:            db,
		APIKeyStore:   newFakeAdminStore(),
		ProjectStore:  newFakeAdminStore(),
		SessionStore:  &FakeSessionStore{projectID: "proj-1"},
		IngestQueueDB: fakeRedis,
	}

	req := httptest.NewRequest("GET", "/api/v1/admin/ops", nil)
	req = withAdminContext(req, "admin@test.com")

	w := httptest.NewRecorder()
	handler.HandleAdminOps(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]int
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if resp["ingest_queue_depth"] != 42 {
		t.Errorf("ingest_queue_depth = %d, want 42", resp["ingest_queue_depth"])
	}
}

func TestAdminHandler_OpsMethodNotAllowed(t *testing.T) {
	t.Parallel()

	db := setupTestDB(t)
	handler := &AdminHandler{
		DB:            db,
		APIKeyStore:   newFakeAdminStore(),
		ProjectStore:  newFakeAdminStore(),
		SessionStore:  &FakeSessionStore{projectID: "proj-1"},
		IngestQueueDB: &fakeRedisLLEN{depth: 0},
	}

	req := httptest.NewRequest("POST", "/api/v1/admin/ops", nil)
	req = withAdminContext(req, "admin@test.com")

	w := httptest.NewRecorder()
	handler.HandleAdminOps(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d: %s", w.Code, w.Body.String())
	}
}

func TestAdminHandler_OpsNonAdminForbidden(t *testing.T) {
	t.Parallel()

	db := setupTestDB(t)
	handler := &AdminHandler{
		DB:            db,
		APIKeyStore:   newFakeAdminStore(),
		ProjectStore:  newFakeAdminStore(),
		SessionStore:  &FakeSessionStore{projectID: "proj-1"},
		IngestQueueDB: &fakeRedisLLEN{depth: 0},
	}

	req := httptest.NewRequest("GET", "/api/v1/admin/ops", nil)
	ctx := req.Context()
	ctx = context.WithValue(ctx, auth.AdminContextKey, "admin@test.com")
	ctx = context.WithValue(ctx, auth.CurrentUserKey, &auth.CurrentUser{
		UserID: "test-user",
		Email:  "non-admin@test.com",
	})
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	handler.HandleAdminOps(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d: %s", w.Code, w.Body.String())
	}
}

// fakeRedisLLEN is a hand-written fake for the ingest queue depth check.
type fakeRedisLLEN struct {
	depth int
}

var _ ingestQueueLLEN = (*fakeRedisLLEN)(nil)

func (f *fakeRedisLLEN) LLen(ctx context.Context, key string) *redis.IntCmd {
	return redis.NewIntResult(int64(f.depth), nil)
}

func TestAdminAPIKeysList_WithProjectKeys(t *testing.T) {
	t.Parallel()

	db := setupTestDB(t)
	projStore := newFakeAdminStore()
	apiKeyStore := newFakeAdminStore()

	// Seed a project so that ListProjects returns it.
	now := time.Now().UTC()
	projStore.projects = []*domain.Project{
		{ProjectID: "proj-admin-1", OrgID: "org-1", Name: "admin-project", CreatedAt: now},
	}

	// Seed an API key for that project on the APIKeyStore.
	apiKeyStore.keys = []*domain.APIKey{
		{
			KeyID:     "key-admin-1",
			ProjectID: "proj-admin-1",
			Kind:      domain.APIKeyKindProject,
			Name:      "test-key",
			CreatedAt: now,
		},
	}

	handler := &AdminHandler{
		DB:            db,
		APIKeyStore:   apiKeyStore,
		ProjectStore:  projStore,
		SessionStore:  &FakeSessionStore{projectID: "proj-1"},
		IngestQueueDB: &fakeRedisLLEN{depth: 0},
	}

	req := httptest.NewRequest("GET", "/api/v1/admin/api-keys", nil)
	req = withAdminContext(req, "admin@test.com")

	w := httptest.NewRecorder()
	handler.HandleAdminAPIKeysList(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var keys []adminAPIKeyInfo
	if err := json.Unmarshal(w.Body.Bytes(), &keys); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if len(keys) != 1 {
		t.Fatalf("expected 1 API key in admin listing, got %d", len(keys))
	}

	if keys[0].KeyID != "key-admin-1" {
		t.Errorf("key_id = %q, want %q", keys[0].KeyID, "key-admin-1")
	}
	if keys[0].ProjectID != "proj-admin-1" {
		t.Errorf("project_id = %q, want %q", keys[0].ProjectID, "proj-admin-1")
	}
}

func TestAdminHandler_MethodNotAllowed(t *testing.T) {
	db := setupTestDB(t)
	handler := &AdminHandler{DB: db, APIKeyStore: newFakeAdminStore(), ProjectStore: newFakeAdminStore(), SessionStore: &FakeSessionStore{projectID: "proj-1"}}

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
	handler := &AdminHandler{DB: db, APIKeyStore: newFakeAdminStore(), ProjectStore: newFakeAdminStore(), SessionStore: &FakeSessionStore{projectID: "proj-1"}}

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
	handler := &AdminHandler{DB: db, APIKeyStore: newFakeAdminStore(), ProjectStore: newFakeAdminStore(), SessionStore: &FakeSessionStore{projectID: "proj-1"}}

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
	handler := &AdminHandler{DB: db, APIKeyStore: newFakeAdminStore(), ProjectStore: newFakeAdminStore(), SessionStore: &FakeSessionStore{projectID: "proj-1"}}

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
	handler := &AdminHandler{DB: db, APIKeyStore: newFakeAdminStore(), ProjectStore: newFakeAdminStore(), SessionStore: &FakeSessionStore{projectID: "proj-1"}}

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
	handler := &AdminHandler{DB: db, APIKeyStore: newFakeAdminStore(), ProjectStore: newFakeAdminStore(), SessionStore: &FakeSessionStore{projectID: "proj-1"}}

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
	store.projects = []*domain.Project{
		{ProjectID: "proj-1"},
		{ProjectID: "proj-2"},
	}
	store.keys = []*domain.APIKey{
		{KeyID: "key-1", ProjectID: "proj-1", Kind: domain.APIKeyKindProject, CreatedAt: now},
		{KeyID: "key-2", ProjectID: "proj-1", Kind: domain.APIKeyKindService, ServiceName: "agent", CreatedAt: now},
		{KeyID: "key-3", ProjectID: "proj-2", Kind: domain.APIKeyKindProject, CreatedAt: now},
	}

	handler := &AdminHandler{
		DB:           db,
		APIKeyStore:  store,
		ProjectStore: store,
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

// TestAdminHandler_APIKeysList_IncludesNameWithFallback verifies that the
// admin API keys endpoint includes a non-empty "name" for every key (#143):
// the user-supplied name when set, and a derived fallback (never the
// generic "Project Key" / "Service Key" label) when not.
func TestAdminHandler_APIKeysList_IncludesNameWithFallback(t *testing.T) {
	db := setupTestDB(t)

	store := newFakeAdminStore()
	now := time.Now().UTC()
	store.projects = []*domain.Project{
		{ProjectID: "proj-1"},
	}
	store.keys = []*domain.APIKey{
		{KeyID: "key-1", ProjectID: "proj-1", Kind: domain.APIKeyKindProject, Name: "CI ingest", CreatedAt: now},
		{KeyID: "key-2", ProjectID: "proj-1", Kind: domain.APIKeyKindProject, CreatedAt: now},
	}

	handler := &AdminHandler{
		DB:           db,
		APIKeyStore:  store,
		ProjectStore: store,
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
	if len(keys) != 2 {
		t.Fatalf("expected 2 keys, got %d: %v", len(keys), keys)
	}
	if keys[0]["name"] != "CI ingest" {
		t.Errorf("named key: got name %v, want %q", keys[0]["name"], "CI ingest")
	}
	name, _ := keys[1]["name"].(string)
	if name == "" || name == "Project Key" {
		t.Errorf("unnamed key fallback name: got %q, want a non-empty derived label", name)
	}
}

// TestAdminHandler_APIKeysList_EmptyWhenNoKeys verifies the admin endpoint
// returns an empty array (not null/error) when there are no keys.
func TestAdminHandler_APIKeysList_EmptyWhenNoKeys(t *testing.T) {
	db := setupTestDB(t)

	store := newFakeAdminStore()
	// No keys inserted.

	handler := &AdminHandler{
		DB:           db,
		APIKeyStore:  store,
		ProjectStore: store,
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
	keys                           []*domain.APIKey
	projects                       []*domain.Project
	removeBookmarksForProjectCalls int
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

func (f *fakeAdminStore) MarkBatchCommitted(ctx context.Context, batchID string, committedAt time.Time) error {
	return nil
}
func (f *fakeAdminStore) IsBatchCommitted(ctx context.Context, batchID string) (bool, error) {
	return false, nil
}

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
	if f.projects == nil {
		return nil, nil
	}
	return f.projects, nil
}
func (f *fakeAdminStore) CreateUser(_ context.Context, _ *domain.User) error { return nil }
func (f *fakeAdminStore) GetUserByEmail(_ context.Context, _ string) (*domain.User, error) {
	return nil, metadata.ErrNotFound
}
func (f *fakeAdminStore) GetUserByID(_ context.Context, _ string) (*domain.User, error) {
	return nil, metadata.ErrNotFound
}
func (f *fakeAdminStore) CountUsers(_ context.Context) (int, error)               { return 0, nil }
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
func (f *fakeAdminStore) CheckPassword(_, _ string) error                          { return nil }
func (f *fakeAdminStore) CreateSession(_ context.Context, _ *domain.Session) error { return nil }
func (f *fakeAdminStore) GetSession(_ context.Context, _ string) (*domain.Session, error) {
	return nil, metadata.ErrNotFound
}
func (f *fakeAdminStore) DeleteSession(_ context.Context, _ string) error        { return nil }
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
func (f *fakeAdminStore) CreateDatasetItemsBatch(_ context.Context, _ []*domain.DatasetItem) error {
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
	ctx = context.WithValue(ctx, auth.AdminEmailContextKey, email)
	ctx = context.WithValue(ctx, auth.UserIDContextKey, &auth.CurrentUser{
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
			span_id         VARCHAR NOT NULL,
			trace_id        VARCHAR NOT NULL,
			parent_id       VARCHAR,
			conversation_id VARCHAR,
			project_id      VARCHAR NOT NULL,
			service_name    VARCHAR,
			name            VARCHAR,
			kind            VARCHAR,
			start_time      TIMESTAMPTZ NOT NULL,
			end_time        TIMESTAMPTZ,
			model           VARCHAR,
			input           JSON,
			output          JSON,
			token_input     INTEGER DEFAULT 0,
			token_output    INTEGER DEFAULT 0,
			cost_usd        DECIMAL(12,8) DEFAULT 0,
			observation     JSON,
			attributes      JSON,
			bookmark        INTEGER DEFAULT 0,
			scores          JSON DEFAULT '[]',
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

func (f *fakeAdminStore) SetBookmark(_ context.Context, _ *domain.Bookmark) error { return nil }
func (f *fakeAdminStore) RemoveBookmark(_ context.Context, _, _ string) error     { return nil }
func (f *fakeAdminStore) RemoveBookmarksForProject(_ context.Context, _ string) error {
	f.removeBookmarksForProjectCalls++
	return nil
}
func (f *fakeAdminStore) IsBookmarked(_ context.Context, _, _ string) (bool, error) {
	return false, nil
}
func (f *fakeAdminStore) ListBookmarkedTraceIDs(_ context.Context, _ string) ([]string, error) {
	return nil, nil
}
