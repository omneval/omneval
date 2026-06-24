package pricing

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

//go:generate go run -tags=embedgen .

var (
	// bundledPricing is embedded from model_prices_and_context_window.json.
	bundledPricing []byte

	liteLLMPricingURL = "https://raw.githubusercontent.com/BerriAI/litellm/main/model_prices_and_context_window.json"

	// defaultBundledPricing is the bundled fallback loaded once at init time.
	defaultBundledPricing *Table
	defaultBundleOnce     sync.Once
)

// ModelOverride is a per-model price override expressed in USD per million tokens
// (the human-readable convention). Converted to per-token at load time.
type ModelOverride struct {
	InputPerMillion  float64
	OutputPerMillion float64
}

// Table holds per-model token pricing. Prices are stored as USD per token.
type Table struct {
	mu         sync.RWMutex
	inputCost  map[string]float64
	outputCost map[string]float64
}

// InitBundledPricing loads the bundled pricing snapshot into defaultBundledPricing.
func InitBundledPricing() {
	defaultBundleOnce.Do(func() {
		if len(bundledPricing) == 0 {
			return
		}
		defaultBundledPricing = NewTableFromBytes(bundledPricing)
	})
}

// GetDefaultBundled returns the bundled pricing table, loading it if not yet done.
// This ensures the bundled snapshot is always available as a fallback.
func GetDefaultBundled() *Table {
	defaultBundleOnce.Do(func() {
		if len(bundledPricing) > 0 {
			defaultBundledPricing = NewTableFromBytes(bundledPricing)
		}
	})
	return defaultBundledPricing
}

// NewTableFromBytes parses a LiteLLM-format pricing JSON and returns a Table.
func NewTableFromBytes(data []byte) *Table {
	t := &Table{
		inputCost:  make(map[string]float64),
		outputCost: make(map[string]float64),
	}
	// The LiteLLM JSON has objects with input_cost_per_token and output_cost_per_token.
	var raw map[string]map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return t
	}
	for model, fields := range raw {
		if in, ok := fields["input_cost_per_token"]; ok {
			var val float64
			if err := json.Unmarshal(in, &val); err == nil && val != 0 {
				t.inputCost[model] = val
			}
		}
		if out, ok := fields["output_cost_per_token"]; ok {
			var val float64
			if err := json.Unmarshal(out, &val); err == nil && val != 0 {
				t.outputCost[model] = val
			}
		}
	}
	return t
}

// Fetch downloads LiteLLM's model pricing JSON and returns a populated Table.
// Falls back to the bundled snapshot if the live fetch fails. overrides maps
// model name to per-million-token prices that take precedence over LiteLLM data.
func Fetch(overrides map[string]ModelOverride) (*Table, error) {
	// Try the live fetch first (with timeout).
	table, err := fetchRemote()
	if err != nil {
		// Fall back to bundled.
		bundled := GetDefaultBundled()
		if bundled == nil {
			return nil, fmt.Errorf("pricing: live fetch failed and no bundled pricing available: %w", err)
		}
		table = bundled
	}

	// Apply overrides (they take precedence).
	if len(overrides) > 0 {
		table.ApplyOverrides(overrides)
	}

	return table, nil
}

// fetchRemote downloads the LiteLLM pricing JSON.
func fetchRemote() (*Table, error) {
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(liteLLMPricingURL)
	if err != nil {
		return nil, fmt.Errorf("pricing: fetch %s: %w", liteLLMPricingURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("pricing: %s returned %d", liteLLMPricingURL, resp.StatusCode)
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, 10*1024*1024)) // 10 MB limit
	if err != nil {
		return nil, fmt.Errorf("pricing: read response: %w", err)
	}

	table := NewTableFromBytes(data)
	if len(table.inputCost) == 0 {
		return nil, fmt.Errorf("pricing: empty pricing table from remote")
	}
	return table, nil
}

// ApplyOverrides merges per-model price overrides into the table.
func (t *Table) ApplyOverrides(overrides map[string]ModelOverride) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for model, ov := range overrides {
		if ov.InputPerMillion > 0 {
			t.inputCost[model] = ov.InputPerMillion / 1e6
		}
		if ov.OutputPerMillion > 0 {
			t.outputCost[model] = ov.OutputPerMillion / 1e6
		}
	}
}

// Cost computes the USD cost for a span given its model and token counts.
// Returns 0 if the model is not in the pricing table.
func (t *Table) Cost(model string, inputTokens, outputTokens int64) float64 {
	t.mu.RLock()
	defer t.mu.RUnlock()

	inputCost, inputOk := t.inputCost[model]
	outputCost, outputOk := t.outputCost[model]

	if !inputOk && !outputOk {
		return 0
	}

	var cost float64
	if inputOk && inputTokens > 0 {
		cost += float64(inputTokens) * inputCost
	}
	if outputOk && outputTokens > 0 {
		cost += float64(outputTokens) * outputCost
	}
	return cost
}

// HasModel reports whether the table knows pricing for the given model.
func (t *Table) HasModel(model string) bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	_, in := t.inputCost[model]
	_, out := t.outputCost[model]
	return in || out
}

// Models returns a sorted slice of all model names known in the table.
func (t *Table) Models() []string {
	t.mu.RLock()
	defer t.mu.RUnlock()

	seen := make(map[string]struct{}, len(t.inputCost))
	for model := range t.inputCost {
		seen[model] = struct{}{}
	}
	for model := range t.outputCost {
		seen[model] = struct{}{}
	}
	models := make([]string, 0, len(seen))
	for model := range seen {
		models = append(models, model)
	}
	sort.Strings(models)
	return models
}

// NormalizeModel canonicalizes a model name for pricing lookup:
// strips all known provider prefixes iteratively and lowercases.
func NormalizeModel(model string) string {
	model = strings.TrimSpace(model)
	for {
		stripped := false
		for _, prefix := range []string{
			"openai/", "anthropic/", "ollama/", "nvidia/",
			"vertex_ai/", "bedrock/", "azure/", "groq/",
		} {
			if strings.HasPrefix(model, prefix) {
				model = model[len(prefix):]
				stripped = true
				break
			}
		}
		if !stripped {
			break
		}
	}
	return strings.ToLower(model)
}
