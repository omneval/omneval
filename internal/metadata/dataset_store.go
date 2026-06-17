package metadata

import (
	"context"

	"github.com/omneval/omneval/internal/domain"
)

// DatasetStore is the domain interface for dataset CRUD and dataset run operations.
type DatasetStore interface {
	CreateDataset(ctx context.Context, ds *domain.Dataset) error
	ListDatasets(ctx context.Context, projectID string) ([]*domain.Dataset, error)
	GetDataset(ctx context.Context, datasetID string) (*domain.Dataset, error)
	DeleteDataset(ctx context.Context, datasetID string) error

	CreateDatasetItem(ctx context.Context, item *domain.DatasetItem) error
	ListDatasetItems(ctx context.Context, datasetID string) ([]*domain.DatasetItem, error)
	ListDatasetItemsPaginated(ctx context.Context, datasetID, cursor string, limit int) ([]*domain.DatasetItem, string, error)

	CreateDatasetRun(ctx context.Context, run *domain.DatasetRun) error
	GetDatasetRun(ctx context.Context, runID string) (*domain.DatasetRun, error)
	UpdateDatasetRun(ctx context.Context, run *domain.DatasetRun) error
	ListDatasetRuns(ctx context.Context, datasetID string) ([]*domain.DatasetRun, error)

	CreateDatasetRunItem(ctx context.Context, item *domain.DatasetRunItem) error
	ListDatasetRunItems(ctx context.Context, runID string) ([]*domain.DatasetRunItem, error)
	GetDatasetRunItem(ctx context.Context, id string) (*domain.DatasetRunItem, error)
}