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
