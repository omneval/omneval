package pricing

// Table holds per-model token pricing fetched from LiteLLM's pricing JSON.
// A bundled snapshot is used as fallback if the fetch fails at startup.
type Table struct {
	// inputPerToken and outputPerToken are USD cost per token keyed by model name.
	inputPerToken  map[string]float64
	outputPerToken map[string]float64
}

// Fetch downloads LiteLLM's model pricing JSON and returns a populated Table.
// Falls back to the bundled snapshot on error.
func Fetch(overrides map[string]float64) (*Table, error) {
	panic("not implemented")
}

// Cost computes the USD cost for a span given its model and token counts.
func (t *Table) Cost(model string, inputTokens, outputTokens int64) float64 {
	panic("not implemented")
}
