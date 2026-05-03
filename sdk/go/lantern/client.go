package lantern

// Client is an HTTP client for prompt fetch and manual score writes.
// Prompt responses are cached client-side: version lookups use an LRU with
// no TTL (immutable); label lookups use a 30-second TTL.
type Client struct {
	baseURL string
	apiKey  string
}

// NewClient creates a Client targeting the given Query API base URL.
func NewClient(baseURL, apiKey string) *Client {
	return &Client{baseURL: baseURL, apiKey: apiKey}
}

// GetPrompt resolves a prompt by name and label (default "production").
// Returns the template string and model config.
func (c *Client) GetPrompt(name, label string) (template string, err error) {
	panic("not implemented")
}

// GetPromptVersion resolves a prompt by name and explicit version number.
func (c *Client) GetPromptVersion(name string, version int64) (template string, err error) {
	panic("not implemented")
}

// WriteScore submits a manual score for a span.
func (c *Client) WriteScore(spanID, evalName string, value float64, reasoning string) error {
	panic("not implemented")
}
