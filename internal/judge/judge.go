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
	"time"
)

// LLMClient is the interface for calling an OpenAI-compatible chat completions endpoint.
type LLMClient interface {
	Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error)
}

// ChatRequest is the request sent to the OpenAI-compatible chat completions endpoint.
type ChatRequest struct {
	Model       string        `json:"model"`
	Messages    []ChatMessage `json:"messages"`
	Temperature float64       `json:"temperature,omitempty"`
	MaxTokens   int           `json:"max_tokens,omitempty"`
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
		return nil, fmt.Errorf("judge: marshal payload: %w", err)
	}

	url := c.baseURL + "/v1/chat/completions"
	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("judge: create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	resp, err := c.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("judge: send request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("judge: read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("judge: upstream LLM error (status %d): %s", resp.StatusCode, string(body))
	}

	var apiResp ChatResponse
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return nil, fmt.Errorf("judge: decode response: %w", err)
	}

	if len(apiResp.Choices) == 0 {
		return nil, fmt.Errorf("judge: no choices in response")
	}

	return &apiResp, nil
}

// variableRegex matches {{variable}} patterns, allowing optional whitespace inside.
var variableRegex = regexp.MustCompile(`\{\{\s*([a-zA-Z_][a-zA-Z0-9_]*)\s*\}\}`)

// Interpolate renders a template with variable substitutions.
// Returns the interpolated string and a list of missing variable names.
func Interpolate(template string, variables map[string]string) (string, []string) {
	var missing []string

	// Collect all match indices once, so the callback can look up data without re-parsing.
	matches := variableRegex.FindAllStringSubmatchIndex(template, -1)
	var result strings.Builder
	result.Grow(len(template))

	prevEnd := 0
	for _, match := range matches {
		nameStart, nameEnd := match[2], match[3]
		name := template[nameStart:nameEnd]
		fullStart, fullEnd := match[0], match[1]

		// Write the literal text before this match.
		result.WriteString(template[prevEnd:fullStart])

		val, ok := variables[name]
		if !ok {
			missing = append(missing, name)
			result.WriteString(template[fullStart:fullEnd]) // leave uninterpolated
		} else {
			result.WriteString(val)
		}
		prevEnd = fullEnd
	}

	// Write remaining text after the last match.
	result.WriteString(template[prevEnd:])

	return result.String(), missing
}

// BuildJudgeMessages constructs OpenAI-compatible messages for judge/eval LLM calls.
func BuildJudgeMessages(interpolatedTemplate string) []ChatMessage {
	return []ChatMessage{
		{
			Role:    "system",
			Content: "You are a helpful assistant. Evaluate the given input against the expected output and return a numeric score.",
		},
		{
			Role:    "user",
			Content: interpolatedTemplate,
		},
	}
}
