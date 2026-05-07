package playground

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/zbloss/lantern/internal/domain"
	"github.com/zbloss/lantern/services/query/internal/handler"
)

// Request is the JSON body for POST /api/v1/playground/run.
type Request struct {
	PromptName       string            `json:"prompt_name"`
	Version          *int64            `json:"version,omitempty"`
	Label            *string           `json:"label,omitempty"`
	Variables        map[string]string `json:"variables"`
	ModelOverride    *string           `json:"model_override,omitempty"`
	TemperatureOverride *float64       `json:"temperature_override,omitempty"`
}

// Response is the JSON body returned by POST /api/v1/playground/run.
type Response struct {
	Output       string  `json:"output"`
	Model        string  `json:"model"`
	InputTokens  int     `json:"input_tokens"`
	OutputTokens int     `json:"output_tokens"`
	DurationMs   int64   `json:"duration_ms"`
}

// LLMClient is the interface for calling an OpenAI-compatible chat completions endpoint.
type LLMClient interface {
	Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error)
}

// ChatRequest is the request sent to the OpenAI-compatible chat completions endpoint.
type ChatRequest struct {
	Model       string           `json:"model"`
	Messages    []ChatMessage    `json:"messages"`
	Temperature float64          `json:"temperature,omitempty"`
	MaxTokens   int              `json:"max_tokens,omitempty"`
}

// ChatMessage represents a single message in a chat conversation.
type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ChatResponse is the response from the OpenAI-compatible chat completions endpoint.
type ChatResponse struct {
	Choices []Choice `json:"choices"`
	Usage   Usage    `json:"usage"`
}

// Choice is a single choice in the chat response.
type Choice struct {
	Message ChatMessage `json:"message"`
}

// Usage contains token usage information.
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
}

// HTTPClient implements LLMClient by making actual HTTP requests.
type HTTPClient struct {
	client *http.Client
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

// Chat sends a chat completion request to the OpenAI-compatible endpoint.
func (c *HTTPClient) Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
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

	var apiResp ChatResponse
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return nil, fmt.Errorf("playground: decode response: %w", err)
	}

	if len(apiResp.Choices) == 0 {
		return nil, fmt.Errorf("playground: no choices in response")
	}

	return &apiResp, nil
}

// variableRegex matches {{variable}} patterns, allowing optional whitespace inside.
var variableRegex = regexp.MustCompile(`\{\{\s*([a-zA-Z_][a-zA-Z0-9_]*)\s*\}\}`)

// Interpolate renders a template with variable substitutions.
// Returns the interpolated string and a list of missing variable names.
func Interpolate(template string, variables map[string]string) (string, []string) {
	var missing []string

	result := variableRegex.ReplaceAllStringFunc(template, func(match string) string {
		// The regex group captures the variable name (without whitespace).
		// Extract the captured group from the match.
		submatches := variableRegex.FindStringSubmatch(match)
		name := submatches[1]

		val, ok := variables[name]
		if !ok {
			missing = append(missing, name)
			return match // leave uninterpolated
		}
		return val
	})

	return result, missing
}

// buildMessages constructs OpenAI-compatible messages from an interpolated prompt template.
// The template is assumed to already be interpolated with variable values.
// We wrap it in a system + user message structure.
func buildMessages(interpolatedTemplate string) []ChatMessage {
	return []ChatMessage{
		{
			Role:    "system",
			Content: "You are a helpful assistant.",
		},
		{
			Role:    "user",
			Content: interpolatedTemplate,
		},
	}
}

// resolvePrompt fetches a prompt version from the cache, resolving by version or label.
func resolvePrompt(
	cache *handler.PromptCache,
	projectID string,
	name string,
	version *int64,
	label *string,
) (*domain.PromptVersion, error) {
	if version != nil && *version > 0 {
		return cache.GetVersion(nil, projectID, name, *version)
	}
	if label != nil && *label != "" {
		return cache.GetLabel(nil, projectID, name, *label)
	}
	return nil, fmt.Errorf("playground: provide version or label")
}
