package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/zbloss/lantern/internal/domain"
	"github.com/zbloss/lantern/internal/fake"
	"github.com/zbloss/lantern/internal/metadata"
)

// ---- Test helpers ----

// erroringEvalRuleStore wraps a FakeMetadataStore and overrides only eval-rule
// methods to return errors, enabling tests for store-failure paths.
type erroringEvalRuleStore struct {
	*fake.FakeMetadataStore
}

func (s *erroringEvalRuleStore) CreateEvalRule(ctx context.Context, r *domain.EvalRule) error {
	return errors.New("store error")
}
func (s *erroringEvalRuleStore) GetEvalRule(ctx context.Context, id string) (*domain.EvalRule, error) {
	return nil, errors.New("store error")
}
func (s *erroringEvalRuleStore) ListEvalRules(ctx context.Context, projectID string) ([]*domain.EvalRule, error) {
	return nil, errors.New("store error")
}
func (s *erroringEvalRuleStore) UpdateEvalRule(ctx context.Context, r *domain.EvalRule) error {
	return errors.New("store error")
}
func (s *erroringEvalRuleStore) DeleteEvalRule(ctx context.Context, ruleID string) error {
	return errors.New("store error")
}

// ---- Tests ----

func TestEvalRuleHandler_CreateEvalRule(t *testing.T) {
	store := fake.NewFakeMetadataStore()
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

func TestEvalRuleHandler_CreateEvalRule_InvalidSampleRate(t *testing.T) {
	store := fake.NewFakeMetadataStore()
	handler := &EvalRuleHandler{
		Store:        store,
		SessionStore: &FakeSessionStore{projectID: "test-proj"},
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/eval-rules", strings.NewReader(`{
		"name": "my-eval",
		"judge_model": "gpt-4",
		"sample_rate": 1.5
	}`))
	w := httptest.NewRecorder()
	handler.HandleCreate(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status: got %d, want %d\nbody: %s", w.Code, http.StatusBadRequest, w.Body.String())
	}
}

func TestEvalRuleHandler_CreateEvalRule_InvalidSampleRateNegative(t *testing.T) {
	store := fake.NewFakeMetadataStore()
	handler := &EvalRuleHandler{
		Store:        store,
		SessionStore: &FakeSessionStore{projectID: "test-proj"},
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/eval-rules", strings.NewReader(`{
		"name": "my-eval",
		"judge_model": "gpt-4",
		"sample_rate": -0.5
	}`))
	w := httptest.NewRecorder()
	handler.HandleCreate(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status: got %d, want %d\nbody: %s", w.Code, http.StatusBadRequest, w.Body.String())
	}
}

func TestEvalRuleHandler_CreateEvalRule_MissingName(t *testing.T) {
	store := fake.NewFakeMetadataStore()
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

func TestEvalRuleHandler_CreateEvalRule_MissingJudgeModel(t *testing.T) {
	store := fake.NewFakeMetadataStore()
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

func TestEvalRuleHandler_CreateEvalRule_AuthRequired(t *testing.T) {
	store := fake.NewFakeMetadataStore()
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

func TestEvalRuleHandler_CreateEvalRule_MethodNotAllowed(t *testing.T) {
	store := fake.NewFakeMetadataStore()
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

func TestEvalRuleHandler_CreateEvalRule_InvalidJSON(t *testing.T) {
	store := fake.NewFakeMetadataStore()
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

func TestEvalRuleHandler_ListEvalRules(t *testing.T) {
	store := fake.NewFakeMetadataStore()
	handler := &EvalRuleHandler{
		Store:        store,
		SessionStore: &FakeSessionStore{projectID: "test-proj"},
	}

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

func TestEvalRuleHandler_ListEvalRules_AuthRequired(t *testing.T) {
	store := fake.NewFakeMetadataStore()
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

func TestEvalRuleHandler_ListEvalRules_EmptyList(t *testing.T) {
	store := fake.NewFakeMetadataStore()
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

func TestEvalRuleHandler_DeleteEvalRule(t *testing.T) {
	store := fake.NewFakeMetadataStore()
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

	_, err := store.GetEvalRule(context.Background(), rule.RuleID)
	if err != metadata.ErrNotFound {
		t.Errorf("expected ErrNotFound after delete, got: %v", err)
	}
}

func TestEvalRuleHandler_DeleteEvalRule_NotFound(t *testing.T) {
	store := fake.NewFakeMetadataStore()
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

func TestEvalRuleHandler_DeleteEvalRule_AuthRequired(t *testing.T) {
	store := fake.NewFakeMetadataStore()
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

func TestEvalRuleHandler_DeleteEvalRule_MethodNotAllowed(t *testing.T) {
	store := fake.NewFakeMetadataStore()
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

func TestEvalRuleHandler_ListEvalRules_MethodNotAllowed(t *testing.T) {
	store := fake.NewFakeMetadataStore()
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

func TestEvalRuleHandler_CreateEvalRule_Filter(t *testing.T) {
	store := fake.NewFakeMetadataStore()
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

func TestEvalRuleHandler_CreateEvalRule_DefaultSampleRate(t *testing.T) {
	store := fake.NewFakeMetadataStore()
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

func TestEvalRuleHandler_CreateEvalRule_DefaultEnabled(t *testing.T) {
	store := fake.NewFakeMetadataStore()
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

func TestEvalRuleHandler_CreateEvalRule_StoreError(t *testing.T) {
	store := &erroringEvalRuleStore{FakeMetadataStore: fake.NewFakeMetadataStore()}
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

func TestEvalRuleHandler_CreateEvalRule_AttributesMatch_TopLevel(t *testing.T) {
	store := fake.NewFakeMetadataStore()
	handler := &EvalRuleHandler{
		Store:        store,
		SessionStore: &FakeSessionStore{projectID: "test-proj"},
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/eval-rules", strings.NewReader(`{
		"name": "attr-eval",
		"judge_model": "gpt-4",
		"filter": {
			"attributes_match": [
				{"key": "user_id", "pattern": "usr-.*"}
			]
		}
	}`))
	w := httptest.NewRecorder()
	handler.HandleCreate(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("status: got %d, want %d\nbody: %s", w.Code, http.StatusCreated, w.Body.String())
	}
}

func TestEvalRuleHandler_CreateEvalRule_AttributesMatch_NestedDotPath(t *testing.T) {
	store := fake.NewFakeMetadataStore()
	handler := &EvalRuleHandler{
		Store:        store,
		SessionStore: &FakeSessionStore{projectID: "test-proj"},
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/eval-rules", strings.NewReader(`{
		"name": "attr-eval",
		"judge_model": "gpt-4",
		"filter": {
			"attributes_match": [
				{"key": "metadata.user_id", "pattern": "usr-456"}
			]
		}
	}`))
	w := httptest.NewRecorder()
	handler.HandleCreate(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("status: got %d, want %d\nbody: %s", w.Code, http.StatusCreated, w.Body.String())
	}
}

func TestEvalRuleHandler_CreateEvalRule_AttributesMatch_DepthLimitExceeded(t *testing.T) {
	store := fake.NewFakeMetadataStore()
	handler := &EvalRuleHandler{
		Store:        store,
		SessionStore: &FakeSessionStore{projectID: "test-proj"},
	}

	// 6 segments = too deep (> max depth 5)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/eval-rules", strings.NewReader(`{
		"name": "attr-eval",
		"judge_model": "gpt-4",
		"filter": {
			"attributes_match": [
				{"key": "a.b.c.d.e.f", "pattern": ".*"}
			]
		}
	}`))
	w := httptest.NewRecorder()
	handler.HandleCreate(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status: got %d, want %d\nbody: %s", w.Code, http.StatusBadRequest, w.Body.String())
	}
}

func TestEvalRuleHandler_CreateEvalRule_AttributesMatch_InvalidPattern(t *testing.T) {
	store := fake.NewFakeMetadataStore()
	handler := &EvalRuleHandler{
		Store:        store,
		SessionStore: &FakeSessionStore{projectID: "test-proj"},
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/eval-rules", strings.NewReader(`{
		"name": "attr-eval",
		"judge_model": "gpt-4",
		"filter": {
			"attributes_match": [
				{"key": "user_id", "pattern": "[invalid"}
			]
		}
	}`))
	w := httptest.NewRecorder()
	handler.HandleCreate(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status: got %d, want %d\nbody: %s", w.Code, http.StatusBadRequest, w.Body.String())
	}
}
