package pipeline

import (
	"testing"
	"time"

	"github.com/zbloss/lantern/internal/domain"
)

func TestMatchesFilter_AllNil(t *testing.T) {
	span := &domain.Span{
		Name:      "chat",
		Kind:      domain.SpanKindLLM,
		Model:     "gpt-4o",
		ProjectID: "proj-1",
	}
	f := domain.EvalFilter{}
	if !matchesFilter(span, f) {
		t.Error("expected match with empty filter")
	}
}

func TestMatchesFilter_Kind(t *testing.T) {
	span := &domain.Span{Kind: domain.SpanKindLLM}
	kind := domain.SpanKindTool
	f := domain.EvalFilter{Kind: &kind}
	if matchesFilter(span, f) {
		t.Error("expected no match for wrong kind")
	}

	kind2 := domain.SpanKindLLM
	f2 := domain.EvalFilter{Kind: &kind2}
	if !matchesFilter(span, f2) {
		t.Error("expected match for correct kind")
	}
}

func TestMatchesFilter_Model(t *testing.T) {
	span := &domain.Span{Model: "gpt-4o"}
	f := domain.EvalFilter{Model: strPtr("gpt-4o")}
	if !matchesFilter(span, f) {
		t.Error("expected match for model")
	}

	f2 := domain.EvalFilter{Model: strPtr("claude-sonnet-4-6")}
	if matchesFilter(span, f2) {
		t.Error("expected no match for different model")
	}
}

func TestMatchesFilter_ServiceName(t *testing.T) {
	span := &domain.Span{ServiceName: "my-service"}
	f := domain.EvalFilter{ServiceName: strPtr("my-service")}
	if !matchesFilter(span, f) {
		t.Error("expected match for service name")
	}
}

func TestMatchesFilter_PromptName(t *testing.T) {
	span := &domain.Span{PromptName: "chat-prompt"}
	f := domain.EvalFilter{PromptName: strPtr("chat-prompt")}
	if !matchesFilter(span, f) {
		t.Error("expected match for prompt name")
	}
}

func TestMatchesFilter_StatusCode(t *testing.T) {
	span := &domain.Span{StatusCode: "ok"}
	f := domain.EvalFilter{StatusCode: strPtr("ok")}
	if !matchesFilter(span, f) {
		t.Error("expected match for status code")
	}
}

func TestMatchesFilter_MinCost(t *testing.T) {
	span := &domain.Span{CostUSD: 0.05}
	min := 0.01
	f := domain.EvalFilter{MinCostUSD: &min}
	if !matchesFilter(span, f) {
		t.Error("expected match for min cost")
	}

	min2 := 0.1
	f2 := domain.EvalFilter{MinCostUSD: &min2}
	if matchesFilter(span, f2) {
		t.Error("expected no match, cost below min")
	}
}

func TestMatchesFilter_MaxCost(t *testing.T) {
	span := &domain.Span{CostUSD: 0.05}
	max := 0.1
	f := domain.EvalFilter{MaxCostUSD: &max}
	if !matchesFilter(span, f) {
		t.Error("expected match for max cost")
	}

	max2 := 0.01
	f2 := domain.EvalFilter{MaxCostUSD: &max2}
	if matchesFilter(span, f2) {
		t.Error("expected no match, cost above max")
	}
}

func TestMatchesFilter_MinDurationMS(t *testing.T) {
	start := time.Now().Add(-2 * time.Second)
	end := time.Now()
	span := &domain.Span{StartTime: start, EndTime: end}

	minDur := int64(1000)
	f := domain.EvalFilter{MinDurationMS: &minDur}
	if !matchesFilter(span, f) {
		t.Error("expected match for min duration")
	}

	minDur2 := int64(3000)
	f2 := domain.EvalFilter{MinDurationMS: &minDur2}
	if matchesFilter(span, f2) {
		t.Error("expected no match, duration below min")
	}
}

func TestMatchesFilter_MaxDurationMS(t *testing.T) {
	start := time.Now().Add(-2 * time.Second)
	end := time.Now()
	span := &domain.Span{StartTime: start, EndTime: end}

	maxDur := int64(5000)
	f := domain.EvalFilter{MaxDurationMS: &maxDur}
	if !matchesFilter(span, f) {
		t.Error("expected match for max duration")
	}

	maxDur2 := int64(500)
	f2 := domain.EvalFilter{MaxDurationMS: &maxDur2}
	if matchesFilter(span, f2) {
		t.Error("expected no match, duration above max")
	}
}

func TestMatchesFilter_MultipleConditions(t *testing.T) {
	span := &domain.Span{
		Kind:      domain.SpanKindLLM,
		Model:     "gpt-4o",
		CostUSD:   0.05,
		StartTime: time.Now().Add(-1 * time.Second),
		EndTime:   time.Now(),
	}

	kind := domain.SpanKindLLM
	model := "gpt-4o"
	minCost := 0.01
	maxCost := 0.10

	f := domain.EvalFilter{
		Kind:       &kind,
		Model:      &model,
		MinCostUSD: &minCost,
		MaxCostUSD: &maxCost,
	}
	if !matchesFilter(span, f) {
		t.Error("expected match for multiple conditions")
	}

	// Change one condition to fail.
	model2 := "claude-sonnet-4-6"
	f2 := domain.EvalFilter{
		Kind:   &kind,
		Model:  &model2,
		MinCostUSD: &minCost,
	}
	if matchesFilter(span, f2) {
		t.Error("expected no match, wrong model")
	}
}

func strPtr(s string) *string {
	return &s
}

func TestMatchesFilter_AttributesMatch_TopLevel(t *testing.T) {
	span := &domain.Span{
		Attributes: map[string]any{
			"user_id": "abc-123",
		},
	}
	pattern := `.*-123$`
	f := domain.EvalFilter{
		AttributesMatch: []domain.AttributeRegexFilter{
			{Key: "user_id", Pattern: pattern},
		},
	}
	if !matchesFilter(span, f) {
		t.Error("expected match for top-level key")
	}

	pattern2 := `^xyz$`
	f2 := domain.EvalFilter{
		AttributesMatch: []domain.AttributeRegexFilter{
			{Key: "user_id", Pattern: pattern2},
		},
	}
	if matchesFilter(span, f2) {
		t.Error("expected no match, pattern doesn't match value")
	}
}

func TestMatchesFilter_AttributesMatch_NestedDotPath(t *testing.T) {
	span := &domain.Span{
		Attributes: map[string]any{
			"metadata": `{"user_id": "usr-456", "tier": "premium"}`,
		},
	}

	f := domain.EvalFilter{
		AttributesMatch: []domain.AttributeRegexFilter{
			{Key: "metadata.user_id", Pattern: `usr-456`},
		},
	}
	if !matchesFilter(span, f) {
		t.Error("expected match for dot-notation path")
	}

	f2 := domain.EvalFilter{
		AttributesMatch: []domain.AttributeRegexFilter{
			{Key: "metadata.tier", Pattern: `premium`},
		},
	}
	if !matchesFilter(span, f2) {
		t.Error("expected match for nested key 'tier'")
	}
}

func TestMatchesFilter_AttributesMatch_NestedDepth3(t *testing.T) {
	span := &domain.Span{
		Attributes: map[string]any{
			"a": `{"b": {"c": "deep-value"}}`,
		},
	}

	f := domain.EvalFilter{
		AttributesMatch: []domain.AttributeRegexFilter{
			{Key: "a.b.c", Pattern: `deep-value`},
		},
	}
	if !matchesFilter(span, f) {
		t.Error("expected match for depth-3 dot path")
	}
}

func TestMatchesFilter_AttributesMatch_MissingIntermediateKey(t *testing.T) {
	span := &domain.Span{
		Attributes: map[string]any{
			"metadata": `{"user_id": "abc"}`,
		},
	}

	// "metadata" exists but "metadata.extra" does not.
	f := domain.EvalFilter{
		AttributesMatch: []domain.AttributeRegexFilter{
			{Key: "metadata.extra", Pattern: `.*`},
		},
	}
	if matchesFilter(span, f) {
		t.Error("expected no match for missing intermediate key")
	}
}

func TestMatchesFilter_AttributesMatch_MissingTopLevelKey(t *testing.T) {
	span := &domain.Span{
		Attributes: map[string]any{},
	}

	f := domain.EvalFilter{
		AttributesMatch: []domain.AttributeRegexFilter{
			{Key: "nonexistent", Pattern: `.*`},
		},
	}
	if matchesFilter(span, f) {
		t.Error("expected no match for missing top-level key")
	}
}

func TestMatchesFilter_AttributesMatch_NonObjectIntermediate(t *testing.T) {
	span := &domain.Span{
		Attributes: map[string]any{
			"metadata": "just-a-string", // not JSON
		},
	}

	f := domain.EvalFilter{
		AttributesMatch: []domain.AttributeRegexFilter{
			{Key: "metadata.user_id", Pattern: `.*`},
		},
	}
	if matchesFilter(span, f) {
		t.Error("expected no match for non-object intermediate")
	}
}

func TestMatchesFilter_AttributesMatch_AttributesNil(t *testing.T) {
	span := &domain.Span{
		Attributes: nil,
	}

	f := domain.EvalFilter{
		AttributesMatch: []domain.AttributeRegexFilter{
			{Key: "user_id", Pattern: `.*`},
		},
	}
	if matchesFilter(span, f) {
		t.Error("expected no match when attributes is nil")
	}
}

func TestMatchesFilter_AttributesMatch_MultipleConditionsAllMatch(t *testing.T) {
	span := &domain.Span{
		Attributes: map[string]any{
			"tier":     "premium",
			"metadata": `{"user_id": "abc-123"}`,
		},
	}

	f := domain.EvalFilter{
		AttributesMatch: []domain.AttributeRegexFilter{
			{Key: "tier", Pattern: `premium`},
			{Key: "metadata.user_id", Pattern: `abc-123`},
		},
	}
	if !matchesFilter(span, f) {
		t.Error("expected match for multiple conditions all matching")
	}
}

func TestMatchesFilter_AttributesMatch_MultipleConditionsOneFails(t *testing.T) {
	span := &domain.Span{
		Attributes: map[string]any{
			"tier":     "basic",
			"metadata": `{"user_id": "abc-123"}`,
		},
	}

	f := domain.EvalFilter{
		AttributesMatch: []domain.AttributeRegexFilter{
			{Key: "tier", Pattern: `premium`},
			{Key: "metadata.user_id", Pattern: `abc-123`},
		},
	}
	if matchesFilter(span, f) {
		t.Error("expected no match, one condition fails")
	}
}

// ---- OR filter tests ----

func TestMatchesFilter_OR_TwoLeafConditions(t *testing.T) {
	span := &domain.Span{Model: "gpt-4o"}
	model1 := "gpt-4o"
	model2 := "claude-sonnet-4-6"

	// OR of two leaf conditions: model==gpt-4o OR model==claude-sonnet-4-6
	f := domain.EvalFilter{
		Or: []*domain.EvalFilter{
			{Model: &model1},
			{Model: &model2},
		},
	}
	if !matchesFilter(span, f) {
		t.Error("expected match: span model matches first OR branch")
	}
}

func TestMatchesFilter_OR_NeitherBranchMatches(t *testing.T) {
	span := &domain.Span{Model: "llama-3"}
	model1 := "gpt-4o"
	model2 := "claude-sonnet-4-6"

	f := domain.EvalFilter{
		Or: []*domain.EvalFilter{
			{Model: &model1},
			{Model: &model2},
		},
	}
	if matchesFilter(span, f) {
		t.Error("expected no match: span model matches neither OR branch")
	}
}

func TestMatchesFilter_OR_CombinedWithTopLevelAnd(t *testing.T) {
	// kind==llm (top-level leaf, matches) AND OR: [model==gpt-4o, model==claude]
	// Overall: matches because kind matches and one OR branch matches
	span := &domain.Span{Kind: domain.SpanKindLLM, Model: "claude-sonnet-4-6"}
	model1 := "gpt-4o"
	model2 := "claude-sonnet-4-6"
	kind := domain.SpanKindLLM

	f := domain.EvalFilter{
		Kind: &kind,
		Or: []*domain.EvalFilter{
			{Model: &model1},
			{Model: &model2},
		},
	}
	if !matchesFilter(span, f) {
		t.Error("expected match: kind matches AND Or branch matches")
	}
}

// ---- NOT filter tests ----

func TestMatchesFilter_NOT(t *testing.T) {
	// NOT model==gpt-4o: should match spans where model is NOT gpt-4o
	span := &domain.Span{Model: "claude-sonnet-4-6"}
	model := "gpt-4o"

	f := domain.EvalFilter{
		Not: &domain.EvalFilter{
			Model: &model,
		},
	}
	if !matchesFilter(span, f) {
		t.Error("expected match: span model is NOT gpt-4o")
	}
}

func TestMatchesFilter_NOT_Fails(t *testing.T) {
	// NOT model==gpt-4o: should NOT match spans where model IS gpt-4o
	span := &domain.Span{Model: "gpt-4o"}
	model := "gpt-4o"

	f := domain.EvalFilter{
		Not: &domain.EvalFilter{
			Model: &model,
		},
	}
	if matchesFilter(span, f) {
		t.Error("expected no match: span model IS gpt-4o, so NOT should reject")
	}
}

func TestMatchesFilter_NOT_CombinedWithTopLevel(t *testing.T) {
	// kind==llm AND NOT model==gpt-4o
	span := &domain.Span{Kind: domain.SpanKindLLM, Model: "claude-sonnet-4-6"}
	model := "gpt-4o"

	kindLLM := domain.SpanKindLLM
	f := domain.EvalFilter{
		Kind: &kindLLM,
		Not: &domain.EvalFilter{
			Model: &model,
		},
	}
	if !matchesFilter(span, f) {
		t.Error("expected match: kind is llm and model is NOT gpt-4o")
	}
}

// ---- Nested AND inside OR ----

func TestMatchesFilter_NestedANDInsideOR(t *testing.T) {
	// OR: (kind==llm AND model==gpt-4o) OR (kind==tool AND model==claude)
	span := &domain.Span{Kind: domain.SpanKindTool, Model: "claude-sonnet-4-6"}

	kindLLM := domain.SpanKindLLM
	kindTool := domain.SpanKindTool
	modelGPT := "gpt-4o"
	modelClaude := "claude-sonnet-4-6"

	f := domain.EvalFilter{
		Or: []*domain.EvalFilter{
			{
				Kind:  &kindLLM,
				Model: &modelGPT,
			},
			{
				Kind:  &kindTool,
				Model: &modelClaude,
			},
		},
	}
	if !matchesFilter(span, f) {
		t.Error("expected match: second AND branch matches")
	}
}

func TestMatchesFilter_NestedANDInsideOR_NeitherMatches(t *testing.T) {
	span := &domain.Span{Kind: domain.SpanKindLLM, Model: "claude-sonnet-4-6"}

	kindLLM := domain.SpanKindLLM
	kindTool := domain.SpanKindTool
	modelGPT := "gpt-4o"
	modelClaude := "claude-sonnet-4-6"

	f := domain.EvalFilter{
		Or: []*domain.EvalFilter{
			{
				Kind:  &kindLLM,
				Model: &modelGPT,
			},
			{
				Kind:  &kindTool,
				Model: &modelClaude,
			},
		},
	}
	if matchesFilter(span, f) {
		t.Error("expected no match: neither AND branch matches (wrong model for llm, wrong kind for tool)")
	}
}

// ---- Deeply nested structure ----

func TestMatchesFilter_DeeplyNested(t *testing.T) {
	// NOT: AND: OR: model==gpt-4o
	// Span has model=claude, so:
	//   OR matches (gpt-4o doesn't match, but wait, only one branch)
	// Actually let me construct a clearer nested structure:
	// OR: [AND: [kind==llm, model==gpt-4o], NOT: [model==claude]]
	// Span: kind=llm, model=claude
	//   AND branch: kind=llm (match) AND model=gpt-4o (fail) → fail
	//   NOT branch: NOT model=claude → span model IS claude → NOT fails
	// Overall: neither branch matches → no match

	span := &domain.Span{Kind: domain.SpanKindLLM, Model: "claude-sonnet-4-6"}
	kindLLM := domain.SpanKindLLM
	modelGPT := "gpt-4o"
	modelClaude := "claude-sonnet-4-6"

	f := domain.EvalFilter{
		Or: []*domain.EvalFilter{
			{
				And: []*domain.EvalFilter{
					{Kind: &kindLLM, Model: &modelGPT},
				},
			},
			{
				Not: &domain.EvalFilter{Model: &modelClaude},
			},
		},
	}
	if matchesFilter(span, f) {
		t.Error("expected no match: neither OR branch matches")
	}
}

func TestMatchesFilter_DeeplyNested_Matches(t *testing.T) {
	// Same structure as above, but span has model=gpt-4o
	//   AND branch: kind=llm (match) AND model=gpt-4o (match) → match
	//   NOT branch: NOT model=claude → span model IS NOT claude → NOT succeeds → match
	// Overall: at least one branch matches → match
	span := &domain.Span{Kind: domain.SpanKindLLM, Model: "gpt-4o"}
	kindLLM := domain.SpanKindLLM
	modelGPT := "gpt-4o"
	modelClaude := "claude-sonnet-4-6"

	f := domain.EvalFilter{
		Or: []*domain.EvalFilter{
			{
				And: []*domain.EvalFilter{
					{Kind: &kindLLM, Model: &modelGPT},
				},
			},
			{
				Not: &domain.EvalFilter{Model: &modelClaude},
			},
		},
	}
	if !matchesFilter(span, f) {
		t.Error("expected match: at least one OR branch matches")
	}
}

// ---- Empty filter (matches all) ----

func TestMatchesFilter_EmptyFilter(t *testing.T) {
	span := &domain.Span{Model: "anything"}
	f := domain.EvalFilter{}
	if !matchesFilter(span, f) {
		t.Error("expected match: empty filter matches all spans")
	}
}

func TestMatchesFilter_EmptyANDMatchesAll(t *testing.T) {
	// Empty AND array is invalid, so skip this. Instead test nil AND.
	span := &domain.Span{Model: "anything"}
	f := domain.EvalFilter{And: nil}
	if !matchesFilter(span, f) {
		t.Error("expected match: nil AND matches all")
	}
}

func TestMatchesFilter_EmptyORMatchesAll(t *testing.T) {
	// Empty OR array is invalid, so skip this. Instead test nil OR.
	span := &domain.Span{Model: "anything"}
	f := domain.EvalFilter{Or: nil}
	if !matchesFilter(span, f) {
		t.Error("expected match: nil OR matches all")
	}
}

func TestMatchesFilter_EmptyNOTMatchesAll(t *testing.T) {
	// NOT is nil (not present), so no NOT evaluation happens.
	// The filter should match all spans (empty filter behavior).
	span := &domain.Span{Model: "anything"}
	f := domain.EvalFilter{Not: nil}
	if !matchesFilter(span, f) {
		t.Error("expected match: nil NOT matches all")
	}
}
