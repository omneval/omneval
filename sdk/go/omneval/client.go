package omneval

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/omneval/omneval/internal/domain"
)

// Client is an HTTP client for prompt fetch and manual score writes.
// Prompt responses are cached client-side: version lookups use a map with
// no TTL (immutable); label lookups use a 30-second TTL.
type Client struct {
	baseURL string
	apiKey  string
	http    *http.Client

	// Label cache: key = name + "|" + label, value = cacheEntry.
	labelMu    sync.RWMutex
	labelCache map[string]*cacheEntry

	// Version cache: key = name + "|" + version, value = template string.
	versionMu    sync.RWMutex
	versionCache map[string]string
}

type cacheEntry struct {
	Template  string
	ExpiresAt time.Time
}

// NewClient creates a Client targeting the given Query API base URL.
func NewClient(baseURL, apiKey string) *Client {
	return &Client{
		baseURL:      baseURL,
		apiKey:       apiKey,
		http:         &http.Client{Timeout: 10 * time.Second},
		labelCache:   make(map[string]*cacheEntry),
		versionCache: make(map[string]string),
	}
}

// GetPrompt resolves a prompt by name and label (default "production").
// Returns the template string and model config.
func (c *Client) GetPrompt(name, label string) (template string, err error) {
	if label == "" {
		label = "production"
	}

	// Check label cache first.
	cacheKey := name + "|" + label
	c.labelMu.RLock()
	if entry, ok := c.labelCache[cacheKey]; ok && time.Now().Before(entry.ExpiresAt) {
		c.labelMu.RUnlock()
		return entry.Template, nil
	}
	c.labelMu.RUnlock()

	// Cache miss or expired — fetch from server.
	pv, err := c.getPromptFromServer(name, label)
	if err != nil {
		return "", err
	}

	// Store in label cache with 30-second TTL.
	c.labelMu.Lock()
	c.labelCache[cacheKey] = &cacheEntry{
		Template:  pv.Template,
		ExpiresAt: time.Now().Add(30 * time.Second),
	}
	c.labelMu.Unlock()

	return pv.Template, nil
}

// GetPromptVersion resolves a prompt by name and explicit version number.
func (c *Client) GetPromptVersion(name string, version int64) (template string, err error) {
	// Check version cache first.
	cacheKey := name + "|" + fmt.Sprintf("%d", version)
	c.versionMu.RLock()
	if tmpl, ok := c.versionCache[cacheKey]; ok {
		c.versionMu.RUnlock()
		return tmpl, nil
	}
	c.versionMu.RUnlock()

	// Cache miss — fetch from server.
	pv, err := c.getPromptFromServer(name, "", version)
	if err != nil {
		return "", err
	}

	// Store in version cache (no TTL — immutable).
	c.versionMu.Lock()
	c.versionCache[cacheKey] = pv.Template
	c.versionMu.Unlock()

	return pv.Template, nil
}

// WriteScore submits a manual score for a span.
func (c *Client) WriteScore(spanID, evalName string, value float64, reasoning string) error {
	if spanID == "" {
		return fmt.Errorf("span_id is required")
	}

	// Generate a trace ID (required by the API).
	traceID := generateTraceID()

	req := domain.ScoreRequest{
		SpanID:    spanID,
		TraceID:   traceID,
		EvalName:  evalName,
		Value:     value,
		Reasoning: reasoning,
	}

	url := c.baseURL + "/api/v1/scores"
	body, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal score: %w", err)
	}

	httpReq, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("write score: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		httpReq.Header.Set("X-API-Key", c.apiKey)
	}
	resp, err := c.http.Do(httpReq)
	if err != nil {
		return fmt.Errorf("write score: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("write score: %s: %s", resp.Status, string(respBody))
	}

	return nil
}

// PromptListItem is a prompt summary returned by GET /api/v1/prompts.
type PromptListItem struct {
	Name          string           `json:"name"`
	LatestVersion int64            `json:"latest_version"`
	Labels        map[string]int64 `json:"labels"`
}

// CreatePromptRequest is the body sent to POST /api/v1/prompts.
type CreatePromptRequest struct {
	Name        string                    `json:"name"`
	Template    string                    `json:"template"`
	ModelConfig *domain.PromptModelConfig `json:"model_config,omitempty"`
	Label       string                    `json:"label,omitempty"`
}

// EvalRuleSummary is an eval rule returned by the API.
type EvalRuleSummary struct {
	RuleID        string  `json:"rule_id"`
	ProjectID     string  `json:"project_id"`
	Name          string  `json:"name"`
	JudgeModel    string  `json:"judge_model"`
	PromptName    string  `json:"prompt_name"`
	PromptVersion int64   `json:"prompt_version"`
	SampleRate    float64 `json:"sample_rate"`
	Enabled       bool    `json:"enabled"`
}

// CreateEvalRuleRequest is the body sent to POST /api/v1/eval-rules.
type CreateEvalRuleRequest struct {
	Name          string                 `json:"name"`
	PromptName    string                 `json:"prompt_name"`
	PromptVersion int64                  `json:"prompt_version,omitempty"`
	PromptLabel   string                 `json:"prompt_label,omitempty"`
	JudgeModel    string                 `json:"judge_model,omitempty"`
	Filter        map[string]interface{} `json:"filter"`
	SampleRate    float64                `json:"sample_rate"`
}

// CreatePrompt creates a new prompt version via POST /api/v1/prompts.
// modelConfig is optional; pass nil to omit it.
// Returns the created prompt version.
func (c *Client) CreatePrompt(name, template string, modelConfig *domain.PromptModelConfig) (domain.PromptVersionJSON, error) {
	if name == "" {
		return domain.PromptVersionJSON{}, fmt.Errorf("name is required")
	}
	if template == "" {
		return domain.PromptVersionJSON{}, fmt.Errorf("template is required")
	}

	req := CreatePromptRequest{
		Name:        name,
		Template:    template,
		ModelConfig: modelConfig,
	}

	url := c.baseURL + "/api/v1/prompts"
	body, err := json.Marshal(req)
	if err != nil {
		return domain.PromptVersionJSON{}, fmt.Errorf("create prompt: marshal: %w", err)
	}

	httpReq, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return domain.PromptVersionJSON{}, fmt.Errorf("create prompt: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		httpReq.Header.Set("X-API-Key", c.apiKey)
	}

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return domain.PromptVersionJSON{}, fmt.Errorf("create prompt: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		respBody, _ := io.ReadAll(resp.Body)
		return domain.PromptVersionJSON{}, fmt.Errorf("create prompt: %s: %s", resp.Status, string(respBody))
	}

	var pv domain.PromptVersionJSON
	if err := json.NewDecoder(resp.Body).Decode(&pv); err != nil {
		return domain.PromptVersionJSON{}, fmt.Errorf("create prompt: decode: %w", err)
	}
	return pv, nil
}

// ListPrompts fetches all prompts for the project via GET /api/v1/prompts.
// Returns a list of PromptListItem summaries.
func (c *Client) ListPrompts() ([]PromptListItem, error) {
	url := c.baseURL + "/api/v1/prompts"

	httpReq, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("list prompts: %w", err)
	}
	if c.apiKey != "" {
		httpReq.Header.Set("X-API-Key", c.apiKey)
	}

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("list prompts: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("list prompts: %s: %s", resp.Status, string(respBody))
	}

	var items []PromptListItem
	if err := json.NewDecoder(resp.Body).Decode(&items); err != nil {
		return nil, fmt.Errorf("list prompts: decode: %w", err)
	}
	return items, nil
}

// SetPromptLabel assigns a label to a specific prompt version via
// PUT /api/v1/prompts/{name}/labels/{label}.
func (c *Client) SetPromptLabel(name, label string, version int64) error {
	if name == "" {
		return fmt.Errorf("name is required")
	}

	url := fmt.Sprintf("%s/api/v1/prompts/%s/labels/%s", c.baseURL, name, label)
	payload := map[string]int64{"version": version}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("set prompt label: marshal: %w", err)
	}

	httpReq, err := http.NewRequest(http.MethodPut, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("set prompt label: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		httpReq.Header.Set("X-API-Key", c.apiKey)
	}

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return fmt.Errorf("set prompt label: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("set prompt label: %s: %s", resp.Status, string(respBody))
	}
	return nil
}

// ListEvalRules fetches all eval rules for the project via GET /api/v1/eval-rules.
func (c *Client) ListEvalRules() ([]EvalRuleSummary, error) {
	url := c.baseURL + "/api/v1/eval-rules"

	httpReq, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("list eval rules: %w", err)
	}
	if c.apiKey != "" {
		httpReq.Header.Set("X-API-Key", c.apiKey)
	}

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("list eval rules: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("list eval rules: %s: %s", resp.Status, string(respBody))
	}

	var data struct {
		Rules []EvalRuleSummary `json:"rules"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, fmt.Errorf("list eval rules: decode: %w", err)
	}
	if data.Rules == nil {
		return []EvalRuleSummary{}, nil
	}
	return data.Rules, nil
}

// CreateEvalRule creates an eval rule via POST /api/v1/eval-rules.
// opts is optional; pass nil to use defaults.
func (c *Client) CreateEvalRule(name, promptName string, opts *CreateEvalRuleRequest) (EvalRuleSummary, error) {
	if name == "" {
		return EvalRuleSummary{}, fmt.Errorf("name is required")
	}
	if promptName == "" {
		return EvalRuleSummary{}, fmt.Errorf("prompt_name is required")
	}

	req := CreateEvalRuleRequest{
		Name:       name,
		PromptName: promptName,
		Filter:     map[string]interface{}{},
		SampleRate: 1.0,
	}
	if opts != nil {
		if opts.JudgeModel != "" {
			req.JudgeModel = opts.JudgeModel
		}
		if opts.Filter != nil {
			req.Filter = opts.Filter
		}
		if opts.SampleRate > 0 {
			req.SampleRate = opts.SampleRate
		}
		if opts.PromptVersion > 0 {
			req.PromptVersion = opts.PromptVersion
		}
		if opts.PromptLabel != "" {
			req.PromptLabel = opts.PromptLabel
		}
	}

	url := c.baseURL + "/api/v1/eval-rules"
	body, err := json.Marshal(req)
	if err != nil {
		return EvalRuleSummary{}, fmt.Errorf("create eval rule: marshal: %w", err)
	}

	httpReq, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return EvalRuleSummary{}, fmt.Errorf("create eval rule: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		httpReq.Header.Set("X-API-Key", c.apiKey)
	}

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return EvalRuleSummary{}, fmt.Errorf("create eval rule: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		respBody, _ := io.ReadAll(resp.Body)
		return EvalRuleSummary{}, fmt.Errorf("create eval rule: %s: %s", resp.Status, string(respBody))
	}

	var rule EvalRuleSummary
	if err := json.NewDecoder(resp.Body).Decode(&rule); err != nil {
		return EvalRuleSummary{}, fmt.Errorf("create eval rule: decode: %w", err)
	}
	return rule, nil
}

// getPromptFromServer fetches a prompt version from the server API.
// If label is provided, uses label resolution; if version is > 0, uses version.
func (c *Client) getPromptFromServer(name string, label string, version ...int64) (*domain.PromptVersion, error) {
	var url string
	if len(version) > 0 && version[0] > 0 {
		url = fmt.Sprintf("%s/api/v1/prompts/%s?version=%d", c.baseURL, name, version[0])
	} else {
		if label == "" {
			label = "production"
		}
		url = fmt.Sprintf("%s/api/v1/prompts/%s?label=%s", c.baseURL, name, label)
	}

	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("get prompt: %w", err)
	}
	if c.apiKey != "" {
		req.Header.Set("X-API-Key", c.apiKey)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("get prompt: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("prompt not found: %s", name)
	}
	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("get prompt: %s: %s", resp.Status, string(respBody))
	}

	var pv domain.PromptVersion
	if err := json.NewDecoder(resp.Body).Decode(&pv); err != nil {
		return nil, fmt.Errorf("decode prompt: %w", err)
	}

	return &pv, nil
}
