package metadata

import (
	"context"

	"github.com/omneval/omneval/internal/domain"
)

// PromptStore is the domain interface for prompt registry operations:
// CRUD for prompt versions and label management.
type PromptStore interface {
	CreatePromptVersion(ctx context.Context, pv *domain.PromptVersion) error
	GetPromptVersion(ctx context.Context, projectID, name string, version int64) (*domain.PromptVersion, error)
	GetPromptByLabel(ctx context.Context, projectID, name, label string) (*domain.PromptVersion, error)
	ListPromptNames(ctx context.Context, projectID string) ([]string, error)
	ListPromptVersions(ctx context.Context, projectID, name string) ([]*domain.PromptVersion, error)
	SetPromptLabel(ctx context.Context, label *domain.PromptLabel) error
}