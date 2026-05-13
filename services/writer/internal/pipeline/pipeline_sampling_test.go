package pipeline

import (
	"context"
	"testing"

	"github.com/zbloss/lantern/internal/domain"
	"github.com/zbloss/lantern/internal/pricing"
)

// mockEvalQueue captures enqueued eval jobs for test assertions.
type mockEvalQueue struct {
	jobs []*domain.EvalJob
}

func (m *mockEvalQueue) Enqueue(_ context.Context, job *domain.EvalJob) error {
	m.jobs = append(m.jobs, job)
	return nil
}
func (m *mockEvalQueue) Dequeue(_ context.Context) (*domain.EvalJob, error) {
	return nil, nil
}

// mockIngestQueue satisfies queue.IngestQueue.
type mockIngestQueue struct{}

func (m *mockIngestQueue) Enqueue(_ context.Context, _ []*domain.Span) error {
	return nil
}
func (m *mockIngestQueue) Dequeue(_ context.Context) ([]*domain.Span, error) {
	return nil, nil
}

// mockStore satisfies metadata.Store with zero-value implementations.
type mockStore struct {
	evalRules []domain.EvalRule
}

func (m *mockStore) CreateOrganization(_ context.Context, _ *domain.Organization) error                    { return nil }
func (m *mockStore) GetOrganization(_ context.Context, _ string) (*domain.Organization, error)             { return nil, nil }
func (m *mockStore) CreateProject(_ context.Context, _ *domain.Project) error                              { return nil }
func (m *mockStore) GetProject(_ context.Context, _ string) (*domain.Project, error)                       { return nil, nil }
func (m *mockStore) ListProjects(_ context.Context, _ string) ([]*domain.Project, error)                   { return nil, nil }
func (m *mockStore) CreateUser(_ context.Context, _ *domain.User) error                                    { return nil }
func (m *mockStore) GetUserByEmail(_ context.Context, _ string) (*domain.User, error)                      { return nil, nil }
func (m *mockStore) GetUserByID(_ context.Context, _ string) (*domain.User, error)                         { return nil, nil }
func (m *mockStore) ListUsers(_ context.Context, _ string) ([]*domain.User, error)                         { return nil, nil }
func (m *mockStore) CountUsers(_ context.Context) (int, error)                                             { return 0, nil }
func (m *mockStore) UpdateUserPassword(_ context.Context, _, _ string) error                               { return nil }
func (m *mockStore) CreateSession(_ context.Context, _ *domain.Session) error                              { return nil }
func (m *mockStore) GetSession(_ context.Context, _ string) (*domain.Session, error)                       { return nil, nil }
func (m *mockStore) DeleteSession(_ context.Context, _ string) error                                       { return nil }
func (m *mockStore) CheckPassword(_, _ string) error                                                       { return nil }
func (m *mockStore) CreateAPIKey(_ context.Context, _ *domain.APIKey) error                                { return nil }
func (m *mockStore) GetAPIKeyByHash(_ context.Context, _ string) (*domain.APIKey, error)                  { return nil, nil }
func (m *mockStore) RevokeAPIKey(_ context.Context, _ string) error                                        { return nil }
func (m *mockStore) ListAPIKeys(_ context.Context, _ string) ([]*domain.APIKey, error)                    { return nil, nil }
func (m *mockStore) CreatePromptVersion(_ context.Context, _ *domain.PromptVersion) error                  { return nil }
func (m *mockStore) GetPromptVersion(_ context.Context, _, _ string, _ int64) (*domain.PromptVersion, error) {
	return nil, nil
}
func (m *mockStore) GetPromptByLabel(_ context.Context, _, _, _ string) (*domain.PromptVersion, error) {
	return nil, nil
}
func (m *mockStore) ListPromptNames(_ context.Context, _ string) ([]string, error)                  { return nil, nil }
func (m *mockStore) ListPromptVersions(_ context.Context, _, _ string) ([]*domain.PromptVersion, error) {
	return nil, nil
}
func (m *mockStore) SetPromptLabel(_ context.Context, _ *domain.PromptLabel) error         { return nil }
func (m *mockStore) CreateEvalRule(_ context.Context, _ *domain.EvalRule) error            { return nil }
func (m *mockStore) GetEvalRule(_ context.Context, _ string) (*domain.EvalRule, error)     { return nil, nil }
func (m *mockStore) ListEvalRules(_ context.Context, _ string) ([]*domain.EvalRule, error) {
	result := make([]*domain.EvalRule, len(m.evalRules))
	for i, r := range m.evalRules {
		result[i] = &r
	}
	return result, nil
}
func (m *mockStore) UpdateEvalRule(_ context.Context, _ *domain.EvalRule) error   { return nil }
func (m *mockStore) DeleteEvalRule(_ context.Context, _ string) error             { return nil }
func (m *mockStore) CreateDataset(_ context.Context, _ *domain.Dataset) error     { return nil }
func (m *mockStore) ListDatasets(_ context.Context, _ string) ([]*domain.Dataset, error) {
	return nil, nil
}
func (m *mockStore) GetDataset(_ context.Context, _ string) (*domain.Dataset, error) {
	return nil, nil
}
func (m *mockStore) DeleteDataset(_ context.Context, _ string) error { return nil }
func (m *mockStore) CreateDatasetItem(_ context.Context, _ *domain.DatasetItem) error {
	return nil
}
func (m *mockStore) ListDatasetItems(_ context.Context, _ string) ([]*domain.DatasetItem, error) {
	return nil, nil
}
func (m *mockStore) ListDatasetItemsPaginated(_ context.Context, _, _ string, _ int) ([]*domain.DatasetItem, string, error) {
	return nil, "", nil
}
func (m *mockStore) CreateDatasetRun(_ context.Context, _ *domain.DatasetRun) error { return nil }
func (m *mockStore) GetDatasetRun(_ context.Context, _ string) (*domain.DatasetRun, error) {
	return nil, nil
}
func (m *mockStore) UpdateDatasetRun(_ context.Context, _ *domain.DatasetRun) error { return nil }
func (m *mockStore) ListDatasetRuns(_ context.Context, _ string) ([]*domain.DatasetRun, error) {
	return nil, nil
}
func (m *mockStore) CreateDatasetRunItem(_ context.Context, _ *domain.DatasetRunItem) error {
	return nil
}
func (m *mockStore) GetDatasetRunItem(_ context.Context, _ string) (*domain.DatasetRunItem, error) {
	return nil, nil
}
func (m *mockStore) UpdateDatasetRunItem(_ context.Context, _ *domain.DatasetRunItem) error {
	return nil
}
func (m *mockStore) ListDatasetRunItems(_ context.Context, _ string) ([]*domain.DatasetRunItem, error) {
	return nil, nil
}
func (m *mockStore) Migrate(_ context.Context) error  { return nil }
func (m *mockStore) Close() error                     { return nil }

// ---- tests ----

func TestEvalSpans_SampleRate1EnqueuesAll(t *testing.T) {
	mq := &mockEvalQueue{}
	store := &mockStore{
		evalRules: []domain.EvalRule{
			{RuleID: "rule-1", Enabled: true, SampleRate: 1.0, Filter: domain.EvalFilter{}},
		},
	}
	p := &Pipeline{
		ingest:  &mockIngestQueue{},
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
	mq := &mockEvalQueue{}
	store := &mockStore{
		evalRules: []domain.EvalRule{
			{RuleID: "rule-1", Enabled: true, SampleRate: 0.0, Filter: domain.EvalFilter{}},
		},
	}
	p := &Pipeline{
		ingest:  &mockIngestQueue{},
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
	mq := &mockEvalQueue{}
	store := &mockStore{
		evalRules: []domain.EvalRule{
			{RuleID: "rule-1", Enabled: true, SampleRate: 0.5, Filter: domain.EvalFilter{}},
		},
	}
	p := &Pipeline{
		ingest:  &mockIngestQueue{},
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
	mq := &mockEvalQueue{}
	store := &mockStore{
		evalRules: []domain.EvalRule{
			{RuleID: "rule-1", Enabled: false, SampleRate: 1.0, Filter: domain.EvalFilter{}},
		},
	}
	p := &Pipeline{
		ingest:  &mockIngestQueue{},
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
	mq := &mockEvalQueue{}
	store := &mockStore{
		evalRules: []domain.EvalRule{
			{RuleID: "rule-1", Enabled: true, SampleRate: 1.0, Filter: domain.EvalFilter{Model: strPtr("gpt-4o")}},
		},
	}
	p := &Pipeline{
		ingest:  &mockIngestQueue{},
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
	// isSampled should return true for rate >= 1.0 (deterministic).
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
	// Run 1000 trials at 0.5 rate and check ~50% return true.
	const trials = 1000
	trueCount := 0
	for i := 0; i < trials; i++ {
		if isSampled(0.5) {
			trueCount++
		}
		_ = i
	}

	lower := float64(trials) * 0.3
	upper := float64(trials) * 0.7
	if float64(trueCount) < lower || float64(trueCount) > upper {
		t.Errorf("sample_rate=0.5: expected ~%d true (range %.0f–%.0f), got %d",
			trials/2, lower, upper, trueCount)
	}
}


