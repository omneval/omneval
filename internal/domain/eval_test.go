package domain

import (
	"testing"
)

func TestAttributeValue_TopLevelKey(t *testing.T) {
	span := &Span{Attributes: map[string]any{"user_id": "abc-123"}}
	got := attributeValue(span, "user_id")
	if got != "abc-123" {
		t.Errorf("got %q, want %q", got, "abc-123")
	}
}

func TestAttributeValue_TopLevelKeyNil(t *testing.T) {
	span := &Span{Attributes: map[string]any{}}
	got := attributeValue(span, "user_id")
	if got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

func TestAttributeValue_TopLevelKeyNilValue(t *testing.T) {
	span := &Span{Attributes: map[string]any{"user_id": nil}}
	got := attributeValue(span, "user_id")
	if got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

func TestAttributeValue_NestedDotPath(t *testing.T) {
	span := &Span{Attributes: map[string]any{
		"metadata": `{"user_id": "usr-456"}`,
	}}
	got := attributeValue(span, "metadata.user_id")
	if got != "usr-456" {
		t.Errorf("got %q, want %q", got, "usr-456")
	}
}

func TestAttributeValue_NestedDepth3(t *testing.T) {
	span := &Span{Attributes: map[string]any{
		"a": `{"b": {"c": "deep-value"}}`,
	}}
	got := attributeValue(span, "a.b.c")
	if got != "deep-value" {
		t.Errorf("got %q, want %q", got, "deep-value")
	}
}

func TestAttributeValue_NestedDepth5(t *testing.T) {
	span := &Span{Attributes: map[string]any{
		"a": `{"b": {"c": {"d": {"e": "max-depth"}}}}`,
	}}
	got := attributeValue(span, "a.b.c.d.e")
	if got != "max-depth" {
		t.Errorf("got %q, want %q", got, "max-depth")
	}
}

func TestAttributeValue_MissingIntermediate(t *testing.T) {
	span := &Span{Attributes: map[string]any{
		"metadata": `{"user_id": "abc"}`,
	}}
	got := attributeValue(span, "metadata.extra")
	if got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

func TestAttributeValue_NonJSONIntermediate(t *testing.T) {
	span := &Span{Attributes: map[string]any{
		"metadata": "just-a-string",
	}}
	got := attributeValue(span, "metadata.user_id")
	if got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

func TestAttributeValue_NonStringIntermediate(t *testing.T) {
	span := &Span{Attributes: map[string]any{
		"metadata": `{"count": 42}`,
	}}
	got := attributeValue(span, "metadata.count")
	// Should return "42" (formatted float64)
	if got != "42" {
		t.Errorf("got %q, want %q", got, "42")
	}
}

func TestAttributeValue_NilAttributes(t *testing.T) {
	span := &Span{Attributes: nil}
	got := attributeValue(span, "user_id")
	if got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

func TestEvalFilter_CompilePatterns_NoError(t *testing.T) {
	f := EvalFilter{
		AttributesMatch: []AttributeRegexFilter{
			{Key: "user_id", Pattern: `.*-123$`},
			{Key: "tier", Pattern: `premium|basic`},
		},
	}
	if err := f.CompilePatterns(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestEvalFilter_CompilePatterns_Error(t *testing.T) {
	f := EvalFilter{
		AttributesMatch: []AttributeRegexFilter{
			{Key: "user_id", Pattern: `[invalid`},
		},
	}
	err := f.CompilePatterns()
	if err == nil {
		t.Fatal("expected error for invalid pattern")
	}
}

func TestEvalFilter_CompilePatterns_EmptyPattern(t *testing.T) {
	f := EvalFilter{
		AttributesMatch: []AttributeRegexFilter{
			{Key: "user_id", Pattern: ""},
		},
	}
	if err := f.CompilePatterns(); err != nil {
		t.Fatalf("unexpected error for empty pattern: %v", err)
	}
}

func TestEvalFilter_ValidateDotPaths_MaxDepth5OK(t *testing.T) {
	f := EvalFilter{
		AttributesMatch: []AttributeRegexFilter{
			{Key: "a.b.c.d.e", Pattern: `.*`}, // 5 segments, OK
		},
	}
	if err := f.ValidateDotPaths(); err != nil {
		t.Fatalf("depth 5 should be OK: %v", err)
	}
}

func TestEvalFilter_ValidateDotPaths_MaxDepth6Fails(t *testing.T) {
	f := EvalFilter{
		AttributesMatch: []AttributeRegexFilter{
			{Key: "a.b.c.d.e.f", Pattern: `.*`}, // 6 segments, too deep
		},
	}
	if err := f.ValidateDotPaths(); err == nil {
		t.Fatal("expected error for depth > 5")
	}
}

func TestEvalFilter_ValidateDotPaths_TopLevelOK(t *testing.T) {
	f := EvalFilter{
		AttributesMatch: []AttributeRegexFilter{
			{Key: "user_id", Pattern: `.*`},
		},
	}
	if err := f.ValidateDotPaths(); err != nil {
		t.Fatalf("top-level key should pass: %v", err)
	}
}

func TestMatchesFilterAttributeRegex_Match(t *testing.T) {
	span := &Span{Attributes: map[string]any{
		"metadata": `{"user_id": "abc-123"}`,
	}}
	af := AttributeRegexFilter{Key: "metadata.user_id", Pattern: `abc-123`}
	if !MatchesFilterAttributeRegex(span, af) {
		t.Error("expected match")
	}
}

func TestMatchesFilterAttributeRegex_NoMatch(t *testing.T) {
	span := &Span{Attributes: map[string]any{
		"metadata": `{"user_id": "xyz-789"}`,
	}}
	af := AttributeRegexFilter{Key: "metadata.user_id", Pattern: `abc-123`}
	if MatchesFilterAttributeRegex(span, af) {
		t.Error("expected no match")
	}
}

func TestMatchesFilterAttributeRegex_MissingKey(t *testing.T) {
	span := &Span{Attributes: map[string]any{}}
	af := AttributeRegexFilter{Key: "user_id", Pattern: `.*`}
	if MatchesFilterAttributeRegex(span, af) {
		t.Error("expected no match for missing key")
	}
}
