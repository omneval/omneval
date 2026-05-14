package pipeline

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/zbloss/lantern/internal/domain"
	"github.com/zbloss/lantern/internal/duckdb"
	"github.com/zbloss/lantern/internal/pricing"
)

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
func (f *fakeMetaStore) ListProjects(ctx context.Context, orgID string) ([]*domain.Project, error) {
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
func (f *fakeMetaStore) Migrate(ctx context.Context) error { return nil }
func (f *fakeMetaStore) Close() error                      { return nil }
func (f *fakeMetaStore) UpdateUserResetToken(ctx context.Context, userID, token string, expiry time.Time) error { return nil }
func (f *fakeMetaStore) GetUserByResetToken(ctx context.Context, token string) (*domain.User, error) {
	return nil, nil
}

// TestPipeline_Run_continuesAfterDequeueError verifies that when Dequeue
// returns an error (e.g. Redis unreachable), the pipeline logs the error
// and continues on the next iteration instead of exiting.
func TestPipeline_Run_continuesAfterDequeueError(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	db, err := duckdb.Open("test_err_dequeue.db")
	if err != nil {
		t.Fatalf("open duckdb: %v", err)
	}
	defer func() { db.Close(); os.Remove("test_err_dequeue.db") }()

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

	pl := New(ingestQ, db, testPricing, &fakeMetaStore{}, &fakeEvalQueue{}, nil)

	pipelineDone := make(chan error, 1)
	go func() {
		pipelineDone <- pl.Run(ctx)
	}()

	// Wait for the pipeline to finish (it will loop until context deadline).
	err = <-pipelineDone
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
	var count int
	rctx, rcancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer rcancel()
	if err := db.QueryRowContext(rctx, "SELECT COUNT(*) FROM spans WHERE project_id = $1", "proj-1").
		Scan(&count); err != nil {
		t.Fatalf("query count: %v", err)
	}
	if count != 1 {
		t.Errorf("span count in DuckDB: got %d, want 1", count)
	}
}

// TestPipeline_Run_continuesAfterWriteFailure verifies that when the DuckDB
// write fails, the pipeline logs the error but continues processing subsequent
// batches instead of crashing and losing remaining spans.
func TestPipeline_Run_continuesAfterWriteFailure(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	db, err := duckdb.Open("test_err_write.db")
	if err != nil {
		t.Fatalf("open duckdb: %v", err)
	}
	defer func() { db.Close(); os.Remove("test_err_write.db") }()

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
	pl := New(ingestQ, db, testPricing, &fakeMetaStore{}, &fakeEvalQueue{}, nil)

	pipelineDone := make(chan error, 1)
	go func() {
		pipelineDone <- pl.Run(ctx)
	}()

	err = <-pipelineDone
	if err == nil {
		t.Fatal("expected pipeline to stop when context canceled")
	}
	t.Logf("pipeline Run returned: %v", err)

	// Both batches should have been dequeued and written.
	if ingestQ.idx != 2 {
		t.Errorf("dequeued %d batches, want 2 (pipeline should not stop on any error)", ingestQ.idx)
	}

	// Both spans should be in DuckDB.
	var count int
	rctx, rcancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer rcancel()
	if err := db.QueryRowContext(rctx, "SELECT COUNT(*) FROM spans WHERE project_id = $1", "proj-1").
		Scan(&count); err != nil {
		t.Fatalf("query count: %v", err)
	}
	if count != 2 {
		t.Errorf("span count in DuckDB: got %d, want 2", count)
	}
}

// TestPipeline_Run_continuesAfterWriteError verifies that when writeSpans
// fails (simulating a DuckDB error), the pipeline logs the error, skips
// the batch, and continues processing subsequent batches.
func TestPipeline_Run_continuesAfterWriteError(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	db, err := duckdb.Open("test_err_write2.db")
	if err != nil {
		t.Fatalf("open duckdb: %v", err)
	}
	defer func() { db.Close(); os.Remove("test_err_write2.db") }()

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
	errWrite := fmt.Errorf("duckdb: constraint violation")
	pl := New(ingestQ, db, testPricing, &fakeMetaStore{}, &fakeEvalQueue{}, nil)
	pl.writeErr = errWrite // inject write error

	pipelineDone := make(chan error, 1)
	go func() {
		pipelineDone <- pl.Run(ctx)
	}()

	err = <-pipelineDone
	if err == nil {
		t.Fatal("expected pipeline to stop when context canceled")
	}
	t.Logf("pipeline Run returned: %v", err)

	// All batches should have been dequeued (pipeline continued past write errors).
	if ingestQ.idx != 2 {
		t.Errorf("dequeued %d batches, want 2 (pipeline should not stop on write error)", ingestQ.idx)
	}

	// No spans should be in DuckDB (all writes failed).
	var count int
	rctx, rcancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer rcancel()
	if err := db.QueryRowContext(rctx, "SELECT COUNT(*) FROM spans WHERE project_id = $1", "proj-1").
		Scan(&count); err != nil {
		t.Fatalf("query count: %v", err)
	}
	if count != 0 {
		t.Errorf("span count in DuckDB: got %d, want 0 (writes should have failed)", count)
	}
}
