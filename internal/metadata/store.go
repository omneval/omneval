package metadata

import (
	"context"

	"github.com/zbloss/lantern/internal/domain"
)

// Store is the interface all metadata backends must satisfy. Implementations
// exist for Postgres (production) and SQLite (demo / docker-compose).
type Store interface {
	// Organizations
	CreateOrganization(ctx context.Context, org *domain.Organization) error
	GetOrganization(ctx context.Context, orgID string) (*domain.Organization, error)

	// Projects
	CreateProject(ctx context.Context, project *domain.Project) error
	GetProject(ctx context.Context, projectID string) (*domain.Project, error)
	ListProjects(ctx context.Context, orgID string) ([]*domain.Project, error)

	// Users
	CreateUser(ctx context.Context, user *domain.User) error
	GetUserByEmail(ctx context.Context, email string) (*domain.User, error)
	ListUsers(ctx context.Context, orgID string) ([]*domain.User, error)

	// Sessions
	CreateSession(ctx context.Context, session *domain.Session) error
	GetSession(ctx context.Context, sessionID string) (*domain.Session, error)
	DeleteSession(ctx context.Context, sessionID string) error

	// API Keys
	CreateAPIKey(ctx context.Context, key *domain.APIKey) error
	GetAPIKeyByHash(ctx context.Context, hashedKey string) (*domain.APIKey, error)
	RevokeAPIKey(ctx context.Context, keyID string) error
	ListAPIKeys(ctx context.Context, projectID string) ([]*domain.APIKey, error)

	// Prompt Registry
	CreatePromptVersion(ctx context.Context, pv *domain.PromptVersion) error
	GetPromptVersion(ctx context.Context, projectID, name string, version int64) (*domain.PromptVersion, error)
	GetPromptByLabel(ctx context.Context, projectID, name, label string) (*domain.PromptVersion, error)
	ListPromptVersions(ctx context.Context, projectID, name string) ([]*domain.PromptVersion, error)
	SetPromptLabel(ctx context.Context, label *domain.PromptLabel) error

	// Eval Rules
	CreateEvalRule(ctx context.Context, rule *domain.EvalRule) error
	GetEvalRule(ctx context.Context, ruleID string) (*domain.EvalRule, error)
	ListEvalRules(ctx context.Context, projectID string) ([]*domain.EvalRule, error)
	UpdateEvalRule(ctx context.Context, rule *domain.EvalRule) error

	// Datasets
	CreateDataset(ctx context.Context, ds *domain.Dataset) error
	GetDataset(ctx context.Context, datasetID string) (*domain.Dataset, error)
	CreateDatasetItem(ctx context.Context, item *domain.DatasetItem) error
	ListDatasetItems(ctx context.Context, datasetID string) ([]*domain.DatasetItem, error)
	CreateDatasetRun(ctx context.Context, run *domain.DatasetRun) error
	GetDatasetRun(ctx context.Context, runID string) (*domain.DatasetRun, error)

	// Migrations
	Migrate(ctx context.Context) error

	// Lifecycle
	Close() error
}
