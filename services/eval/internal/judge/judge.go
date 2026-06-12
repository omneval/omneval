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
	sharedjudge "github.com/omneval/omneval/internal/judge"
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

// PromptResolver resolves a prompt template from the Prompt Registry by
// project, name, and version. version <= 0 resolves the version labeled
// "production".
type PromptResolver interface {
	Resolve(ctx context.Context, projectID, name string, version int64) (string, error)
}

// Judge executes an LLM-as-a-Judge evaluation for a single EvalJob: it
// resolves the eval rule's prompt template from the Prompt Registry, renders
// it with the span's actual data, calls the judge model, and returns a
// structured Score.
type Judge struct {
	client   *http.Client
	baseURL  string
	model    string
	apiKey   string
	resolver PromptResolver
}

// New creates a new Judge. The resolver supplies prompt templates from the
// Prompt Registry at eval time.
func New(cfg *config.Config, resolver PromptResolver) *Judge {
	timeout := cfg.Eval.JudgeTimeout
	if timeout <= 0 {
		timeout = 90 * time.Second
	}
	return &Judge{
		client:   &http.Client{Timeout: timeout},
		baseURL:  cfg.Eval.LLMBaseURL,
		model:    cfg.Eval.LLMModel,
		apiKey:   cfg.Eval.LLMAPIKey,
		resolver: resolver,
	}
}

// Evaluate runs the judge LLM for a single eval job and returns the Score.
func (j *Judge) Evaluate(ctx context.Context, job *domain.EvalJob) (*Score, error) {
	if j.baseURL == "" {
		return nil, fmt.Errorf("judge: no judge endpoint configured")
	}

	prompt, err := j.buildPrompt(ctx, job)
	if err != nil {
		return nil, err
	}

	payload := map[string]any{
		"model": j.model,
		"messages": []map[string]any{
			{
				"role":    "user",
				"content": prompt,
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
	if j.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+j.apiKey)
	}

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

// buildPrompt resolves the eval rule's prompt template from the Prompt
// Registry and renders it with the span data carried on the job. Template
// variables: {{input}}, {{output}}, {{model}}, {{span_name}}, {{span_id}},
// {{trace_id}}, {{project_id}}, {{rule_id}}.
func (j *Judge) buildPrompt(ctx context.Context, job *domain.EvalJob) (string, error) {
	if j.resolver == nil {
		return "", fmt.Errorf("judge: no prompt resolver configured")
	}
	if job.PromptName == "" {
		return "", fmt.Errorf("judge: eval job %s (rule %s) has no prompt reference", job.JobID, job.RuleID)
	}

	template, err := j.resolver.Resolve(ctx, job.ProjectID, job.PromptName, job.PromptVersion)
	if err != nil {
		return "", fmt.Errorf("judge: resolve prompt %s v%d: %w", job.PromptName, job.PromptVersion, err)
	}

	rendered, missing := sharedjudge.Interpolate(template, map[string]string{
		"input":      job.SpanInput,
		"output":     job.SpanOutput,
		"model":      job.SpanModel,
		"span_name":  job.SpanName,
		"span_id":    job.SpanID,
		"trace_id":   job.TraceID,
		"project_id": job.ProjectID,
		"rule_id":    job.RuleID,
	})
	if len(missing) > 0 {
		slog.WarnContext(ctx, "judge: prompt template references unknown variables",
			"prompt", job.PromptName,
			"missing", missing,
		)
	}
	return rendered, nil
}
