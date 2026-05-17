package pipeline

import (
	"context"
	"testing"

	"github.com/zbloss/lantern/internal/domain"
	"github.com/zbloss/lantern/internal/pricing"
)

// FakeEvalQueue captures enqueued eval jobs for test assertions.
type FakeEvalQueue struct {
	jobs []*domain.EvalJob
}

func (f *FakeEvalQueue) Enqueue(_ context.Context, job *domain.EvalJob) error {
	f.jobs = append(f.jobs, job)
	return nil
}
func (f *FakeEvalQueue) Dequeue(_ context.Context) (*domain.EvalJob, error) {
	return nil, nil
}

// ---- tests ----

func TestEvalSpans_SampleRate1EnqueuesAll(t *testing.T) {
	mq := &FakeEvalQueue{}
	store := &fakeMetaStore{
		evalRules: []domain.EvalRule{
			{RuleID: "rule-1", Enabled: true, SampleRate: 1.0, Filter: domain.EvalFilter{}},
		},
	}
	p := &Pipeline{
		ingest:  &fakeIngestQueue{},
		db:      nil,
		pricing: pricing.GetDefaultBundled(),
		store:   store,
		evalQ:   mq,
	}

	span := &domain.Span{SpanID: "span-1", TraceID: "trace-1", ProjectID: "proj-1"}
	p.evalSpans(context.Background(), span, store.evalRules)

	if len(mq.jobs) != 1 {
		t.Fatalf("expected 1 eval job for sample_rate=1.0, got %d", len(mq.jobs))
	}
	if mq.jobs[0].RuleID != "rule-1" {
		t.Errorf("expected rule_id=rule-1, got %s", mq.jobs[0].RuleID)
	}
	if mq.jobs[0].SpanID != "span-1" {
		t.Errorf("expected span_id=span-1, got %s", mq.jobs[0].SpanID)
	}
}

func TestEvalSpans_SampleRateZeroEnqueuesNone(t *testing.T) {
	mq := &FakeEvalQueue{}
	store := &fakeMetaStore{
		evalRules: []domain.EvalRule{
			{RuleID: "rule-1", Enabled: true, SampleRate: 0.0, Filter: domain.EvalFilter{}},
		},
	}
	p := &Pipeline{
		ingest:  &fakeIngestQueue{},
		db:      nil,
		pricing: pricing.GetDefaultBundled(),
		store:   store,
		evalQ:   mq,
	}

	span := &domain.Span{SpanID: "span-1", TraceID: "trace-1", ProjectID: "proj-1"}
	p.evalSpans(context.Background(), span, store.evalRules)

	if len(mq.jobs) != 0 {
		t.Fatalf("expected 0 eval jobs for sample_rate=0.0, got %d", len(mq.jobs))
	}
}

func TestEvalSpans_SampleRateFractional(t *testing.T) {
	mq := &FakeEvalQueue{}
	store := &fakeMetaStore{
		evalRules: []domain.EvalRule{
			{RuleID: "rule-1", Enabled: true, SampleRate: 0.5, Filter: domain.EvalFilter{}},
		},
	}
	p := &Pipeline{
		ingest:  &fakeIngestQueue{},
		db:      nil,
		pricing: pricing.GetDefaultBundled(),
		store:   store,
		evalQ:   mq,
	}

	const trials = 100
	totalJobs := 0
	for i := 0; i < trials; i++ {
		mq.jobs = nil
		span := &domain.Span{
			SpanID:    "span",
			TraceID:   "trace",
			ProjectID: "proj",
		}
		p.evalSpans(context.Background(), span, store.evalRules)
		totalJobs += len(mq.jobs)
	}

	lower := float64(trials) * 0.3
	upper := float64(trials) * 0.7
	if float64(totalJobs) < lower || float64(totalJobs) > upper {
		t.Errorf("sample_rate=0.5: expected ~%d jobs (range %.0f–%.0f), got %d",
			trials/2, lower, upper, totalJobs)
	}
}

func TestEvalSpans_DisabledRule(t *testing.T) {
	mq := &FakeEvalQueue{}
	store := &fakeMetaStore{
		evalRules: []domain.EvalRule{
			{RuleID: "rule-1", Enabled: false, SampleRate: 1.0, Filter: domain.EvalFilter{}},
		},
	}
	p := &Pipeline{
		ingest:  &fakeIngestQueue{},
		db:      nil,
		pricing: pricing.GetDefaultBundled(),
		store:   store,
		evalQ:   mq,
	}

	span := &domain.Span{SpanID: "span-1", TraceID: "trace-1", ProjectID: "proj-1"}
	p.evalSpans(context.Background(), span, store.evalRules)

	if len(mq.jobs) != 0 {
		t.Fatalf("expected 0 jobs for disabled rule, got %d", len(mq.jobs))
	}
}

func TestEvalSpans_FilterNoMatch(t *testing.T) {
	mq := &FakeEvalQueue{}
	store := &fakeMetaStore{
		evalRules: []domain.EvalRule{
			{RuleID: "rule-1", Enabled: true, SampleRate: 1.0, Filter: domain.EvalFilter{Model: strPtr("gpt-4o")}},
		},
	}
	p := &Pipeline{
		ingest:  &fakeIngestQueue{},
		db:      nil,
		pricing: pricing.GetDefaultBundled(),
		store:   store,
		evalQ:   mq,
	}

	span := &domain.Span{SpanID: "span-1", TraceID: "trace-1", ProjectID: "proj-1", Model: "claude-sonnet-4-6"}
	p.evalSpans(context.Background(), span, store.evalRules)

	if len(mq.jobs) != 0 {
		t.Fatalf("expected 0 jobs when filter doesn't match, got %d", len(mq.jobs))
	}
}

func TestIsSampled_Rate1ReturnsTrue(t *testing.T) {
	if !isSampled(1.0) {
		t.Error("expected isSampled(1.0) to return true")
	}
	if !isSampled(2.0) {
		t.Error("expected isSampled(2.0) to return true")
	}
}

func TestIsSampled_RateZeroReturnsFalse(t *testing.T) {
	if isSampled(0.0) {
		t.Error("expected isSampled(0.0) to return false")
	}
	if isSampled(-1.0) {
		t.Error("expected isSampled(-1.0) to return false")
	}
}

func TestIsSampled_RateFractional(t *testing.T) {
	const trials = 1000
	trueCount := 0
	for range trials {
		if isSampled(0.5) {
			trueCount++
		}
	}

	lower := float64(trials) * 0.3
	upper := float64(trials) * 0.7
	if float64(trueCount) < lower || float64(trueCount) > upper {
		t.Errorf("sample_rate=0.5: expected ~%d true (range %.0f–%.0f), got %d",
			trials/2, lower, upper, trueCount)
	}
}
