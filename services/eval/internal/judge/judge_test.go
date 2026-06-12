package judge

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/omneval/omneval/internal/config"
	"github.com/omneval/omneval/internal/domain"
)

// fakeResolver implements PromptResolver with a fixed template or error.
type fakeResolver struct {
	template string
	err      error

	// records the last resolve call
	projectID string
	name      string
	version   int64
}

func (f *fakeResolver) Resolve(_ context.Context, projectID, name string, version int64) (string, error) {
	f.projectID, f.name, f.version = projectID, name, version
	if f.err != nil {
		return "", f.err
	}
	return f.template, nil
}

func testJob() *domain.EvalJob {
	return &domain.EvalJob{
		JobID:         "job-1",
		RuleID:        "test-rule",
		SpanID:        "span-1",
		TraceID:       "trace-1",
		ProjectID:     "proj-1",
		PromptName:    "toxicity-judge",
		PromptVersion: 3,
		SpanName:      "chat.completion",
		SpanModel:     "gpt-4o",
		SpanInput:     `[{"role":"user","content":"hello"}]`,
		SpanOutput:    `{"role":"assistant","content":"hi there"}`,
	}
}

func TestEvaluate_Success(t *testing.T) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{
			"choices": [{
				"message": {
					"content": "{\"score\": 0.8, \"reasoning\": \"test reasoning\"}"
				}
			}]
		}`))
	}))
	defer mockServer.Close()

	cfg := &config.Config{
		Eval: config.EvalConfig{
			LLMBaseURL: mockServer.URL,
		},
	}
	resolver := &fakeResolver{template: "Score this span: {{input}} -> {{output}}"}
	j := New(cfg, resolver)

	score, err := j.Evaluate(context.Background(), testJob())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if score.Score != 0.8 {
		t.Errorf("score: got %f, want 0.8", score.Score)
	}
	if score.Reasoning != "test reasoning" {
		t.Errorf("reasoning: got %q, want %q", score.Reasoning, "test reasoning")
	}
	if resolver.projectID != "proj-1" || resolver.name != "toxicity-judge" || resolver.version != 3 {
		t.Errorf("resolver called with (%s, %s, %d), want (proj-1, toxicity-judge, 3)",
			resolver.projectID, resolver.name, resolver.version)
	}
}

// TestEvaluate_RendersSpanData verifies the prompt template is rendered with
// the span's actual data and sent as the user message — and that no
// hardcoded system message is sent.
func TestEvaluate_RendersSpanData(t *testing.T) {
	var gotBody []byte
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"choices": [{"message": {"content": "{\"score\": 1.0}"}}]}`))
	}))
	defer mockServer.Close()

	cfg := &config.Config{Eval: config.EvalConfig{LLMBaseURL: mockServer.URL, LLMModel: "judge-model"}}
	resolver := &fakeResolver{template: "Rate {{span_name}} on model {{model}}.\nInput: {{input}}\nOutput: {{output}}"}
	j := New(cfg, resolver)

	if _, err := j.Evaluate(context.Background(), testJob()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var payload struct {
		Model    string `json:"model"`
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(gotBody, &payload); err != nil {
		t.Fatalf("decode request body: %v", err)
	}
	if len(payload.Messages) != 1 {
		t.Fatalf("expected exactly 1 message (no hardcoded system prompt), got %d", len(payload.Messages))
	}
	msg := payload.Messages[0]
	if msg.Role != "user" {
		t.Errorf("message role: got %q, want user", msg.Role)
	}
	for _, want := range []string{
		"Rate chat.completion on model gpt-4o.",
		`Input: [{"role":"user","content":"hello"}]`,
		`Output: {"role":"assistant","content":"hi there"}`,
	} {
		if !strings.Contains(msg.Content, want) {
			t.Errorf("rendered prompt missing %q; got: %s", want, msg.Content)
		}
	}
}

func TestEvaluate_PromptFetchFailure(t *testing.T) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("LLM must not be called when prompt resolution fails")
	}))
	defer mockServer.Close()

	cfg := &config.Config{Eval: config.EvalConfig{LLMBaseURL: mockServer.URL}}
	resolver := &fakeResolver{err: fmt.Errorf("registry unavailable")}
	j := New(cfg, resolver)

	_, err := j.Evaluate(context.Background(), testJob())
	if err == nil {
		t.Fatal("expected error when prompt fetch fails")
	}
	if !strings.Contains(err.Error(), "resolve prompt") {
		t.Errorf("error %q should mention resolve prompt", err)
	}
}

func TestEvaluate_NoPromptReference(t *testing.T) {
	cfg := &config.Config{Eval: config.EvalConfig{LLMBaseURL: "http://127.0.0.1:1"}}
	j := New(cfg, &fakeResolver{template: "x"})

	job := testJob()
	job.PromptName = ""
	_, err := j.Evaluate(context.Background(), job)
	if err == nil {
		t.Fatal("expected error for job without prompt reference")
	}
	if !strings.Contains(err.Error(), "no prompt reference") {
		t.Errorf("error %q should mention no prompt reference", err)
	}
}

func TestEvaluate_NoResolver(t *testing.T) {
	cfg := &config.Config{Eval: config.EvalConfig{LLMBaseURL: "http://127.0.0.1:1"}}
	j := New(cfg, nil)

	_, err := j.Evaluate(context.Background(), testJob())
	if err == nil {
		t.Fatal("expected error when no resolver is configured")
	}
}

func TestEvaluate_DefaultScoreForNonJSON(t *testing.T) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{
			"choices": [{
				"message": {
					"content": "This span looks good overall."
				}
			}]
		}`))
	}))
	defer mockServer.Close()

	cfg := &config.Config{Eval: config.EvalConfig{LLMBaseURL: mockServer.URL}}
	j := New(cfg, &fakeResolver{template: "Judge {{input}}"})

	score, err := j.Evaluate(context.Background(), testJob())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if score.Score != 1.0 {
		t.Errorf("score: got %f, want 1.0 (default pass)", score.Score)
	}
}

func TestEvaluate_NoBaseURL(t *testing.T) {
	j := New(&config.Config{}, &fakeResolver{template: "x"})
	_, err := j.Evaluate(context.Background(), testJob())
	if err == nil {
		t.Fatal("expected error for empty base URL")
	}
}

func TestEvaluate_APIError(t *testing.T) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("server error"))
	}))
	defer mockServer.Close()

	cfg := &config.Config{Eval: config.EvalConfig{LLMBaseURL: mockServer.URL}}
	j := New(cfg, &fakeResolver{template: "x"})

	_, err := j.Evaluate(context.Background(), testJob())
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
}

func TestEvaluate_EmptyChoices(t *testing.T) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"choices": []}`))
	}))
	defer mockServer.Close()

	cfg := &config.Config{Eval: config.EvalConfig{LLMBaseURL: mockServer.URL}}
	j := New(cfg, &fakeResolver{template: "x"})

	_, err := j.Evaluate(context.Background(), testJob())
	if err == nil {
		t.Fatal("expected error for empty choices")
	}
}
