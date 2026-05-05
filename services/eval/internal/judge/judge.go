package judge

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"text/template"
	"time"

	"github.com/zbloss/lantern/internal/domain"
	"github.com/zbloss/lantern/internal/idgen"
	"github.com/zbloss/lantern/internal/metadata"
)

// JudgeClient defines the interface for calling an LLM judge model.
// This allows injecting a fake LLM client in tests.
type JudgeClient interface {
	Eval(ctx context.Context, input string) (*LLMResponse, error)
}

// LLMResponse is the structured output from a judge LLM call.
type LLMResponse struct {
	Score       float64
	Reasoning   string
	JudgeModel  string
}

// Judge renders a prompt template with span data, calls the judge LLM,
// and returns a structured Score.
type Judge struct {
	client JudgeClient
	store  metadata.Store
}

// New creates a new Judge with an HTTP client for OpenAI-compatible API calls.
func New(store metadata.Store, baseURL, apiKey string) *Judge {
	return &Judge{
		client:  &httpJudgeClient{baseURL: baseURL, apiKey: apiKey, agent: &http.Client{}},
		store:   store,
	}
}

// Evaluate renders the judge prompt for the given EvalJob, calls the judge LLM,
// and returns a structured Score.
func (j *Judge) Evaluate(ctx context.Context, job *domain.EvalJob, span *domain.Span) (*domain.Score, error) {
	// Fetch the judge prompt template.
	prompt, err := j.fetchPrompt(ctx, job)
	if err != nil {
		return nil, fmt.Errorf("judge: fetch prompt: %w", err)
	}

	// Render the template with span data.
	rendered, err := j.renderPrompt(prompt, span)
	if err != nil {
		return nil, fmt.Errorf("judge: render prompt: %w", err)
	}

	// Call the judge LLM.
	resp, err := j.client.Eval(ctx, rendered)
	if err != nil {
		return nil, fmt.Errorf("judge: eval: %w", err)
	}

	scoreID := idgen.Generate()
	return &domain.Score{
		ScoreID:       scoreID,
		SpanID:        span.SpanID,
		TraceID:       span.TraceID,
		ProjectID:     span.ProjectID,
		EvalName:      job.RuleID,
		Value:         resp.Score,
		Reasoning:     resp.Reasoning,
		JudgeModel:    resp.JudgeModel,
		PromptName:    prompt.Name,
		PromptVersion: prompt.Version,
		CreatedAt:     time.Now(),
	}, nil
}

// fetchPrompt retrieves the judge prompt template from the metadata store.
func (j *Judge) fetchPrompt(ctx context.Context, job *domain.EvalJob) (*domain.PromptVersion, error) {
	if job.PromptName != "" && job.PromptVersion > 0 {
		return j.store.GetPromptVersion(ctx, job.ProjectID, job.PromptName, job.PromptVersion)
	}
	// Fallback to the "production" label.
	return j.store.GetPromptByLabel(ctx, job.ProjectID, job.PromptName, "production")
}

// renderPrompt renders the prompt template with span data using text/template.
func (j *Judge) renderPrompt(prompt *domain.PromptVersion, span *domain.Span) (string, error) {
	tmpl, err := template.New("judge").Parse(prompt.Template)
	if err != nil {
		return "", fmt.Errorf("judge: parse template: %w", err)
	}

	data := map[string]any{
		"span_id":         span.SpanID,
		"trace_id":        span.TraceID,
		"model":           span.Model,
		"input":           span.Input,
		"output":          span.Output,
		"input_tokens":    span.InputTokens,
		"output_tokens":   span.OutputTokens,
		"cost_usd":        span.CostUSD,
		"kind":            string(span.Kind),
		"service_name":    span.ServiceName,
		"status_code":     span.StatusCode,
		"status_message":  span.StatusMessage,
		"prompt_name":     span.PromptName,
		"prompt_version":  span.PromptVersion,
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("judge: execute template: %w", err)
	}
	return buf.String(), nil
}

// httpJudgeClient calls an OpenAI-compatible API for LLM-as-a-Judge scoring.
type httpJudgeClient struct {
	baseURL string
	apiKey  string
	agent   *http.Client
}

// markdownFence matches markdown code fences (optional "json" label).
var markdownFence = regexp.MustCompile("```(?:json)?\n?")

// floatRegex matches a floating point number in text.
var floatRegex = regexp.MustCompile(`([0-9]+\.[0-9]+)`)

// Eval calls the OpenAI-compatible API with the rendered prompt and parses
// the numeric score and reasoning from the response.
func (c *httpJudgeClient) Eval(ctx context.Context, input string) (*LLMResponse, error) {
	if c.baseURL == "" {
		return nil, fmt.Errorf("judge: no LLM base URL configured")
	}

	// Build the OpenAI-compatible chat completion request.
	reqBody := map[string]any{
		"model": "judge", // Will use whatever model the judge is configured with
		"messages": []map[string]any{
			{
				"role":    "system",
				"content": "You are an evaluation judge. Analyze the given span and return a JSON object with \"score\" (a float between 0 and 1) and \"reasoning\" (a string explaining your evaluation). Return ONLY the JSON object, no markdown formatting.",
			},
			{
				"role":    "user",
				"content": input,
			},
		},
		"response_format": map[string]string{
			"type": "json_object",
		},
	}

	reqBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("judge: marshal request: %w", err)
	}

	reqURL := c.baseURL + "/v1/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, bytes.NewReader(reqBytes))
	if err != nil {
		return nil, fmt.Errorf("judge: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	resp, err := c.agent.Do(req)
	if err != nil {
		return nil, fmt.Errorf("judge: request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("judge: API error %d: %s", resp.StatusCode, string(body))
	}

	var apiResp struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("judge: read response: %w", err)
	}

	if err := json.Unmarshal(body, &apiResp); err != nil {
		// Fallback: try to parse the raw response as JSON directly.
		return c.parseRawJSON(string(body))
	}

	if len(apiResp.Choices) == 0 {
		return nil, fmt.Errorf("judge: empty choices")
	}

	content := apiResp.Choices[0].Message.Content
	return c.parseScoreResponse(content)
}

// parseScoreResponse parses the judge's JSON response to extract score and reasoning.
func (c *httpJudgeClient) parseScoreResponse(content string) (*LLMResponse, error) {
	// Strip markdown code fences if present.
	content = strings.TrimSpace(content)
	content = markdownFence.ReplaceAllString(content, "")
	content = strings.TrimSpace(content)

	var result struct {
		Score     float64 `json:"score"`
		Reasoning string  `json:"reasoning"`
	}

	if err := json.Unmarshal([]byte(content), &result); err != nil {
		// Last resort: try to extract a number from the response.
		return c.parseFallback(content)
	}

	if result.Score < 0 {
		result.Score = 0
	}
	if result.Score > 1 {
		result.Score = 1
	}

	return &LLMResponse{
		Score:     result.Score,
		Reasoning: result.Reasoning,
		JudgeModel: "unknown", // Will be filled by the judge struct
	}, nil
}

// parseFallback tries to extract a score number from the response text.
func (c *httpJudgeClient) parseFallback(content string) (*LLMResponse, error) {
	// Look for any floating point number in [0,1] range.
	matches := floatRegex.FindStringSubmatch(content)
	if len(matches) >= 2 {
		var score float64
		if _, err := fmt.Sscanf(matches[1], "%f", &score); err == nil {
			return &LLMResponse{
				Score:     score,
				Reasoning: content,
				JudgeModel: "unknown",
			}, nil
		}
	}

	return nil, fmt.Errorf("judge: could not parse score from response: %s", content)
}

// parseRawJSON handles cases where the response format differs from expected.
func (c *httpJudgeClient) parseRawJSON(body string) (*LLMResponse, error) {
	// Try parsing directly as score/reasoning JSON.
	var direct struct {
		Score     float64 `json:"score"`
		Reasoning string  `json:"reasoning"`
	}
	if err := json.Unmarshal([]byte(body), &direct); err == nil {
		return &LLMResponse{
			Score:     direct.Score,
			Reasoning: direct.Reasoning,
			JudgeModel: "unknown",
		}, nil
	}

	return nil, fmt.Errorf("judge: could not parse response: %s", body)
}


