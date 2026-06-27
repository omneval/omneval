package pipeline

import (
	"context"
	"fmt"
	"time"

	"github.com/omneval/omneval/internal/domain"
	"github.com/omneval/omneval/internal/metadata"
	"github.com/omneval/omneval/internal/pricing"
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

// PromptStore returns a focused PromptStore interface for callers that only
// need prompt operations.
func (f *fakeMetaStore) PromptStore() metadata.PromptStore { return f }

func (f *fakeMetaStore) CreateEvalRule(ctx context.Context, r *domain.EvalRule) error { return nil }
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
func (f *fakeMetaStore) CreateDatasetItemsBatch(ctx context.Context, items []*domain.DatasetItem) error {
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

// BookmarkStore returns the focused BookmarkStore interface.
func (f *fakeMetaStore) BookmarkStore() metadata.BookmarkStore { return f }

// DatasetStore returns the focused DatasetStore interface.
func (f *fakeMetaStore) DatasetStore() metadata.DatasetStore { return f }