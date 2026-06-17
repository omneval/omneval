package metadata

import (
	"context"

	"github.com/omneval/omneval/internal/domain"
)

// ProjectStore is the domain interface for project CRUD and organization lookups.
type ProjectStore interface {
	CreateProject(ctx context.Context, project *domain.Project) error
	GetProject(ctx context.Context, projectID string) (*domain.Project, error)
	ListProjects(ctx context.Context, orgID string) ([]*domain.Project, error)
}