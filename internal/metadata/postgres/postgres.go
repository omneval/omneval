package postgres

import (
	"context"

	"github.com/zbloss/lantern/internal/domain"
	"github.com/zbloss/lantern/internal/metadata"
)

// Store is the Postgres-backed implementation of metadata.Store.
// Migrations live in ./migrations/ and are applied via golang-migrate.
type Store struct {
	// TODO: embed *sql.DB or pgx pool
}

// New opens a Postgres connection and returns a Store.
func New(dsn string) (*Store, error) {
	// TODO: implement
	panic("not implemented")
}

func (s *Store) Close() error                                         { return nil }
func (s *Store) Migrate(ctx context.Context) error                   { return nil }
func (s *Store) CreateOrganization(ctx context.Context, o *domain.Organization) error { return metadata.ErrNotFound }
func (s *Store) GetOrganization(ctx context.Context, id string) (*domain.Organization, error) { return nil, metadata.ErrNotFound }
func (s *Store) CreateProject(ctx context.Context, p *domain.Project) error              { return metadata.ErrNotFound }
func (s *Store) GetProject(ctx context.Context, id string) (*domain.Project, error)      { return nil, metadata.ErrNotFound }
func (s *Store) ListProjects(ctx context.Context, orgID string) ([]*domain.Project, error) { return nil, metadata.ErrNotFound }
func (s *Store) CreateUser(ctx context.Context, u *domain.User) error                     { return metadata.ErrNotFound }
func (s *Store) GetUserByEmail(ctx context.Context, email string) (*domain.User, error)   { return nil, metadata.ErrNotFound }
func (s *Store) ListUsers(ctx context.Context, orgID string) ([]*domain.User, error)      { return nil, metadata.ErrNotFound }
func (s *Store) CreateSession(ctx context.Context, sess *domain.Session) error            { return metadata.ErrNotFound }
func (s *Store) GetSession(ctx context.Context, id string) (*domain.Session, error)       { return nil, metadata.ErrNotFound }
func (s *Store) DeleteSession(ctx context.Context, id string) error                       { return metadata.ErrNotFound }
func (s *Store) CreateAPIKey(ctx context.Context, k *domain.APIKey) error                 { return metadata.ErrNotFound }
func (s *Store) GetAPIKeyByHash(ctx context.Context, hash string) (*domain.APIKey, error) { return nil, metadata.ErrNotFound }
func (s *Store) RevokeAPIKey(ctx context.Context, id string) error                        { return metadata.ErrNotFound }
func (s *Store) ListAPIKeys(ctx context.Context, projectID string) ([]*domain.APIKey, error) { return nil, metadata.ErrNotFound }
func (s *Store) CreatePromptVersion(ctx context.Context, p *domain.PromptVersion) error   { return metadata.ErrNotFound }
func (s *Store) GetPromptVersion(ctx context.Context, projectID, name string, version int64) (*domain.PromptVersion, error) { return nil, metadata.ErrNotFound }
func (s *Store) GetPromptByLabel(ctx context.Context, projectID, name, label string) (*domain.PromptVersion, error) { return nil, metadata.ErrNotFound }
func (s *Store) ListPromptVersions(ctx context.Context, projectID, name string) ([]*domain.PromptVersion, error) { return nil, metadata.ErrNotFound }
func (s *Store) SetPromptLabel(ctx context.Context, l *domain.PromptLabel) error          { return metadata.ErrNotFound }
func (s *Store) CreateEvalRule(ctx context.Context, r *domain.EvalRule) error             { return metadata.ErrNotFound }
func (s *Store) GetEvalRule(ctx context.Context, id string) (*domain.EvalRule, error)     { return nil, metadata.ErrNotFound }
func (s *Store) ListEvalRules(ctx context.Context, projectID string) ([]*domain.EvalRule, error) { return nil, metadata.ErrNotFound }
func (s *Store) UpdateEvalRule(ctx context.Context, r *domain.EvalRule) error             { return metadata.ErrNotFound }
func (s *Store) CreateDataset(ctx context.Context, d *domain.Dataset) error               { return metadata.ErrNotFound }
func (s *Store) GetDataset(ctx context.Context, id string) (*domain.Dataset, error)       { return nil, metadata.ErrNotFound }
func (s *Store) CreateDatasetItem(ctx context.Context, i *domain.DatasetItem) error       { return metadata.ErrNotFound }
func (s *Store) ListDatasetItems(ctx context.Context, datasetID string) ([]*domain.DatasetItem, error) { return nil, metadata.ErrNotFound }
func (s *Store) CreateDatasetRun(ctx context.Context, r *domain.DatasetRun) error         { return metadata.ErrNotFound }
func (s *Store) GetDatasetRun(ctx context.Context, id string) (*domain.DatasetRun, error) { return nil, metadata.ErrNotFound }
