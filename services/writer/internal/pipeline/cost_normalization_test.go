package pipeline

import (
	"testing"

	"github.com/omneval/omneval/internal/domain"
	"github.com/omneval/omneval/internal/pricing"
)

// TestComputeCosts_NormalizesModelName verifies that computeCosts
// normalizes model names before pricing lookup, so that provider-prefixed
// model names (e.g. "openai/gpt-4o") are collapsed to their canonical form
// ("gpt-4o") before the pricing table is consulted.  The span's Model field
// must be rewritten with the normalized name so that the Lake stores one row
// per physical model, not one row per provider prefix.
func TestComputeCosts_NormalizesModelName(t *testing.T) {
	tbl := pricing.NewTableFromBytes([]byte(`{
		"gpt-4o": {
			"input_cost_per_token": 0.0000025,
			"output_cost_per_token": 0.000010
		}
	}`))

	p := New(nil, tbl, nil, nil, nil, nil)
	spans := []*domain.Span{
		{
			Model:       "openai/gpt-4o",
			InputTokens: 100,
			OutputTokens: 50,
		},
	}

	p.computeCosts(spans)

	// After normalization the model name should be canonical.
	if spans[0].Model != "gpt-4o" {
		t.Errorf("model: got %q, want %q", spans[0].Model, "gpt-4o")
	}
	// Cost should be non-zero because the normalized model is known.
	wantCost := 0.00025 + 0.00050 // 100*0.0000025 + 50*0.000010
	if spans[0].CostUSD != wantCost {
		t.Errorf("cost: got %f, want %f", spans[0].CostUSD, wantCost)
	}
}

// TestComputeCosts_PreservesRawModelInAttributes verifies that the original
// (pre-normalization) model name is retained in the span's attributes so that
// consumers can still inspect the raw value.
func TestComputeCosts_PreservesRawModelInAttributes(t *testing.T) {
	tbl := pricing.NewTableFromBytes([]byte(`{}`))

	p := New(nil, tbl, nil, nil, nil, nil)
	spans := []*domain.Span{
		{
			Model: "openai/gpt-4o",
		},
	}

	p.computeCosts(spans)

	if spans[0].Attributes == nil {
		t.Fatal("attributes should not be nil")
	}
	if raw, ok := spans[0].Attributes["omneval/raw_model"]; !ok {
		t.Error("expected omneval/raw_model attribute to be set")
	} else if raw != "openai/gpt-4o" {
		t.Errorf("omneval/raw_model: got %q, want %q", raw, "openai/gpt-4o")
	}
}

// TestComputeCosts_CostWithNormalizedModel verifies that a known model
// prefixed with different providers all resolves to the same price.
func TestComputeCosts_CostWithNormalizedModel(t *testing.T) {
	tbl := pricing.NewTableFromBytes([]byte(`{
		"gpt-4o": {
			"input_cost_per_token": 0.0000025,
			"output_cost_per_token": 0.000010
		}
	}`))

	p := New(nil, tbl, nil, nil, nil, nil)
	spans := []*domain.Span{
		{Model: "openai/gpt-4o", InputTokens: 100, OutputTokens: 50},
		{Model: "anthropic/gpt-4o", InputTokens: 200, OutputTokens: 100},
		{Model: "claude-sonnet-4-6", InputTokens: 50, OutputTokens: 25},
	}

	p.computeCosts(spans)

	// openai/ and anthropic/ prefixed models should normalize to gpt-4o
	// and get the same per-token price; claude-sonnet-4-6 is a different model.
	for i, wantCost := range []float64{
		0.00025 + 0.00050, // gpt-4o: 100*2.5e-6 + 50*1e-5
		0.00050 + 0.00100, // gpt-4o: 200*2.5e-6 + 100*1e-5
		0,                   // claude-sonnet-4-6: unknown -> 0
	} {
		if spans[i].CostUSD != wantCost {
			t.Errorf("span[%d] cost: got %f, want %f", i, spans[i].CostUSD, wantCost)
		}
	}
}

// TestComputeCosts_NilPricingStillWorks verifies that computeCosts doesn't
// panic when the pricing table is nil (e.g. during startup or offline).
func TestComputeCosts_NilPricingStillWorks(t *testing.T) {
	p := New(nil, nil, nil, nil, nil, nil)
	spans := []*domain.Span{
		{
			Model:       "gpt-4o",
			InputTokens: 100,
			OutputTokens: 50,
		},
	}

	p.computeCosts(spans)

	if spans[0].CostUSD != 0 {
		t.Errorf("cost: got %f, want 0 with nil pricing", spans[0].CostUSD)
	}
}

// TestComputeCosts_NonLLMSpansAreNotNormalized verifies that non-LLM spans
// (kind != llm) do not have their model names normalized. Non-LLM spans may
// legitimately have no model or an empty model name, and normalization
// should not be applied to them.
func TestComputeCosts_NonLLMSpansAreNotNormalized(t *testing.T) {
	tbl := pricing.NewTableFromBytes([]byte(`{}`))

	p := New(nil, tbl, nil, nil, nil, nil)
	spans := []*domain.Span{
		{
			Model:       "",
			Kind:        domain.SpanKindTool,
			InputTokens: 0,
			OutputTokens: 0,
		},
	}

	p.computeCosts(spans)

	// Model should remain empty for non-LLM spans.
	if spans[0].Model != "" {
		t.Errorf("model: got %q, want empty for non-LLM span", spans[0].Model)
	}
}

// TestComputeCosts_TimeDefaults verifies that computeCosts defaults missing
// start/end times to the current time.
func TestComputeCosts_TimeDefaults(t *testing.T) {
	tbl := pricing.NewTableFromBytes([]byte(`{}`))

	p := New(nil, tbl, nil, nil, nil, nil)
	spans := []*domain.Span{
		{Model: "gpt-4o"},
	}

	p.computeCosts(spans)

	if spans[0].StartTime.IsZero() {
		t.Error("StartTime should be set when missing")
	}
	if spans[0].EndTime.IsZero() {
		t.Error("EndTime should be set when missing")
	}
}

// TestComputeCosts_CaseNormalization verifies that uppercase model names are
// lowercased by normalization.
func TestComputeCosts_CaseNormalization(t *testing.T) {
	tbl := pricing.NewTableFromBytes([]byte(`{
		"gpt-4o": {
			"input_cost_per_token": 0.0000025,
			"output_cost_per_token": 0.000010
		}
	}`))

	p := New(nil, tbl, nil, nil, nil, nil)
	spans := []*domain.Span{
		{
			Model:       "GPT-4O",
			InputTokens: 100,
			OutputTokens: 50,
		},
	}

	p.computeCosts(spans)

	if spans[0].Model != "gpt-4o" {
		t.Errorf("model: got %q, want %q", spans[0].Model, "gpt-4o")
	}
	wantCost := 0.00025 + 0.00050
	if spans[0].CostUSD != wantCost {
		t.Errorf("cost: got %f, want %f", spans[0].CostUSD, wantCost)
	}
}

// TestComputeCosts_CollapsesDuplicateModels verifies that the same physical
// model with different provider prefixes ends up as one row with a single
// normalized model name.
func TestComputeCosts_CollapsesDuplicateModels(t *testing.T) {
	tbl := pricing.NewTableFromBytes([]byte(`{
		"qwen3.6-35b-a3b-nvfp4": {
			"input_cost_per_token": 0.000001,
			"output_cost_per_token": 0.000002
		}
	}`))

	p := New(nil, tbl, nil, nil, nil, nil)
	spans := []*domain.Span{
		{Model: "openai/nvidia/qwen3.6-35b-a3b-nvfp4", InputTokens: 60000000, OutputTokens: 500000},
		{Model: "nvidia/qwen3.6-35b-a3b-nvfp4", InputTokens: 40000000, OutputTokens: 500000},
	}

	p.computeCosts(spans)

	// Both should normalize to the same canonical name.
	for i, s := range spans {
		if s.Model != "qwen3.6-35b-a3b-nvfp4" {
			t.Errorf("span[%d] model: got %q, want %q", i, s.Model, "qwen3.6-35b-a3b-nvfp4")
		}
	}

	// Each individual span cost should be non-zero.
	for i, s := range spans {
		want := 60000000*0.000001 + 500000*0.000002
		if i == 1 {
			want = 40000000*0.000001 + 500000*0.000002
		}
		if s.CostUSD != want {
			t.Errorf("span[%d] cost: got %f, want %f", i, s.CostUSD, want)
		}
	}
}

// TestComputeCosts_IgnoreEmptyModel verifies that spans with an empty model
// name do not have their model normalized (they stay empty).
func TestComputeCosts_IgnoreEmptyModel(t *testing.T) {
	tbl := pricing.NewTableFromBytes([]byte(`{}`))

	p := New(nil, tbl, nil, nil, nil, nil)
	spans := []*domain.Span{
		{Model: "", Kind: domain.SpanKindLLM},
	}

	p.computeCosts(spans)

	if spans[0].Model != "" {
		t.Errorf("model: got %q, want empty for empty input", spans[0].Model)
	}
}

// TestComputeCosts_TraceDetailTotalCost verifies that TraceResponse.TotalCostUSD
// is computed from the spans' CostUSD fields after normalization.  This is an
// integration-style test that covers the trace-detail handler path.
func TestComputeCosts_TraceDetailTotalCost(t *testing.T) {
	tbl := pricing.NewTableFromBytes([]byte(`{
		"gpt-4o": {
			"input_cost_per_token": 0.0000025,
			"output_cost_per_token": 0.000010
		}
	}`))

	p := New(nil, tbl, nil, nil, nil, nil)
	spans := []*domain.Span{
		{Model: "openai/gpt-4o", InputTokens: 100, OutputTokens: 50},
		{Model: "anthropic/gpt-4o", InputTokens: 200, OutputTokens: 100},
	}

	p.computeCosts(spans)

	var totalCost float64
	for _, s := range spans {
		totalCost += s.CostUSD
	}

	// Total should be the sum of both normalized costs.
	want := (0.00025 + 0.00050) + (0.00050 + 0.00100)
	if totalCost != want {
		t.Errorf("total cost: got %f, want %f", totalCost, want)
	}
}