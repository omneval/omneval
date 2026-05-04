package pricing

import (
	"testing"
)

func TestCost_KnownModel(t *testing.T) {
	tbl := NewTableFromBytes([]byte(`{
		"gpt-4o": {
			"input_cost_per_token": 0.0000025,
			"output_cost_per_token": 0.000010
		}
	}`))

	got := tbl.Cost("gpt-4o", 100, 50)
	// 100 * 0.0000025 + 50 * 0.000010 = 0.00025 + 0.00050 = 0.00075
	want := 0.00075
	if got != want {
		t.Errorf("cost: got %f, want %f", got, want)
	}
}

func TestCost_ZeroTokens(t *testing.T) {
	tbl := NewTableFromBytes([]byte(`{
		"gpt-4o": {
			"input_cost_per_token": 0.0000025,
			"output_cost_per_token": 0.000010
		}
	}`))

	// Zero input tokens, only output.
	got := tbl.Cost("gpt-4o", 0, 100)
	want := 0.001
	if got != want {
		t.Errorf("cost: got %f, want %f", got, want)
	}

	// Zero output tokens, only input.
	got = tbl.Cost("gpt-4o", 100, 0)
	want = 0.00025
	if got != want {
		t.Errorf("cost: got %f, want %f", got, want)
	}
}

func TestCost_UnknownModel(t *testing.T) {
	tbl := NewTableFromBytes([]byte(`{
		"gpt-4o": {
			"input_cost_per_token": 0.0000025,
			"output_cost_per_token": 0.000010
		}
	}`))

	got := tbl.Cost("unknown-model", 100, 50)
	if got != 0 {
		t.Errorf("cost: got %f, want 0 for unknown model", got)
	}
}

func TestCost_EmptyTable(t *testing.T) {
	tbl := NewTableFromBytes([]byte(`{}`))

	got := tbl.Cost("any-model", 100, 50)
	if got != 0 {
		t.Errorf("cost: got %f, want 0 for empty table", got)
	}
}

func TestCost_MultipleModels(t *testing.T) {
	tbl := NewTableFromBytes([]byte(`{
		"gpt-4o": {
			"input_cost_per_token": 0.0000025,
			"output_cost_per_token": 0.000010
		},
		"claude-sonnet-4-6": {
			"input_cost_per_token": 0.000003,
			"output_cost_per_token": 0.000015
		}
	}`))

	got := tbl.Cost("claude-sonnet-4-6", 200, 100)
	// 200*0.000003 + 100*0.000015 = 0.0006 + 0.0015 = 0.0021
	want := 0.0021
	if diff := got - want; diff < -1e-9 || diff > 1e-9 {
		t.Errorf("cost: got %f, want %f (diff %e)", got, want, diff)
	}
}

func TestHasModel(t *testing.T) {
	tbl := NewTableFromBytes([]byte(`{
		"gpt-4o": {
			"input_cost_per_token": 0.0000025,
			"output_cost_per_token": 0.000010
		}
	}`))

	if !tbl.HasModel("gpt-4o") {
		t.Error("HasModel: expected true for gpt-4o")
	}
	if tbl.HasModel("unknown") {
		t.Error("HasModel: expected false for unknown")
	}
}

func TestNormalizeModel(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"openai/gpt-4o", "gpt-4o"},
		{"anthropic/claude-sonnet-4-6", "claude-sonnet-4-6"},
		{"GPT-4O", "gpt-4o"},
		{"gpt-4o", "gpt-4o"},
		{"  gpt-4o  ", "gpt-4o"},
	}

	for _, tc := range tests {
		got := NormalizeModel(tc.input)
		if got != tc.expected {
			t.Errorf("NormalizeModel(%q): got %q, want %q", tc.input, got, tc.expected)
		}
	}
}

func TestApplyOverrides(t *testing.T) {
	tbl := NewTableFromBytes([]byte(`{
		"gpt-4o": {
			"input_cost_per_token": 0.0000025,
			"output_cost_per_token": 0.000010
		}
	}`))

	overrides := map[string]ModelOverride{
		"gpt-4o": {
			InputPerMillion:  0.000005 * 1e6, // 5 per million -> 0.000005 per token
			OutputPerMillion: 0.000020 * 1e6,
		},
	}
	tbl.ApplyOverrides(overrides)

	got := tbl.Cost("gpt-4o", 100, 50)
	// 100*0.000005 + 50*0.000020 = 0.0005 + 0.001 = 0.0015
	want := 0.0015
	if got != want {
		t.Errorf("cost after override: got %f, want %f", got, want)
	}
}
