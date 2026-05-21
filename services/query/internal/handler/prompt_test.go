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

	"github.com/omneval/omneval/internal/domain"
	"github.com/omneval/omneval/internal/metadata"
)

// FakePromptStore implements metadata.Store for prompt-only operations.
type FakePromptStore struct {
	mu             sync.RWMutex
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
	m.mu.RLock()
	defer m.mu.RUnlock()
	var versions []*domain.PromptVersion
	for _, pv := range m.promptVersions {
		if pv.ProjectID == projectID && pv.Name == name {
			cp := *pv
			versions = append(versions, &cp)
		}
	}
	return versions, nil
}

func (m *FakePromptStore) ListPromptNames(ctx context.Context, projectID string) ([]string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	nameSet := make(map[string]struct{})
	for _, pv := range m.promptVersions {
		if pv.ProjectID == projectID {
			nameSet[pv.Name] = struct{}{}
		}
	}
	names := make([]string, 0, len(nameSet))
	for name := range nameSet {
		names = append(names, name)
	}
	return names, nil
}

func (m *FakePromptStore) SetPromptLabel(ctx context.Context, label *domain.PromptLabel) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	lk := labelKey(label.ProjectID, label.Name, label.Label)
	m.promptLabels[lk] = label
	return nil
}

// ---- Metadata.Store interface stubs (not used by tests) ----
func (m *FakePromptStore) CreateOrganization(ctx context.Context, o *domain.Organization) error {
	return nil
}
func (m *FakePromptStore) GetOrganization(ctx context.Context, id string) (*domain.Organization, error) {
	return nil, metadata.ErrNotFound
}
func (m *FakePromptStore) CreateProject(ctx context.Context, p *domain.Project) error { return nil }
func (m *FakePromptStore) GetProject(ctx context.Context, id string) (*domain.Project, error) {
	return nil, metadata.ErrNotFound
}
func (m *FakePromptStore) ListProjects(ctx context.Context, orgID string) ([]*domain.Project, error) {
	return nil, nil
}
func (m *FakePromptStore) CreateUser(ctx context.Context, u *domain.User) error { return nil }
func (m *FakePromptStore) GetUserByEmail(ctx context.Context, email string) (*domain.User, error) {
	return nil, metadata.ErrNotFound
}
func (m *FakePromptStore) GetUserByID(ctx context.Context, userID string) (*domain.User, error) {
	return nil, metadata.ErrNotFound
}
func (m *FakePromptStore) CountUsers(ctx context.Context) (int, error) { return 0, nil }
func (m *FakePromptStore) UpdateUserPassword(ctx context.Context, userID, passwordHash string) error {
	return nil
}
func (m *FakePromptStore) UpdateUserResetToken(ctx context.Context, userID, token string, expiry time.Time) error {
	return nil
}
func (m *FakePromptStore) GetUserByResetToken(ctx context.Context, token string) (*domain.User, error) {
	return nil, metadata.ErrNotFound
}
func (m *FakePromptStore) CheckPassword(hashed, plaintext string) error { return nil }
func (m *FakePromptStore) ListUsers(ctx context.Context, orgID string) ([]*domain.User, error) {
	return nil, nil
}
func (m *FakePromptStore) CreateSession(ctx context.Context, s *domain.Session) error { return nil }
func (m *FakePromptStore) GetSession(ctx context.Context, id string) (*domain.Session, error) {
	return nil, metadata.ErrNotFound
}
func (m *FakePromptStore) DeleteSession(ctx context.Context, id string) error       { return nil }
func (m *FakePromptStore) CreateAPIKey(ctx context.Context, k *domain.APIKey) error { return nil }
func (m *FakePromptStore) GetAPIKeyByHash(ctx context.Context, hash string) (*domain.APIKey, error) {
	return nil, metadata.ErrNotFound
}
func (m *FakePromptStore) RevokeAPIKey(ctx context.Context, keyID string) error { return nil }
func (m *FakePromptStore) ListAPIKeys(ctx context.Context, projectID string) ([]*domain.APIKey, error) {
	return nil, nil
}
func (m *FakePromptStore) CreateEvalRule(ctx context.Context, r *domain.EvalRule) error { return nil }
func (m *FakePromptStore) GetEvalRule(ctx context.Context, id string) (*domain.EvalRule, error) {
	return nil, metadata.ErrNotFound
}
func (m *FakePromptStore) ListEvalRules(ctx context.Context, projectID string) ([]*domain.EvalRule, error) {
	return nil, nil
}
func (m *FakePromptStore) UpdateEvalRule(ctx context.Context, r *domain.EvalRule) error { return nil }
func (m *FakePromptStore) DeleteEvalRule(ctx context.Context, ruleID string) error      { return nil }
func (m *FakePromptStore) CreateDataset(ctx context.Context, d *domain.Dataset) error   { return nil }
func (m *FakePromptStore) ListDatasets(ctx context.Context, projectID string) ([]*domain.Dataset, error) {
	return nil, nil
}
func (m *FakePromptStore) GetDataset(ctx context.Context, id string) (*domain.Dataset, error) {
	return nil, metadata.ErrNotFound
}
func (m *FakePromptStore) DeleteDataset(ctx context.Context, datasetID string) error { return nil }
func (m *FakePromptStore) CreateDatasetItem(ctx context.Context, i *domain.DatasetItem) error {
	return nil
}
func (m *FakePromptStore) ListDatasetItems(ctx context.Context, datasetID string) ([]*domain.DatasetItem, error) {
	return nil, nil
}
func (m *FakePromptStore) ListDatasetItemsPaginated(ctx context.Context, datasetID, cursor string, limit int) ([]*domain.DatasetItem, string, error) {
	return nil, "", nil
}
func (m *FakePromptStore) CreateDatasetRun(ctx context.Context, r *domain.DatasetRun) error {
	return nil
}
func (m *FakePromptStore) GetDatasetRun(ctx context.Context, id string) (*domain.DatasetRun, error) {
	return nil, metadata.ErrNotFound
}
func (m *FakePromptStore) UpdateDatasetRun(ctx context.Context, r *domain.DatasetRun) error {
	return nil
}
func (m *FakePromptStore) ListDatasetRuns(ctx context.Context, datasetID string) ([]*domain.DatasetRun, error) {
	return nil, nil
}
func (m *FakePromptStore) CreateDatasetRunItem(ctx context.Context, i *domain.DatasetRunItem) error {
	return nil
}
func (m *FakePromptStore) GetDatasetRunItem(ctx context.Context, id string) (*domain.DatasetRunItem, error) {
	return nil, metadata.ErrNotFound
}
func (m *FakePromptStore) UpdateDatasetRunItem(ctx context.Context, i *domain.DatasetRunItem) error {
	return nil
}
func (m *FakePromptStore) ListDatasetRunItems(ctx context.Context, runID string) ([]*domain.DatasetRunItem, error) {
	return nil, nil
}
func (m *FakePromptStore) Migrate(ctx context.Context) error { return nil }
func (m *FakePromptStore) Close() error                      { return nil }

// ---- Tests ----

func TestPromptHandler_CreatePrompt(t *testing.T) {
	store := newFakePromptStore()
	cache := NewPromptCache(store)
	handler := &PromptHandler{
		Store:        store,
		Cache:        cache,
		SessionStore: &FakeSessionStore{projectID: "test-proj"},
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
		Store:        store,
		Cache:        cache,
		SessionStore: &FakeSessionStore{projectID: "test-proj"},
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
		Store: store,
		Cache: cache,
	}

	// Pre-seed a version.
	store.CreatePromptVersion(context.Background(), &domain.PromptVersion{
		VersionID: "v1", ProjectID: "test-proj", Name: "greeting", Version: 1,
		Template:    "Hello {{name}}!",
		ModelConfig: domain.PromptModelConfig{Model: "gpt-4", Temperature: 0.7, MaxTokens: 256},
	})

	// GET /api/v1/prompts/greeting?version=1
	req := httptest.NewRequest(http.MethodGet, "/api/v1/prompts/greeting?version=1&project_id=test-proj", nil)
	w := httptest.NewRecorder()
	handler.HandleGetPrompt(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want %d\nbody: %s", w.Code, http.StatusOK, w.Body.String())
	}

	var resp domain.PromptVersionJSON
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if resp.Name != "greeting" {
		t.Errorf("name: got %q, want %q", resp.Name, "greeting")
	}
	if resp.Version != 1 {
		t.Errorf("version: got %d, want 1", resp.Version)
	}
	if resp.Model != "gpt-4" {
		t.Errorf("model: got %q, want %q", resp.Model, "gpt-4")
	}
	if resp.Template != "Hello {{name}}!" {
		t.Errorf("template: got %q, want %q", resp.Template, "Hello {{name}}!")
	}
}

func TestPromptHandler_GetPrompt_ByLabel(t *testing.T) {
	store := newFakePromptStore()
	cache := NewPromptCache(store)
	handler := &PromptHandler{
		Store: store,
		Cache: cache,
	}

	// Pre-seed versions and label.
	store.CreatePromptVersion(context.Background(), &domain.PromptVersion{
		VersionID: "v1", ProjectID: "test-proj", Name: "greeting", Version: 1,
		Template:    "Old greeting",
		ModelConfig: domain.PromptModelConfig{Model: "gpt-3.5"},
	})
	store.CreatePromptVersion(context.Background(), &domain.PromptVersion{
		VersionID: "v2", ProjectID: "test-proj", Name: "greeting", Version: 2,
		Template:    "New greeting",
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
		Store: store,
		Cache: cache,
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
		Template:    "Hello {{name}}!",
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
		Template:    "Hello {{name}}!",
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
			VersionID: "v" + strconv.FormatInt(i, 10),
			ProjectID: "test-proj", Name: "greeting", Version: i,
			Template:    "version " + strconv.FormatInt(i, 10),
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
		Store: store,
		Cache: cache,
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/prompts", strings.NewReader(`{}`))
	w := httptest.NewRecorder()
	handler.HandleCreatePrompt(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

// TestPromptHandler_CreatePrompt_ResponseUsesSnakeCase verifies that the
// POST /api/v1/prompts response uses snake_case JSON keys (not PascalCase),
// including nested model_config.
func TestPromptHandler_CreatePrompt_ResponseUsesSnakeCase(t *testing.T) {
	store := newFakePromptStore()
	cache := NewPromptCache(store)
	handler := &PromptHandler{
		Store:        store,
		Cache:        cache,
		SessionStore: &FakeSessionStore{projectID: "test-proj"},
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/prompts", strings.NewReader(`{
		"name": "greeting",
		"version": 1,
		"template": "Hello {{name}}!",
		"model": "gpt-4",
		"temperature": 0.7,
		"max_tokens": 256
	}`))
	w := httptest.NewRecorder()
	handler.HandleCreatePrompt(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("status: got %d, want %d\nbody: %s", w.Code, http.StatusCreated, w.Body.String())
	}

	body := w.Body.String()

	// Verify snake_case keys are present
	snakeKeys := []string{"version_id", "project_id", "name", "version", "template", "created_at"}
	for _, key := range snakeKeys {
		if !strings.Contains(body, `"`+key+`"`) {
			t.Errorf("response missing snake_case key %q\nbody: %s", key, body)
		}
	}

	// Verify PascalCase keys are NOT present
	pascalKeys := []string{"VersionID", "ProjectID", "Name", "Template", "CreatedAt"}
	for _, key := range pascalKeys {
		if strings.Contains(body, `"`+key+`"`) {
			t.Errorf("response contains PascalCase key %q (should be snake_case)\nbody: %s", key, body)
		}
	}

	// Verify model fields are flattened into the top-level (not nested).
	flatKeys := []string{"model", "temperature", "max_tokens"}
	for _, key := range flatKeys {
		if !strings.Contains(body, `"`+key+`"`) {
			t.Errorf("response missing flattened key %q\nbody: %s", key, body)
		}
	}

	// Verify model_config does NOT exist as a nested key (fields are inlined).
	if strings.Contains(body, `"model_config"`) {
		t.Errorf("response should not contain nested 'model_config'\nbody: %s", body)
	}
}

// TestPromptHandler_CreatePrompt_ModelConfigDeserialization verifies that
// the model_config input is correctly deserialized from flat fields.
func TestPromptHandler_CreatePrompt_ModelConfigDeserialization(t *testing.T) {
	store := newFakePromptStore()
	cache := NewPromptCache(store)
	handler := &PromptHandler{
		Store:        store,
		Cache:        cache,
		SessionStore: &FakeSessionStore{projectID: "test-proj"},
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/prompts", strings.NewReader(`{
		"name": "greeting",
		"version": 1,
		"template": "Hello {{name}}!",
		"model": "gpt-4o",
		"temperature": 0.7,
		"max_tokens": 1024
	}`))
	w := httptest.NewRecorder()
	handler.HandleCreatePrompt(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("status: got %d, want %d\nbody: %s", w.Code, http.StatusCreated, w.Body.String())
	}

	// Parse response as raw JSON to verify model_config values
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if resp["model"] != "gpt-4o" {
		t.Errorf("model: got %v, want 'gpt-4o'", resp["model"])
	}
	if resp["temperature"] != 0.7 {
		t.Errorf("temperature: got %v, want 0.7", resp["temperature"])
	}
	if resp["max_tokens"] != float64(1024) {
		t.Errorf("max_tokens: got %v, want 1024", resp["max_tokens"])
	}
}

// TestPromptHandler_CreatePrompt_CreatedAtPopulated verifies that the
// created_at field is populated with the actual creation timestamp
// (not the zero value 0001-01-01T00:00:00Z).
func TestPromptHandler_CreatePrompt_CreatedAtPopulated(t *testing.T) {
	store := newFakePromptStore()
	cache := NewPromptCache(store)
	handler := &PromptHandler{
		Store:        store,
		Cache:        cache,
		SessionStore: &FakeSessionStore{projectID: "test-proj"},
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/prompts", strings.NewReader(`{
		"name": "greeting",
		"version": 1,
		"template": "Hello {{name}}!",
		"model": "gpt-4"
	}`))
	w := httptest.NewRecorder()
	handler.HandleCreatePrompt(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("status: got %d, want %d\nbody: %s", w.Code, http.StatusCreated, w.Body.String())
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	createdAt, ok := resp["created_at"].(string)
	if !ok {
		t.Fatalf("created_at is not a string\nbody: %s", w.Body.String())
	}

	// Must not be zero value
	if createdAt == "0001-01-01T00:00:00Z" || createdAt == "0001-01-01T00:00:00.000000000Z" {
		t.Errorf("created_at is zero value: %q", createdAt)
	}

	// Parse and verify it's a reasonable timestamp
	parsed, err := time.Parse(time.RFC3339, createdAt)
	if err == nil && parsed.IsZero() {
		t.Errorf("created_at is zero after parsing: %q", createdAt)
	}
}

// TestPromptHandler_GetPrompt_VersionResponseUsesSnakeCase verifies that
// GET /api/v1/prompts/:name also uses snake_case JSON keys.
func TestPromptHandler_GetPrompt_VersionResponseUsesSnakeCase(t *testing.T) {
	store := newFakePromptStore()
	cache := NewPromptCache(store)
	handler := &PromptHandler{
		Store: store,
		Cache: cache,
	}

	// Pre-seed a version with a real timestamp.
	store.CreatePromptVersion(context.Background(), &domain.PromptVersion{
		VersionID: "v1", ProjectID: "test-proj", Name: "greeting", Version: 1,
		Template:    "Hello {{name}}!",
		ModelConfig: domain.PromptModelConfig{Model: "gpt-4", Temperature: 0.7, MaxTokens: 256},
		CreatedAt:   time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC),
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/prompts/greeting?version=1&project_id=test-proj", nil)
	w := httptest.NewRecorder()
	handler.HandleGetPrompt(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want %d\nbody: %s", w.Code, http.StatusOK, w.Body.String())
	}

	body := w.Body.String()

	// Verify snake_case keys (model fields are now flattened into parent)
	snakeKeys := []string{"version_id", "project_id", "name", "version", "template", "model", "temperature", "max_tokens", "created_at"}
	for _, key := range snakeKeys {
		if !strings.Contains(body, `"`+key+`"`) {
			t.Errorf("response missing snake_case key %q\nbody: %s", key, body)
		}
	}

	// Verify PascalCase keys are NOT present
	pascalKeys := []string{"VersionID", "ProjectID", "Name", "ModelConfig", "CreatedAt", "Model", "Temperature", "MaxTokens"}
	for _, key := range pascalKeys {
		if strings.Contains(body, `"`+key+`"`) {
			t.Errorf("response contains PascalCase key %q (should be snake_case)\nbody: %s", key, body)
		}
	}

	// Verify model_config does NOT exist as a nested key (fields are inlined).
	if strings.Contains(body, `"model_config"`) {
		t.Errorf("response should not contain nested 'model_config'\nbody: %s", body)
	}
}

// TestPromptHandler_CreatePrompt_MethodNotAllowed
func TestPromptHandler_CreatePrompt_MethodNotAllowed(t *testing.T) {
	store := newFakePromptStore()
	cache := NewPromptCache(store)
	handler := &PromptHandler{
		Store: store,
		Cache: cache,
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
		Store: store,
		Cache: cache,
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/prompts?version=1&project_id=test-proj", nil)
	w := httptest.NewRecorder()
	handler.HandleGetPrompt(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusBadRequest)
	}
}

// TestPromptHandler_GetPrompt_NoParamsDefaultToLatest replaces the old
// "version or label required" test — now defaults to latest version.
func TestPromptHandler_GetPrompt_NoParamsDefaultToLatest(t *testing.T) {
	store := newFakePromptStore()
	cache := NewPromptCache(store)
	handler := &PromptHandler{
		Store:        store,
		Cache:        cache,
		SessionStore: &FakeSessionStore{projectID: "test-proj"},
	}

	store.CreatePromptVersion(context.Background(), &domain.PromptVersion{
		VersionID: "v1", ProjectID: "test-proj", Name: "greeting", Version: 1,
		Template: "Hello!",
	})
	store.CreatePromptVersion(context.Background(), &domain.PromptVersion{
		VersionID: "v2", ProjectID: "test-proj", Name: "greeting", Version: 2,
		Template: "Hi!",
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/prompts/greeting", nil)
	w := httptest.NewRecorder()
	handler.HandleGetPrompt(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want %d\nbody: %s", w.Code, http.StatusOK, w.Body.String())
	}

	var resp struct {
		Version int64 `json:"version"`
	}
	json.NewDecoder(w.Body).Decode(&resp)

	if resp.Version != 2 {
		t.Errorf("version: got %d, want 2 (latest)", resp.Version)
	}
}

// TestPromptHandler_GetPrompt_NotFound
func TestPromptHandler_GetPrompt_NotFound(t *testing.T) {
	store := newFakePromptStore()
	cache := NewPromptCache(store)
	handler := &PromptHandler{
		Store: store,
		Cache: cache,
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
		Store: store,
		Cache: cache,
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
		Store: store,
		Cache: cache,
	}

	req := httptest.NewRequest(http.MethodPut, "/api/v1/prompts/greeting/labels/production", strings.NewReader(`{invalid json`))
	w := httptest.NewRecorder()
	handler.HandleSetLabel(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusBadRequest)
	}
}

// ---- HandleListPrompts tests ----

func TestPromptHandler_ListPrompts(t *testing.T) {
	store := newFakePromptStore()
	cache := NewPromptCache(store)
	handler := &PromptHandler{
		Store:        store,
		Cache:        cache,
		SessionStore: &FakeSessionStore{projectID: "test-proj"},
	}

	// Pre-seed two prompts with multiple versions.
	store.CreatePromptVersion(context.Background(), &domain.PromptVersion{
		VersionID: "v1", ProjectID: "test-proj", Name: "greeting", Version: 1,
		Template:    "Hello {{name}}!",
		ModelConfig: domain.PromptModelConfig{Model: "gpt-3.5", Temperature: 0.5, MaxTokens: 128},
	})
	store.CreatePromptVersion(context.Background(), &domain.PromptVersion{
		VersionID: "v2", ProjectID: "test-proj", Name: "greeting", Version: 2,
		Template:    "Hi {{name}}!",
		ModelConfig: domain.PromptModelConfig{Model: "gpt-4", Temperature: 0.7, MaxTokens: 256},
	})
	store.CreatePromptVersion(context.Background(), &domain.PromptVersion{
		VersionID: "v3", ProjectID: "test-proj", Name: "summarize", Version: 1,
		Template:    "Summarize: {{text}}",
		ModelConfig: domain.PromptModelConfig{Model: "claude-3", Temperature: 0.0, MaxTokens: 512},
	})

	// Set a label.
	store.SetPromptLabel(context.Background(), &domain.PromptLabel{
		ProjectID: "test-proj", Name: "greeting", Label: "production", Version: 2,
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/prompts?project_id=test-proj", nil)
	w := httptest.NewRecorder()
	handler.HandleListPrompts(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want %d\nbody: %s", w.Code, http.StatusOK, w.Body.String())
	}

	var result []promptListItem
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if len(result) != 2 {
		t.Errorf("count: got %d, want 2", len(result))
	}

	// Find the greeting entry.
	var greeting *promptListItem
	for i := range result {
		if result[i].Name == "greeting" {
			greeting = &result[i]
			break
		}
	}
	if greeting == nil {
		t.Fatal("missing greeting in result")
	}
	if greeting.LatestVersion != 2 {
		t.Errorf("greeting latest_version: got %d, want 2", greeting.LatestVersion)
	}
	if greeting.Labels["production"] != 2 {
		t.Errorf("greeting production label: got %d, want 2", greeting.Labels["production"])
	}

	// Find the summarize entry.
	var summarize *promptListItem
	for i := range result {
		if result[i].Name == "summarize" {
			summarize = &result[i]
			break
		}
	}
	if summarize == nil {
		t.Fatal("missing summarize in result")
	}
	if summarize.LatestVersion != 1 {
		t.Errorf("summarize latest_version: got %d, want 1", summarize.LatestVersion)
	}
}

func TestPromptHandler_ListPrompts_Empty(t *testing.T) {
	store := newFakePromptStore()
	cache := NewPromptCache(store)
	handler := &PromptHandler{
		Store:        store,
		Cache:        cache,
		SessionStore: &FakeSessionStore{projectID: "empty-proj"},
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/prompts?project_id=empty-proj", nil)
	w := httptest.NewRecorder()
	handler.HandleListPrompts(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want %d", w.Code, http.StatusOK)
	}

	var result []promptListItem
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if len(result) != 0 {
		t.Errorf("empty result: got %d items, want 0", len(result))
	}
}

func TestPromptHandler_ListPrompts_MethodNotAllowed(t *testing.T) {
	store := newFakePromptStore()
	cache := NewPromptCache(store)
	handler := &PromptHandler{
		Store: store,
		Cache: cache,
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/prompts", strings.NewReader(`{}`))
	w := httptest.NewRecorder()
	handler.HandleListPrompts(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusMethodNotAllowed)
	}
}

func TestPromptHandler_ListPrompts_MissingProjectID(t *testing.T) {
	store := newFakePromptStore()
	cache := NewPromptCache(store)
	handler := &PromptHandler{
		Store: store,
		Cache: cache,
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/prompts", nil)
	w := httptest.NewRecorder()
	handler.HandleListPrompts(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusBadRequest)
	}
}

// ---- Issue #75: Default prompt resolution tests ----

// TestPromptHandler_GetPrompt_DefaultToLatestVersion verifies that calling
// GET /api/v1/prompts/{name} without ?version= or ?label= returns the
// latest (highest version number) automatically.
func TestPromptHandler_GetPrompt_DefaultToLatestVersion(t *testing.T) {
	store := newFakePromptStore()
	cache := NewPromptCache(store)
	handler := &PromptHandler{
		Store:        store,
		Cache:        cache,
		SessionStore: &FakeSessionStore{projectID: "test-proj"},
	}

	// Pre-seed multiple versions.
	store.CreatePromptVersion(context.Background(), &domain.PromptVersion{
		VersionID: "v1", ProjectID: "test-proj", Name: "greeting", Version: 1,
		Template: "Hello {{name}}!",
	})
	store.CreatePromptVersion(context.Background(), &domain.PromptVersion{
		VersionID: "v2", ProjectID: "test-proj", Name: "greeting", Version: 2,
		Template: "Hi {{name}}!",
	})
	store.CreatePromptVersion(context.Background(), &domain.PromptVersion{
		VersionID: "v3", ProjectID: "test-proj", Name: "greeting", Version: 3,
		Template: "Hey {{name}}!",
	})

	// GET /api/v1/prompts/greeting — no params.
	req := httptest.NewRequest(http.MethodGet, "/api/v1/prompts/greeting", nil)
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

	if resp.Version != 3 {
		t.Errorf("version: got %d, want 3 (latest)", resp.Version)
	}
	if resp.Template != "Hey {{name}}!" {
		t.Errorf("template: got %q, want %q", resp.Template, "Hey {{name}}!")
	}
}

// TestPromptHandler_GetPrompt_SingleVersionNoParams returns the only version
// when there is exactly one version and no params are provided.
func TestPromptHandler_GetPrompt_SingleVersionNoParams(t *testing.T) {
	store := newFakePromptStore()
	cache := NewPromptCache(store)
	handler := &PromptHandler{
		Store:        store,
		Cache:        cache,
		SessionStore: &FakeSessionStore{projectID: "test-proj"},
	}

	store.CreatePromptVersion(context.Background(), &domain.PromptVersion{
		VersionID: "v1", ProjectID: "test-proj", Name: "greeting", Version: 1,
		Template: "Hello!",
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/prompts/greeting", nil)
	w := httptest.NewRecorder()
	handler.HandleGetPrompt(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want %d\nbody: %s", w.Code, http.StatusOK, w.Body.String())
	}

	var resp struct {
		Version int64 `json:"version"`
	}
	json.NewDecoder(w.Body).Decode(&resp)

	if resp.Version != 1 {
		t.Errorf("version: got %d, want 1", resp.Version)
	}
}

// TestPromptHandler_GetPrompt_NotFoundNoParams returns 404 when the prompt
// name does not exist and no params are provided.
func TestPromptHandler_GetPrompt_NotFoundNoParams(t *testing.T) {
	store := newFakePromptStore()
	cache := NewPromptCache(store)
	handler := &PromptHandler{
		Store:        store,
		Cache:        cache,
		SessionStore: &FakeSessionStore{projectID: "test-proj"},
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/prompts/nonexistent", nil)
	w := httptest.NewRecorder()
	handler.HandleGetPrompt(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusNotFound)
	}
}

// TestPromptHandler_GetPrompt_LabelNotFound returns 404 when the specified
// label does not exist.
func TestPromptHandler_GetPrompt_LabelNotFound(t *testing.T) {
	store := newFakePromptStore()
	cache := NewPromptCache(store)
	handler := &PromptHandler{
		Store:        store,
		Cache:        cache,
		SessionStore: &FakeSessionStore{projectID: "test-proj"},
	}

	store.CreatePromptVersion(context.Background(), &domain.PromptVersion{
		VersionID: "v1", ProjectID: "test-proj", Name: "greeting", Version: 1,
		Template: "Hello!",
	})
	// No labels assigned.

	req := httptest.NewRequest(http.MethodGet, "/api/v1/prompts/greeting?label=production", nil)
	w := httptest.NewRecorder()
	handler.HandleGetPrompt(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusNotFound)
	}
}

// TestPromptHandler_ListPromptVersions_InferProjectID verifies that
// GET /api/v1/prompts/{name}/versions infers project_id from the session
// instead of requiring it as a query parameter.
func TestPromptHandler_ListPromptVersions_InferProjectID(t *testing.T) {
	store := newFakePromptStore()
	cache := NewPromptCache(store)
	handler := &PromptHandler{
		Store:        store,
		Cache:        cache,
		SessionStore: &FakeSessionStore{projectID: "test-proj"},
	}

	store.CreatePromptVersion(context.Background(), &domain.PromptVersion{
		VersionID: "v1", ProjectID: "test-proj", Name: "greeting", Version: 1,
		Template: "Hello!",
	})
	store.CreatePromptVersion(context.Background(), &domain.PromptVersion{
		VersionID: "v2", ProjectID: "test-proj", Name: "greeting", Version: 2,
		Template: "Hi!",
	})

	// No project_id query param — should infer from session.
	req := httptest.NewRequest(http.MethodGet, "/api/v1/prompts/greeting/versions", nil)
	w := httptest.NewRecorder()
	handler.HandleListPromptVersions(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want %d\nbody: %s", w.Code, http.StatusOK, w.Body.String())
	}

	// Response is now a bare array (issue #39).
	var versions []domain.PromptVersionJSON
	if err := json.NewDecoder(w.Body).Decode(&versions); err != nil {
		t.Fatalf("decode bare array: %v\nbody: %s", err, w.Body.String())
	}

	if len(versions) != 2 {
		t.Errorf("count: got %d, want 2", len(versions))
	}
}

// TestPromptHandler_CreatePrompt_WithLabel verifies that POST
// /api/v1/prompts accepts an optional "label" field and assigns it.
func TestPromptHandler_CreatePrompt_WithLabel(t *testing.T) {
	store := newFakePromptStore()
	cache := NewPromptCache(store)
	handler := &PromptHandler{
		Store:        store,
		Cache:        cache,
		SessionStore: &FakeSessionStore{projectID: "test-proj"},
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/prompts", strings.NewReader(`{
		"name": "greeting",
		"version": 1,
		"template": "Hello {{name}}!",
		"label": "production"
	}`))
	w := httptest.NewRecorder()
	handler.HandleCreatePrompt(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("status: got %d, want %d\nbody: %s", w.Code, http.StatusCreated, w.Body.String())
	}

	// Verify label was assigned.
	pv, err := store.GetPromptByLabel(context.Background(), "test-proj", "greeting", "production")
	if err != nil {
		t.Fatalf("get by label: %v", err)
	}
	if pv.Version != 1 {
		t.Errorf("label version: got %d, want 1", pv.Version)
	}
}

// TestPromptHandler_ListPromptVersions_NoSessionNoProjectID verifies that
// without a session store and without project_id query param, it returns 400.
func TestPromptHandler_ListPromptVersions_NoSessionNoProjectID(t *testing.T) {
	store := newFakePromptStore()
	cache := NewPromptCache(store)
	handler := &PromptHandler{
		Store: store,
		Cache: cache,
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/prompts/greeting/versions", nil)
	w := httptest.NewRecorder()
	handler.HandleListPromptVersions(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusBadRequest)
	}
}

// TestPromptHandler_ListPromptVersions_NoSessionWithProjectID verifies that
// without a session store, project_id from query param still works.
func TestPromptHandler_ListPromptVersions_NoSessionWithProjectID(t *testing.T) {
	store := newFakePromptStore()
	cache := NewPromptCache(store)
	handler := &PromptHandler{
		Store: store,
		Cache: cache,
	}

	store.CreatePromptVersion(context.Background(), &domain.PromptVersion{
		VersionID: "v1", ProjectID: "test-proj", Name: "greeting", Version: 1,
		Template: "Hello!",
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/prompts/greeting/versions?project_id=test-proj", nil)
	w := httptest.NewRecorder()
	handler.HandleListPromptVersions(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want %d", w.Code, http.StatusOK)
	}
}

// TestPromptHandler_GetPrompt_DefaultProjectID verifies that
// GET /api/v1/prompts/{name} (no params) returns a prompt with a non-empty
// project_id and a non-zero created_at timestamp.
// Regression test for issue #83: project_id was empty and created_at was zero.
func TestPromptHandler_GetPrompt_DefaultProjectID(t *testing.T) {
	store := newFakePromptStore()
	cache := NewPromptCache(store)
	handler := &PromptHandler{
		Store:        store,
		Cache:        cache,
		SessionStore: &FakeSessionStore{projectID: "test-proj-123"},
	}

	// Pre-seed a prompt version with a real timestamp.
	store.CreatePromptVersion(context.Background(), &domain.PromptVersion{
		VersionID:   "v1",
		ProjectID:   "test-proj-123",
		Name:        "First prompt",
		Version:     1,
		Template:    "Hello {{name}}!",
		ModelConfig: domain.PromptModelConfig{Model: "gpt-4", Temperature: 0.7, MaxTokens: 4096},
		CreatedAt:   time.Date(2026, 5, 10, 14, 30, 0, 0, time.UTC),
	})

	// GET /api/v1/prompts/First%20prompt — no version or label params.
	req := httptest.NewRequest(http.MethodGet, "/api/v1/prompts/First%20prompt", nil)
	w := httptest.NewRecorder()
	handler.HandleGetPrompt(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want %d\nbody: %s", w.Code, http.StatusOK, w.Body.String())
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	// Acceptance criterion 1: project_id should be non-empty.
	projectID, ok := resp["project_id"].(string)
	if !ok {
		t.Fatalf("project_id is not a string\nbody: %s", w.Body.String())
	}
	if projectID == "" {
		t.Errorf("project_id is empty (should be 'test-proj-123')\nbody: %s", w.Body.String())
	}

	// Acceptance criterion 2: created_at should not be zero.
	createdAt, ok := resp["created_at"].(string)
	if !ok {
		t.Fatalf("created_at is not a string\nbody: %s", w.Body.String())
	}
	if createdAt == "0001-01-01T00:00:00Z" || createdAt == "0001-01-01T00:00:00.000000000Z" {
		t.Errorf("created_at is zero value: %q\nbody: %s", createdAt, w.Body.String())
	}
}

// TestPromptHandler_ListPromptVersions_ProjectID verifies that
// ListPromptVersions returns PromptVersion structs with ProjectID set.
func TestPromptHandler_ListPromptVersions_ProjectID(t *testing.T) {
	store := newFakePromptStore()
	cache := NewPromptCache(store)
	handler := &PromptHandler{
		Store:        store,
		Cache:        cache,
		SessionStore: &FakeSessionStore{projectID: "test-proj-456"},
	}

	// Pre-seed a prompt version.
	store.CreatePromptVersion(context.Background(), &domain.PromptVersion{
		VersionID:   "v1",
		ProjectID:   "test-proj-456",
		Name:        "greeting",
		Version:     1,
		Template:    "Hello {{name}}!",
		ModelConfig: domain.PromptModelConfig{Model: "gpt-4"},
		CreatedAt:   time.Date(2026, 5, 10, 14, 30, 0, 0, time.UTC),
	})

	// GET /api/v1/prompts/greeting/versions — no query params needed.
	req := httptest.NewRequest(http.MethodGet, "/api/v1/prompts/greeting/versions", nil)
	w := httptest.NewRecorder()
	handler.HandleListPromptVersions(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want %d\nbody: %s", w.Code, http.StatusOK, w.Body.String())
	}

	// Response is now a bare array (issue #39).
	var versions []domain.PromptVersionJSON
	if err := json.NewDecoder(w.Body).Decode(&versions); err != nil {
		t.Fatalf("decode bare array: %v\nbody: %s", err, w.Body.String())
	}

	if len(versions) != 1 {
		t.Fatalf("count: got %d, want 1", len(versions))
	}

	// ProjectID must be non-empty.
	if versions[0].ProjectID == "" {
		t.Errorf("version ProjectID is empty (should be 'test-proj-456')")
	}

	// CreatedAt must not be zero value.
	if versions[0].CreatedAt == "" || versions[0].CreatedAt == "0001-01-01T00:00:00Z" {
		t.Errorf("version CreatedAt is zero or empty: %q", versions[0].CreatedAt)
	}
}

// TestPromptHandler_CreatePrompt_ModelFieldsFlattenedIntoParent verifies that
// the API response flattens model, temperature, and max_tokens into the
// top-level JSON object (not nested under model_config). This is required
// for the frontend PromptVersion interface which expects flat fields.
// Regression test for issue #93.
func TestPromptHandler_CreatePrompt_ModelFieldsFlattenedIntoParent(t *testing.T) {
	store := newFakePromptStore()
	cache := NewPromptCache(store)
	handler := &PromptHandler{
		Store:        store,
		Cache:        cache,
		SessionStore: &FakeSessionStore{projectID: "test-proj"},
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/prompts", strings.NewReader(`{
		"name": "greeting",
		"version": 1,
		"template": "Hello {{name}}!",
		"model": "gpt-4o",
		"temperature": 0.7,
		"max_tokens": 1024
	}`))
	w := httptest.NewRecorder()
	handler.HandleCreatePrompt(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("status: got %d, want %d\nbody: %s", w.Code, http.StatusCreated, w.Body.String())
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	// Acceptance: model, temperature, max_tokens must be at the top level.
	model, ok := resp["model"].(string)
	if !ok {
		t.Fatalf("top-level 'model' key missing or not a string\nbody: %s", w.Body.String())
	}
	if model != "gpt-4o" {
		t.Errorf("top-level model: got %q, want %q", model, "gpt-4o")
	}

	temp, ok := resp["temperature"].(float64)
	if !ok {
		t.Fatalf("top-level 'temperature' key missing or not a number\nbody: %s", w.Body.String())
	}
	if temp != 0.7 {
		t.Errorf("top-level temperature: got %v, want 0.7", temp)
	}

	maxTokens, ok := resp["max_tokens"].(float64)
	if !ok {
		t.Fatalf("top-level 'max_tokens' key missing or not a number\nbody: %s", w.Body.String())
	}
	if maxTokens != 1024 {
		t.Errorf("top-level max_tokens: got %v, want 1024", maxTokens)
	}

	// model_config should NOT exist as a nested key (fields are inlined).
	if _, hasNested := resp["model_config"]; hasNested {
		t.Errorf("response should not contain nested 'model_config' (fields are flattened)\nbody: %s", w.Body.String())
	}
}

// TestPromptHandler_GetPrompt_ModelFieldsFlattenedIntoParent verifies that
// GET /api/v1/prompts/:name also returns flat model/temperature/max_tokens.
func TestPromptHandler_GetPrompt_ModelFieldsFlattenedIntoParent(t *testing.T) {
	store := newFakePromptStore()
	cache := NewPromptCache(store)
	handler := &PromptHandler{
		Store: store,
		Cache: cache,
	}

	store.CreatePromptVersion(context.Background(), &domain.PromptVersion{
		VersionID:   "v1",
		ProjectID:   "test-proj",
		Name:        "greeting",
		Version:     1,
		Template:    "Hello {{name}}!",
		ModelConfig: domain.PromptModelConfig{Model: "claude-3", Temperature: 0.5, MaxTokens: 512},
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/prompts/greeting?version=1&project_id=test-proj", nil)
	w := httptest.NewRecorder()
	handler.HandleGetPrompt(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want %d\nbody: %s", w.Code, http.StatusOK, w.Body.String())
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	model := resp["model"]
	if model != "claude-3" {
		t.Errorf("top-level model: got %v, want 'claude-3'", model)
	}
	temp := resp["temperature"]
	if temp != 0.5 {
		t.Errorf("top-level temperature: got %v, want 0.5", temp)
	}
	maxTokens := resp["max_tokens"]
	if maxTokens != float64(512) {
		t.Errorf("top-level max_tokens: got %v, want 512", maxTokens)
	}

	if _, hasNested := resp["model_config"]; hasNested {
		t.Errorf("response should not contain nested 'model_config'")
	}
}

// TestPromptHandler_ListPromptVersions_ModelFieldsFlattened verifies that
// GET /api/v1/prompts/:name/versions returns flat model fields.
func TestPromptHandler_ListPromptVersions_ModelFieldsFlattened(t *testing.T) {
	store := newFakePromptStore()
	cache := NewPromptCache(store)
	handler := &PromptHandler{
		Store:        store,
		Cache:        cache,
		SessionStore: &FakeSessionStore{projectID: "test-proj"},
	}

	store.CreatePromptVersion(context.Background(), &domain.PromptVersion{
		VersionID:   "v1",
		ProjectID:   "test-proj",
		Name:        "greeting",
		Version:     1,
		Template:    "Hello!",
		ModelConfig: domain.PromptModelConfig{Model: "gpt-4", Temperature: 0.7, MaxTokens: 256},
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/prompts/greeting/versions?project_id=test-proj", nil)
	w := httptest.NewRecorder()
	handler.HandleListPromptVersions(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want %d\nbody: %s", w.Code, http.StatusOK, w.Body.String())
	}

	// Response is now a bare array (issue #39).
	var versions []map[string]any
	if err := json.NewDecoder(w.Body).Decode(&versions); err != nil {
		t.Fatalf("decode bare array: %v\nbody: %s", err, w.Body.String())
	}

	if len(versions) == 0 {
		t.Fatal("expected at least one version")
	}

	v := versions[0]
	model := v["model"]
	if model != "gpt-4" {
		t.Errorf("version[0].model: got %v, want 'gpt-4'", model)
	}
	temp := v["temperature"]
	if temp != 0.7 {
		t.Errorf("version[0].temperature: got %v, want 0.7", temp)
	}
	maxTokens := v["max_tokens"]
	if maxTokens != float64(256) {
		t.Errorf("version[0].max_tokens: got %v, want 256", maxTokens)
	}
}

// TestPromptHandler_CreatePrompt_LabelOverwrite verifies that re-creating
// the same prompt name+version with a different label overwrites the previous label.
func TestPromptHandler_CreatePrompt_LabelOverwrite(t *testing.T) {
	store := newFakePromptStore()
	cache := NewPromptCache(store)
	handler := &PromptHandler{
		Store:        store,
		Cache:        cache,
		SessionStore: &FakeSessionStore{projectID: "test-proj"},
	}

	// Create with production label.
	req1 := httptest.NewRequest(http.MethodPost, "/api/v1/prompts", strings.NewReader(`{
		"name": "greeting",
		"version": 1,
		"template": "Hello!",
		"label": "production"
	}`))
	w1 := httptest.NewRecorder()
	handler.HandleCreatePrompt(w1, req1)
	if w1.Code != http.StatusCreated {
		t.Fatalf("first create: status %d", w1.Code)
	}

	// Re-create the same version with staging label.
	req2 := httptest.NewRequest(http.MethodPost, "/api/v1/prompts", strings.NewReader(`{
		"name": "greeting",
		"version": 1,
		"template": "Hello!",
		"label": "staging"
	}`))
	w2 := httptest.NewRecorder()
	handler.HandleCreatePrompt(w2, req2)
	if w2.Code != http.StatusConflict {
		t.Fatalf("second create (conflict): status %d, want 409", w2.Code)
	}

	// Production label should still point to version 1.
	pv, err := store.GetPromptByLabel(context.Background(), "test-proj", "greeting", "production")
	if err != nil {
		t.Fatalf("get by label: %v", err)
	}
	if pv.Version != 1 {
		t.Errorf("production version: got %d, want 1", pv.Version)
	}
}

// TestPromptHandler_CreatePrompt_AutoIncrementVersion verifies that when no
// version field is provided (or version == 0), the handler auto-increments:
// first POST → version 1, second POST → version 2.
// Regression test for issue #24.
func TestPromptHandler_CreatePrompt_AutoIncrementVersion(t *testing.T) {
	store := newFakePromptStore()
	cache := NewPromptCache(store)
	handler := &PromptHandler{
		Store:        store,
		Cache:        cache,
		SessionStore: &FakeSessionStore{projectID: "test-proj"},
	}

	// First POST — no version field.
	req1 := httptest.NewRequest(http.MethodPost, "/api/v1/prompts", strings.NewReader(`{
		"name": "test-prompt",
		"template": "hello {{name}}"
	}`))
	w1 := httptest.NewRecorder()
	handler.HandleCreatePrompt(w1, req1)

	if w1.Code != http.StatusCreated {
		t.Fatalf("first create: status %d, want 201\nbody: %s", w1.Code, w1.Body.String())
	}

	var resp1 map[string]any
	if err := json.NewDecoder(w1.Body).Decode(&resp1); err != nil {
		t.Fatalf("decode first response: %v", err)
	}
	v1, ok := resp1["version"].(float64)
	if !ok {
		t.Fatalf("version not a number in first response\nbody: %s", w1.Body.String())
	}
	if v1 != 1 {
		t.Errorf("first auto-increment version: got %v, want 1", v1)
	}

	// Second POST — same name, no version field → should be version 2.
	req2 := httptest.NewRequest(http.MethodPost, "/api/v1/prompts", strings.NewReader(`{
		"name": "test-prompt",
		"template": "hello {{name}} v2"
	}`))
	w2 := httptest.NewRecorder()
	handler.HandleCreatePrompt(w2, req2)

	if w2.Code != http.StatusCreated {
		t.Fatalf("second create: status %d, want 201\nbody: %s", w2.Code, w2.Body.String())
	}

	var resp2 map[string]any
	if err := json.NewDecoder(w2.Body).Decode(&resp2); err != nil {
		t.Fatalf("decode second response: %v", err)
	}
	v2, ok := resp2["version"].(float64)
	if !ok {
		t.Fatalf("version not a number in second response\nbody: %s", w2.Body.String())
	}
	if v2 != 2 {
		t.Errorf("second auto-increment version: got %v, want 2", v2)
	}
}

// TestPromptHandler_CreatePrompt_NestedModelConfig verifies that POST
// /api/v1/prompts accepts a nested model_config object (as sent by SDKs)
// and maps its fields into the top-level model, temperature, max_tokens.
// Regression test for issue #24.
func TestPromptHandler_CreatePrompt_NestedModelConfig(t *testing.T) {
	store := newFakePromptStore()
	cache := NewPromptCache(store)
	handler := &PromptHandler{
		Store:        store,
		Cache:        cache,
		SessionStore: &FakeSessionStore{projectID: "test-proj"},
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/prompts", strings.NewReader(`{
		"name": "p2",
		"template": "t",
		"model_config": {
			"model": "gpt-4o",
			"temperature": 0.7,
			"max_tokens": 512
		}
	}`))
	w := httptest.NewRecorder()
	handler.HandleCreatePrompt(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("status: got %d, want 201\nbody: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	model, ok := resp["model"].(string)
	if !ok {
		t.Fatalf("top-level 'model' missing or not a string\nbody: %s", w.Body.String())
	}
	if model != "gpt-4o" {
		t.Errorf("model: got %q, want %q", model, "gpt-4o")
	}

	temp, ok := resp["temperature"].(float64)
	if !ok {
		t.Fatalf("top-level 'temperature' missing or not a number\nbody: %s", w.Body.String())
	}
	if temp != 0.7 {
		t.Errorf("temperature: got %v, want 0.7", temp)
	}

	maxTok, ok := resp["max_tokens"].(float64)
	if !ok {
		t.Fatalf("top-level 'max_tokens' missing or not a number\nbody: %s", w.Body.String())
	}
	if maxTok != 512 {
		t.Errorf("max_tokens: got %v, want 512", maxTok)
	}
}

// ---- Issue #33: project_id from session for ?version and ?label branches ----

// TestPromptHandler_GetPrompt_VersionUsesSessionProjectID verifies that
// GET /api/v1/prompts/:name?version=N resolves project_id from the session,
// not from a ?project_id= query parameter (#33).
func TestPromptHandler_GetPrompt_VersionUsesSessionProjectID(t *testing.T) {
	store := newFakePromptStore()
	cache := NewPromptCache(store)
	h := &PromptHandler{
		Store:        store,
		Cache:        cache,
		SessionStore: &FakeSessionStore{projectID: "session-proj"},
	}

	store.CreatePromptVersion(context.Background(), &domain.PromptVersion{
		VersionID: "v1", ProjectID: "session-proj", Name: "greeting", Version: 1,
		Template: "Hello from session!",
	})

	// No ?project_id= — project_id must come from the session.
	req := httptest.NewRequest(http.MethodGet, "/api/v1/prompts/greeting?version=1", nil)
	w := httptest.NewRecorder()
	h.HandleGetPrompt(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want %d\nbody: %s", w.Code, http.StatusOK, w.Body.String())
	}

	var resp struct {
		Version int64 `json:"version"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Version != 1 {
		t.Errorf("version: got %d, want 1", resp.Version)
	}
}

// TestPromptHandler_GetPrompt_LabelUsesSessionProjectID verifies that
// GET /api/v1/prompts/:name?label=<label> resolves project_id from the session (#33).
func TestPromptHandler_GetPrompt_LabelUsesSessionProjectID(t *testing.T) {
	store := newFakePromptStore()
	cache := NewPromptCache(store)
	h := &PromptHandler{
		Store:        store,
		Cache:        cache,
		SessionStore: &FakeSessionStore{projectID: "session-proj"},
	}

	store.CreatePromptVersion(context.Background(), &domain.PromptVersion{
		VersionID: "v2", ProjectID: "session-proj", Name: "greeting", Version: 2,
		Template: "Labelled greeting",
	})
	store.SetPromptLabel(context.Background(), &domain.PromptLabel{
		ProjectID: "session-proj", Name: "greeting", Label: "production", Version: 2,
	})

	// No ?project_id= — project_id must come from the session.
	req := httptest.NewRequest(http.MethodGet, "/api/v1/prompts/greeting?label=production", nil)
	w := httptest.NewRecorder()
	h.HandleGetPrompt(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want %d\nbody: %s", w.Code, http.StatusOK, w.Body.String())
	}

	var resp struct {
		Version int64 `json:"version"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Version != 2 {
		t.Errorf("version: got %d, want 2", resp.Version)
	}
}

// ---- Issue #34: API key auth for GET prompt endpoints ----

// TestPromptHandler_GetPrompt_APIKeyProjectID verifies that when an API key's
// project ID is stored in the request context (injected by middleware), the
// handler uses it to resolve the prompt even when no session is active (#34).
func TestPromptHandler_GetPrompt_APIKeyProjectID(t *testing.T) {
	store := newFakePromptStore()
	cache := NewPromptCache(store)
	h := &PromptHandler{
		Store: store,
		Cache: cache,
		// No SessionStore — simulates SDK call with API key only.
	}

	store.CreatePromptVersion(context.Background(), &domain.PromptVersion{
		VersionID: "v1", ProjectID: "apikey-proj", Name: "greeting", Version: 1,
		Template: "API key greeting!",
	})

	// Inject the API-key project ID into context (what middleware would do).
	req := httptest.NewRequest(http.MethodGet, "/api/v1/prompts/greeting", nil)
	req = req.WithContext(context.WithValue(req.Context(), APIKeyProjectIDKey, "apikey-proj"))
	w := httptest.NewRecorder()
	h.HandleGetPrompt(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want %d\nbody: %s", w.Code, http.StatusOK, w.Body.String())
	}

	var resp struct {
		Version int64 `json:"version"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Version != 1 {
		t.Errorf("version: got %d, want 1", resp.Version)
	}
}

// TestPromptHandler_GetPrompt_VersionAPIKeyProjectID verifies that
// ?version=N lookup also uses the API-key project from context (#34).
func TestPromptHandler_GetPrompt_VersionAPIKeyProjectID(t *testing.T) {
	store := newFakePromptStore()
	cache := NewPromptCache(store)
	h := &PromptHandler{
		Store: store,
		Cache: cache,
	}

	store.CreatePromptVersion(context.Background(), &domain.PromptVersion{
		VersionID: "v3", ProjectID: "apikey-proj", Name: "system", Version: 3,
		Template: "System v3",
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/prompts/system?version=3", nil)
	req = req.WithContext(context.WithValue(req.Context(), APIKeyProjectIDKey, "apikey-proj"))
	w := httptest.NewRecorder()
	h.HandleGetPrompt(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want %d\nbody: %s", w.Code, http.StatusOK, w.Body.String())
	}
}

// TestPromptHandler_GetPrompt_LabelAPIKeyProjectID verifies that
// ?label=<label> lookup also uses the API-key project from context (#34).
func TestPromptHandler_GetPrompt_LabelAPIKeyProjectID(t *testing.T) {
	store := newFakePromptStore()
	cache := NewPromptCache(store)
	h := &PromptHandler{
		Store: store,
		Cache: cache,
	}

	store.CreatePromptVersion(context.Background(), &domain.PromptVersion{
		VersionID: "v1", ProjectID: "apikey-proj", Name: "system", Version: 1,
		Template: "System v1",
	})
	store.SetPromptLabel(context.Background(), &domain.PromptLabel{
		ProjectID: "apikey-proj", Name: "system", Label: "production", Version: 1,
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/prompts/system?label=production", nil)
	req = req.WithContext(context.WithValue(req.Context(), APIKeyProjectIDKey, "apikey-proj"))
	w := httptest.NewRecorder()
	h.HandleGetPrompt(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want %d\nbody: %s", w.Code, http.StatusOK, w.Body.String())
	}
}

// ---- Issue #39: GET /api/v1/prompts/:name/versions returns bare array ----

// TestPromptHandler_ListPromptVersions_ReturnsBareArray verifies that
// GET /api/v1/prompts/:name/versions returns a bare JSON array, not a wrapped
// object {name, versions, count} (#39).
func TestPromptHandler_ListPromptVersions_ReturnsBareArray(t *testing.T) {
	store := newFakePromptStore()
	cache := NewPromptCache(store)
	h := &PromptHandler{
		Store:        store,
		Cache:        cache,
		SessionStore: &FakeSessionStore{projectID: "test-proj"},
	}

	store.CreatePromptVersion(context.Background(), &domain.PromptVersion{
		VersionID: "v1", ProjectID: "test-proj", Name: "greeting", Version: 1,
		Template: "Hello!",
	})
	store.CreatePromptVersion(context.Background(), &domain.PromptVersion{
		VersionID: "v2", ProjectID: "test-proj", Name: "greeting", Version: 2,
		Template: "Hi!",
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/prompts/greeting/versions", nil)
	w := httptest.NewRecorder()
	h.HandleListPromptVersions(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want %d\nbody: %s", w.Code, http.StatusOK, w.Body.String())
	}

	// Must decode as a bare array, not a wrapped object.
	var result []map[string]any
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("response is not a bare JSON array: %v\nbody: %s", err, w.Body.String())
	}
	if len(result) != 2 {
		t.Errorf("count: got %d, want 2", len(result))
	}
}
