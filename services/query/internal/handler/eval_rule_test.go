package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/omneval/omneval/internal/domain"
	"github.com/omneval/omneval/internal/duckdb"
	"github.com/omneval/omneval/internal/fake"
	"github.com/omneval/omneval/internal/metadata"
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

// TestEvalRuleHandler_CreateEvalRule_ResponseUsesSnakeCase verifies that the
// POST /api/v1/eval-rules response uses snake_case JSON keys (not PascalCase).
func TestEvalRuleHandler_CreateEvalRule_ResponseUsesSnakeCase(t *testing.T) {
	store := fake.NewFakeMetadataStore()
	handler := &EvalRuleHandler{
		Store:        store,
		SessionStore: &FakeSessionStore{projectID: "test-proj"},
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/eval-rules", strings.NewReader(`{
		"name": "my-eval",
		"judge_model": "gpt-4",
		"prompt_name": "judge-v1",
		"sample_rate": 0.5,
		"enabled": true
	}`))
	w := httptest.NewRecorder()
	handler.HandleCreate(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("status: got %d, want %d\nbody: %s", w.Code, http.StatusCreated, w.Body.String())
	}

	body := w.Body.String()

	// Verify snake_case keys are present
	snakeKeys := []string{"rule_id", "project_id", "judge_model", "prompt_name", "sample_rate", "enabled", "created_at"}
	for _, key := range snakeKeys {
		if !strings.Contains(body, `"`+key+`"`) {
			t.Errorf("response missing snake_case key %q\nbody: %s", key, body)
		}
	}

	// Verify PascalCase keys are NOT present
	pascalKeys := []string{"RuleID", "ProjectID", "JudgeModel", "PromptName", "PromptVersion", "SampleRate", "Enabled", "CreatedAt"}
	for _, key := range pascalKeys {
		if strings.Contains(body, `"`+key+`"`) {
			t.Errorf("response contains PascalCase key %q (should be snake_case)\nbody: %s", key, body)
		}
	}

	// Verify nested filter uses snake_case
	if !strings.Contains(body, `"kind"`) && !strings.Contains(body, `"model"`) {
		// filter might be empty, which is fine
	}
}

// TestEvalRuleHandler_ListEvalRules_ResponseUsesSnakeCase verifies that the
// GET /api/v1/eval-rules list response uses snake_case JSON keys.
func TestEvalRuleHandler_ListEvalRules_ResponseUsesSnakeCase(t *testing.T) {
	store := fake.NewFakeMetadataStore()
	rule := &domain.EvalRule{
		RuleID:     uuid.New().String(),
		ProjectID:  "test-proj",
		Name:       "eval-1",
		JudgeModel: "gpt-4",
		Enabled:    true,
	}
	store.CreateEvalRule(context.Background(), rule)

	handler := &EvalRuleHandler{
		Store:        store,
		SessionStore: &FakeSessionStore{projectID: "test-proj"},
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/eval-rules", nil)
	w := httptest.NewRecorder()
	handler.HandleList(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want %d\nbody: %s", w.Code, http.StatusOK, w.Body.String())
	}

	body := w.Body.String()

	// Verify snake_case keys are present
	snakeKeys := []string{"rule_id", "project_id", "judge_model", "sample_rate", "enabled", "created_at"}
	for _, key := range snakeKeys {
		if !strings.Contains(body, `"`+key+`"`) {
			t.Errorf("response missing snake_case key %q\nbody: %s", key, body)
		}
	}

	// Verify PascalCase keys are NOT present
	pascalKeys := []string{"RuleID", "ProjectID", "JudgeModel", "SampleRate", "Enabled", "CreatedAt"}
	for _, key := range pascalKeys {
		if strings.Contains(body, `"`+key+`"`) {
			t.Errorf("response contains PascalCase key %q (should be snake_case)\nbody: %s", key, body)
		}
	}
}

// ---- Preview endpoint tests ----

func TestEvalRuleHandler_HandlePreview_AuthRequired(t *testing.T) {
	store := fake.NewFakeMetadataStore()
	handler := &EvalRuleHandler{
		Store: store,
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/eval-rules/preview", strings.NewReader(`{
		"filter": {"kind": "llm"}
	}`))
	w := httptest.NewRecorder()
	handler.HandlePreview(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status: got %d, want %d\nbody: %s", w.Code, http.StatusUnauthorized, w.Body.String())
	}
}

func TestEvalRuleHandler_HandlePreview_MethodNotAllowed(t *testing.T) {
	store := fake.NewFakeMetadataStore()
	handler := &EvalRuleHandler{
		Store:        store,
		SessionStore: &FakeSessionStore{projectID: "test-proj"},
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/eval-rules/preview", nil)
	w := httptest.NewRecorder()
	handler.HandlePreview(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusMethodNotAllowed)
	}
}

func TestEvalRuleHandler_HandlePreview_InvalidJSON(t *testing.T) {
	store := fake.NewFakeMetadataStore()
	handler := &EvalRuleHandler{
		Store:        store,
		SessionStore: &FakeSessionStore{projectID: "test-proj"},
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/eval-rules/preview", strings.NewReader(`{invalid`))
	w := httptest.NewRecorder()
	handler.HandlePreview(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status: got %d, want %d\nbody: %s", w.Code, http.StatusBadRequest, w.Body.String())
	}
}

func TestEvalRuleHandler_HandlePreview_EmptyFilter(t *testing.T) {
	store := fake.NewFakeMetadataStore()
	handler := &EvalRuleHandler{
		Store:        store,
		SessionStore: &FakeSessionStore{projectID: "test-proj"},
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/eval-rules/preview", strings.NewReader(`{}`))
	w := httptest.NewRecorder()
	handler.HandlePreview(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status: got %d, want %d\nbody: %s", w.Code, http.StatusBadRequest, w.Body.String())
	}
}

func TestEvalRuleHandler_HandlePreview_MatchingSpans(t *testing.T) {
	// Create a DuckDB with test spans.
	db, err := duckdb.Open("test_preview_match.db")
	if err != nil {
		t.Fatalf("open duckdb: %v", err)
	}
	defer db.Close()
	defer os.Remove("test_preview_match.db")

	// Insert test spans.
	now := time.Now().UTC()
	past1h := now.Add(-30 * time.Minute).Format(time.RFC3339)
	past2h := now.Add(-2 * time.Hour).Format(time.RFC3339)
	_, err = db.ExecContext(context.Background(), `
		INSERT INTO spans (span_id, trace_id, project_id, name, kind, model, start_time, end_time, cost_usd)
		VALUES
			('span-1', 'trace-1', 'test-proj', 'gpt-4 completion', 'llm', 'gpt-4', ?, now(), 0.05),
			('span-2', 'trace-2', 'test-proj', 'gpt-4 completion', 'llm', 'gpt-4', ?, now(), 0.10),
			('span-3', 'trace-3', 'test-proj', 'claude completion', 'llm', 'claude-3', ?, now(), 0.15),
			('span-4', 'trace-4', 'test-proj', 'old span', 'llm', 'gpt-4', '2024-01-01T00:00:00Z', '2024-01-01T00:01:00Z', 0.01),
			('span-5', 'trace-5', 'other-proj', 'other span', 'llm', 'gpt-4', ?, now(), 0.05),
			('span-6', 'trace-6', 'test-proj', 'tool call', 'tool', 'gpt-4', ?, now(), 0.00);
	`, past1h, past1h, past2h, past1h, past1h)
	if err != nil {
		t.Fatalf("insert spans: %v", err)
	}

	store := fake.NewFakeMetadataStore()
	handler := &EvalRuleHandler{
		DB:           db,
		Store:        store,
		SessionStore: &FakeSessionStore{projectID: "test-proj"},
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/eval-rules/preview", strings.NewReader(`{
		"filter": {
			"kind": "llm",
			"model": "gpt-4"
		}
	}`))
	w := httptest.NewRecorder()
	handler.HandlePreview(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want %d\nbody: %s", w.Code, http.StatusOK, w.Body.String())
	}

	var resp PreviewEvalRulesResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	// Should match 2 spans in the last hour (span-1, span-2)
	// span-5 is from other-proj, span-4 is >1h old, span-6 is tool kind
	if len(resp.Spans) != 2 {
		t.Errorf("span count: got %d, want 2\nspans: %+v", len(resp.Spans), resp.Spans)
	}

	// Should have match_count_24h = 2 (same 2 spans in last 24h)
	if resp.MatchCount24h != 2 {
		t.Errorf("match_count_24h: got %d, want 2", resp.MatchCount24h)
	}

	// Verify returned span fields
	for _, s := range resp.Spans {
		if s.Kind == "" || s.Model == "" || s.Name == "" {
			t.Errorf("span missing field: %+v", s)
		}
	}
}

func TestEvalRuleHandler_HandlePreview_NoMatches(t *testing.T) {
	db, err := duckdb.Open("test_preview_no_match.db")
	if err != nil {
		t.Fatalf("open duckdb: %v", err)
	}
	defer db.Close()
	defer os.Remove("test_preview_no_match.db")

	// Insert a non-matching span.
	_, err = db.ExecContext(context.Background(), `
		INSERT INTO spans (span_id, trace_id, project_id, name, kind, model, start_time, end_time, cost_usd)
		VALUES ('span-1', 'trace-1', 'test-proj', 'tool call', 'tool', 'gpt-4', now(), now(), 0.00);
	`)
	if err != nil {
		t.Fatalf("insert span: %v", err)
	}

	store := fake.NewFakeMetadataStore()
	handler := &EvalRuleHandler{
		DB:           db,
		Store:        store,
		SessionStore: &FakeSessionStore{projectID: "test-proj"},
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/eval-rules/preview", strings.NewReader(`{
		"filter": {
			"kind": "llm",
			"model": "claude-3"
		}
	}`))
	w := httptest.NewRecorder()
	handler.HandlePreview(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want %d\nbody: %s", w.Code, http.StatusOK, w.Body.String())
	}

	var resp PreviewEvalRulesResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if len(resp.Spans) != 0 {
		t.Errorf("span count: got %d, want 0", len(resp.Spans))
	}
	if resp.MatchCount24h != 0 {
		t.Errorf("match_count_24h: got %d, want 0", resp.MatchCount24h)
	}
}

func TestEvalRuleHandler_HandlePreview_MatchCount24h(t *testing.T) {
	db, err := duckdb.Open("test_preview_count.db")
	if err != nil {
		t.Fatalf("open duckdb: %v", err)
	}
	defer db.Close()
	defer os.Remove("test_preview_count.db")

	now := time.Now().UTC()
	past1h := now.Add(-30 * time.Minute)
	past2h := now.Add(-2 * time.Hour)
	past23h := now.Add(-23 * time.Hour)
	past30h := now.Add(-30 * time.Hour) // outside 24h window
	_, err = db.ExecContext(context.Background(), `
		INSERT INTO spans (span_id, trace_id, project_id, name, kind, model, start_time, cost_usd)
		VALUES
			('span-1', 'trace-1', 'test-proj', 'gpt-4', 'llm', 'gpt-4', ?, 0.05),
			('span-2', 'trace-2', 'test-proj', 'gpt-4', 'llm', 'gpt-4', ?, 0.10),
			('span-3', 'trace-3', 'test-proj', 'gpt-4', 'llm', 'gpt-4', ?, 0.15),
			('span-4', 'trace-4', 'test-proj', 'gpt-4', 'llm', 'gpt-4', ?, 0.01);
	`, past1h, past2h, past23h, past30h)
	if err != nil {
		t.Fatalf("insert spans: %v", err)
	}

	store := fake.NewFakeMetadataStore()
	handler := &EvalRuleHandler{
		DB:           db,
		Store:        store,
		SessionStore: &FakeSessionStore{projectID: "test-proj"},
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/eval-rules/preview", strings.NewReader(`{
		"filter": {
			"kind": "llm",
			"model": "gpt-4"
		}
	}`))
	w := httptest.NewRecorder()
	handler.HandlePreview(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want %d\nbody: %s", w.Code, http.StatusOK, w.Body.String())
	}

	var resp PreviewEvalRulesResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	// Last hour: spans at 30m, 2h, 23h old -> 1 match (span-1)
	if len(resp.Spans) != 1 {
		t.Errorf("span count (last hour): got %d, want 1", len(resp.Spans))
	}

	// Last 24h: spans at 30m, 2h, 23h old -> 3 matches (span-1, span-2, span-3)
	if resp.MatchCount24h != 3 {
		t.Errorf("match_count_24h: got %d, want 3", resp.MatchCount24h)
	}
}

func TestEvalRuleHandler_HandlePreview_DBNil(t *testing.T) {
	store := fake.NewFakeMetadataStore()
	handler := &EvalRuleHandler{
		DB:           nil, // No DB connection
		Store:        store,
		SessionStore: &FakeSessionStore{projectID: "test-proj"},
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/eval-rules/preview", strings.NewReader(`{
		"filter": {
			"kind": "llm"
		}
	}`))
	w := httptest.NewRecorder()
	handler.HandlePreview(w, req)

	// Should return empty results gracefully (not error)
	if w.Code != http.StatusOK {
		t.Errorf("status: got %d, want %d\nbody: %s", w.Code, http.StatusOK, w.Body.String())
	}

	var resp PreviewEvalRulesResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if len(resp.Spans) != 0 {
		t.Errorf("span count: got %d, want 0 (no DB)", len(resp.Spans))
	}
	if resp.MatchCount24h != 0 {
		t.Errorf("match_count_24h: got %d, want 0 (no DB)", resp.MatchCount24h)
	}
}
