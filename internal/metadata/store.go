package metadata

import (
	"context"
	"errors"
	"time"

	"github.com/omneval/omneval/internal/domain"
)

// ErrNotFound is returned when a requested entity does not exist.
var ErrNotFound = errors.New("metadata: not found")

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
	GetUserByID(ctx context.Context, userID string) (*domain.User, error)
	ListUsers(ctx context.Context, orgID string) ([]*domain.User, error)
	CountUsers(ctx context.Context) (int, error)
	UpdateUserPassword(ctx context.Context, userID, passwordHash string) error
	// Password reset token management
	UpdateUserResetToken(ctx context.Context, userID, token string, expiry time.Time) error
	GetUserByResetToken(ctx context.Context, token string) (*domain.User, error)

	// Sessions
	CreateSession(ctx context.Context, session *domain.Session) error
	GetSession(ctx context.Context, sessionID string) (*domain.Session, error)
	DeleteSession(ctx context.Context, sessionID string) error

	// Auth helpers (available on all implementations)
	CheckPassword(hashed, plaintext string) error

	// API Keys
	CreateAPIKey(ctx context.Context, key *domain.APIKey) error
	GetAPIKeyByHash(ctx context.Context, hashedKey string) (*domain.APIKey, error)
	RevokeAPIKey(ctx context.Context, keyID string) error
	ListAPIKeys(ctx context.Context, projectID string) ([]*domain.APIKey, error)

	// Prompt Registry
	CreatePromptVersion(ctx context.Context, pv *domain.PromptVersion) error
	GetPromptVersion(ctx context.Context, projectID, name string, version int64) (*domain.PromptVersion, error)
	GetPromptByLabel(ctx context.Context, projectID, name, label string) (*domain.PromptVersion, error)
	ListPromptNames(ctx context.Context, projectID string) ([]string, error)
	ListPromptVersions(ctx context.Context, projectID, name string) ([]*domain.PromptVersion, error)
	SetPromptLabel(ctx context.Context, label *domain.PromptLabel) error

	// Eval Rules
	CreateEvalRule(ctx context.Context, rule *domain.EvalRule) error
	GetEvalRule(ctx context.Context, ruleID string) (*domain.EvalRule, error)
	ListEvalRules(ctx context.Context, projectID string) ([]*domain.EvalRule, error)
	UpdateEvalRule(ctx context.Context, rule *domain.EvalRule) error
	DeleteEvalRule(ctx context.Context, ruleID string) error

	// Datasets
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
	GetDatasetRunItem(ctx context.Context, runItemID string) (*domain.DatasetRunItem, error)
	UpdateDatasetRunItem(ctx context.Context, item *domain.DatasetRunItem) error
	ListDatasetRunItems(ctx context.Context, runID string) ([]*domain.DatasetRunItem, error)

	// Migrations
	Migrate(ctx context.Context) error

	// Lifecycle
	Close() error
}
