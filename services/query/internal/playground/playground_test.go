package playground

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
	"github.com/omneval/omneval/internal/judge"
	"github.com/omneval/omneval/internal/metadata"
	"github.com/omneval/omneval/services/query/internal/handler"
)

// ---- Tests for Interpolate ----

func TestInterpolate_Basic(t *testing.T) {
	template := "Hello {{name}}, welcome to {{place}}!"
	variables := map[string]string{
		"name":  "Alice",
		"place": "Wonderland",
	}

	result, missing := Interpolate(template, variables)
	if result != "Hello Alice, welcome to Wonderland!" {
		t.Errorf("got %q, want %q", result, "Hello Alice, welcome to Wonderland!")
	}
	if len(missing) != 0 {
		t.Errorf("unexpected missing variables: %v", missing)
	}
}

func TestInterpolate_MissingVariable(t *testing.T) {
	template := "Hello {{name}}, age {{age}}"
	variables := map[string]string{
		"name": "Bob",
	}

	result, missing := Interpolate(template, variables)
	if result != "Hello Bob, age {{age}}" {
		t.Errorf("got %q, want uninterpolated template", result)
	}
	if len(missing) != 1 || missing[0] != "age" {
		t.Errorf("missing: got %v, want [age]", missing)
	}
}

func TestInterpolate_NoVariables(t *testing.T) {
	template := "Just a plain template"
	variables := map[string]string{}

	result, missing := Interpolate(template, variables)
	if result != template {
		t.Errorf("got %q, want %q", result, template)
	}
	if len(missing) != 0 {
		t.Errorf("unexpected missing: %v", missing)
	}
}

func TestInterpolate_DuplicateVariables(t *testing.T) {
	template := "{{greeting}} {{name}}, {{greeting}} {{name}}"
	variables := map[string]string{
		"greeting": "Hi",
		"name":     "Carol",
	}

	result, missing := Interpolate(template, variables)
	if result != "Hi Carol, Hi Carol" {
		t.Errorf("got %q, want %q", result, "Hi Carol, Hi Carol")
	}
	if len(missing) != 0 {
		t.Errorf("unexpected missing: %v", missing)
	}
}

func TestInterpolate_EmptyVariablesMap(t *testing.T) {
	template := "Hello {{name}}"
	variables := map[string]string{}

	_, missing := Interpolate(template, variables)
	if len(missing) != 1 || missing[0] != "name" {
		t.Errorf("missing: got %v, want [name]", missing)
	}
}

func TestInterpolate_InvalidVariableName(t *testing.T) {
	template := "Hello {{123invalid}}"
	variables := map[string]string{}

	// The regex only matches valid variable names: [a-zA-Z_][a-zA-Z0-9_]*
	// {{123invalid}} should not be matched by the regex
	result, _ := Interpolate(template, variables)
	if result != "Hello {{123invalid}}" {
		t.Errorf("got %q, want unmodified template", result)
	}
}

func TestInterpolate_VariableNameWithUnderscore(t *testing.T) {
	template := "Hello {{first_name}}"
	variables := map[string]string{
		"first_name": "Alice",
	}

	result, _ := Interpolate(template, variables)
	if result != "Hello Alice" {
		t.Errorf("got %q, want %q", result, "Hello Alice")
	}
}

func TestInterpolate_WhitespaceInBraces(t *testing.T) {
	template := "Hello {{  name  }}!"
	variables := map[string]string{
		"name": "Alice",
	}

	result, _ := Interpolate(template, variables)
	if result != "Hello Alice!" {
		t.Errorf("got %q, want %q", result, "Hello Alice!")
	}
}

// ---- Fake LLM Client ----

// FakeLLMClient is a test double for LLMClient that returns pre-programmed responses.
type FakeLLMClient struct {
	mu sync.RWMutex
	// responses is a queue of responses to return
	responses []*judge.ChatResponse
	// errors is a queue of errors to return
	errors []error
	// calledRequests records the last request received
	calledRequests []judge.ChatRequest
}

func NewFakeLLMClient(responses ...*judge.ChatResponse) *FakeLLMClient {
	return &FakeLLMClient{
		responses:      responses,
		errors:         []error{},
		calledRequests: []judge.ChatRequest{},
	}
}

func (f *FakeLLMClient) SetResponse(resp *judge.ChatResponse) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.responses = append(f.responses, resp)
}

func (f *FakeLLMClient) SetError(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.errors = append(f.errors, err)
}

func (f *FakeLLMClient) Chat(ctx context.Context, req judge.ChatRequest) (*judge.ChatResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.calledRequests = append(f.calledRequests, req)

	// Check errors first
	if len(f.errors) > 0 {
		err := f.errors[0]
		f.errors = f.errors[1:]
		return nil, err
	}

	// Then check responses
	if len(f.responses) > 0 {
		resp := f.responses[0]
		f.responses = f.responses[1:]
		return resp, nil
	}

	// Default response
	return &judge.ChatResponse{
		Choices: []judge.Choice{
			{
				Message: judge.ChatMessage{
					Role:    "assistant",
					Content: "default response",
				},
			},
		},
		Usage: judge.Usage{
			PromptTokens:     10,
			CompletionTokens: 5,
		},
	}, nil
}

func (f *FakeLLMClient) LastRequest() judge.ChatRequest {
	f.mu.RLock()
	defer f.mu.RUnlock()
	if len(f.calledRequests) == 0 {
		return judge.ChatRequest{}
	}
	return f.calledRequests[len(f.calledRequests)-1]
}

func (f *FakeLLMClient) RequestCount() int {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return len(f.calledRequests)
}

// ---- Fake SessionStore ----

type fakeSessionStore struct {
	projectID string
}

func (f *fakeSessionStore) ProjectID(r *http.Request) (string, bool) {
	if f.projectID == "" {
		return "", false
	}
	return f.projectID, true
}

func (f *fakeSessionStore) ListProjects(r *http.Request) ([]*domain.Project, error) {
	if f.projectID == "" {
		return nil, errors.New("unauthenticated")
	}
	return []*domain.Project{{ProjectID: f.projectID}}, nil
}

// ---- Fake Prompt Cache ----

// fakePromptCache wraps a FakePromptStore for testing.
type fakePromptCache struct {
	*handler.PromptCache
}

// ---- Fake Prompt Store ----

type fakePromptStore struct {
	mu             sync.RWMutex
	promptVersions map[string]*domain.PromptVersion
	promptLabels   map[string]*domain.PromptLabel
}

func newFakePromptStore() *fakePromptStore {
	return &fakePromptStore{
		promptVersions: make(map[string]*domain.PromptVersion),
		promptLabels:   make(map[string]*domain.PromptLabel),
	}
}

func (m *fakePromptStore) versionKey(projectID, name string, version int64) string {
	return projectID + "|" + name + "|" + strconv.FormatInt(version, 10)
}

func labelKey(projectID, name, label string) string {
	return projectID + "|" + name + "|" + label
}

func (m *fakePromptStore) CreatePromptVersion(ctx context.Context, pv *domain.PromptVersion) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := m.versionKey(pv.ProjectID, pv.Name, pv.Version)
	if _, exists := m.promptVersions[key]; exists {
		return errors.New("conflict")
	}
	m.promptVersions[key] = pv
	return nil
}

func (m *fakePromptStore) GetPromptVersion(ctx context.Context, projectID, name string, version int64) (*domain.PromptVersion, error) {
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

func (m *fakePromptStore) GetPromptByLabel(ctx context.Context, projectID, name, label string) (*domain.PromptVersion, error) {
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

func (m *fakePromptStore) ListPromptVersions(ctx context.Context, projectID, name string) ([]*domain.PromptVersion, error) {
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

func (m *fakePromptStore) ListPromptNames(ctx context.Context, projectID string) ([]string, error) {
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

func (m *fakePromptStore) SetPromptLabel(ctx context.Context, label *domain.PromptLabel) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	lk := labelKey(label.ProjectID, label.Name, label.Label)
	m.promptLabels[lk] = label
	return nil
}

// Metadata.Store interface stubs (not used by playground tests)
func (m *fakePromptStore) CreateOrganization(ctx context.Context, o *domain.Organization) error {
	return nil
}
func (m *fakePromptStore) GetOrganization(ctx context.Context, id string) (*domain.Organization, error) {
	return nil, metadata.ErrNotFound
}
func (m *fakePromptStore) CreateProject(ctx context.Context, p *domain.Project) error { return nil }
func (m *fakePromptStore) GetProject(ctx context.Context, id string) (*domain.Project, error) {
	return nil, metadata.ErrNotFound
}
func (m *fakePromptStore) ListProjects(ctx context.Context, orgID string) ([]*domain.Project, error) {
	return nil, nil
}
func (m *fakePromptStore) CreateUser(ctx context.Context, u *domain.User) error { return nil }
func (m *fakePromptStore) GetUserByEmail(ctx context.Context, email string) (*domain.User, error) {
	return nil, metadata.ErrNotFound
}
func (m *fakePromptStore) GetUserByID(ctx context.Context, userID string) (*domain.User, error) {
	return nil, metadata.ErrNotFound
}
func (m *fakePromptStore) CountUsers(ctx context.Context) (int, error) { return 0, nil }
func (m *fakePromptStore) UpdateUserPassword(ctx context.Context, userID, passwordHash string) error {
	return nil
}
func (m *fakePromptStore) UpdateUserResetToken(ctx context.Context, userID, token string, expiry time.Time) error {
	return nil
}
func (m *fakePromptStore) GetUserByResetToken(ctx context.Context, token string) (*domain.User, error) {
	return nil, metadata.ErrNotFound
}
func (m *fakePromptStore) CheckPassword(hashed, plaintext string) error { return nil }
func (m *fakePromptStore) ListUsers(ctx context.Context, orgID string) ([]*domain.User, error) {
	return nil, nil
}
func (m *fakePromptStore) CreateSession(ctx context.Context, s *domain.Session) error { return nil }
func (m *fakePromptStore) GetSession(ctx context.Context, id string) (*domain.Session, error) {
	return nil, metadata.ErrNotFound
}
func (m *fakePromptStore) DeleteSession(ctx context.Context, id string) error       { return nil }
func (m *fakePromptStore) CreateAPIKey(ctx context.Context, k *domain.APIKey) error { return nil }
func (m *fakePromptStore) GetAPIKeyByHash(ctx context.Context, hash string) (*domain.APIKey, error) {
	return nil, metadata.ErrNotFound
}
func (m *fakePromptStore) RevokeAPIKey(ctx context.Context, keyID string) error { return nil }
func (m *fakePromptStore) ListAPIKeys(ctx context.Context, projectID string) ([]*domain.APIKey, error) {
	return nil, nil
}
func (m *fakePromptStore) CreateEvalRule(ctx context.Context, r *domain.EvalRule) error { return nil }
func (m *fakePromptStore) GetEvalRule(ctx context.Context, id string) (*domain.EvalRule, error) {
	return nil, metadata.ErrNotFound
}
func (m *fakePromptStore) ListEvalRules(ctx context.Context, projectID string) ([]*domain.EvalRule, error) {
	return nil, nil
}
func (m *fakePromptStore) UpdateEvalRule(ctx context.Context, r *domain.EvalRule) error { return nil }
func (m *fakePromptStore) DeleteEvalRule(ctx context.Context, ruleID string) error      { return nil }
func (m *fakePromptStore) CreateDataset(ctx context.Context, d *domain.Dataset) error   { return nil }
func (m *fakePromptStore) ListDatasets(ctx context.Context, projectID string) ([]*domain.Dataset, error) {
	return nil, nil
}
func (m *fakePromptStore) GetDataset(ctx context.Context, id string) (*domain.Dataset, error) {
	return nil, metadata.ErrNotFound
}
func (m *fakePromptStore) DeleteDataset(ctx context.Context, datasetID string) error { return nil }
func (m *fakePromptStore) CreateDatasetItem(ctx context.Context, i *domain.DatasetItem) error {
	return nil
}
func (m *fakePromptStore) ListDatasetItems(ctx context.Context, datasetID string) ([]*domain.DatasetItem, error) {
	return nil, nil
}
func (m *fakePromptStore) ListDatasetItemsPaginated(ctx context.Context, datasetID, cursor string, limit int) ([]*domain.DatasetItem, string, error) {
	return nil, "", nil
}
func (m *fakePromptStore) CreateDatasetRun(ctx context.Context, r *domain.DatasetRun) error {
	return nil
}
func (m *fakePromptStore) GetDatasetRun(ctx context.Context, id string) (*domain.DatasetRun, error) {
	return nil, metadata.ErrNotFound
}
func (m *fakePromptStore) UpdateDatasetRun(ctx context.Context, r *domain.DatasetRun) error {
	return nil
}
func (m *fakePromptStore) ListDatasetRuns(ctx context.Context, datasetID string) ([]*domain.DatasetRun, error) {
	return nil, nil
}
func (m *fakePromptStore) CreateDatasetRunItem(ctx context.Context, i *domain.DatasetRunItem) error {
	return nil
}
func (m *fakePromptStore) GetDatasetRunItem(ctx context.Context, id string) (*domain.DatasetRunItem, error) {
	return nil, metadata.ErrNotFound
}
func (m *fakePromptStore) UpdateDatasetRunItem(ctx context.Context, i *domain.DatasetRunItem) error {
	return nil
}
func (m *fakePromptStore) ListDatasetRunItems(ctx context.Context, runID string) ([]*domain.DatasetRunItem, error) {
	return nil, nil
}
func (m *fakePromptStore) Migrate(ctx context.Context) error { return nil }
func (m *fakePromptStore) Close() error                      { return nil }

// ---- Tests for HandleRun ----

func TestHandleRun_Success(t *testing.T) {
	store := newFakePromptStore()
	cache := handler.NewPromptCache(store)
	fakeLLM := NewFakeLLMClient(&judge.ChatResponse{
		Choices: []judge.Choice{
			{
				Message: judge.ChatMessage{
					Role:    "assistant",
					Content: "Hello Alice! Welcome to Wonderland!",
				},
			},
		},
		Usage: judge.Usage{
			PromptTokens:     20,
			CompletionTokens: 8,
		},
	})

	handler := &PlaygroundHandler{
		Cache:        cache,
		LLMClient:    fakeLLM,
		SessionStore: &fakeSessionStore{projectID: "test-proj"},
	}

	// Pre-seed a prompt version.
	store.CreatePromptVersion(context.Background(), &domain.PromptVersion{
		VersionID:   "v1",
		ProjectID:   "test-proj",
		Name:        "greeting",
		Version:     1,
		Template:    "Hello {{name}}! Welcome to {{place}}!",
		ModelConfig: domain.PromptModelConfig{Model: "gpt-4", Temperature: 0.7, MaxTokens: 256},
	})

	reqBody := `{"prompt_name": "greeting", "version": 1, "variables": {"name": "Alice", "place": "Wonderland"}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/playground/run", strings.NewReader(reqBody))
	w := httptest.NewRecorder()

	handler.HandleRun(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want %d\nbody: %s", w.Code, http.StatusOK, w.Body.String())
	}

	var resp Response
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if resp.Output != "Hello Alice! Welcome to Wonderland!" {
		t.Errorf("output: got %q, want %q", resp.Output, "Hello Alice! Welcome to Wonderland!")
	}
	if resp.Model != "gpt-4" {
		t.Errorf("model: got %q, want %q", resp.Model, "gpt-4")
	}
	if resp.InputTokens != 20 {
		t.Errorf("input_tokens: got %d, want %d", resp.InputTokens, 20)
	}
	if resp.OutputTokens != 8 {
		t.Errorf("output_tokens: got %d, want %d", resp.OutputTokens, 8)
	}
	if resp.DurationMs < 0 {
		t.Errorf("duration_ms: got %d, want >= 0", resp.DurationMs)
	}

	// Verify the LLM was called with correct parameters.
	lastReq := fakeLLM.LastRequest()
	if lastReq.Model != "gpt-4" {
		t.Errorf("llm model: got %q, want %q", lastReq.Model, "gpt-4")
	}
	if lastReq.Temperature != 0.7 {
		t.Errorf("llm temperature: got %f, want %f", lastReq.Temperature, 0.7)
	}
	if lastReq.MaxTokens != 256 {
		t.Errorf("llm max_tokens: got %d, want %d", lastReq.MaxTokens, 256)
	}
	if len(lastReq.Messages) != 2 {
		t.Fatalf("llm messages count: got %d, want 2", len(lastReq.Messages))
	}
	if lastReq.Messages[0].Role != "system" {
		t.Errorf("system message role: got %q, want %q", lastReq.Messages[0].Role, "system")
	}
	if lastReq.Messages[1].Content != "Hello Alice! Welcome to Wonderland!" {
		t.Errorf("user message: got %q, want %q", lastReq.Messages[1].Content, "Hello Alice! Welcome to Wonderland!")
	}
}

func TestHandleRun_ByLabel(t *testing.T) {
	store := newFakePromptStore()
	cache := handler.NewPromptCache(store)
	fakeLLM := NewFakeLLMClient(&judge.ChatResponse{
		Choices: []judge.Choice{
			{
				Message: judge.ChatMessage{
					Role:    "assistant",
					Content: "Hi there!",
				},
			},
		},
		Usage: judge.Usage{PromptTokens: 5, CompletionTokens: 3},
	})

	handler := &PlaygroundHandler{
		Cache:        cache,
		LLMClient:    fakeLLM,
		SessionStore: &fakeSessionStore{projectID: "test-proj"},
	}

	// Pre-seed versions and label.
	store.CreatePromptVersion(context.Background(), &domain.PromptVersion{
		VersionID:   "v1",
		ProjectID:   "test-proj",
		Name:        "summarize",
		Version:     1,
		Template:    "Summarize: {{text}}",
		ModelConfig: domain.PromptModelConfig{Model: "claude-3", Temperature: 0.0, MaxTokens: 512},
	})
	store.CreatePromptVersion(context.Background(), &domain.PromptVersion{
		VersionID:   "v2",
		ProjectID:   "test-proj",
		Name:        "summarize",
		Version:     2,
		Template:    "TL;DR: {{text}}",
		ModelConfig: domain.PromptModelConfig{Model: "claude-3-sonnet", Temperature: 0.0, MaxTokens: 128},
	})
	store.SetPromptLabel(context.Background(), &domain.PromptLabel{
		ProjectID: "test-proj", Name: "summarize", Label: "production", Version: 2,
	})

	reqBody := `{"prompt_name": "summarize", "label": "production", "variables": {"text": "hello world"}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/playground/run", strings.NewReader(reqBody))
	w := httptest.NewRecorder()

	handler.HandleRun(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want %d\nbody: %s", w.Code, http.StatusOK, w.Body.String())
	}

	var resp Response
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if resp.Output != "Hi there!" {
		t.Errorf("output: got %q, want %q", resp.Output, "Hi there!")
	}
	if resp.Model != "claude-3-sonnet" {
		t.Errorf("model: got %q, want %q", resp.Model, "claude-3-sonnet")
	}
}

func TestHandleRun_ModelOverride(t *testing.T) {
	store := newFakePromptStore()
	cache := handler.NewPromptCache(store)
	fakeLLM := NewFakeLLMClient(&judge.ChatResponse{
		Choices: []judge.Choice{
			{
				Message: judge.ChatMessage{Content: "overridden"},
			},
		},
		Usage: judge.Usage{PromptTokens: 5, CompletionTokens: 3},
	})

	handler := &PlaygroundHandler{
		Cache:        cache,
		LLMClient:    fakeLLM,
		SessionStore: &fakeSessionStore{projectID: "test-proj"},
	}

	store.CreatePromptVersion(context.Background(), &domain.PromptVersion{
		VersionID:   "v1",
		ProjectID:   "test-proj",
		Name:        "greeting",
		Version:     1,
		Template:    "Hello!",
		ModelConfig: domain.PromptModelConfig{Model: "gpt-4", Temperature: 0.7, MaxTokens: 256},
	})

	// Override model via request.
	overrideModel := "gpt-3.5-turbo"
	reqBody := `{"prompt_name": "greeting", "version": 1, "variables": {}, "model_override": "` + overrideModel + `"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/playground/run", strings.NewReader(reqBody))
	w := httptest.NewRecorder()

	handler.HandleRun(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want %d\nbody: %s", w.Code, http.StatusOK, w.Body.String())
	}

	var resp Response
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if resp.Model != overrideModel {
		t.Errorf("model: got %q, want %q", resp.Model, overrideModel)
	}

	// Verify LLM was called with overridden model.
	lastReq := fakeLLM.LastRequest()
	if lastReq.Model != overrideModel {
		t.Errorf("llm model: got %q, want %q", lastReq.Model, overrideModel)
	}
	// Temperature and max_tokens should still be from prompt config.
	if lastReq.Temperature != 0.7 {
		t.Errorf("llm temperature: got %f, want %f (from prompt config)", lastReq.Temperature, 0.7)
	}
	if lastReq.MaxTokens != 256 {
		t.Errorf("llm max_tokens: got %d, want %d (from prompt config)", lastReq.MaxTokens, 256)
	}
}

func TestHandleRun_TemperatureOverride(t *testing.T) {
	store := newFakePromptStore()
	cache := handler.NewPromptCache(store)
	fakeLLM := NewFakeLLMClient(&judge.ChatResponse{
		Choices: []judge.Choice{{Message: judge.ChatMessage{Content: "response"}}},
		Usage:   judge.Usage{PromptTokens: 5, CompletionTokens: 3},
	})

	handler := &PlaygroundHandler{
		Cache:        cache,
		LLMClient:    fakeLLM,
		SessionStore: &fakeSessionStore{projectID: "test-proj"},
	}

	store.CreatePromptVersion(context.Background(), &domain.PromptVersion{
		VersionID:   "v1",
		ProjectID:   "test-proj",
		Name:        "greeting",
		Version:     1,
		Template:    "Hello!",
		ModelConfig: domain.PromptModelConfig{Model: "gpt-4", Temperature: 0.7, MaxTokens: 256},
	})

	// Override temperature via request.
	overrideTemp := 0.9
	reqBody := `{"prompt_name": "greeting", "version": 1, "variables": {}, "temperature_override": ` + strconv.FormatFloat(overrideTemp, 'f', -1, 64) + `}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/playground/run", strings.NewReader(reqBody))
	w := httptest.NewRecorder()

	handler.HandleRun(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want %d", w.Code, http.StatusOK)
	}

	lastReq := fakeLLM.LastRequest()
	if lastReq.Temperature != overrideTemp {
		t.Errorf("temperature: got %f, want %f", lastReq.Temperature, overrideTemp)
	}
	// Model should still be from prompt config.
	if lastReq.Model != "gpt-4" {
		t.Errorf("model: got %q, want %q", lastReq.Model, "gpt-4")
	}
}

func TestHandleRun_MissingPromptName(t *testing.T) {
	store := newFakePromptStore()
	cache := handler.NewPromptCache(store)
	fakeLLM := NewFakeLLMClient(&judge.ChatResponse{
		Choices: []judge.Choice{{Message: judge.ChatMessage{Content: "response"}}},
		Usage:   judge.Usage{PromptTokens: 5, CompletionTokens: 3},
	})

	handler := &PlaygroundHandler{
		Cache:        cache,
		LLMClient:    fakeLLM,
		SessionStore: &fakeSessionStore{projectID: "test-proj"},
	}

	reqBody := `{"version": 1, "variables": {}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/playground/run", strings.NewReader(reqBody))
	w := httptest.NewRecorder()

	handler.HandleRun(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandleRun_PromptNotFound(t *testing.T) {
	store := newFakePromptStore()
	cache := handler.NewPromptCache(store)
	fakeLLM := NewFakeLLMClient(&judge.ChatResponse{
		Choices: []judge.Choice{{Message: judge.ChatMessage{Content: "response"}}},
		Usage:   judge.Usage{PromptTokens: 5, CompletionTokens: 3},
	})

	handler := &PlaygroundHandler{
		Cache:        cache,
		LLMClient:    fakeLLM,
		SessionStore: &fakeSessionStore{projectID: "test-proj"},
	}

	reqBody := `{"prompt_name": "nonexistent", "version": 1, "variables": {}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/playground/run", strings.NewReader(reqBody))
	w := httptest.NewRecorder()

	handler.HandleRun(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandleRun_MissingVariables(t *testing.T) {
	store := newFakePromptStore()
	cache := handler.NewPromptCache(store)
	fakeLLM := NewFakeLLMClient(&judge.ChatResponse{
		Choices: []judge.Choice{{Message: judge.ChatMessage{Content: "response"}}},
		Usage:   judge.Usage{PromptTokens: 5, CompletionTokens: 3},
	})

	handler := &PlaygroundHandler{
		Cache:        cache,
		LLMClient:    fakeLLM,
		SessionStore: &fakeSessionStore{projectID: "test-proj"},
	}

	store.CreatePromptVersion(context.Background(), &domain.PromptVersion{
		VersionID:   "v1",
		ProjectID:   "test-proj",
		Name:        "greeting",
		Version:     1,
		Template:    "Hello {{name}}!",
		ModelConfig: domain.PromptModelConfig{Model: "gpt-4", Temperature: 0.7, MaxTokens: 256},
	})

	reqBody := `{"prompt_name": "greeting", "version": 1, "variables": {}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/playground/run", strings.NewReader(reqBody))
	w := httptest.NewRecorder()

	handler.HandleRun(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandleRun_MissingVersionOrLabel(t *testing.T) {
	store := newFakePromptStore()
	cache := handler.NewPromptCache(store)
	fakeLLM := NewFakeLLMClient(&judge.ChatResponse{
		Choices: []judge.Choice{{Message: judge.ChatMessage{Content: "response"}}},
		Usage:   judge.Usage{PromptTokens: 5, CompletionTokens: 3},
	})

	handler := &PlaygroundHandler{
		Cache:        cache,
		LLMClient:    fakeLLM,
		SessionStore: &fakeSessionStore{projectID: "test-proj"},
	}

	reqBody := `{"prompt_name": "greeting", "variables": {}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/playground/run", strings.NewReader(reqBody))
	w := httptest.NewRecorder()

	handler.HandleRun(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandleRun_LLMNotConfigured(t *testing.T) {
	store := newFakePromptStore()
	cache := handler.NewPromptCache(store)

	handler := &PlaygroundHandler{
		Cache:        cache,
		LLMClient:    nil, // not configured
		SessionStore: &fakeSessionStore{projectID: "test-proj"},
	}

	store.CreatePromptVersion(context.Background(), &domain.PromptVersion{
		VersionID:   "v1",
		ProjectID:   "test-proj",
		Name:        "greeting",
		Version:     1,
		Template:    "Hello!",
		ModelConfig: domain.PromptModelConfig{Model: "gpt-4"},
	})

	reqBody := `{"prompt_name": "greeting", "version": 1, "variables": {}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/playground/run", strings.NewReader(reqBody))
	w := httptest.NewRecorder()

	handler.HandleRun(w, req)

	// Should return 503 Service Unavailable, not 400 Bad Request.
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusServiceUnavailable)
	}

	// Response must be JSON, never HTML.
	contentType := w.Header().Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("content-type: got %q, want %q", contentType, "application/json")
	}

	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v\nbody: %s", err, w.Body.String())
	}
	if resp["error"] != "playground LLM not configured" {
		t.Errorf("error: got %q, want %q", resp["error"], "playground LLM not configured")
	}
}

func TestHandleRun_LLMCallFails(t *testing.T) {
	store := newFakePromptStore()
	cache := handler.NewPromptCache(store)
	fakeLLM := NewFakeLLMClient()
	fakeLLM.SetError(errors.New("upstream connection refused"))

	handler := &PlaygroundHandler{
		Cache:        cache,
		LLMClient:    fakeLLM,
		SessionStore: &fakeSessionStore{projectID: "test-proj"},
	}

	store.CreatePromptVersion(context.Background(), &domain.PromptVersion{
		VersionID:   "v1",
		ProjectID:   "test-proj",
		Name:        "greeting",
		Version:     1,
		Template:    "Hello {{name}}!",
		ModelConfig: domain.PromptModelConfig{Model: "gpt-4"},
	})

	reqBody := `{"prompt_name": "greeting", "version": 1, "variables": {"name": "Alice"}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/playground/run", strings.NewReader(reqBody))
	w := httptest.NewRecorder()

	handler.HandleRun(w, req)

	if w.Code != http.StatusUnprocessableEntity {
		t.Errorf("status: got %d, want %d\nbody: %s", w.Code, http.StatusUnprocessableEntity, w.Body.String())
	}

	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["error"] == "" {
		t.Error("error field should contain the upstream error")
	}
}

func TestHandleRun_MethodNotAllowed(t *testing.T) {
	store := newFakePromptStore()
	cache := handler.NewPromptCache(store)
	fakeLLM := NewFakeLLMClient(&judge.ChatResponse{
		Choices: []judge.Choice{{Message: judge.ChatMessage{Content: "response"}}},
		Usage:   judge.Usage{PromptTokens: 5, CompletionTokens: 3},
	})

	handler := &PlaygroundHandler{
		Cache:        cache,
		LLMClient:    fakeLLM,
		SessionStore: &fakeSessionStore{projectID: "test-proj"},
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/playground/run", nil)
	w := httptest.NewRecorder()

	handler.HandleRun(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusMethodNotAllowed)
	}
}

func TestHandleRun_InvalidJSON(t *testing.T) {
	store := newFakePromptStore()
	cache := handler.NewPromptCache(store)
	fakeLLM := NewFakeLLMClient(&judge.ChatResponse{
		Choices: []judge.Choice{{Message: judge.ChatMessage{Content: "response"}}},
		Usage:   judge.Usage{PromptTokens: 5, CompletionTokens: 3},
	})

	handler := &PlaygroundHandler{
		Cache:        cache,
		LLMClient:    fakeLLM,
		SessionStore: &fakeSessionStore{projectID: "test-proj"},
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/playground/run", strings.NewReader(`{invalid json`))
	w := httptest.NewRecorder()

	handler.HandleRun(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandleRun_BothOverrides(t *testing.T) {
	store := newFakePromptStore()
	cache := handler.NewPromptCache(store)
	fakeLLM := NewFakeLLMClient(&judge.ChatResponse{
		Choices: []judge.Choice{{Message: judge.ChatMessage{Content: "response"}}},
		Usage:   judge.Usage{PromptTokens: 5, CompletionTokens: 3},
	})

	handler := &PlaygroundHandler{
		Cache:        cache,
		LLMClient:    fakeLLM,
		SessionStore: &fakeSessionStore{projectID: "test-proj"},
	}

	store.CreatePromptVersion(context.Background(), &domain.PromptVersion{
		VersionID:   "v1",
		ProjectID:   "test-proj",
		Name:        "greeting",
		Version:     1,
		Template:    "Hello!",
		ModelConfig: domain.PromptModelConfig{Model: "gpt-4", Temperature: 0.7, MaxTokens: 256},
	})

	// Override both model and temperature.
	reqBody := `{"prompt_name": "greeting", "version": 1, "variables": {}, "model_override": "gpt-3.5-turbo", "temperature_override": 0.9}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/playground/run", strings.NewReader(reqBody))
	w := httptest.NewRecorder()

	handler.HandleRun(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want %d", w.Code, http.StatusOK)
	}

	var resp Response
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if resp.Model != "gpt-3.5-turbo" {
		t.Errorf("model: got %q, want %q", resp.Model, "gpt-3.5-turbo")
	}

	lastReq := fakeLLM.LastRequest()
	if lastReq.Model != "gpt-3.5-turbo" {
		t.Errorf("llm model: got %q, want %q", lastReq.Model, "gpt-3.5-turbo")
	}
	if lastReq.Temperature != 0.9 {
		t.Errorf("temperature: got %f, want %f", lastReq.Temperature, 0.9)
	}
	// MaxTokens should still be from prompt config.
	if lastReq.MaxTokens != 256 {
		t.Errorf("max_tokens: got %d, want %d (from prompt config)", lastReq.MaxTokens, 256)
	}
}

func TestHandleRun_EmptyVariablesMap(t *testing.T) {
	// An empty variables map is valid when the template has no {{variables}}.
	store := newFakePromptStore()
	cache := handler.NewPromptCache(store)
	fakeLLM := NewFakeLLMClient(&judge.ChatResponse{
		Choices: []judge.Choice{{Message: judge.ChatMessage{Content: "plain response"}}},
		Usage:   judge.Usage{PromptTokens: 5, CompletionTokens: 3},
	})

	handler := &PlaygroundHandler{
		Cache:        cache,
		LLMClient:    fakeLLM,
		SessionStore: &fakeSessionStore{projectID: "test-proj"},
	}

	store.CreatePromptVersion(context.Background(), &domain.PromptVersion{
		VersionID:   "v1",
		ProjectID:   "test-proj",
		Name:        "greeting",
		Version:     1,
		Template:    "Hello!",
		ModelConfig: domain.PromptModelConfig{Model: "gpt-4"},
	})

	reqBody := `{"prompt_name": "greeting", "version": 1, "variables": {}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/playground/run", strings.NewReader(reqBody))
	w := httptest.NewRecorder()

	handler.HandleRun(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want %d\nbody: %s", w.Code, http.StatusOK, w.Body.String())
	}
}

func TestHandleRun_ProjectIDFromQuery(t *testing.T) {
	// When SessionStore returns no project, fall back to query param.
	store := newFakePromptStore()
	cache := handler.NewPromptCache(store)
	fakeLLM := NewFakeLLMClient(&judge.ChatResponse{
		Choices: []judge.Choice{{Message: judge.ChatMessage{Content: "response"}}},
		Usage:   judge.Usage{PromptTokens: 5, CompletionTokens: 3},
	})

	handler := &PlaygroundHandler{
		Cache:        cache,
		LLMClient:    fakeLLM,
		SessionStore: &fakeSessionStore{projectID: ""}, // no authenticated project
	}

	store.CreatePromptVersion(context.Background(), &domain.PromptVersion{
		VersionID:   "v1",
		ProjectID:   "query-proj",
		Name:        "greeting",
		Version:     1,
		Template:    "Hello!",
		ModelConfig: domain.PromptModelConfig{Model: "gpt-4"},
	})

	reqBody := `{"prompt_name": "greeting", "version": 1, "variables": {}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/playground/run?project_id=query-proj", strings.NewReader(reqBody))
	w := httptest.NewRecorder()

	handler.HandleRun(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want %d\nbody: %s", w.Code, http.StatusOK, w.Body.String())
	}
}
