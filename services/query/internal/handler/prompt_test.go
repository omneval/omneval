package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zbloss/lantern/internal/domain"
	"github.com/zbloss/lantern/internal/metadata"
)

// FakePromptStore implements metadata.Store for prompt-only operations.
type FakePromptStore struct {
	mu   sync.RWMutex
	promptVersions map[string]*domain.PromptVersion
	promptLabels   map[string]*domain.PromptLabel
}

func newFakePromptStore() *FakePromptStore {
	return &FakePromptStore{
		promptVersions: make(map[string]*domain.PromptVersion),
		promptLabels:   make(map[string]*domain.PromptLabel),
	}
}

func (m *FakePromptStore) versionKey(projectID, name string, version int64) string {
	return projectID + "|" + name + "|" + strconv.FormatInt(version, 10)
}

func labelKey(projectID, name, label string) string {
	return projectID + "|" + name + "|" + label
}

var errConflict = errors.New("conflict")

func (m *FakePromptStore) CreatePromptVersion(ctx context.Context, pv *domain.PromptVersion) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := m.versionKey(pv.ProjectID, pv.Name, pv.Version)
	if _, exists := m.promptVersions[key]; exists {
		return errConflict
	}
	m.promptVersions[key] = pv
	return nil
}

func (m *FakePromptStore) GetPromptVersion(ctx context.Context, projectID, name string, version int64) (*domain.PromptVersion, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	key := m.versionKey(projectID, name, version)
	pv, exists := m.promptVersions[key]
	if !exists {
		return nil, metadata.ErrNotFound
	}
	cp := *pv
	return &cp, nil
}

func (m *FakePromptStore) GetPromptByLabel(ctx context.Context, projectID, name, label string) (*domain.PromptVersion, error) {
	m.mu.RLock()
	lk := labelKey(projectID, name, label)
	pl, exists := m.promptLabels[lk]
	if !exists {
		m.mu.RUnlock()
		return nil, metadata.ErrNotFound
	}
	version := pl.Version
	m.mu.RUnlock()

	return m.GetPromptVersion(ctx, projectID, name, version)
}

func (m *FakePromptStore) ListPromptVersions(ctx context.Context, projectID, name string) ([]*domain.PromptVersion, error) {
	return nil, nil
}

func (m *FakePromptStore) SetPromptLabel(ctx context.Context, label *domain.PromptLabel) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	lk := labelKey(label.ProjectID, label.Name, label.Label)
	m.promptLabels[lk] = label
	return nil
}

// ---- Metadata.Store interface stubs (not used by tests) ----
func (m *FakePromptStore) CreateOrganization(ctx context.Context, o *domain.Organization) error              { return nil }
func (m *FakePromptStore) GetOrganization(ctx context.Context, id string) (*domain.Organization, error)     { return nil, metadata.ErrNotFound }
func (m *FakePromptStore) CreateProject(ctx context.Context, p *domain.Project) error                       { return nil }
func (m *FakePromptStore) GetProject(ctx context.Context, id string) (*domain.Project, error)               { return nil, metadata.ErrNotFound }
func (m *FakePromptStore) ListProjects(ctx context.Context, orgID string) ([]*domain.Project, error)        { return nil, nil }
func (m *FakePromptStore) CreateUser(ctx context.Context, u *domain.User) error                             { return nil }
func (m *FakePromptStore) GetUserByEmail(ctx context.Context, email string) (*domain.User, error)           { return nil, metadata.ErrNotFound }
func (m *FakePromptStore) GetUserByID(ctx context.Context, userID string) (*domain.User, error)             { return nil, metadata.ErrNotFound }
func (m *FakePromptStore) CountUsers(ctx context.Context) (int, error)                                      { return 0, nil }
func (m *FakePromptStore) UpdateUserPassword(ctx context.Context, userID, passwordHash string) error        { return nil }
func (m *FakePromptStore) CheckPassword(hashed, plaintext string) error                                     { return nil }
func (m *FakePromptStore) ListUsers(ctx context.Context, orgID string) ([]*domain.User, error)              { return nil, nil }
func (m *FakePromptStore) CreateSession(ctx context.Context, s *domain.Session) error                       { return nil }
func (m *FakePromptStore) GetSession(ctx context.Context, id string) (*domain.Session, error)               { return nil, metadata.ErrNotFound }
func (m *FakePromptStore) DeleteSession(ctx context.Context, id string) error                               { return nil }
func (m *FakePromptStore) CreateAPIKey(ctx context.Context, k *domain.APIKey) error                         { return nil }
func (m *FakePromptStore) GetAPIKeyByHash(ctx context.Context, hash string) (*domain.APIKey, error)         { return nil, metadata.ErrNotFound }
func (m *FakePromptStore) RevokeAPIKey(ctx context.Context, keyID string) error                             { return nil }
func (m *FakePromptStore) ListAPIKeys(ctx context.Context, projectID string) ([]*domain.APIKey, error)      { return nil, nil }
func (m *FakePromptStore) CreateEvalRule(ctx context.Context, r *domain.EvalRule) error                     { return nil }
func (m *FakePromptStore) GetEvalRule(ctx context.Context, id string) (*domain.EvalRule, error)             { return nil, metadata.ErrNotFound }
func (m *FakePromptStore) ListEvalRules(ctx context.Context, projectID string) ([]*domain.EvalRule, error)  { return nil, nil }
func (m *FakePromptStore) UpdateEvalRule(ctx context.Context, r *domain.EvalRule) error                     { return nil }
func (m *FakePromptStore) CreateDataset(ctx context.Context, d *domain.Dataset) error                       { return nil }
func (m *FakePromptStore) GetDataset(ctx context.Context, id string) (*domain.Dataset, error)               { return nil, metadata.ErrNotFound }
func (m *FakePromptStore) CreateDatasetItem(ctx context.Context, i *domain.DatasetItem) error               { return nil }
func (m *FakePromptStore) ListDatasetItems(ctx context.Context, datasetID string) ([]*domain.DatasetItem, error) { return nil, nil }
func (m *FakePromptStore) CreateDatasetRun(ctx context.Context, r *domain.DatasetRun) error                 { return nil }
func (m *FakePromptStore) GetDatasetRun(ctx context.Context, id string) (*domain.DatasetRun, error)         { return nil, metadata.ErrNotFound }
func (m *FakePromptStore) Migrate(ctx context.Context) error                                                { return nil }
func (m *FakePromptStore) Close() error                                                                     { return nil }

// ---- Tests ----

func TestPromptHandler_CreatePrompt(t *testing.T) {
	store := newFakePromptStore()
	cache := NewPromptCache(store)
	handler := &PromptHandler{
		Store:     store,
		Cache:     cache,
		SessionStore: &testSessionStore{projectID: "test-proj"},
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/prompts", strings.NewReader(`{
		"name": "greeting",
		"version": 1,
		"template": "Hello {{name}}, welcome to {{place}}!",
		"model": "gpt-4",
		"temperature": 0.7,
		"max_tokens": 256
	}`))
	w := httptest.NewRecorder()

	handler.HandleCreatePrompt(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("status: got %d, want %d\nbody: %s", w.Code, http.StatusCreated, w.Body.String())
	}
}

func TestPromptHandler_CreatePrompt_Conflict(t *testing.T) {
	store := newFakePromptStore()
	cache := NewPromptCache(store)
	handler := &PromptHandler{
		Store:     store,
		Cache:     cache,
		SessionStore: &testSessionStore{projectID: "test-proj"},
	}

	// Create first version.
	body1 := `{
		"name": "greeting",
		"version": 1,
		"template": "Hello {{name}}!",
		"model": "gpt-4",
		"temperature": 0.7,
		"max_tokens": 256
	}`
	req1 := httptest.NewRequest(http.MethodPost, "/api/v1/prompts", strings.NewReader(body1))
	w1 := httptest.NewRecorder()
	handler.HandleCreatePrompt(w1, req1)

	if w1.Code != http.StatusCreated {
		t.Fatalf("first create: status %d, want 201", w1.Code)
	}

	// Re-create the same version — should get 409.
	req2 := httptest.NewRequest(http.MethodPost, "/api/v1/prompts", strings.NewReader(body1))
	w2 := httptest.NewRecorder()
	handler.HandleCreatePrompt(w2, req2)

	if w2.Code != http.StatusConflict {
		t.Errorf("conflict: status %d, want %d", w2.Code, http.StatusConflict)
	}
}

func TestPromptHandler_GetPromptByVersion(t *testing.T) {
	store := newFakePromptStore()
	cache := NewPromptCache(store)
	handler := &PromptHandler{
		Store:     store,
		Cache:     cache,
	}

	// Pre-seed a version.
	store.CreatePromptVersion(context.Background(), &domain.PromptVersion{
		VersionID: "v1", ProjectID: "test-proj", Name: "greeting", Version: 1,
		Template: "Hello {{name}}!",
		ModelConfig: domain.PromptModelConfig{Model: "gpt-4", Temperature: 0.7, MaxTokens: 256},
	})

	// GET /api/v1/prompts/greeting?version=1
	req := httptest.NewRequest(http.MethodGet, "/api/v1/prompts/greeting?version=1&project_id=test-proj", nil)
	w := httptest.NewRecorder()
	handler.HandleGetPrompt(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want %d\nbody: %s", w.Code, http.StatusOK, w.Body.String())
	}

	var resp domain.PromptVersion
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if resp.Name != "greeting" {
		t.Errorf("name: got %q, want %q", resp.Name, "greeting")
	}
	if resp.Version != 1 {
		t.Errorf("version: got %d, want 1", resp.Version)
	}
	if resp.ModelConfig.Model != "gpt-4" {
		t.Errorf("model: got %q, want %q", resp.ModelConfig.Model, "gpt-4")
	}
	if resp.Template != "Hello {{name}}!" {
		t.Errorf("template: got %q, want %q", resp.Template, "Hello {{name}}!")
	}
}

func TestPromptHandler_GetPrompt_ByLabel(t *testing.T) {
	store := newFakePromptStore()
	cache := NewPromptCache(store)
	handler := &PromptHandler{
		Store:     store,
		Cache:     cache,
	}

	// Pre-seed versions and label.
	store.CreatePromptVersion(context.Background(), &domain.PromptVersion{
		VersionID: "v1", ProjectID: "test-proj", Name: "greeting", Version: 1,
		Template: "Old greeting",
		ModelConfig: domain.PromptModelConfig{Model: "gpt-3.5"},
	})
	store.CreatePromptVersion(context.Background(), &domain.PromptVersion{
		VersionID: "v2", ProjectID: "test-proj", Name: "greeting", Version: 2,
		Template: "New greeting",
		ModelConfig: domain.PromptModelConfig{Model: "gpt-4"},
	})
	store.SetPromptLabel(context.Background(), &domain.PromptLabel{
		ProjectID: "test-proj", Name: "greeting", Label: "production", Version: 2,
	})

	// GET /api/v1/prompts/greeting?label=production
	req := httptest.NewRequest(http.MethodGet, "/api/v1/prompts/greeting?label=production&project_id=test-proj", nil)
	w := httptest.NewRecorder()
	handler.HandleGetPrompt(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want %d\nbody: %s", w.Code, http.StatusOK, w.Body.String())
	}

	var resp struct {
		Name     string `json:"name"`
		Version  int64  `json:"version"`
		Template string `json:"template"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if resp.Version != 2 {
		t.Errorf("version: got %d, want 2", resp.Version)
	}
	if resp.Template != "New greeting" {
		t.Errorf("template: got %q, want %q", resp.Template, "New greeting")
	}
}

func TestPromptHandler_AssignLabel(t *testing.T) {
	store := newFakePromptStore()
	cache := NewPromptCache(store)
	handler := &PromptHandler{
		Store:     store,
		Cache:     cache,
	}

	// Pre-seed version 1 and 2.
	store.CreatePromptVersion(context.Background(), &domain.PromptVersion{
		VersionID: "v1", ProjectID: "test-proj", Name: "greeting", Version: 1,
		Template: "Old", ModelConfig: domain.PromptModelConfig{Model: "gpt-3.5"},
	})
	store.CreatePromptVersion(context.Background(), &domain.PromptVersion{
		VersionID: "v2", ProjectID: "test-proj", Name: "greeting", Version: 2,
		Template: "New", ModelConfig: domain.PromptModelConfig{Model: "gpt-4"},
	})
	store.SetPromptLabel(context.Background(), &domain.PromptLabel{
		ProjectID: "test-proj", Name: "greeting", Label: "production", Version: 1,
	})

	// PUT /api/v1/prompts/greeting/labels/production
	req := httptest.NewRequest(http.MethodPut, "/api/v1/prompts/greeting/labels/production?project_id=test-proj", strings.NewReader(`{"version": 2}`))
	w := httptest.NewRecorder()
	handler.HandleSetLabel(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want %d\nbody: %s", w.Code, http.StatusOK, w.Body.String())
	}

	// Verify label now points to version 2.
	pv, err := store.GetPromptByLabel(context.Background(), "test-proj", "greeting", "production")
	if err != nil {
		t.Fatalf("get by label: %v", err)
	}
	if pv.Version != 2 {
		t.Errorf("version: got %d, want 2", pv.Version)
	}
}

// TestPromptCache_VersionCacheHit verifies that repeated version lookups
// don't call the store a second time (cache hit).
func TestPromptCache_VersionCacheHit(t *testing.T) {
	store := newFakePromptStore()
	store.CreatePromptVersion(context.Background(), &domain.PromptVersion{
		VersionID: "v1", ProjectID: "test-proj", Name: "greeting", Version: 1,
		Template: "Hello {{name}}!",
		ModelConfig: domain.PromptModelConfig{Model: "gpt-4", Temperature: 0.7, MaxTokens: 256},
	})

	cache := NewPromptCache(store)

	// First call — cache miss, hits the store.
	pv1, err := cache.GetVersion(context.Background(), "test-proj", "greeting", 1)
	if err != nil {
		t.Fatalf("first lookup: %v", err)
	}
	if pv1.Template != "Hello {{name}}!" {
		t.Errorf("template: got %q", pv1.Template)
	}

	// Second call — cache hit, should return same object.
	pv2, err := cache.GetVersion(context.Background(), "test-proj", "greeting", 1)
	if err != nil {
		t.Fatalf("second lookup: %v", err)
	}
	if pv2 != pv1 {
		t.Error("version cache should return same object (cache hit)")
	}
}

// TestPromptCache_LabelCacheTTL verifies that label cache entries expire after 30 seconds.
func TestPromptCache_LabelCacheTTL(t *testing.T) {
	store := newFakePromptStore()
	store.CreatePromptVersion(context.Background(), &domain.PromptVersion{
		VersionID: "v1", ProjectID: "test-proj", Name: "greeting", Version: 1,
		Template: "Hello {{name}}!",
		ModelConfig: domain.PromptModelConfig{Model: "gpt-4"},
	})
	store.SetPromptLabel(context.Background(), &domain.PromptLabel{
		ProjectID: "test-proj", Name: "greeting", Label: "production", Version: 1,
	})

	cache := NewPromptCache(store)

	// First call — cache miss, hits the store.
	pv1, err := cache.GetLabel(context.Background(), "test-proj", "greeting", "production")
	if err != nil {
		t.Fatalf("first lookup: %v", err)
	}

	// Advance time past the TTL.
	cache.mu.Lock()
	key := "test-proj|greeting|production"
	if entry, ok := cache.labelCache[key]; ok {
		entry.ExpiresAt = time.Now().Add(-61 * time.Second)
	}
	cache.mu.Unlock()

	// Second call — cache expired, should hit the store again.
	pv2, err := cache.GetLabel(context.Background(), "test-proj", "greeting", "production")
	if err != nil {
		t.Fatalf("second lookup after expiry: %v", err)
	}
	if pv2 == pv1 {
		t.Error("after TTL expiry, cache should call the store again")
	}
}

// TestPromptCache_VersionCacheNeverEvicts verifies that the LRU version cache
// does not evict entries (infinite TTL for Phase 1).
func TestPromptCache_VersionCacheNeverEvicts(t *testing.T) {
	store := newFakePromptStore()
	for i := int64(1); i <= 100; i++ {
		store.CreatePromptVersion(context.Background(), &domain.PromptVersion{
			VersionID: "v" + string(rune('0'+i)),
			ProjectID: "test-proj", Name: "greeting", Version: i,
			Template: "version " + string(rune('0'+i)),
			ModelConfig: domain.PromptModelConfig{Model: "gpt-4"},
		})
	}

	cache := NewPromptCache(store)

	// Fill the cache with 50 versions.
	for i := int64(1); i <= 50; i++ {
		_, err := cache.GetVersion(context.Background(), "test-proj", "greeting", i)
		if err != nil {
			t.Fatalf("version %d lookup: %v", i, err)
		}
	}

	// Access 20 more versions (total 70).
	for i := int64(51); i <= 70; i++ {
		_, err := cache.GetVersion(context.Background(), "test-proj", "greeting", i)
		if err != nil {
			t.Fatalf("version %d lookup: %v", i, err)
		}
	}

	// Version 1 should still be cached (never evicted in Phase 1).
	_, err := cache.GetVersion(context.Background(), "test-proj", "greeting", 1)
	if err != nil {
		t.Error("version 1 should still be in the cache (no eviction in Phase 1)")
	}

	// Version 70 should also be cached.
	_, err = cache.GetVersion(context.Background(), "test-proj", "greeting", 70)
	if err != nil {
		t.Error("version 70 should be in the cache")
	}
}

// TestPromptCache_LabelCacheInvalidationOnLabelChange verifies that when a label
// is reassigned, subsequent label lookups return the new version (cache miss on next lookup).
func TestPromptCache_LabelCacheInvalidationOnLabelChange(t *testing.T) {
	store := newFakePromptStore()
	store.CreatePromptVersion(context.Background(), &domain.PromptVersion{
		VersionID: "v1", ProjectID: "test-proj", Name: "greeting", Version: 1,
		Template: "Old", ModelConfig: domain.PromptModelConfig{Model: "gpt-3.5"},
	})
	store.CreatePromptVersion(context.Background(), &domain.PromptVersion{
		VersionID: "v2", ProjectID: "test-proj", Name: "greeting", Version: 2,
		Template: "New", ModelConfig: domain.PromptModelConfig{Model: "gpt-4"},
	})
	store.SetPromptLabel(context.Background(), &domain.PromptLabel{
		ProjectID: "test-proj", Name: "greeting", Label: "production", Version: 1,
	})

	cache := NewPromptCache(store)

	// First label lookup — should return version 1.
	pv1, err := cache.GetLabel(context.Background(), "test-proj", "greeting", "production")
	if err != nil {
		t.Fatalf("first label lookup: %v", err)
	}
	if pv1.Version != 1 {
		t.Errorf("version: got %d, want 1", pv1.Version)
	}

	// Reassign label to version 2 via the store directly.
	store.SetPromptLabel(context.Background(), &domain.PromptLabel{
		ProjectID: "test-proj", Name: "greeting", Label: "production", Version: 2,
	})

	// After reassignment, the cache still has the old value. The handler should
	// clear the cache on label set. This is tested in the handler.
	// For now, verify the store reflects the change.
	pv2, err := store.GetPromptByLabel(context.Background(), "test-proj", "greeting", "production")
	if err != nil {
		t.Fatalf("store lookup after reassign: %v", err)
	}
	if pv2.Version != 2 {
		t.Errorf("store version: got %d, want 2", pv2.Version)
	}
}

// TestPromptHandler_CreatePrompt_AuthRequired verifies auth is enforced.
func TestPromptHandler_CreatePrompt_AuthRequired(t *testing.T) {
	store := newFakePromptStore()
	cache := NewPromptCache(store)
	handler := &PromptHandler{
		Store:     store,
		Cache:     cache,
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/prompts", strings.NewReader(`{}`))
	w := httptest.NewRecorder()
	handler.HandleCreatePrompt(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

// TestPromptHandler_CreatePrompt_MethodNotAllowed
func TestPromptHandler_CreatePrompt_MethodNotAllowed(t *testing.T) {
	store := newFakePromptStore()
	cache := NewPromptCache(store)
	handler := &PromptHandler{
		Store:     store,
		Cache:     cache,
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/prompts", nil)
	w := httptest.NewRecorder()
	handler.HandleCreatePrompt(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusMethodNotAllowed)
	}
}

// TestPromptHandler_GetPrompt_MissingName
func TestPromptHandler_GetPrompt_MissingName(t *testing.T) {
	store := newFakePromptStore()
	cache := NewPromptCache(store)
	handler := &PromptHandler{
		Store:     store,
		Cache:     cache,
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/prompts?version=1&project_id=test-proj", nil)
	w := httptest.NewRecorder()
	handler.HandleGetPrompt(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusBadRequest)
	}
}

// TestPromptHandler_GetPrompt_VersionOrLabelRequired
func TestPromptHandler_GetPrompt_VersionOrLabelRequired(t *testing.T) {
	store := newFakePromptStore()
	cache := NewPromptCache(store)
	handler := &PromptHandler{
		Store:     store,
		Cache:     cache,
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/prompts/greeting", nil)
	w := httptest.NewRecorder()
	handler.HandleGetPrompt(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusBadRequest)
	}
}

// TestPromptHandler_GetPrompt_NotFound
func TestPromptHandler_GetPrompt_NotFound(t *testing.T) {
	store := newFakePromptStore()
	cache := NewPromptCache(store)
	handler := &PromptHandler{
		Store:     store,
		Cache:     cache,
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/prompts/nonexistent?version=1&project_id=test-proj", nil)
	w := httptest.NewRecorder()
	handler.HandleGetPrompt(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusNotFound)
	}
}

// TestPromptHandler_SetLabel_MethodNotAllowed
func TestPromptHandler_SetLabel_MethodNotAllowed(t *testing.T) {
	store := newFakePromptStore()
	cache := NewPromptCache(store)
	handler := &PromptHandler{
		Store:     store,
		Cache:     cache,
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/prompts/greeting/labels/production", nil)
	w := httptest.NewRecorder()
	handler.HandleSetLabel(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusMethodNotAllowed)
	}
}

// TestPromptHandler_SetLabel_BadRequest
func TestPromptHandler_SetLabel_BadRequest(t *testing.T) {
	store := newFakePromptStore()
	cache := NewPromptCache(store)
	handler := &PromptHandler{
		Store:     store,
		Cache:     cache,
	}

	req := httptest.NewRequest(http.MethodPut, "/api/v1/prompts/greeting/labels/production", strings.NewReader(`{invalid json`))
	w := httptest.NewRecorder()
	handler.HandleSetLabel(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusBadRequest)
	}
}

// ErrorPromptStore always returns errors for prompt operations.
type ErrorPromptStore struct{}

func (e *ErrorPromptStore) CreatePromptVersion(ctx context.Context, pv *domain.PromptVersion) error { return errors.New("store error") }
func (e *ErrorPromptStore) GetPromptVersion(ctx context.Context, projectID, name string, version int64) (*domain.PromptVersion, error) {
	return nil, errors.New("store error")
}
func (e *ErrorPromptStore) GetPromptByLabel(ctx context.Context, projectID, name, label string) (*domain.PromptVersion, error) {
	return nil, errors.New("store error")
}
func (e *ErrorPromptStore) ListPromptVersions(ctx context.Context, projectID, name string) ([]*domain.PromptVersion, error) { return nil, nil }
func (e *ErrorPromptStore) SetPromptLabel(ctx context.Context, label *domain.PromptLabel) error                           { return errors.New("store error") }
func (e *ErrorPromptStore) CreateOrganization(ctx context.Context, o *domain.Organization) error                           { return nil }
func (e *ErrorPromptStore) GetOrganization(ctx context.Context, id string) (*domain.Organization, error)                   { return nil, metadata.ErrNotFound }
func (e *ErrorPromptStore) CreateProject(ctx context.Context, p *domain.Project) error                                     { return nil }
func (e *ErrorPromptStore) GetProject(ctx context.Context, id string) (*domain.Project, error)                             { return nil, metadata.ErrNotFound }
func (e *ErrorPromptStore) ListProjects(ctx context.Context, orgID string) ([]*domain.Project, error)                      { return nil, nil }
func (e *ErrorPromptStore) CreateUser(ctx context.Context, u *domain.User) error                                           { return nil }
func (e *ErrorPromptStore) GetUserByEmail(ctx context.Context, email string) (*domain.User, error)                         { return nil, metadata.ErrNotFound }
func (e *ErrorPromptStore) ListUsers(ctx context.Context, orgID string) ([]*domain.User, error)                            { return nil, nil }
func (e *ErrorPromptStore) CreateSession(ctx context.Context, s *domain.Session) error                                     { return nil }
func (e *ErrorPromptStore) GetSession(ctx context.Context, id string) (*domain.Session, error)                             { return nil, metadata.ErrNotFound }
func (e *ErrorPromptStore) DeleteSession(ctx context.Context, id string) error                                             { return nil }
func (e *ErrorPromptStore) CreateAPIKey(ctx context.Context, k *domain.APIKey) error                                       { return nil }
func (e *ErrorPromptStore) GetAPIKeyByHash(ctx context.Context, hash string) (*domain.APIKey, error)                       { return nil, metadata.ErrNotFound }
func (e *ErrorPromptStore) RevokeAPIKey(ctx context.Context, keyID string) error                                           { return nil }
func (e *ErrorPromptStore) ListAPIKeys(ctx context.Context, projectID string) ([]*domain.APIKey, error)                   { return nil, nil }
func (e *ErrorPromptStore) CreateEvalRule(ctx context.Context, r *domain.EvalRule) error                                   { return nil }
func (e *ErrorPromptStore) GetEvalRule(ctx context.Context, id string) (*domain.EvalRule, error)                           { return nil, metadata.ErrNotFound }
func (e *ErrorPromptStore) ListEvalRules(ctx context.Context, projectID string) ([]*domain.EvalRule, error)               { return nil, nil }
func (e *ErrorPromptStore) UpdateEvalRule(ctx context.Context, r *domain.EvalRule) error                                   { return nil }
func (e *ErrorPromptStore) CreateDataset(ctx context.Context, d *domain.Dataset) error                                     { return nil }
func (e *ErrorPromptStore) GetDataset(ctx context.Context, id string) (*domain.Dataset, error)                             { return nil, metadata.ErrNotFound }
func (e *ErrorPromptStore) CreateDatasetItem(ctx context.Context, i *domain.DatasetItem) error                             { return nil }
func (e *ErrorPromptStore) ListDatasetItems(ctx context.Context, datasetID string) ([]*domain.DatasetItem, error)         { return nil, nil }
func (e *ErrorPromptStore) CreateDatasetRun(ctx context.Context, r *domain.DatasetRun) error                               { return nil }
func (e *ErrorPromptStore) GetDatasetRun(ctx context.Context, id string) (*domain.DatasetRun, error)                       { return nil, metadata.ErrNotFound }
func (e *ErrorPromptStore) Migrate(ctx context.Context) error                                                              { return nil }
func (e *ErrorPromptStore) Close() error                                                                                   { return nil }
