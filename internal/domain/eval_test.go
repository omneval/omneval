package domain

import (
	"encoding/json"
	"testing"
)

// spanKindPtr returns a pointer to the given SpanKind.
func spanKindPtr(k SpanKind) *SpanKind { return &k }

func TestAttributeValue(t *testing.T) {
	tests := []struct {
		name   string
		attrs  map[string]any
		key    string
		expect string
	}{
		{
			name:   "top-level key",
			attrs:  map[string]any{"user_id": "abc-123"},
			key:    "user_id",
			expect: "abc-123",
		},
		{
			name:   "top-level key missing",
			attrs:  map[string]any{},
			key:    "user_id",
			expect: "",
		},
		{
			name:   "top-level key nil value",
			attrs:  map[string]any{"user_id": nil},
			key:    "user_id",
			expect: "",
		},
		{
			name: "nested dot path",
			attrs: map[string]any{
				"metadata": `{"user_id": "usr-456"}`,
			},
			key:    "metadata.user_id",
			expect: "usr-456",
		},
		{
			name: "nested depth 3",
			attrs: map[string]any{
				"a": `{"b": {"c": "deep-value"}}`,
			},
			key:    "a.b.c",
			expect: "deep-value",
		},
		{
			name: "nested depth 5",
			attrs: map[string]any{
				"a": `{"b": {"c": {"d": {"e": "max-depth"}}}}`,
			},
			key:    "a.b.c.d.e",
			expect: "max-depth",
		},
		{
			name: "missing intermediate key",
			attrs: map[string]any{
				"metadata": `{"user_id": "abc"}`,
			},
			key:    "metadata.extra",
			expect: "",
		},
		{
			name: "non-JSON intermediate",
			attrs: map[string]any{
				"metadata": "just-a-string",
			},
			key:    "metadata.user_id",
			expect: "",
		},
		{
			name: "non-string intermediate value",
			attrs: map[string]any{
				"metadata": `{"count": 42}`,
			},
			key:    "metadata.count",
			expect: "42",
		},
		{
			name:   "nil attributes",
			attrs:  nil,
			key:    "user_id",
			expect: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			span := &Span{Attributes: tt.attrs}
			got := attributeValue(span, tt.key)
			if got != tt.expect {
				t.Errorf("got %q, want %q", got, tt.expect)
			}
		})
	}
}

func TestEvalFilter_CompilePatterns(t *testing.T) {
	tests := []struct {
		name    string
		filters []AttributeRegexFilter
		wantErr bool
	}{
		{
			name: "valid patterns",
			filters: []AttributeRegexFilter{
				{Key: "user_id", Pattern: `.*-123$`},
				{Key: "tier", Pattern: `premium|basic`},
			},
			wantErr: false,
		},
		{
			name: "empty pattern",
			filters: []AttributeRegexFilter{
				{Key: "user_id", Pattern: ""},
			},
			wantErr: false,
		},
		{
			name: "invalid pattern",
			filters: []AttributeRegexFilter{
				{Key: "user_id", Pattern: `[invalid`},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := EvalFilter{AttributesMatch: tt.filters}
			err := f.CompilePatterns()
			if (err != nil) != tt.wantErr {
				t.Fatalf("CompilePatterns() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestEvalFilter_ValidateDotPaths(t *testing.T) {
	tests := []struct {
		name    string
		key     string
		wantErr bool
	}{
		{name: "top-level key", key: "user_id", wantErr: false},
		{name: "depth 5 OK", key: "a.b.c.d.e", wantErr: false},
		{name: "depth 6 exceeds limit", key: "a.b.c.d.e.f", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := EvalFilter{
				AttributesMatch: []AttributeRegexFilter{
					{Key: tt.key, Pattern: `.*`},
				},
			}
			err := f.ValidateDotPaths()
			if (err != nil) != tt.wantErr {
				t.Fatalf("ValidateDotPaths() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestMatchesFilterAttributeRegex(t *testing.T) {
	tests := []struct {
		name  string
		attrs map[string]any
		af    AttributeRegexFilter
		want  bool
	}{
		{
			name: "match",
			attrs: map[string]any{
				"metadata": `{"user_id": "abc-123"}`,
			},
			af:   AttributeRegexFilter{Key: "metadata.user_id", Pattern: `abc-123`},
			want: true,
		},
		{
			name: "no match",
			attrs: map[string]any{
				"metadata": `{"user_id": "xyz-789"}`,
			},
			af:   AttributeRegexFilter{Key: "metadata.user_id", Pattern: `abc-123`},
			want: false,
		},
		{
			name:  "missing key",
			attrs: map[string]any{},
			af:    AttributeRegexFilter{Key: "user_id", Pattern: `.*`},
			want:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			span := &Span{Attributes: tt.attrs}
			got := MatchesFilterAttributeRegex(span, tt.af)
			if got != tt.want {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

// ---- Validate tests ----

func TestEvalFilter_Validate_EmptyANDArray(t *testing.T) {
	f := EvalFilter{And: []*EvalFilter{}}
	if err := f.Validate(); err == nil {
		t.Error("expected error for empty and array")
	}
}

func TestEvalFilter_Validate_EmptyORArray(t *testing.T) {
	f := EvalFilter{Or: []*EvalFilter{}}
	if err := f.Validate(); err == nil {
		t.Error("expected error for empty or array")
	}
}

func TestEvalFilter_Validate_ValidFlatFilter(t *testing.T) {
	model := "gpt-4o"
	f := EvalFilter{Model: &model}
	if err := f.Validate(); err != nil {
		t.Errorf("unexpected error for valid flat filter: %v", err)
	}
}

func TestEvalFilter_Validate_ValidORFilter(t *testing.T) {
	model1 := "gpt-4o"
	model2 := "claude-sonnet"
	f := EvalFilter{
		Or: []*EvalFilter{
			{Model: &model1},
			{Model: &model2},
		},
	}
	if err := f.Validate(); err != nil {
		t.Errorf("unexpected error for valid or filter: %v", err)
	}
}

func TestEvalFilter_Validate_ValidNOTFilter(t *testing.T) {
	model := "gpt-4o"
	f := EvalFilter{
		Not: &EvalFilter{Model: &model},
	}
	if err := f.Validate(); err != nil {
		t.Errorf("unexpected error for valid not filter: %v", err)
	}
}

func TestEvalFilter_Validate_InvalidNestedPattern(t *testing.T) {
	f := EvalFilter{
		And: []*EvalFilter{
			{
				AttributesMatch: []AttributeRegexFilter{
					{Key: "user_id", Pattern: `[invalid`},
				},
			},
		},
	}
	if err := f.Validate(); err == nil {
		t.Error("expected error for invalid nested pattern")
	}
}

func TestEvalFilter_Validate_InvalidNestedDotPath(t *testing.T) {
	f := EvalFilter{
		Or: []*EvalFilter{
			{
				AttributesMatch: []AttributeRegexFilter{
					{Key: "a.b.c.d.e.f", Pattern: `.*`},
				},
			},
		},
	}
	if err := f.Validate(); err == nil {
		t.Error("expected error for invalid nested dot path")
	}
}

// ---- Recursive Matches tests ----

func TestEvalFilter_Matches_OR(t *testing.T) {
	model1 := "gpt-4o"
	model2 := "claude-sonnet"

	// Span matches first OR branch.
	span := &Span{Model: "gpt-4o"}
	f := EvalFilter{
		Or: []*EvalFilter{
			{Model: &model1},
			{Model: &model2},
		},
	}
	if !f.Matches(span) {
		t.Error("expected match: first OR branch")
	}

	// Span matches second OR branch.
	span2 := &Span{Model: "claude-sonnet"}
	if !f.Matches(span2) {
		t.Error("expected match: second OR branch")
	}

	// Span matches neither.
	span3 := &Span{Model: "llama"}
	if f.Matches(span3) {
		t.Error("expected no match: neither OR branch")
	}
}

func TestEvalFilter_Matches_NOT(t *testing.T) {
	model := "gpt-4o"

	// NOT model=gpt-4o: matches claude.
	span := &Span{Model: "claude-sonnet"}
	f := EvalFilter{
		Not: &EvalFilter{Model: &model},
	}
	if !f.Matches(span) {
		t.Error("expected match: not gpt-4o")
	}

	// NOT model=gpt-4o: fails for gpt-4o.
	span2 := &Span{Model: "gpt-4o"}
	if f.Matches(span2) {
		t.Error("expected no match: is gpt-4o")
	}
}

func TestEvalFilter_Matches_NestedANDInsideOR(t *testing.T) {
	kindLLM := SpanKindLLM
	modelGPT := "gpt-4o"
	modelClaude := "claude-sonnet"

	f := EvalFilter{
		Or: []*EvalFilter{
			{Kind: &kindLLM, Model: &modelGPT},
			{Kind: &kindLLM, Model: &modelClaude},
		},
	}

	// Matches first AND branch.
	span1 := &Span{Kind: SpanKindLLM, Model: "gpt-4o"}
	if !f.Matches(span1) {
		t.Error("expected match: first AND branch")
	}

	// Matches second AND branch.
	span2 := &Span{Kind: SpanKindLLM, Model: "claude-sonnet"}
	if !f.Matches(span2) {
		t.Error("expected match: second AND branch")
	}

	// Fails both (wrong kind).
	span3 := &Span{Kind: SpanKindTool, Model: "gpt-4o"}
	if f.Matches(span3) {
		t.Error("expected no match: wrong kind in both branches")
	}
}

// ---- JSON round-trip / backward compatibility tests ----

func TestEvalFilter_JSON_RoundTripFlatFilter(t *testing.T) {
	// Existing flat filter from Phase 1.
	original := EvalFilter{
		Model:      strPtr("gpt-4o"),
		Kind:       spanKindPtr(SpanKindLLM),
		MinCostUSD: floatPtr(0.01),
		MaxCostUSD: floatPtr(1.0),
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var deserialized EvalFilter
	if err := json.Unmarshal(data, &deserialized); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	span := &Span{Kind: SpanKindLLM, Model: "gpt-4o", CostUSD: 0.05}
	if !deserialized.Matches(span) {
		t.Error("expected match after round-trip")
	}
}

func TestEvalFilter_JSON_RoundTripORFilter(t *testing.T) {
	// New OR filter.
	model1 := "gpt-4o"
	model2 := "claude-sonnet"
	original := EvalFilter{
		Or: []*EvalFilter{
			{Model: &model1},
			{Model: &model2},
		},
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var deserialized EvalFilter
	if err := json.Unmarshal(data, &deserialized); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	span1 := &Span{Model: "gpt-4o"}
	if !deserialized.Matches(span1) {
		t.Error("expected match: first OR branch after round-trip")
	}

	span2 := &Span{Model: "claude-sonnet"}
	if !deserialized.Matches(span2) {
		t.Error("expected match: second OR branch after round-trip")
	}
}

func TestEvalFilter_JSON_RoundTripNestedFilter(t *testing.T) {
	// Deeply nested filter: NOT(AND(OR(...)))
	model1 := "gpt-4o"
	model2 := "claude-sonnet"
	model3 := "llama"

	original := EvalFilter{
		And: []*EvalFilter{
			{
				Or: []*EvalFilter{
					{Model: &model1},
					{Model: &model2},
				},
			},
		},
		Not: &EvalFilter{
			Model: &model3,
		},
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var deserialized EvalFilter
	if err := json.Unmarshal(data, &deserialized); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// Matches: model=gpt-4o (first OR branch) AND NOT model=llama
	span1 := &Span{Kind: SpanKindLLM, Model: "gpt-4o"}
	if !deserialized.Matches(span1) {
		t.Error("expected match: model in OR branch and NOT llama")
	}

	// Fails: NOT model=llama fails for llama
	span2 := &Span{Kind: SpanKindLLM, Model: "llama"}
	if deserialized.Matches(span2) {
		t.Error("expected no match: model is llama (not excluded)")
	}
}

func strPtr(s string) *string     { return &s }
func floatPtr(f float64) *float64 { return &f }
