package judge

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/zbloss/lantern/internal/domain"
)

func TestParseScoreResponse_JSON(t *testing.T) {
	resp := `{ "score": 0.85, "reasoning": "Good output" }`
	result, err := (&httpJudgeClient{}).parseScoreResponse(resp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Score != 0.85 {
		t.Errorf("score: got %f, want 0.85", result.Score)
	}
	if result.Reasoning != "Good output" {
		t.Errorf("reasoning: got %q, want %q", result.Reasoning, "Good output")
	}
}

func TestParseScoreResponse_MarkdownFenced(t *testing.T) {
	resp := "```json\n{\"score\": 0.9, \"reasoning\": \"Excellent\"}\n```"
	result, err := (&httpJudgeClient{}).parseScoreResponse(resp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Score != 0.9 {
		t.Errorf("score: got %f, want 0.9", result.Score)
	}
	if result.Reasoning != "Excellent" {
		t.Errorf("reasoning: got %q, want %q", result.Reasoning, "Excellent")
	}
}

func TestParseScoreResponse_ClampHigh(t *testing.T) {
	resp := `{ "score": 1.5, "reasoning": "over 1" }`
	result, err := (&httpJudgeClient{}).parseScoreResponse(resp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Score != 1.0 {
		t.Errorf("score: got %f, want 1.0 (clamped)", result.Score)
	}
}

func TestParseScoreResponse_ClampLow(t *testing.T) {
	resp := `{ "score": -0.2, "reasoning": "under 0" }`
	result, err := (&httpJudgeClient{}).parseScoreResponse(resp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Score != 0.0 {
		t.Errorf("score: got %f, want 0.0 (clamped)", result.Score)
	}
}

func TestParseScoreResponse_FallbackNumber(t *testing.T) {
	resp := `The span is good. Score: 0.75 out of 1.0`
	result, err := (&httpJudgeClient{}).parseScoreResponse(resp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Score != 0.75 {
		t.Errorf("score: got %f, want 0.75", result.Score)
	}
	if result.Reasoning != resp {
		t.Errorf("reasoning: got %q, want original", result.Reasoning)
	}
}

func TestParseScoreResponse_FallbackNoNumber(t *testing.T) {
	resp := `This is just text with no score.`
	_, err := (&httpJudgeClient{}).parseScoreResponse(resp)
	if err == nil {
		t.Fatal("expected error for unparseable response")
	}
}

func TestHTTPJudgeClient_Eval_Success(t *testing.T) {
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

	client := &httpJudgeClient{baseURL: mockServer.URL, agent: mockServer.Client()}
	result, err := client.Eval(context.Background(), "test input")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Score != 0.8 {
		t.Errorf("score: got %f, want 0.8", result.Score)
	}
	if result.Reasoning != "test reasoning" {
		t.Errorf("reasoning: got %q, want %q", result.Reasoning, "test reasoning")
	}
}

func TestHTTPJudgeClient_Eval_AuthHeader(t *testing.T) {
	var receivedAuth string
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"choices": [{"message": {"content": "{\"score\": 1, \"reasoning\": \"ok\"}"}}]}`))
	}))
	defer mockServer.Close()

	client := &httpJudgeClient{baseURL: mockServer.URL, apiKey: "test-key", agent: mockServer.Client()}
	_, err := client.Eval(context.Background(), "test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if receivedAuth != "Bearer test-key" {
		t.Errorf("auth header: got %q, want %q", receivedAuth, "Bearer test-key")
	}
}

func TestHTTPJudgeClient_Eval_NoBaseURL(t *testing.T) {
	client := &httpJudgeClient{baseURL: ""}
	_, err := client.Eval(context.Background(), "test")
	if err == nil {
		t.Fatal("expected error for empty base URL")
	}
}

func TestHTTPJudgeClient_Eval_APIError(t *testing.T) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("server error"))
	}))
	defer mockServer.Close()

	client := &httpJudgeClient{baseURL: mockServer.URL, agent: mockServer.Client()}
	_, err := client.Eval(context.Background(), "test")
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
}

func TestRenderPrompt_SpansWithPrompt(t *testing.T) {
	j := &Judge{}
	span := &domain.Span{
		SpanID: "span-1", TraceID: "trace-1", ProjectID: "proj-1",
		Model: "gpt-4o", Input: "Hello", Output: "Hi there!",
		InputTokens: 10, OutputTokens: 5, CostUSD: 0.01,
		Kind: "llm", ServiceName: "my-service",
		PromptName:    "test-prompt",
		PromptVersion: 1,
		StartTime:     time.Now(),
		EndTime:       time.Now().Add(time.Second),
	}
	prompt := &domain.PromptVersion{
		Name:      "test-prompt",
		Version:   1,
		Template:  "Evaluate span {{.span_id}}. Input: {{.input}}, Output: {{.output}}",
		CreatedAt: time.Now(),
	}
	rendered, err := j.renderPrompt(prompt, span)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(rendered, "span-1") {
		t.Errorf("rendered should contain span_id: %s", rendered)
	}
	if !strings.Contains(rendered, "Hello") {
		t.Errorf("rendered should contain input: %s", rendered)
	}
	if !strings.Contains(rendered, "Hi there!") {
		t.Errorf("rendered should contain output: %s", rendered)
	}
}

func TestRenderPrompt_InvalidTemplate(t *testing.T) {
	j := &Judge{}
	span := &domain.Span{SpanID: "span-1", TraceID: "trace-1"}
	prompt := &domain.PromptVersion{Template: "{{.invalid_syntax["}
	_, err := j.renderPrompt(prompt, span)
	if err == nil {
		t.Fatal("expected error for invalid template")
	}
}
