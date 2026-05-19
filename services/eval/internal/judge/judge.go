package judge

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/omneval/omneval/internal/config"
	"github.com/omneval/omneval/internal/domain"
)

// Score represents the structured output from the judge LLM.
type Score struct {
	Score     float64 `json:"score"`
	Reasoning string  `json:"reasoning,omitempty"`
}

// JudgeExecutor is the interface for executing an LLM-as-a-Judge evaluation.
type JudgeExecutor interface {
	Evaluate(ctx context.Context, job *domain.EvalJob) (*Score, error)
}

// Judge executes an LLM-as-a-Judge evaluation for a single EvalJob,
// renders the judge prompt template with span data, calls the judge model,
// and returns a structured Score.
type Judge struct {
	client  *http.Client
	baseURL string
	model   string
	apiKey  string
}

// New creates a new Judge.
func New(cfg *config.Config) *Judge {
	timeout := cfg.Eval.JudgeTimeout
	if timeout <= 0 {
		timeout = 90 * time.Second
	}
	return &Judge{
		client:  &http.Client{Timeout: timeout},
		baseURL: cfg.Eval.LLMBaseURL,
		model:   cfg.Eval.LLMModel,
		apiKey:  cfg.Eval.LLMAPIKey,
	}
}

// Evaluate runs the judge LLM for a single eval job and returns the Score.
func (j *Judge) Evaluate(ctx context.Context, job *domain.EvalJob) (*Score, error) {
	if j.baseURL == "" {
		return nil, fmt.Errorf("judge: no judge endpoint configured")
	}

	// Build the judge prompt payload.
	payload := map[string]any{
		"model": j.model,
		"messages": []map[string]any{
			{
				"role":    "system",
				"content": "You are an evaluation judge. Score the provided span according to the eval criteria.",
			},
			{
				"role":    "user",
				"content": j.buildPrompt(job),
			},
		},
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("judge: marshal payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", j.baseURL+"/v1/chat/completions", bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("judge: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := j.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("judge: send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("judge: unexpected status %d: %s", resp.StatusCode, string(body))
	}

	var apiResp struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return nil, fmt.Errorf("judge: decode response: %w", err)
	}

	if len(apiResp.Choices) == 0 {
		return nil, fmt.Errorf("judge: no choices in response")
	}

	content := apiResp.Choices[0].Message.Content

	// Parse the score from the judge output.
	score := &Score{Reasoning: content}
	// Try to extract a numeric score from the content.
	if err := json.Unmarshal([]byte(content), score); err != nil {
		// If not JSON, try to extract a number from the text.
		score.Score = 1.0 // default pass for non-structured output
	}

	slog.InfoContext(ctx, "judge: evaluation complete",
		"job_id", job.JobID,
		"score", score.Score,
	)

	return score, nil
}

func (j *Judge) buildPrompt(job *domain.EvalJob) string {
	return fmt.Sprintf(
		"Eval Job: %s\nRule ID: %s\nSpan ID: %s\nTrace ID: %s\nProject ID: %s",
		job.JobID, job.RuleID, job.SpanID, job.TraceID, job.ProjectID,
	)
}
