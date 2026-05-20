package playground

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/omneval/omneval/internal/judge"
)

// Request is the JSON body for POST /api/v1/playground/run.
type Request struct {
	PromptName          string            `json:"prompt_name"`
	Version             *int64            `json:"version,omitempty"`
	Label               *string           `json:"label,omitempty"`
	Variables           map[string]string `json:"variables"`
	ModelOverride       *string           `json:"model_override,omitempty"`
	TemperatureOverride *float64          `json:"temperature_override,omitempty"`
}

// Response is the JSON body returned by POST /api/v1/playground/run.
type Response struct {
	Output       string `json:"output"`
	Model        string `json:"model"`
	InputTokens  int    `json:"input_tokens"`
	OutputTokens int    `json:"output_tokens"`
	DurationMs   int64  `json:"duration_ms"`
}

// LLMClient is the interface for calling an OpenAI-compatible chat completions endpoint.
type LLMClient interface {
	Chat(ctx context.Context, req judge.ChatRequest) (*judge.ChatResponse, error)
}

// HTTPClient implements LLMClient by making actual HTTP requests.
type HTTPClient struct {
	client  *http.Client
	baseURL string
	apiKey  string
}

// NewHTTPClient creates a new HTTP LLM client.
func NewHTTPClient(baseURL, apiKey string) *HTTPClient {
	return &HTTPClient{
		client:  &http.Client{Timeout: 120 * time.Second},
		baseURL: strings.TrimRight(baseURL, "/"),
		apiKey:  apiKey,
	}
}

// Interpolate renders a template with variable substitutions.
// Delegates to the shared judge package implementation.
func Interpolate(template string, variables map[string]string) (string, []string) {
	return judge.Interpolate(template, variables)
}

// Chat sends a chat completion request to the OpenAI-compatible endpoint.
func (c *HTTPClient) Chat(ctx context.Context, req judge.ChatRequest) (*judge.ChatResponse, error) {
	payload := map[string]any{
		"model":    req.Model,
		"messages": req.Messages,
	}
	if req.Temperature > 0 {
		payload["temperature"] = req.Temperature
	}
	if req.MaxTokens > 0 {
		payload["max_tokens"] = req.MaxTokens
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("playground: marshal payload: %w", err)
	}

	url := c.baseURL + "/v1/chat/completions"
	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("playground: create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	resp, err := c.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("playground: send request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("playground: read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("playground: upstream LLM error (status %d): %s", resp.StatusCode, string(body))
	}

	var apiResp judge.ChatResponse
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return nil, fmt.Errorf("playground: decode response: %w", err)
	}

	if len(apiResp.Choices) == 0 {
		return nil, fmt.Errorf("playground: no choices in response")
	}

	return &apiResp, nil
}
