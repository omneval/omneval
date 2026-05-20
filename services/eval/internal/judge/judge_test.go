package judge

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/omneval/omneval/internal/domain"
	"github.com/omneval/omneval/internal/config"
)

func TestEvaluate_Success(t *testing.T) {
	// Create a mock server that returns a proper LLM response.
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
	j := New(cfg)

	job := &domain.EvalJob{
		JobID:    "job-1",
		RuleID:   "test-rule",
		SpanID:   "span-1",
		TraceID:  "trace-1",
		ProjectID: "proj-1",
	}

	score, err := j.Evaluate(context.Background(), job)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if score.Score != 0.8 {
		t.Errorf("score: got %f, want 0.8", score.Score)
	}
	if score.Reasoning != "test reasoning" {
		t.Errorf("reasoning: got %q, want %q", score.Reasoning, "test reasoning")
	}
}

func TestEvaluate_DefaultScoreForNonJSON(t *testing.T) {
	// Create a mock server that returns a plain text response.
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

	cfg := &config.Config{
		Eval: config.EvalConfig{
			LLMBaseURL: mockServer.URL,
		},
	}
	j := New(cfg)

	job := &domain.EvalJob{
		JobID: "job-1", RuleID: "test-rule",
		SpanID: "span-1", TraceID: "trace-1", ProjectID: "proj-1",
	}

	score, err := j.Evaluate(context.Background(), job)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if score.Score != 1.0 {
		t.Errorf("score: got %f, want 1.0 (default pass)", score.Score)
	}
}

func TestEvaluate_NoBaseURL(t *testing.T) {
	j := New(&config.Config{})
	job := &domain.EvalJob{JobID: "job-1", RuleID: "test-rule", SpanID: "span-1", TraceID: "trace-1", ProjectID: "proj-1"}
	_, err := j.Evaluate(context.Background(), job)
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

	cfg := &config.Config{
		Eval: config.EvalConfig{
			LLMBaseURL: mockServer.URL,
		},
	}
	j := New(cfg)

	job := &domain.EvalJob{JobID: "job-1", RuleID: "test-rule", SpanID: "span-1", TraceID: "trace-1", ProjectID: "proj-1"}
	_, err := j.Evaluate(context.Background(), job)
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

	cfg := &config.Config{
		Eval: config.EvalConfig{
			LLMBaseURL: mockServer.URL,
		},
	}
	j := New(cfg)

	job := &domain.EvalJob{JobID: "job-1", RuleID: "test-rule", SpanID: "span-1", TraceID: "trace-1", ProjectID: "proj-1"}
	_, err := j.Evaluate(context.Background(), job)
	if err == nil {
		t.Fatal("expected error for empty choices")
	}
}
