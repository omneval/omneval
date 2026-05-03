package pricing

import _ "embed"

//go:embed model_prices_and_context_window.json
var bundledPricing []byte

const liteLLMPricingURL = "https://raw.githubusercontent.com/BerriAI/litellm/main/model_prices_and_context_window.json"

// Table holds per-model token pricing. Prices are stored as USD per token.
type Table struct {
	inputPerToken  map[string]float64
	outputPerToken map[string]float64
}

// ModelOverride is a per-model price override expressed in USD per million tokens
// (the human-readable convention). Converted to per-token at load time.
type ModelOverride struct {
	InputPerMillion  float64
	OutputPerMillion float64
}

// Fetch downloads LiteLLM's model pricing JSON and returns a populated Table.
// Falls back to the bundled snapshot if the live fetch fails. overrides maps
// model name to per-million-token prices that take precedence over LiteLLM data.
func Fetch(overrides map[string]ModelOverride) (*Table, error) {
	panic("not implemented")
}

// Cost computes the USD cost for a span given its model and token counts.
// Returns 0 if the model is not in the pricing table.
func (t *Table) Cost(model string, inputTokens, outputTokens int64) float64 {
	panic("not implemented")
}
