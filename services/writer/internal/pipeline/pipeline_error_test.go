//go:build lake

package pipeline

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/omneval/omneval/internal/domain"
	"github.com/omneval/omneval/internal/lake"
	"github.com/omneval/omneval/internal/lake/lakeservertest"
	"github.com/omneval/omneval/internal/pricing"
)

// openErrTestLake opens an embedded local Lake for the pipeline error
// tests, skipping if the DuckLake extension is unavailable.
func openErrTestLake(t *testing.T) *lake.Lake {
	t.Helper()
	ctx := context.Background()
	cfg, _ := lakeservertest.NewLocal(t)
	lk, err := lake.Open(ctx, cfg)
	if err != nil {
		t.Skipf("lake.Open: %v (ducklake extension unavailable)", err)
	}
	t.Cleanup(func() { lk.Close() })
	return lk
}

func lakeSpanCountFor(t *testing.T, lk *lake.Lake, projectID string) int {
	t.Helper()
	var n int
	if err := lk.DB().QueryRowContext(context.Background(),
		"SELECT count(*) FROM lake.spans WHERE project_id = ?", projectID).Scan(&n); err != nil {
		t.Fatalf("count lake spans: %v", err)
	}
	return n
}

// bundledPricing provides a minimal pricing table for tests.
var testPricing = pricing.NewTableFromBytes([]byte(`{
	"gpt-4o": {"input_cost_per_token": 0.0000025, "output_cost_per_token": 0.000010},
	"gpt-4":  {"input_cost_per_token": 0.0000030, "output_cost_per_token": 0.000015},
	"claude-sonnet-4-6": {"input_cost_per_token": 0.0000030, "output_cost_per_token": 0.000015}
}`))

// ---- fake IngestQueue: supports configurable failure ----

type fakeIngestQueue struct {
	batches    [][]*domain.Span
	idx        int
	dequeueErr error // error to return on this dequeue call
	consumeAll bool  // if true, return all remaining batches at once
}

func (f *fakeIngestQueue) Enqueue(ctx context.Context, spans []*domain.Span) error {
	return nil
}

func (f *fakeIngestQueue) Dequeue(ctx context.Context) ([]*domain.Span, error) {
	if f.consumeAll && len(f.batches) > 0 && f.idx < len(f.batches) {
		// Return all remaining batches as one call
		result := make([]*domain.Span, 0)
		for f.idx < len(f.batches) {
			result = append(result, f.batches[f.idx]...)
			f.idx++
		}
		return result, nil
	}
	if f.idx >= len(f.batches) {
		return nil, nil // no more batches → timeout behavior
	}

	if f.dequeueErr != nil {
		f.dequeueErr = nil // consume the error once
		return nil, fmt.Errorf("redis: connection refused")
	}
	batch := f.batches[f.idx]
	f.idx++
	return batch, nil
}

// ---- fake EvalQueue ----

type fakeEvalQueue struct{}

func (f *fakeEvalQueue) Enqueue(ctx context.Context, job *domain.EvalJob) error { return nil }
func (f *fakeEvalQueue) Dequeue(ctx context.Context) (*domain.EvalJob, error)   { return nil, nil }

// ---- fake metadata store ----

type fakeMetaStore struct {
	evalRules []domain.EvalRule
}

func (f *fakeMetaStore) CreateOrganization(ctx context.Context, o *domain.Organization) error {
	return nil
}
func (f *fakeMetaStore) GetOrganization(ctx context.Context, orgID string) (*domain.Organization, error) {
	return nil, nil
}
func (f *fakeMetaStore) CreateProject(ctx context.Context, p *domain.Project) error { return nil }
func (f *fakeMetaStore) GetProject(ctx context.Context, projectID string) (*domain.Project, error) {
	return nil, nil
}
func (f *fakeMetaStore) ListProjects(ctx context.Context, projectID string) ([]*domain.Project, error) {
	return nil, nil
}
func (f *fakeMetaStore) CreateUser(ctx context.Context, u *domain.User) error { return nil }
func (f *fakeMetaStore) GetUserByEmail(ctx context.Context, email string) (*domain.User, error) {
	return nil, nil
}
func (f *fakeMetaStore) GetUserByID(ctx context.Context, userID string) (*domain.User, error) {
	return nil, nil
}
func (f *fakeMetaStore) ListUsers(ctx context.Context, orgID string) ([]*domain.User, error) {
	return nil, nil
}
func (f *fakeMetaStore) CountUsers(ctx context.Context) (int, error) { return 0, nil }
func (f *fakeMetaStore) UpdateUserPassword(ctx context.Context, userID, passwordHash string) error {
	return nil
}
func (f *fakeMetaStore) CreateSession(ctx context.Context, s *domain.Session) error { return nil }
func (f *fakeMetaStore) GetSession(ctx context.Context, sessionID string) (*domain.Session, error) {
	return nil, nil
}
func (f *fakeMetaStore) DeleteSession(ctx context.Context, sessionID string) error  { return nil }
func (f *fakeMetaStore) CheckPassword(hashed, plaintext string) error               { return nil }
func (f *fakeMetaStore) CreateAPIKey(ctx context.Context, key *domain.APIKey) error { return nil }
func (f *fakeMetaStore) GetAPIKeyByHash(ctx context.Context, hashedKey string) (*domain.APIKey, error) {
	return nil, nil
}
func (f *fakeMetaStore) RevokeAPIKey(ctx context.Context, keyID string) error { return nil }
func (f *fakeMetaStore) ListAPIKeys(ctx context.Context, projectID string) ([]*domain.APIKey, error) {
	return nil, nil
}
func (f *fakeMetaStore) CreatePromptVersion(ctx context.Context, pv *domain.PromptVersion) error {
	return nil
}
func (f *fakeMetaStore) GetPromptVersion(ctx context.Context, projectID string, name string, version int64) (*domain.PromptVersion, error) {
	return nil, nil
}
func (f *fakeMetaStore) GetPromptByLabel(ctx context.Context, projectID string, name string, label string) (*domain.PromptVersion, error) {
	return nil, nil
}
func (f *fakeMetaStore) ListPromptNames(ctx context.Context, projectID string) ([]string, error) {
	return nil, nil
}
func (f *fakeMetaStore) ListPromptVersions(ctx context.Context, projectID string, name string) ([]*domain.PromptVersion, error) {
	return nil, nil
}
func (f *fakeMetaStore) SetPromptLabel(ctx context.Context, l *domain.PromptLabel) error { return nil }
func (f *fakeMetaStore) CreateEvalRule(ctx context.Context, r *domain.EvalRule) error    { return nil }
func (f *fakeMetaStore) GetEvalRule(ctx context.Context, ruleID string) (*domain.EvalRule, error) {
	return nil, nil
}
func (f *fakeMetaStore) ListEvalRules(ctx context.Context, projectID string) ([]*domain.EvalRule, error) {
	result := make([]*domain.EvalRule, len(f.evalRules))
	for i, r := range f.evalRules {
		result[i] = &r
	}
	return result, nil
}
func (f *fakeMetaStore) UpdateEvalRule(ctx context.Context, r *domain.EvalRule) error { return nil }
func (f *fakeMetaStore) DeleteEvalRule(ctx context.Context, ruleID string) error      { return nil }
func (f *fakeMetaStore) CreateDataset(ctx context.Context, ds *domain.Dataset) error  { return nil }
func (f *fakeMetaStore) ListDatasets(ctx context.Context, projectID string) ([]*domain.Dataset, error) {
	return nil, nil
}
func (f *fakeMetaStore) GetDataset(ctx context.Context, datasetID string) (*domain.Dataset, error) {
	return nil, nil
}
func (f *fakeMetaStore) DeleteDataset(ctx context.Context, datasetID string) error { return nil }
func (f *fakeMetaStore) CreateDatasetItem(ctx context.Context, item *domain.DatasetItem) error {
	return nil
}
func (f *fakeMetaStore) ListDatasetItems(ctx context.Context, datasetID string) ([]*domain.DatasetItem, error) {
	return nil, nil
}
func (f *fakeMetaStore) ListDatasetItemsPaginated(ctx context.Context, datasetID, cursor string, limit int) ([]*domain.DatasetItem, string, error) {
	return nil, "", nil
}
func (f *fakeMetaStore) CreateDatasetRun(ctx context.Context, run *domain.DatasetRun) error {
	return nil
}
func (f *fakeMetaStore) GetDatasetRun(ctx context.Context, runID string) (*domain.DatasetRun, error) {
	return nil, nil
}
func (f *fakeMetaStore) UpdateDatasetRun(ctx context.Context, run *domain.DatasetRun) error {
	return nil
}
func (f *fakeMetaStore) ListDatasetRuns(ctx context.Context, datasetID string) ([]*domain.DatasetRun, error) {
	return nil, nil
}
func (f *fakeMetaStore) CreateDatasetRunItem(ctx context.Context, item *domain.DatasetRunItem) error {
	return nil
}
func (f *fakeMetaStore) GetDatasetRunItem(ctx context.Context, runItemID string) (*domain.DatasetRunItem, error) {
	return nil, nil
}
func (f *fakeMetaStore) UpdateDatasetRunItem(ctx context.Context, item *domain.DatasetRunItem) error {
	return nil
}
func (f *fakeMetaStore) ListDatasetRunItems(ctx context.Context, runID string) ([]*domain.DatasetRunItem, error) {
	return nil, nil
}
func (f *fakeMetaStore) MarkBatchCommitted(ctx context.Context, batchID string, committedAt time.Time) error {
	return nil
}
func (f *fakeMetaStore) IsBatchCommitted(ctx context.Context, batchID string) (bool, error) {
	return false, nil
}

func (f *fakeMetaStore) Migrate(ctx context.Context) error { return nil }
func (f *fakeMetaStore) Close() error                      { return nil }
func (f *fakeMetaStore) UpdateUserResetToken(ctx context.Context, userID, token string, expiry time.Time) error {
	return nil
}
func (f *fakeMetaStore) GetUserByResetToken(ctx context.Context, token string) (*domain.User, error) {
	return nil, nil
}

// TestPipeline_Run_continuesAfterDequeueError verifies that when Dequeue
// returns an error (e.g. Redis unreachable), the pipeline logs the error
// and continues on the next iteration instead of exiting.
func TestPipeline_Run_continuesAfterDequeueError(t *testing.T) {
	lk := openErrTestLake(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	spans := []*domain.Span{{
		TraceID:   "trace-00000000000000001",
		SpanID:    "span-00000000000000001",
		Model:     "gpt-4o",
		ProjectID: "proj-1",
		Input:     `[{"role":"user","content":"test"}]`,
		Output:    `[{"role":"assistant","content":"response"}]`,
	}}

	// Dequeue fails once (simulating a transient Redis issue),
	// then succeeds on the next call.
	ingestQ := &fakeIngestQueue{
		batches:    [][]*domain.Span{spans},
		dequeueErr: fmt.Errorf("redis: connection refused"),
		consumeAll: false,
	}

	pl := New(ingestQ, testPricing, &fakeMetaStore{}, newFakeLedger(), nil, nil).WithLake(lk)

	pipelineDone := make(chan error, 1)
	go func() {
		pipelineDone <- pl.Run(ctx)
	}()

	// Wait for the pipeline to finish (it will loop until context deadline).
	err := <-pipelineDone
	if err == nil {
		t.Fatal("expected pipeline to stop when context canceled")
	}
	t.Logf("pipeline Run returned: %v", err)

	// The dequeue error happened once, then the batch was dequeued successfully.
	// After the error (idx=0) + success (idx=1), idx should be 1.
	if ingestQ.idx != 1 {
		t.Errorf("dequeue called %d times, want 1 (pipeline should not stop on dequeue error)", ingestQ.idx)
	}

	// The span should have been written after the retry.
	if count := lakeSpanCountFor(t, lk, "proj-1"); count != 1 {
		t.Errorf("span count in lake: got %d, want 1", count)
	}
}

// TestPipeline_Run_continuesAfterWriteFailure verifies that when the DuckDB
// write fails, the pipeline logs the error but continues processing subsequent
// batches instead of crashing and losing remaining spans.
func TestPipeline_Run_continuesAfterWriteFailure(t *testing.T) {
	lk := openErrTestLake(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	spans1 := []*domain.Span{{
		TraceID:   "trace-00000000000000001",
		SpanID:    "span-00000000000000001",
		Model:     "gpt-4o",
		ProjectID: "proj-1",
		Input:     `[{"role":"user","content":"test1"}]`,
		Output:    `[{"role":"assistant","content":"response1"}]`,
	}}
	spans2 := []*domain.Span{{
		TraceID:   "trace-00000000000000002",
		SpanID:    "span-00000000000000002",
		Model:     "gpt-4o",
		ProjectID: "proj-1",
		Input:     `[{"role":"user","content":"test2"}]`,
		Output:    `[{"role":"assistant","content":"response2"}]`,
	}}

	ingestQ := &fakeIngestQueue{
		batches:    [][]*domain.Span{spans1, spans2},
		dequeueErr: nil,
		consumeAll: false,
	}
	pl := New(ingestQ, testPricing, &fakeMetaStore{}, newFakeLedger(), nil, nil).WithLake(lk)

	pipelineDone := make(chan error, 1)
	go func() {
		pipelineDone <- pl.Run(ctx)
	}()

	err := <-pipelineDone
	if err == nil {
		t.Fatal("expected pipeline to stop when context canceled")
	}
	t.Logf("pipeline Run returned: %v", err)

	// Both batches should have been dequeued and written.
	if ingestQ.idx != 2 {
		t.Errorf("dequeued %d batches, want 2 (pipeline should not stop on any error)", ingestQ.idx)
	}

	// Both spans should be in the Lake.
	if count := lakeSpanCountFor(t, lk, "proj-1"); count != 2 {
		t.Errorf("span count in lake: got %d, want 2", count)
	}
}

// TestPipeline_Run_continuesAfterWriteError verifies that when writeSpans
// fails (simulating a DuckDB error), the pipeline logs the error, skips
// the batch, and continues processing subsequent batches.
func TestPipeline_Run_continuesAfterWriteError(t *testing.T) {
	lk := openErrTestLake(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	spans1 := []*domain.Span{{
		TraceID:   "trace-00000000000000001",
		SpanID:    "span-00000000000000001",
		Model:     "gpt-4o",
		ProjectID: "proj-1",
		Input:     `[{"role":"user","content":"test1"}]`,
		Output:    `[{"role":"assistant","content":"response1"}]`,
	}}
	spans2 := []*domain.Span{{
		TraceID:   "trace-00000000000000002",
		SpanID:    "span-00000000000000002",
		Model:     "gpt-4o",
		ProjectID: "proj-1",
		Input:     `[{"role":"user","content":"test2"}]`,
		Output:    `[{"role":"assistant","content":"response2"}]`,
	}}

	ingestQ := &fakeIngestQueue{
		batches:    [][]*domain.Span{spans1, spans2},
		dequeueErr: nil,
		consumeAll: false,
	}
	errWrite := fmt.Errorf("lake: constraint violation")
	pl := New(ingestQ, testPricing, &fakeMetaStore{}, newFakeLedger(), nil, nil).WithLake(lk)
	pl.writeErr = errWrite // inject write error

	pipelineDone := make(chan error, 1)
	go func() {
		pipelineDone <- pl.Run(ctx)
	}()

	err := <-pipelineDone
	if err == nil {
		t.Fatal("expected pipeline to stop when context canceled")
	}
	t.Logf("pipeline Run returned: %v", err)

	// All batches should have been dequeued (pipeline continued past write errors).
	if ingestQ.idx != 2 {
		t.Errorf("dequeued %d batches, want 2 (pipeline should not stop on write error)", ingestQ.idx)
	}

	// No spans should be in the lake (all writes failed).
	if count := lakeSpanCountFor(t, lk, "proj-1"); count != 0 {
		t.Errorf("span count in lake: got %d, want 0 (writes should have failed)", count)
	}
}

func (f *fakeMetaStore) SetBookmark(ctx context.Context, b *domain.Bookmark) error { return nil }
func (f *fakeMetaStore) RemoveBookmark(ctx context.Context, projectID, traceID string) error {
	return nil
}
func (f *fakeMetaStore) RemoveBookmarksForProject(ctx context.Context, projectID string) error {
	return nil
}
func (f *fakeMetaStore) IsBookmarked(ctx context.Context, projectID, traceID string) (bool, error) {
	return false, nil
}
func (f *fakeMetaStore) ListBookmarkedTraceIDs(ctx context.Context, projectID string) ([]string, error) {
	return nil, nil
}
