package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/omneval/omneval/internal/domain"
	"github.com/omneval/omneval/internal/fake"
	"github.com/omneval/omneval/internal/judge"
	"github.com/omneval/omneval/internal/metadata"
)

// ---- Fake LLM Client for dataset run tests ----

type fakeJudgeLLMClient struct {
	mu sync.RWMutex
	// responses is a queue of responses to return
	responses []*judge.ChatResponse
	// errors is a queue of errors to return
	errors []error
	// calledRequests records the requests received
	calledRequests []judge.ChatRequest
}

func newFakeJudgeLLMClient(responses ...*judge.ChatResponse) *fakeJudgeLLMClient {
	return &fakeJudgeLLMClient{
		responses:      responses,
		errors:         []error{},
		calledRequests: []judge.ChatRequest{},
	}
}

func (f *fakeJudgeLLMClient) SetError(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.errors = append(f.errors, err)
}

func (f *fakeJudgeLLMClient) Chat(ctx context.Context, req judge.ChatRequest) (*judge.ChatResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.calledRequests = append(f.calledRequests, req)

	if len(f.errors) > 0 {
		err := f.errors[0]
		f.errors = f.errors[1:]
		return nil, err
	}

	if len(f.responses) > 0 {
		resp := f.responses[0]
		f.responses = f.responses[1:]
		return resp, nil
	}

	return &judge.ChatResponse{
		Choices: []judge.Choice{
			{
				Message: judge.ChatMessage{
					Role:    "assistant",
					Content: "Score: 5.0\nReasoning: Average performance.",
				},
			},
		},
		Usage: judge.Usage{
			PromptTokens:     10,
			CompletionTokens: 5,
		},
	}, nil
}

func (f *fakeJudgeLLMClient) LastRequest() judge.ChatRequest {
	f.mu.RLock()
	defer f.mu.RUnlock()
	if len(f.calledRequests) == 0 {
		return judge.ChatRequest{}
	}
	return f.calledRequests[len(f.calledRequests)-1]
}

func (f *fakeJudgeLLMClient) RequestCount() int {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return len(f.calledRequests)
}

// ---- Tests for HandleRun ----

func TestHandleRun_CreateRun(t *testing.T) {
	store := fake.NewFakeMetadataStore()
	fakeJudge := newFakeJudgeLLMClient(&judge.ChatResponse{
		Choices: []judge.Choice{
			{
				Message: judge.ChatMessage{
					Role:    "assistant",
					Content: "Score: 8.5\nReasoning: The output matches well.",
				},
			},
		},
		Usage: judge.Usage{PromptTokens: 20, CompletionTokens: 10},
	})

	// Pre-seed data.
	projectID := "test-proj"
	ds := &domain.Dataset{
		DatasetID: uuid.New().String(),
		ProjectID: projectID,
		Name:      "test-dataset",
	}
	store.CreateDataset(context.Background(), ds)

	rule := &domain.EvalRule{
		RuleID:        uuid.New().String(),
		ProjectID:     projectID,
		Name:          "test-rule",
		JudgeModel:    "gpt-4",
		PromptName:    "eval-judge",
		PromptVersion: 1,
	}
	store.CreateEvalRule(context.Background(), rule)

	item := &domain.DatasetItem{
		ItemID:         uuid.New().String(),
		DatasetID:      ds.DatasetID,
		Input:          "hello world",
		ExpectedOutput: "Hi there!",
	}
	store.CreateDatasetItem(context.Background(), item)

	handler := &DatasetRunHandler{
		DatasetStore: store,
		EvalRuleStore: store,
		SessionStore: &FakeSessionStore{projectID: projectID},
		JudgeClient:  fakeJudge,
		Cache:        buildTestPromptCache(store, projectID, rule),
	}

	reqBody := `{"eval_rule_id": "` + rule.RuleID + `"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/datasets/"+ds.DatasetID+"/runs", strings.NewReader(reqBody))
	w := httptest.NewRecorder()

	handler.HandleRun(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("status: got %d, want %d\nbody: %s", w.Code, http.StatusCreated, w.Body.String())
	}

	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	runID := resp["run_id"]
	if runID == "" {
		t.Error("expected run_id in response")
	}
	if resp["status"] != domain.DatasetRunStatusComplete {
		t.Errorf("status: got %q, want %q", resp["status"], domain.DatasetRunStatusComplete)
	}

	// Verify the run was created in the store.
	run, err := store.GetDatasetRun(context.Background(), runID)
	if err != nil {
		t.Fatalf("GetDatasetRun: %v", err)
	}
	if run.EvalRuleID != rule.RuleID {
		t.Errorf("eval_rule_id: got %q, want %q", run.EvalRuleID, rule.RuleID)
	}
	if run.Status != domain.DatasetRunStatusComplete {
		t.Errorf("run status: got %q, want %q", run.Status, domain.DatasetRunStatusComplete)
	}

	// Verify the LLM was called.
	if fakeJudge.RequestCount() != 1 {
		t.Errorf("LLM call count: got %d, want 1", fakeJudge.RequestCount())
	}
	lastReq := fakeJudge.LastRequest()
	if lastReq.Model != "gpt-4" {
		t.Errorf("LLM model: got %q, want %q", lastReq.Model, "gpt-4")
	}
}

func TestHandleRun_MissingEvalRuleID(t *testing.T) {
	store := fake.NewFakeMetadataStore()
	ds := &domain.Dataset{DatasetID: uuid.New().String(), ProjectID: "test-proj", Name: "test-ds"}
	store.CreateDataset(context.Background(), ds)

	handler := &DatasetRunHandler{
		DatasetStore: store,
		EvalRuleStore: store,
		SessionStore: &FakeSessionStore{projectID: "test-proj"},
		JudgeClient:  newFakeJudgeLLMClient(),
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/datasets/"+ds.DatasetID+"/runs", strings.NewReader(`{}`))
	w := httptest.NewRecorder()
	handler.HandleRun(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandleRun_DatasetNotFound(t *testing.T) {
	store := fake.NewFakeMetadataStore()
	fakeJudge := newFakeJudgeLLMClient()
	handler := &DatasetRunHandler{
		DatasetStore: store,
		EvalRuleStore: store,
		SessionStore: &FakeSessionStore{projectID: "test-proj"},
		JudgeClient:  fakeJudge,
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/datasets/nonexistent/runs", strings.NewReader(`{"eval_rule_id": "some-rule"}`))
	w := httptest.NewRecorder()
	handler.HandleRun(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestHandleRun_EvalRuleNotFound(t *testing.T) {
	store := fake.NewFakeMetadataStore()
	fakeJudge := newFakeJudgeLLMClient()

	ds := &domain.Dataset{
		DatasetID: uuid.New().String(),
		ProjectID: "test-proj",
		Name:      "test-ds",
	}
	store.CreateDataset(context.Background(), ds)

	handler := &DatasetRunHandler{
		DatasetStore: store,
		EvalRuleStore: store,
		SessionStore: &FakeSessionStore{projectID: "test-proj"},
		JudgeClient:  fakeJudge,
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/datasets/"+ds.DatasetID+"/runs", strings.NewReader(`{"eval_rule_id": "nonexistent-rule"}`))
	w := httptest.NewRecorder()
	handler.HandleRun(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestHandleRun_JudgeLLMNotConfigured(t *testing.T) {
	store := fake.NewFakeMetadataStore()

	ds := &domain.Dataset{DatasetID: uuid.New().String(), ProjectID: "test-proj", Name: "test-ds"}
	store.CreateDataset(context.Background(), ds)
	rule := &domain.EvalRule{RuleID: uuid.New().String(), ProjectID: "test-proj", Name: "test-rule", JudgeModel: "gpt-4", PromptName: "eval", PromptVersion: 1}
	store.CreateEvalRule(context.Background(), rule)

	handler := &DatasetRunHandler{
		DatasetStore: store,
		EvalRuleStore: store,
		SessionStore: &FakeSessionStore{projectID: "test-proj"},
		JudgeClient:  nil, // not configured
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/datasets/"+ds.DatasetID+"/runs", strings.NewReader(`{"eval_rule_id": "`+rule.RuleID+`"}`))
	w := httptest.NewRecorder()
	handler.HandleRun(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandleRun_DatasetHasNoItems(t *testing.T) {
	store := fake.NewFakeMetadataStore()
	fakeJudge := newFakeJudgeLLMClient()

	ds := &domain.Dataset{DatasetID: uuid.New().String(), ProjectID: "test-proj", Name: "empty-ds"}
	store.CreateDataset(context.Background(), ds)
	rule := &domain.EvalRule{RuleID: uuid.New().String(), ProjectID: "test-proj", Name: "test-rule", JudgeModel: "gpt-4", PromptName: "eval", PromptVersion: 1}
	store.CreateEvalRule(context.Background(), rule)

	handler := &DatasetRunHandler{
		DatasetStore: store,
		EvalRuleStore: store,
		SessionStore: &FakeSessionStore{projectID: "test-proj"},
		JudgeClient:  fakeJudge,
		Cache:        buildTestPromptCache(store, "test-proj", rule),
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/datasets/"+ds.DatasetID+"/runs", strings.NewReader(`{"eval_rule_id": "`+rule.RuleID+`"}`))
	w := httptest.NewRecorder()
	handler.HandleRun(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandleRun_AuthRequired(t *testing.T) {
	store := fake.NewFakeMetadataStore()
	handler := &DatasetRunHandler{
		DatasetStore: store,
		EvalRuleStore: store,
		SessionStore: nil,
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/datasets/some-id/runs", strings.NewReader(`{"eval_rule_id": "some-rule"}`))
	w := httptest.NewRecorder()
	handler.HandleRun(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestHandleRun_MethodNotAllowed(t *testing.T) {
	store := fake.NewFakeMetadataStore()
	handler := &DatasetRunHandler{
		DatasetStore: store,
		EvalRuleStore: store,
		SessionStore: &FakeSessionStore{projectID: "test-proj"},
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/datasets/some-id/runs", nil)
	w := httptest.NewRecorder()
	handler.HandleRun(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusMethodNotAllowed)
	}
}

// ---- Tests for HandleListRuns ----

func TestHandleListRuns_ReturnsRuns(t *testing.T) {
	store := fake.NewFakeMetadataStore()
	fakeJudge := newFakeJudgeLLMClient(
		&judge.ChatResponse{
			Choices: []judge.Choice{
				{Message: judge.ChatMessage{Content: "Score: 7.0\nReasoning: Good."}},
			},
		},
		&judge.ChatResponse{
			Choices: []judge.Choice{
				{Message: judge.ChatMessage{Content: "Score: 9.0\nReasoning: Excellent."}},
			},
		},
	)

	projectID := "test-proj"
	ds := &domain.Dataset{DatasetID: uuid.New().String(), ProjectID: projectID, Name: "test-ds"}
	store.CreateDataset(context.Background(), ds)

	rule := &domain.EvalRule{RuleID: uuid.New().String(), ProjectID: projectID, Name: "test-rule", JudgeModel: "gpt-4", PromptName: "eval", PromptVersion: 1}
	store.CreateEvalRule(context.Background(), rule)

	// Add items.
	for i := 0; i < 2; i++ {
		item := &domain.DatasetItem{
			ItemID:         uuid.New().String(),
			DatasetID:      ds.DatasetID,
			Input:          "input " + string(rune('0'+i)),
			ExpectedOutput: "output " + string(rune('0'+i)),
		}
		store.CreateDatasetItem(context.Background(), item)
	}

	// Run evaluation.
	handler := &DatasetRunHandler{
		DatasetStore: store,
		EvalRuleStore: store,
		SessionStore: &FakeSessionStore{projectID: projectID},
		JudgeClient:  fakeJudge,
		Cache:        buildTestPromptCache(store, projectID, rule),
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/datasets/"+ds.DatasetID+"/runs", strings.NewReader(`{"eval_rule_id": "`+rule.RuleID+`"}`))
	w := httptest.NewRecorder()
	handler.HandleRun(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("run status: got %d, want %d", w.Code, http.StatusCreated)
	}

	var runResp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&runResp); err != nil {
		t.Fatalf("decode run: %v", err)
	}
	runID := runResp["run_id"]

	// Now list runs.
	req = httptest.NewRequest(http.MethodGet, "/api/v1/datasets/"+ds.DatasetID+"/runs", nil)
	w = httptest.NewRecorder()
	handler.HandleListRuns(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("list status: got %d, want %d\nbody: %s", w.Code, http.StatusOK, w.Body.String())
	}

	var listResp ListDatasetRunsResponse
	if err := json.NewDecoder(w.Body).Decode(&listResp); err != nil {
		t.Fatalf("decode list: %v", err)
	}

	if len(listResp.Runs) != 1 {
		t.Errorf("runs count: got %d, want 1", len(listResp.Runs))
	}

	run := listResp.Runs[0]
	if run.RunID != runID {
		t.Errorf("run_id: got %q, want %q", run.RunID, runID)
	}
	if run.Status != domain.DatasetRunStatusComplete {
		t.Errorf("status: got %q, want %q", run.Status, domain.DatasetRunStatusComplete)
	}
	if run.ItemCount != 2 {
		t.Errorf("item_count: got %d, want 2", run.ItemCount)
	}
	if run.MeanScore == 0 {
		t.Error("mean_score: expected non-zero average")
	}
}

func TestHandleListRuns_AuthRequired(t *testing.T) {
	store := fake.NewFakeMetadataStore()
	handler := &DatasetRunHandler{DatasetStore: store, EvalRuleStore: store}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/datasets/some-id/runs", nil)
	w := httptest.NewRecorder()
	handler.HandleListRuns(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestHandleListRuns_DatasetNotFound(t *testing.T) {
	store := fake.NewFakeMetadataStore()
	handler := &DatasetRunHandler{
		DatasetStore: store,
		EvalRuleStore: store,
		SessionStore: &FakeSessionStore{projectID: "test-proj"},
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/datasets/nonexistent/runs", nil)
	w := httptest.NewRecorder()
	handler.HandleListRuns(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestHandleListRuns_MethodNotAllowed(t *testing.T) {
	store := fake.NewFakeMetadataStore()
	handler := &DatasetRunHandler{
		DatasetStore: store,
		EvalRuleStore: store,
		SessionStore: &FakeSessionStore{projectID: "test-proj"},
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/datasets/some-id/runs", nil)
	w := httptest.NewRecorder()
	handler.HandleListRuns(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusMethodNotAllowed)
	}
}

func TestHandleListRuns_EmptyList(t *testing.T) {
	store := fake.NewFakeMetadataStore()
	handler := &DatasetRunHandler{
		DatasetStore: store,
		EvalRuleStore: store,
		SessionStore: &FakeSessionStore{projectID: "test-proj"},
	}

	ds := &domain.Dataset{DatasetID: uuid.New().String(), ProjectID: "test-proj", Name: "empty"}
	store.CreateDataset(context.Background(), ds)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/datasets/"+ds.DatasetID+"/runs", nil)
	w := httptest.NewRecorder()
	handler.HandleListRuns(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want %d", w.Code, http.StatusOK)
	}

	var resp ListDatasetRunsResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if len(resp.Runs) != 0 {
		t.Errorf("runs count: got %d, want 0", len(resp.Runs))
	}
}

// TestHandleListRuns_WithoutJudgeClient verifies that the list runs endpoint
// works without a JudgeClient. This is important because the server.go route
// registration should not gate read endpoints behind judge LLM config.
func TestHandleListRuns_WithoutJudgeClient(t *testing.T) {
	t.Parallel()

	store := fake.NewFakeMetadataStore()
	handler := &DatasetRunHandler{
		DatasetStore: store,
		EvalRuleStore: store,
		SessionStore: &FakeSessionStore{projectID: "test-proj"},
		// JudgeClient is nil — simulating no LLM configured.
	}

	ds := &domain.Dataset{DatasetID: uuid.New().String(), ProjectID: "test-proj", Name: "empty"}
	store.CreateDataset(context.Background(), ds)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/datasets/"+ds.DatasetID+"/runs", nil)
	w := httptest.NewRecorder()
	handler.HandleListRuns(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want %d\nbody: %s", w.Code, http.StatusOK, w.Body.String())
	}

	var resp ListDatasetRunsResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v\nbody: %s", err, w.Body.String())
	}

	if len(resp.Runs) != 0 {
		t.Errorf("runs count: got %d, want 0", len(resp.Runs))
	}
}

// ---- Tests for HandleGetRun ----

func TestHandleGetRun_ReturnsRunWithItems(t *testing.T) {
	store := fake.NewFakeMetadataStore()

	ds := &domain.Dataset{DatasetID: uuid.New().String(), ProjectID: "test-proj", Name: "test-ds"}
	store.CreateDataset(context.Background(), ds)

	run := &domain.DatasetRun{
		RunID:      uuid.New().String(),
		DatasetID:  ds.DatasetID,
		EvalRuleID: "rule-1",
		Status:     domain.DatasetRunStatusComplete,
	}
	store.CreateDatasetRun(context.Background(), run)

	item := &domain.DatasetItem{
		ItemID:         uuid.New().String(),
		DatasetID:      ds.DatasetID,
		Input:          "hello",
		ExpectedOutput: "hi",
	}
	store.CreateDatasetItem(context.Background(), item)

	runItem := &domain.DatasetRunItem{
		RunItemID: uuid.New().String(),
		RunID:     run.RunID,
		ItemID:    item.ItemID,
		Score:     8.5,
		Reasoning: "Great match.",
	}
	store.CreateDatasetRunItem(context.Background(), runItem)

	handler := &DatasetRunHandler{
		DatasetStore: store,
		EvalRuleStore: store,
		SessionStore: &FakeSessionStore{projectID: "test-proj"},
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/datasets/"+ds.DatasetID+"/runs/"+run.RunID, nil)
	w := httptest.NewRecorder()
	handler.HandleGetRun(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want %d\nbody: %s", w.Code, http.StatusOK, w.Body.String())
	}

	var resp getRunResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if resp.RunID != run.RunID {
		t.Errorf("run_id: got %q, want %q", resp.RunID, run.RunID)
	}
	if resp.Status != domain.DatasetRunStatusComplete {
		t.Errorf("status: got %q, want %q", resp.Status, domain.DatasetRunStatusComplete)
	}
	if len(resp.Items) != 1 {
		t.Fatalf("items count: got %d, want 1", len(resp.Items))
	}

	itemResp := resp.Items[0]
	if itemResp.ItemID != item.ItemID {
		t.Errorf("item_id: got %q, want %q", itemResp.ItemID, item.ItemID)
	}
	if itemResp.Score != 8.5 {
		t.Errorf("score: got %f, want %f", itemResp.Score, 8.5)
	}
	if itemResp.Reasoning != "Great match." {
		t.Errorf("reasoning: got %q, want %q", itemResp.Reasoning, "Great match.")
	}
}

func TestHandleGetRun_NotFound(t *testing.T) {
	store := fake.NewFakeMetadataStore()

	ds := &domain.Dataset{DatasetID: uuid.New().String(), ProjectID: "test-proj", Name: "test-ds"}
	store.CreateDataset(context.Background(), ds)

	handler := &DatasetRunHandler{
		DatasetStore: store,
		EvalRuleStore: store,
		SessionStore: &FakeSessionStore{projectID: "test-proj"},
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/datasets/"+ds.DatasetID+"/runs/nonexistent", nil)
	w := httptest.NewRecorder()
	handler.HandleGetRun(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestHandleGetRun_AuthRequired(t *testing.T) {
	store := fake.NewFakeMetadataStore()
	handler := &DatasetRunHandler{DatasetStore: store, EvalRuleStore: store}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/datasets/some-id/runs/some-run", nil)
	w := httptest.NewRecorder()
	handler.HandleGetRun(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestHandleGetRun_MethodNotAllowed(t *testing.T) {
	store := fake.NewFakeMetadataStore()
	handler := &DatasetRunHandler{
		DatasetStore: store,
		EvalRuleStore: store,
		SessionStore: &FakeSessionStore{projectID: "test-proj"},
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/datasets/some-id/runs/some-run", nil)
	w := httptest.NewRecorder()
	handler.HandleGetRun(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusMethodNotAllowed)
	}
}

// ---- Tests for HandleGetRunStatus ----

func TestHandleGetRunStatus(t *testing.T) {
	store := fake.NewFakeMetadataStore()

	ds := &domain.Dataset{DatasetID: uuid.New().String(), ProjectID: "test-proj", Name: "test-ds"}
	store.CreateDataset(context.Background(), ds)

	run := &domain.DatasetRun{
		RunID:     uuid.New().String(),
		DatasetID: ds.DatasetID,
		Status:    domain.DatasetRunStatusRunning,
	}
	store.CreateDatasetRun(context.Background(), run)

	handler := &DatasetRunHandler{
		DatasetStore: store,
		EvalRuleStore: store,
		SessionStore: &FakeSessionStore{projectID: "test-proj"},
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/datasets/"+ds.DatasetID+"/runs/"+run.RunID+"/status", nil)
	w := httptest.NewRecorder()
	handler.HandleGetRunStatus(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want %d\nbody: %s", w.Code, http.StatusOK, w.Body.String())
	}

	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if resp["status"] != domain.DatasetRunStatusRunning {
		t.Errorf("status: got %q, want %q", resp["status"], domain.DatasetRunStatusRunning)
	}
}

func TestHandleGetRunStatus_NotFound(t *testing.T) {
	store := fake.NewFakeMetadataStore()
	handler := &DatasetRunHandler{
		DatasetStore: store,
		EvalRuleStore: store,
		SessionStore: &FakeSessionStore{projectID: "test-proj"},
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/datasets/some-id/runs/nonexistent/status", nil)
	w := httptest.NewRecorder()
	handler.HandleGetRunStatus(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusNotFound)
	}
}

// ---- Tests for parseJudgeResponse ----

func TestParseJudgeResponse_Basic(t *testing.T) {
	score, reasoning := parseJudgeResponse("Score: 8.5\nReasoning: Good output.")
	if score != 8.5 {
		t.Errorf("score: got %f, want %f", score, 8.5)
	}
	if reasoning == "" {
		t.Error("reasoning: expected non-empty")
	}
}

func TestParseJudgeResponse_NoScore(t *testing.T) {
	score, reasoning := parseJudgeResponse("The output is okay but could be better.")
	if score != 0.0 {
		t.Errorf("score: got %f, want 0.0", score)
	}
	if reasoning != "The output is okay but could be better." {
		t.Errorf("reasoning: got %q, want %q", reasoning, "The output is okay but could be better.")
	}
}

func TestParseJudgeResponse_CaseInsensitive(t *testing.T) {
	score, _ := parseJudgeResponse("SCORE: 3.0")
	if score != 3.0 {
		t.Errorf("score: got %f, want 3.0", score)
	}
}

func TestParseJudgeResponse_Whitespace(t *testing.T) {
	score, _ := parseJudgeResponse("  score : 6.5  ")
	if score != 6.5 {
		t.Errorf("score: got %f, want 6.5", score)
	}
}

// ---- Helpers ----

// buildTestPromptCache creates a minimal prompt cache with the given prompt version.
func buildTestPromptCache(store metadata.PromptStore, projectID string, rule *domain.EvalRule) *PromptCache {
	cache := &PromptCache{
		PromptStore:  store,
		versionCache: make(map[string]*cacheEntry),
	}
	// Pre-seed the prompt version in the cache.
	key := projectID + "|" + rule.PromptName + "|" + strconv.FormatInt(rule.PromptVersion, 10)
	cache.versionCache[key] = &cacheEntry{
		PromptVersion: &domain.PromptVersion{
			VersionID:   "v1",
			ProjectID:   projectID,
			Name:        rule.PromptName,
			Version:     rule.PromptVersion,
			Template:    "Input: {{input}}\nExpected: {{expected_output}}\nReturn a score.",
			ModelConfig: domain.PromptModelConfig{Model: "gpt-4", Temperature: 0.0, MaxTokens: 256},
		},
	}
	return cache
}
