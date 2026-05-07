package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/zbloss/lantern/internal/domain"
	"github.com/zbloss/lantern/internal/metadata"
)

// ---- FakeEvalStore: minimal metadata.Store for eval rule tests ----

type FakeEvalStore struct {
	mu          sync.RWMutex
	evalRules   map[string]*domain.EvalRule
	deleteError error
}

func newFakeEvalStore() *FakeEvalStore {
	return &FakeEvalStore{
		evalRules: make(map[string]*domain.EvalRule),
	}
}

func (f *FakeEvalStore) CreateEvalRule(ctx context.Context, r *domain.EvalRule) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.evalRules[r.RuleID] = r
	return nil
}

func (f *FakeEvalStore) GetEvalRule(ctx context.Context, id string) (*domain.EvalRule, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	r, ok := f.evalRules[id]
	if !ok {
		return nil, metadata.ErrNotFound
	}
	cp := *r
	return &cp, nil
}

func (f *FakeEvalStore) ListEvalRules(ctx context.Context, projectID string) ([]*domain.EvalRule, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	var rules []*domain.EvalRule
	for _, r := range f.evalRules {
		if r.ProjectID == projectID {
			rules = append(rules, r)
		}
	}
	return rules, nil
}

func (f *FakeEvalStore) UpdateEvalRule(ctx context.Context, r *domain.EvalRule) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.evalRules[r.RuleID] = r
	return nil
}

func (f *FakeEvalStore) DeleteEvalRule(ctx context.Context, ruleID string) error {
	if f.deleteError != nil {
		return f.deleteError
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.evalRules[ruleID]; !ok {
		return metadata.ErrNotFound
	}
	delete(f.evalRules, ruleID)
	return nil
}

// ---- Metadata.Store interface stubs (not used by tests) ----
func (f *FakeEvalStore) CreateOrganization(ctx context.Context, o *domain.Organization) error              { return nil }
func (f *FakeEvalStore) GetOrganization(ctx context.Context, id string) (*domain.Organization, error)     { return nil, metadata.ErrNotFound }
func (f *FakeEvalStore) CreateProject(ctx context.Context, p *domain.Project) error                       { return nil }
func (f *FakeEvalStore) GetProject(ctx context.Context, id string) (*domain.Project, error)               { return nil, metadata.ErrNotFound }
func (f *FakeEvalStore) ListProjects(ctx context.Context, orgID string) ([]*domain.Project, error)        { return nil, nil }
func (f *FakeEvalStore) CreateUser(ctx context.Context, u *domain.User) error                             { return nil }
func (f *FakeEvalStore) GetUserByEmail(ctx context.Context, email string) (*domain.User, error)           { return nil, metadata.ErrNotFound }
func (f *FakeEvalStore) GetUserByID(ctx context.Context, userID string) (*domain.User, error)             { return nil, metadata.ErrNotFound }
func (f *FakeEvalStore) ListUsers(ctx context.Context, orgID string) ([]*domain.User, error)              { return nil, nil }
func (f *FakeEvalStore) CountUsers(ctx context.Context) (int, error)                                      { return 0, nil }
func (f *FakeEvalStore) UpdateUserPassword(ctx context.Context, userID, passwordHash string) error        { return nil }
func (f *FakeEvalStore) CheckPassword(hashed, plaintext string) error                                     { return nil }
func (f *FakeEvalStore) CreateSession(ctx context.Context, s *domain.Session) error                       { return nil }
func (f *FakeEvalStore) GetSession(ctx context.Context, id string) (*domain.Session, error)               { return nil, metadata.ErrNotFound }
func (f *FakeEvalStore) DeleteSession(ctx context.Context, id string) error                               { return nil }
func (f *FakeEvalStore) CreateAPIKey(ctx context.Context, k *domain.APIKey) error                         { return nil }
func (f *FakeEvalStore) GetAPIKeyByHash(ctx context.Context, hash string) (*domain.APIKey, error)         { return nil, metadata.ErrNotFound }
func (f *FakeEvalStore) RevokeAPIKey(ctx context.Context, keyID string) error                             { return nil }
func (f *FakeEvalStore) ListAPIKeys(ctx context.Context, projectID string) ([]*domain.APIKey, error)      { return nil, nil }
func (f *FakeEvalStore) CreatePromptVersion(ctx context.Context, pv *domain.PromptVersion) error          { return nil }
func (f *FakeEvalStore) GetPromptVersion(ctx context.Context, projectID, name string, version int64) (*domain.PromptVersion, error) {
	return nil, metadata.ErrNotFound
}
func (f *FakeEvalStore) GetPromptByLabel(ctx context.Context, projectID, name, label string) (*domain.PromptVersion, error) {
	return nil, metadata.ErrNotFound
}
func (f *FakeEvalStore) ListPromptVersions(ctx context.Context, projectID, name string) ([]*domain.PromptVersion, error) {
	return nil, nil
}
func (f *FakeEvalStore) SetPromptLabel(ctx context.Context, l *domain.PromptLabel) error { return nil }
func (f *FakeEvalStore) CreateDataset(ctx context.Context, d *domain.Dataset) error                       { return nil }
func (f *FakeEvalStore) GetDataset(ctx context.Context, id string) (*domain.Dataset, error)               { return nil, metadata.ErrNotFound }
func (f *FakeEvalStore) CreateDatasetItem(ctx context.Context, i *domain.DatasetItem) error               { return nil }
func (f *FakeEvalStore) ListDatasetItems(ctx context.Context, datasetID string) ([]*domain.DatasetItem, error) { return nil, nil }
func (f *FakeEvalStore) CreateDatasetRun(ctx context.Context, r *domain.DatasetRun) error                 { return nil }
func (f *FakeEvalStore) GetDatasetRun(ctx context.Context, id string) (*domain.DatasetRun, error)         { return nil, metadata.ErrNotFound }
func (f *FakeEvalStore) Migrate(ctx context.Context) error                                                { return nil }
func (f *FakeEvalStore) Close() error                                                                     { return nil }

// ---- Tests ----

// TestEvalRuleHandler_CreateEvalRule verifies POST creates a rule and returns 201.
func TestEvalRuleHandler_CreateEvalRule(t *testing.T) {
	store := newFakeEvalStore()
	handler := &EvalRuleHandler{
		Store:        store,
		SessionStore: &FakeSessionStore{projectID: "test-proj"},
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/eval-rules", strings.NewReader(`{
		"name": "my-eval",
		"judge_model": "gpt-4",
		"prompt_name": "judge-v1",
		"sample_rate": 1.0,
		"enabled": true
	}`))
	w := httptest.NewRecorder()
	handler.HandleCreate(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("status: got %d, want %d\nbody: %s", w.Code, http.StatusCreated, w.Body.String())
	}

	var resp domain.EvalRule
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if resp.Name != "my-eval" {
		t.Errorf("name: got %q, want %q", resp.Name, "my-eval")
	}
	if resp.ProjectID != "test-proj" {
		t.Errorf("project_id: got %q, want %q", resp.ProjectID, "test-proj")
	}
	if resp.JudgeModel != "gpt-4" {
		t.Errorf("judge_model: got %q, want %q", resp.JudgeModel, "gpt-4")
	}
	if resp.SampleRate != 1.0 {
		t.Errorf("sample_rate: got %v, want 1.0", resp.SampleRate)
	}
	if !resp.Enabled {
		t.Error("enabled: got false, want true")
	}
	if resp.RuleID == "" {
		t.Error("rule_id: expected non-empty UUID")
	}
}

// TestEvalRuleHandler_CreateEvalRule_InvalidSampleRate verifies 400 for sample_rate out of range.
func TestEvalRuleHandler_CreateEvalRule_InvalidSampleRate(t *testing.T) {
	store := newFakeEvalStore()
	handler := &EvalRuleHandler{
		Store:        store,
		SessionStore: &FakeSessionStore{projectID: "test-proj"},
	}

	// sample_rate > 1.0
	req := httptest.NewRequest(http.MethodPost, "/api/v1/eval-rules", strings.NewReader(`{
		"name": "my-eval",
		"judge_model": "gpt-4",
		"prompt_name": "judge-v1",
		"sample_rate": 1.5
	}`))
	w := httptest.NewRecorder()
	handler.HandleCreate(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status: got %d, want %d\nbody: %s", w.Code, http.StatusBadRequest, w.Body.String())
	}
}

// TestEvalRuleHandler_CreateEvalRule_InvalidSampleRateNegative verifies 400 for negative sample_rate.
func TestEvalRuleHandler_CreateEvalRule_InvalidSampleRateNegative(t *testing.T) {
	store := newFakeEvalStore()
	handler := &EvalRuleHandler{
		Store:        store,
		SessionStore: &FakeSessionStore{projectID: "test-proj"},
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/eval-rules", strings.NewReader(`{
		"name": "my-eval",
		"judge_model": "gpt-4",
		"prompt_name": "judge-v1",
		"sample_rate": -0.5
	}`))
	w := httptest.NewRecorder()
	handler.HandleCreate(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status: got %d, want %d\nbody: %s", w.Code, http.StatusBadRequest, w.Body.String())
	}
}

// TestEvalRuleHandler_CreateEvalRule_MissingName verifies 400 when name is empty.
func TestEvalRuleHandler_CreateEvalRule_MissingName(t *testing.T) {
	store := newFakeEvalStore()
	handler := &EvalRuleHandler{
		Store:        store,
		SessionStore: &FakeSessionStore{projectID: "test-proj"},
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/eval-rules", strings.NewReader(`{
		"judge_model": "gpt-4"
	}`))
	w := httptest.NewRecorder()
	handler.HandleCreate(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status: got %d, want %d\nbody: %s", w.Code, http.StatusBadRequest, w.Body.String())
	}
}

// TestEvalRuleHandler_CreateEvalRule_MissingJudgeModel verifies 400 when judge_model is empty.
func TestEvalRuleHandler_CreateEvalRule_MissingJudgeModel(t *testing.T) {
	store := newFakeEvalStore()
	handler := &EvalRuleHandler{
		Store:        store,
		SessionStore: &FakeSessionStore{projectID: "test-proj"},
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/eval-rules", strings.NewReader(`{
		"name": "my-eval"
	}`))
	w := httptest.NewRecorder()
	handler.HandleCreate(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status: got %d, want %d\nbody: %s", w.Code, http.StatusBadRequest, w.Body.String())
	}
}

// TestEvalRuleHandler_CreateEvalRule_AuthRequired verifies auth is enforced.
func TestEvalRuleHandler_CreateEvalRule_AuthRequired(t *testing.T) {
	store := newFakeEvalStore()
	handler := &EvalRuleHandler{
		Store: store,
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/eval-rules", strings.NewReader(`{
		"name": "my-eval",
		"judge_model": "gpt-4"
	}`))
	w := httptest.NewRecorder()
	handler.HandleCreate(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status: got %d, want %d\nbody: %s", w.Code, http.StatusUnauthorized, w.Body.String())
	}
}

// TestEvalRuleHandler_CreateEvalRule_MethodNotAllowed verifies 405 for wrong method.
func TestEvalRuleHandler_CreateEvalRule_MethodNotAllowed(t *testing.T) {
	store := newFakeEvalStore()
	handler := &EvalRuleHandler{
		Store:        store,
		SessionStore: &FakeSessionStore{projectID: "test-proj"},
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/eval-rules", nil)
	w := httptest.NewRecorder()
	handler.HandleCreate(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusMethodNotAllowed)
	}
}

// TestEvalRuleHandler_CreateEvalRule_InvalidJSON verifies 400 for malformed JSON.
func TestEvalRuleHandler_CreateEvalRule_InvalidJSON(t *testing.T) {
	store := newFakeEvalStore()
	handler := &EvalRuleHandler{
		Store:        store,
		SessionStore: &FakeSessionStore{projectID: "test-proj"},
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/eval-rules", strings.NewReader(`{invalid`))
	w := httptest.NewRecorder()
	handler.HandleCreate(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status: got %d, want %d\nbody: %s", w.Code, http.StatusBadRequest, w.Body.String())
	}
}

// TestEvalRuleHandler_ListEvalRules verifies GET returns all rules for the project.
func TestEvalRuleHandler_ListEvalRules(t *testing.T) {
	store := newFakeEvalStore()
	handler := &EvalRuleHandler{
		Store:        store,
		SessionStore: &FakeSessionStore{projectID: "test-proj"},
	}

	// Pre-seed some rules.
	store.CreateEvalRule(context.Background(), &domain.EvalRule{
		RuleID:     uuid.New().String(),
		ProjectID:  "test-proj",
		Name:       "eval-1",
		JudgeModel: "gpt-4",
		Enabled:    true,
	})
	store.CreateEvalRule(context.Background(), &domain.EvalRule{
		RuleID:     uuid.New().String(),
		ProjectID:  "test-proj",
		Name:       "eval-2",
		JudgeModel: "claude-3",
		Enabled:    false,
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/eval-rules", nil)
	w := httptest.NewRecorder()
	handler.HandleList(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want %d\nbody: %s", w.Code, http.StatusOK, w.Body.String())
	}

	var resp struct {
		Rules []domain.EvalRule `json:"rules"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if len(resp.Rules) != 2 {
		t.Errorf("rules count: got %d, want 2", len(resp.Rules))
	}
}

// TestEvalRuleHandler_ListEvalRules_AuthRequired verifies auth is enforced for list.
func TestEvalRuleHandler_ListEvalRules_AuthRequired(t *testing.T) {
	store := newFakeEvalStore()
	handler := &EvalRuleHandler{
		Store: store,
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/eval-rules", nil)
	w := httptest.NewRecorder()
	handler.HandleList(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status: got %d, want %d\nbody: %s", w.Code, http.StatusUnauthorized, w.Body.String())
	}
}

// TestEvalRuleHandler_ListEvalRules_EmptyList verifies empty array when no rules exist.
func TestEvalRuleHandler_ListEvalRules_EmptyList(t *testing.T) {
	store := newFakeEvalStore()
	handler := &EvalRuleHandler{
		Store:        store,
		SessionStore: &FakeSessionStore{projectID: "empty-proj"},
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/eval-rules", nil)
	w := httptest.NewRecorder()
	handler.HandleList(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want %d\nbody: %s", w.Code, http.StatusOK, w.Body.String())
	}

	var resp struct {
		Rules []domain.EvalRule `json:"rules"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if len(resp.Rules) != 0 {
		t.Errorf("rules count: got %d, want 0", len(resp.Rules))
	}
}

// TestEvalRuleHandler_DeleteEvalRule verifies DELETE removes a rule and returns 204.
func TestEvalRuleHandler_DeleteEvalRule(t *testing.T) {
	store := newFakeEvalStore()
	rule := &domain.EvalRule{
		RuleID:     uuid.New().String(),
		ProjectID:  "test-proj",
		Name:       "to-delete",
		JudgeModel: "gpt-4",
	}
	store.CreateEvalRule(context.Background(), rule)

	handler := &EvalRuleHandler{
		Store:        store,
		SessionStore: &FakeSessionStore{projectID: "test-proj"},
	}

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/eval-rules/"+rule.RuleID, nil)
	w := httptest.NewRecorder()
	handler.HandleDelete(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("status: got %d, want %d\nbody: %s", w.Code, http.StatusNoContent, w.Body.String())
	}

	// Verify rule is deleted.
	_, err := store.GetEvalRule(context.Background(), rule.RuleID)
	if err != metadata.ErrNotFound {
		t.Errorf("expected ErrNotFound after delete, got: %v", err)
	}
}

// TestEvalRuleHandler_DeleteEvalRule_NotFound verifies 404 for non-existent rule.
func TestEvalRuleHandler_DeleteEvalRule_NotFound(t *testing.T) {
	store := newFakeEvalStore()
	handler := &EvalRuleHandler{
		Store:        store,
		SessionStore: &FakeSessionStore{projectID: "test-proj"},
	}

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/eval-rules/nonexistent-id", nil)
	w := httptest.NewRecorder()
	handler.HandleDelete(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status: got %d, want %d\nbody: %s", w.Code, http.StatusNotFound, w.Body.String())
	}
}

// TestEvalRuleHandler_DeleteEvalRule_AuthRequired verifies auth is enforced for delete.
func TestEvalRuleHandler_DeleteEvalRule_AuthRequired(t *testing.T) {
	store := newFakeEvalStore()
	handler := &EvalRuleHandler{
		Store: store,
	}

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/eval-rules/some-id", nil)
	w := httptest.NewRecorder()
	handler.HandleDelete(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status: got %d, want %d\nbody: %s", w.Code, http.StatusUnauthorized, w.Body.String())
	}
}

// TestEvalRuleHandler_DeleteEvalRule_MethodNotAllowed verifies 405 for wrong method.
func TestEvalRuleHandler_DeleteEvalRule_MethodNotAllowed(t *testing.T) {
	store := newFakeEvalStore()
	handler := &EvalRuleHandler{
		Store:        store,
		SessionStore: &FakeSessionStore{projectID: "test-proj"},
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/eval-rules/some-id", nil)
	w := httptest.NewRecorder()
	handler.HandleDelete(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusMethodNotAllowed)
	}
}

// TestEvalRuleHandler_ListEvalRules_MethodNotAllowed verifies 405 for wrong method on list.
func TestEvalRuleHandler_ListEvalRules_MethodNotAllowed(t *testing.T) {
	store := newFakeEvalStore()
	handler := &EvalRuleHandler{
		Store:        store,
		SessionStore: &FakeSessionStore{projectID: "test-proj"},
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/eval-rules", nil)
	w := httptest.NewRecorder()
	handler.HandleList(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusMethodNotAllowed)
	}
}

// TestEvalRuleHandler_CreateEvalRule_Filter verifies filter fields are properly handled.
func TestEvalRuleHandler_CreateEvalRule_Filter(t *testing.T) {
	store := newFakeEvalStore()
	handler := &EvalRuleHandler{
		Store:        store,
		SessionStore: &FakeSessionStore{projectID: "test-proj"},
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/eval-rules", strings.NewReader(`{
		"name": "filter-eval",
		"judge_model": "gpt-4",
		"prompt_name": "judge-v1",
		"sample_rate": 0.5,
		"enabled": true,
		"filter": {
			"kind": "llm",
			"model": "gpt-4-turbo",
			"service_name": "my-service",
			"prompt_name": "greeting",
			"status_code": "OK",
			"min_cost_usd": 0.01,
			"max_cost_usd": 1.0,
			"min_duration_ms": 100,
			"max_duration_ms": 5000
		}
	}`))
	w := httptest.NewRecorder()
	handler.HandleCreate(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("status: got %d, want %d\nbody: %s", w.Code, http.StatusCreated, w.Body.String())
	}

	var resp domain.EvalRule
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if resp.Filter.Kind == nil || *resp.Filter.Kind != "llm" {
		got := domain.SpanKind("")
		if resp.Filter.Kind != nil {
			got = *resp.Filter.Kind
		}
		t.Errorf("filter.kind: got %q, want %q", got, "llm")
	}
	if resp.Filter.Model == nil || *resp.Filter.Model != "gpt-4-turbo" {
		t.Errorf("filter.model: got %v", resp.Filter.Model)
	}
	if resp.Filter.ServiceName == nil || *resp.Filter.ServiceName != "my-service" {
		t.Errorf("filter.service_name: got %v", resp.Filter.ServiceName)
	}
}

// TestEvalRuleHandler_CreateEvalRule_DefaultSampleRate verifies default sample_rate of 1.0.
func TestEvalRuleHandler_CreateEvalRule_DefaultSampleRate(t *testing.T) {
	store := newFakeEvalStore()
	handler := &EvalRuleHandler{
		Store:        store,
		SessionStore: &FakeSessionStore{projectID: "test-proj"},
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/eval-rules", strings.NewReader(`{
		"name": "my-eval",
		"judge_model": "gpt-4"
	}`))
	w := httptest.NewRecorder()
	handler.HandleCreate(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("status: got %d, want %d\nbody: %s", w.Code, http.StatusCreated, w.Body.String())
	}

	var resp domain.EvalRule
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if resp.SampleRate != 1.0 {
		t.Errorf("sample_rate: got %v, want 1.0", resp.SampleRate)
	}
}

// TestEvalRuleHandler_CreateEvalRule_DefaultEnabled verifies default enabled of true.
func TestEvalRuleHandler_CreateEvalRule_DefaultEnabled(t *testing.T) {
	store := newFakeEvalStore()
	handler := &EvalRuleHandler{
		Store:        store,
		SessionStore: &FakeSessionStore{projectID: "test-proj"},
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/eval-rules", strings.NewReader(`{
		"name": "my-eval",
		"judge_model": "gpt-4"
	}`))
	w := httptest.NewRecorder()
	handler.HandleCreate(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("status: got %d, want %d\nbody: %s", w.Code, http.StatusCreated, w.Body.String())
	}

	var resp domain.EvalRule
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if !resp.Enabled {
		t.Error("enabled: got false, want true")
	}
}

// TestEvalRuleHandler_CreateEvalRule_StoreError verifies 500 on store failure.
func TestEvalRuleHandler_CreateEvalRule_StoreError(t *testing.T) {
	store := &ErrorEvalStore{}
	handler := &EvalRuleHandler{
		Store:        store,
		SessionStore: &FakeSessionStore{projectID: "test-proj"},
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/eval-rules", strings.NewReader(`{
		"name": "my-eval",
		"judge_model": "gpt-4"
	}`))
	w := httptest.NewRecorder()
	handler.HandleCreate(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status: got %d, want %d\nbody: %s", w.Code, http.StatusInternalServerError, w.Body.String())
	}
}

// ---- ErrorEvalStore: always returns errors for eval rule operations ----

type ErrorEvalStore struct{}

func (e *ErrorEvalStore) CreateEvalRule(ctx context.Context, r *domain.EvalRule) error { return errors.New("store error") }
func (e *ErrorEvalStore) GetEvalRule(ctx context.Context, id string) (*domain.EvalRule, error) {
	return nil, errors.New("store error")
}
func (e *ErrorEvalStore) ListEvalRules(ctx context.Context, projectID string) ([]*domain.EvalRule, error) {
	return nil, errors.New("store error")
}
func (e *ErrorEvalStore) UpdateEvalRule(ctx context.Context, r *domain.EvalRule) error { return errors.New("store error") }
func (e *ErrorEvalStore) DeleteEvalRule(ctx context.Context, ruleID string) error { return errors.New("store error") }
func (e *ErrorEvalStore) CreateOrganization(ctx context.Context, o *domain.Organization) error              { return nil }
func (e *ErrorEvalStore) GetOrganization(ctx context.Context, id string) (*domain.Organization, error)     { return nil, metadata.ErrNotFound }
func (e *ErrorEvalStore) CreateProject(ctx context.Context, p *domain.Project) error                       { return nil }
func (e *ErrorEvalStore) GetProject(ctx context.Context, id string) (*domain.Project, error)               { return nil, metadata.ErrNotFound }
func (e *ErrorEvalStore) ListProjects(ctx context.Context, orgID string) ([]*domain.Project, error)        { return nil, nil }
func (e *ErrorEvalStore) CreateUser(ctx context.Context, u *domain.User) error                             { return nil }
func (e *ErrorEvalStore) GetUserByEmail(ctx context.Context, email string) (*domain.User, error)           { return nil, metadata.ErrNotFound }
func (e *ErrorEvalStore) GetUserByID(ctx context.Context, userID string) (*domain.User, error)             { return nil, metadata.ErrNotFound }
func (e *ErrorEvalStore) ListUsers(ctx context.Context, orgID string) ([]*domain.User, error)              { return nil, nil }
func (e *ErrorEvalStore) CountUsers(ctx context.Context) (int, error)                                      { return 0, nil }
func (e *ErrorEvalStore) UpdateUserPassword(ctx context.Context, userID, passwordHash string) error        { return nil }
func (e *ErrorEvalStore) CheckPassword(hashed, plaintext string) error                                     { return nil }
func (e *ErrorEvalStore) CreateSession(ctx context.Context, s *domain.Session) error                       { return nil }
func (e *ErrorEvalStore) GetSession(ctx context.Context, id string) (*domain.Session, error)               { return nil, metadata.ErrNotFound }
func (e *ErrorEvalStore) DeleteSession(ctx context.Context, id string) error                               { return nil }
func (e *ErrorEvalStore) CreateAPIKey(ctx context.Context, k *domain.APIKey) error                         { return nil }
func (e *ErrorEvalStore) GetAPIKeyByHash(ctx context.Context, hash string) (*domain.APIKey, error)         { return nil, metadata.ErrNotFound }
func (e *ErrorEvalStore) RevokeAPIKey(ctx context.Context, keyID string) error                             { return nil }
func (e *ErrorEvalStore) ListAPIKeys(ctx context.Context, projectID string) ([]*domain.APIKey, error)      { return nil, nil }
func (e *ErrorEvalStore) CreatePromptVersion(ctx context.Context, pv *domain.PromptVersion) error          { return errors.New("store error") }
func (e *ErrorEvalStore) GetPromptVersion(ctx context.Context, projectID, name string, version int64) (*domain.PromptVersion, error) {
	return nil, errors.New("store error")
}
func (e *ErrorEvalStore) GetPromptByLabel(ctx context.Context, projectID, name, label string) (*domain.PromptVersion, error) {
	return nil, errors.New("store error")
}
func (e *ErrorEvalStore) ListPromptVersions(ctx context.Context, projectID, name string) ([]*domain.PromptVersion, error) {
	return nil, errors.New("store error")
}
func (e *ErrorEvalStore) SetPromptLabel(ctx context.Context, l *domain.PromptLabel) error { return errors.New("store error") }
func (e *ErrorEvalStore) CreateDataset(ctx context.Context, d *domain.Dataset) error                       { return nil }
func (e *ErrorEvalStore) GetDataset(ctx context.Context, id string) (*domain.Dataset, error)               { return nil, metadata.ErrNotFound }
func (e *ErrorEvalStore) CreateDatasetItem(ctx context.Context, i *domain.DatasetItem) error               { return nil }
func (e *ErrorEvalStore) ListDatasetItems(ctx context.Context, datasetID string) ([]*domain.DatasetItem, error) { return nil, nil }
func (e *ErrorEvalStore) CreateDatasetRun(ctx context.Context, r *domain.DatasetRun) error                 { return nil }
func (e *ErrorEvalStore) GetDatasetRun(ctx context.Context, id string) (*domain.DatasetRun, error)         { return nil, metadata.ErrNotFound }
func (e *ErrorEvalStore) Migrate(ctx context.Context) error                                                { return nil }
func (e *ErrorEvalStore) Close() error                                                                     { return nil }
