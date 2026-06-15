package postgres

import (
	"context"
	"database/sql"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/omneval/omneval/internal/domain"
	"golang.org/x/crypto/bcrypt"
)

//go:embed migrations/0001_init.up.sql
var migrationSQL string

//go:embed migrations/0002_add_reset_token.up.sql
var migrationSQL2 string

//go:embed migrations/0003_add_bookmarks.up.sql
var migrationSQL3 string

//go:embed migrations/0004_add_committed_batches.up.sql
var migrationSQL4 string

// Store is the Postgres-backed implementation of metadata.Store.
type Store struct {
	db       *sql.DB
	migrated bool
}

// New opens a Postgres connection using the given DSN and returns a Store.
func New(dsn string) (*Store, error) {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("postgres: open: %w", err)
	}

	// Verify the connection is actually working
	if err := db.PingContext(context.Background()); err != nil {
		db.Close()
		return nil, fmt.Errorf("postgres: ping: %w", err)
	}

	s := &Store{db: db}
	return s, nil
}

// Migrate applies pending SQL migrations.
// Applies migration 1 (init) and migration 2 (add reset token) if not already applied.
func (s *Store) Migrate(ctx context.Context) error {
	if s.migrated {
		return nil
	}

	// Create schema_migrations table
	if _, err := s.db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS _schema_migrations (
			version INTEGER PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)
	`); err != nil {
		return fmt.Errorf("postgres: create migrations table: %w", err)
	}

	// Check if migration 1 is already applied
	var count int
	err := s.db.QueryRowContext(ctx, "SELECT count(*) FROM _schema_migrations WHERE version = 1").Scan(&count)
	if err != nil {
		return fmt.Errorf("postgres: check migration status: %w", err)
	}
	if count == 0 {
		// Run migration 1
		if err := s.applyMigration(ctx, 1, migrationSQL); err != nil {
			return err
		}
	}

	// Check if migration 2 is already applied
	err = s.db.QueryRowContext(ctx, "SELECT count(*) FROM _schema_migrations WHERE version = 2").Scan(&count)
	if err != nil {
		return fmt.Errorf("postgres: check migration 2 status: %w", err)
	}
	if count == 0 {
		// Run migration 2
		if err := s.applyMigration(ctx, 2, migrationSQL2); err != nil {
			return err
		}
	}

	// Check if migration 3 is already applied
	err = s.db.QueryRowContext(ctx, "SELECT count(*) FROM _schema_migrations WHERE version = 3").Scan(&count)
	if err != nil {
		return fmt.Errorf("postgres: check migration 3 status: %w", err)
	}
	if count == 0 {
		// Run migration 3
		if err := s.applyMigration(ctx, 3, migrationSQL3); err != nil {
			return err
		}
	}

	// Check if migration 4 is already applied
	err = s.db.QueryRowContext(ctx, "SELECT count(*) FROM _schema_migrations WHERE version = 4").Scan(&count)
	if err != nil {
		return fmt.Errorf("postgres: check migration 4 status: %w", err)
	}
	if count == 0 {
		// Run migration 4
		if err := s.applyMigration(ctx, 4, migrationSQL4); err != nil {
			return err
		}
	}

	s.migrated = true
	return nil
}

// applyMigration wraps a single migration SQL in a transaction.
func (s *Store) applyMigration(ctx context.Context, version int64, sql string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("postgres: begin migration %d tx: %w", version, err)
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, sql); err != nil {
		return fmt.Errorf("postgres: apply migration %d: %w", version, err)
	}

	if _, err := tx.ExecContext(ctx, "INSERT INTO _schema_migrations (version) VALUES ($1)", version); err != nil {
		return fmt.Errorf("postgres: record migration %d version: %w", version, err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("postgres: commit migration %d: %w", version, err)
	}
	return nil
}

// Close releases the database connection.
func (s *Store) Close() error {
	return s.db.Close()
}

// DB returns the underlying *sql.DB for testing purposes.
// It is not part of the public API contract.
func (s *Store) DB() *sql.DB {
	return s.db
}

// ---- Organizations ----

func (s *Store) CreateOrganization(ctx context.Context, org *domain.Organization) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO organizations (org_id, name, created_at) VALUES ($1, $2, $3)`,
		org.OrgID, org.Name, org.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("postgres: create org: %w", err)
	}
	return nil
}

func (s *Store) GetOrganization(ctx context.Context, orgID string) (*domain.Organization, error) {
	var org domain.Organization
	var createdAt time.Time
	err := s.db.QueryRowContext(ctx,
		`SELECT name, created_at FROM organizations WHERE org_id = $1`, orgID,
	).Scan(&org.Name, &createdAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("postgres: get org %s: %w", orgID, err)
	}
	return &domain.Organization{OrgID: orgID, Name: org.Name, CreatedAt: createdAt}, nil
}

// ---- Projects ----

func (s *Store) CreateProject(ctx context.Context, project *domain.Project) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO projects (project_id, org_id, name, created_at) VALUES ($1, $2, $3, $4)`,
		project.ProjectID, project.OrgID, project.Name, project.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("postgres: create project: %w", err)
	}
	return nil
}

func (s *Store) GetProject(ctx context.Context, projectID string) (*domain.Project, error) {
	var p domain.Project
	var createdAt time.Time
	err := s.db.QueryRowContext(ctx,
		`SELECT org_id, name, created_at FROM projects WHERE project_id = $1`, projectID,
	).Scan(&p.OrgID, &p.Name, &createdAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("postgres: get project %s: %w", projectID, err)
	}
	return &domain.Project{ProjectID: projectID, OrgID: p.OrgID, Name: p.Name, CreatedAt: createdAt}, nil
}

func (s *Store) ListProjects(ctx context.Context, orgID string) ([]*domain.Project, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT project_id, org_id, name, created_at FROM projects WHERE org_id = $1 ORDER BY project_id`, orgID,
	)
	if err != nil {
		return nil, fmt.Errorf("postgres: list projects: %w", err)
	}
	defer rows.Close()

	var projects []*domain.Project
	for rows.Next() {
		var p domain.Project
		var createdAt time.Time
		if err := rows.Scan(&p.ProjectID, &p.OrgID, &p.Name, &createdAt); err != nil {
			return nil, fmt.Errorf("postgres: scan project: %w", err)
		}
		p.CreatedAt = createdAt
		projects = append(projects, &p)
	}
	return projects, rows.Err()
}

// ---- Users ----

func (s *Store) CreateUser(ctx context.Context, user *domain.User) error {
	// Hash the password only if it's provided (non-empty).
	// Invite flow creates users without an initial password; the user
	// sets their own via the reset token endpoint.
	if user.PasswordHash != "" {
		hashed, err := bcrypt.GenerateFromPassword([]byte(user.PasswordHash), bcrypt.DefaultCost)
		if err != nil {
			return fmt.Errorf("postgres: bcrypt hash: %w", err)
		}
		user.PasswordHash = string(hashed)
	}

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO users (user_id, org_id, email, password_hash, created_at, password_reset_token, reset_token_expiry)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		user.UserID, user.OrgID, user.Email, user.PasswordHash, user.CreatedAt,
		user.PasswordResetToken, user.ResetTokenExpiry,
	)
	if err != nil {
		return fmt.Errorf("postgres: create user: %w", err)
	}
	return nil
}

func (s *Store) GetUserByEmail(ctx context.Context, email string) (*domain.User, error) {
	var u domain.User
	var createdAt time.Time
	err := s.db.QueryRowContext(ctx,
		`SELECT user_id, org_id, password_hash, created_at FROM users WHERE email = $1`, email,
	).Scan(&u.UserID, &u.OrgID, &u.PasswordHash, &createdAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("postgres: get user by email: %w", err)
	}
	return &domain.User{UserID: u.UserID, OrgID: u.OrgID, Email: email, PasswordHash: u.PasswordHash, CreatedAt: createdAt}, nil
}

func (s *Store) ListUsers(ctx context.Context, orgID string) ([]*domain.User, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT user_id, org_id, email, password_hash, created_at FROM users WHERE org_id = $1 ORDER BY user_id`, orgID,
	)
	if err != nil {
		return nil, fmt.Errorf("postgres: list users: %w", err)
	}
	defer rows.Close()

	var users []*domain.User
	for rows.Next() {
		var u domain.User
		var createdAt time.Time
		if err := rows.Scan(&u.UserID, &u.OrgID, &u.Email, &u.PasswordHash, &createdAt); err != nil {
			return nil, fmt.Errorf("postgres: scan user: %w", err)
		}
		u.CreatedAt = createdAt
		users = append(users, &u)
	}
	return users, rows.Err()
}

// CheckPassword compares a plaintext password against a stored bcrypt hash.
func (s *Store) CheckPassword(hashed, plaintext string) error {
	return bcrypt.CompareHashAndPassword([]byte(hashed), []byte(plaintext))
}

// ---- Sessions ----

func (s *Store) CreateSession(ctx context.Context, session *domain.Session) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO sessions (session_id, user_id, expires_at, created_at) VALUES ($1, $2, $3, $4)`,
		session.SessionID, session.UserID, session.ExpiresAt, session.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("postgres: create session: %w", err)
	}
	return nil
}

func (s *Store) GetSession(ctx context.Context, sessionID string) (*domain.Session, error) {
	var userID string
	var expiresAt, createdAt time.Time
	err := s.db.QueryRowContext(ctx,
		`SELECT user_id, expires_at, created_at FROM sessions WHERE session_id = $1`, sessionID,
	).Scan(&userID, &expiresAt, &createdAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("postgres: get session: %w", err)
	}
	return &domain.Session{SessionID: sessionID, UserID: userID, ExpiresAt: expiresAt, CreatedAt: createdAt}, nil
}

func (s *Store) DeleteSession(ctx context.Context, sessionID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE session_id = $1`, sessionID)
	if err != nil {
		return fmt.Errorf("postgres: delete session: %w", err)
	}
	return nil
}

// ---- API Keys ----

func (s *Store) CreateAPIKey(ctx context.Context, key *domain.APIKey) error {
	var revokedAt *time.Time
	if key.RevokedAt != nil {
		revokedAt = key.RevokedAt
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO api_keys (key_id, project_id, kind, service_name, name, hashed_key, created_at, revoked_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		key.KeyID, key.ProjectID, string(key.Kind), key.ServiceName, key.Name, key.HashedKey, key.CreatedAt, revokedAt,
	)
	if err != nil {
		return fmt.Errorf("postgres: create api key: %w", err)
	}
	return nil
}

func (s *Store) GetAPIKeyByHash(ctx context.Context, hashedKey string) (*domain.APIKey, error) {
	var keyID, projectID, kindStr string
	var serviceName, name sql.NullString
	var createdAt time.Time
	var revokedAt *time.Time
	err := s.db.QueryRowContext(ctx,
		`SELECT key_id, project_id, kind, service_name, name, created_at, revoked_at
		 FROM api_keys WHERE hashed_key = $1`, hashedKey,
	).Scan(&keyID, &projectID, &kindStr, &serviceName, &name, &createdAt, &revokedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("postgres: get api key by hash: %w", err)
	}
	return &domain.APIKey{
		KeyID:       keyID,
		ProjectID:   projectID,
		Kind:        domain.APIKeyKind(kindStr),
		ServiceName: serviceName.String,
		Name:        name.String,
		HashedKey:   hashedKey,
		CreatedAt:   createdAt,
		RevokedAt:   revokedAt,
	}, nil
}

func (s *Store) RevokeAPIKey(ctx context.Context, keyID string) error {
	now := time.Now().UTC()
	_, err := s.db.ExecContext(ctx,
		`UPDATE api_keys SET revoked_at = $1 WHERE key_id = $2`, now, keyID,
	)
	if err != nil {
		return fmt.Errorf("postgres: revoke api key: %w", err)
	}
	return nil
}

func (s *Store) ListAPIKeys(ctx context.Context, projectID string) ([]*domain.APIKey, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT key_id, project_id, kind, service_name, name, hashed_key, created_at, revoked_at
		 FROM api_keys WHERE project_id = $1 ORDER BY key_id`, projectID,
	)
	if err != nil {
		return nil, fmt.Errorf("postgres: list api keys: %w", err)
	}
	defer rows.Close()

	var keys []*domain.APIKey
	for rows.Next() {
		var k domain.APIKey
		var kindStr string
		var serviceName, name sql.NullString
		var createdAt time.Time
		var revokedAt *time.Time
		if err := rows.Scan(&k.KeyID, &k.ProjectID, &kindStr, &serviceName, &name, &k.HashedKey, &createdAt, &revokedAt); err != nil {
			return nil, fmt.Errorf("postgres: scan api key: %w", err)
		}
		k.Kind = domain.APIKeyKind(kindStr)
		k.ServiceName = serviceName.String
		k.Name = name.String
		k.CreatedAt = createdAt
		keys = append(keys, &k)
	}
	return keys, rows.Err()
}

func (s *Store) GetUserByID(ctx context.Context, userID string) (*domain.User, error) {
	var u domain.User
	var createdAt time.Time
	err := s.db.QueryRowContext(ctx,
		`SELECT org_id, email, password_hash, created_at FROM users WHERE user_id = $1`, userID,
	).Scan(&u.OrgID, &u.Email, &u.PasswordHash, &createdAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("postgres: get user by id %s: %w", userID, err)
	}
	return &domain.User{UserID: userID, OrgID: u.OrgID, Email: u.Email, PasswordHash: u.PasswordHash, CreatedAt: createdAt}, nil
}

func (s *Store) CountUsers(ctx context.Context) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM users`).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("postgres: count users: %w", err)
	}
	return count, nil
}

func (s *Store) UpdateUserPassword(ctx context.Context, userID, passwordHash string) error {
	hashed, err := bcrypt.GenerateFromPassword([]byte(passwordHash), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("postgres: bcrypt hash: %w", err)
	}
	_, err = s.db.ExecContext(ctx, `UPDATE users SET password_hash = $1 WHERE user_id = $2`, string(hashed), userID)
	if err != nil {
		return fmt.Errorf("postgres: update user password: %w", err)
	}
	return nil
}

func (s *Store) UpdateUserResetToken(ctx context.Context, userID, token string, expiry time.Time) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE users SET password_reset_token = $1, reset_token_expiry = $2 WHERE user_id = $3`,
		token, expiry, userID,
	)
	if err != nil {
		return fmt.Errorf("postgres: update user reset token: %w", err)
	}
	return nil
}

func (s *Store) GetUserByResetToken(ctx context.Context, token string) (*domain.User, error) {
	var u domain.User
	var createdAt, expiryAt time.Time
	err := s.db.QueryRowContext(ctx,
		`SELECT user_id, org_id, email, password_hash, created_at, password_reset_token, reset_token_expiry
		 FROM users WHERE password_reset_token = $1 AND reset_token_expiry > now()`,
		token,
	).Scan(&u.UserID, &u.OrgID, &u.Email, &u.PasswordHash, &createdAt, &u.PasswordResetToken, &expiryAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("postgres: get user by reset token: %w", err)
	}
	u.CreatedAt = createdAt
	u.ResetTokenExpiry = expiryAt
	return &u, nil
}

// ---- Prompt Registry ----

func (s *Store) CreatePromptVersion(ctx context.Context, pv *domain.PromptVersion) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO prompt_versions (version_id, project_id, name, version, template, model, temperature, max_tokens, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		pv.VersionID, pv.ProjectID, pv.Name, pv.Version, pv.Template,
		pv.ModelConfig.Model, pv.ModelConfig.Temperature, pv.ModelConfig.MaxTokens, pv.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("postgres: create prompt version: %w", err)
	}
	return nil
}

func (s *Store) GetPromptVersion(ctx context.Context, projectID, name string, version int64) (*domain.PromptVersion, error) {
	var pv domain.PromptVersion
	var model string
	var temperature *float64
	var maxTokens *int
	var createdAt time.Time
	err := s.db.QueryRowContext(ctx,
		`SELECT version_id, template, model, temperature, max_tokens, created_at
		 FROM prompt_versions WHERE project_id = $1 AND name = $2 AND version = $3`,
		projectID, name, version,
	).Scan(&pv.VersionID, &pv.Template, &model, &temperature, &maxTokens, &createdAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("postgres: get prompt version: %w", err)
	}
	pv.Version = version
	pv.CreatedAt = createdAt
	pv.ModelConfig = domain.PromptModelConfig{
		Model:       model,
		Temperature: derefOr(temperature, 0.7),
		MaxTokens:   derefOr(maxTokens, 0),
	}
	return &pv, nil
}

func (s *Store) GetPromptByLabel(ctx context.Context, projectID, name, label string) (*domain.PromptVersion, error) {
	var version int64
	var updatedAt time.Time
	err := s.db.QueryRowContext(ctx,
		`SELECT version, updated_at FROM prompt_labels WHERE project_id = $1 AND name = $2 AND label = $3`,
		projectID, name, label,
	).Scan(&version, &updatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("postgres: get prompt by label: %w", err)
	}
	return s.GetPromptVersion(ctx, projectID, name, version)
}

func (s *Store) ListPromptVersions(ctx context.Context, projectID, name string) ([]*domain.PromptVersion, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT version_id, name, version, template, model, temperature, max_tokens, created_at
		 FROM prompt_versions WHERE project_id = $1 AND name = $2 ORDER BY version`, projectID, name,
	)
	if err != nil {
		return nil, fmt.Errorf("postgres: list prompt versions: %w", err)
	}
	defer rows.Close()

	var versions []*domain.PromptVersion
	for rows.Next() {
		var pv domain.PromptVersion
		var model string
		var temperature *float64
		var maxTokens *int
		var createdAt time.Time
		if err := rows.Scan(&pv.VersionID, &pv.Name, &pv.Version, &pv.Template, &model, &temperature, &maxTokens, &createdAt); err != nil {
			return nil, fmt.Errorf("postgres: scan prompt version: %w", err)
		}
		pv.CreatedAt = createdAt
		pv.ProjectID = projectID
		pv.ModelConfig = domain.PromptModelConfig{
			Model:       model,
			Temperature: derefOr(temperature, 0.7),
			MaxTokens:   derefOr(maxTokens, 0),
		}
		versions = append(versions, &pv)
	}
	return versions, rows.Err()
}

func (s *Store) ListPromptNames(ctx context.Context, projectID string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT DISTINCT name FROM prompt_versions WHERE project_id = $1 ORDER BY name`, projectID,
	)
	if err != nil {
		return nil, fmt.Errorf("postgres: list prompt names: %w", err)
	}
	defer rows.Close()

	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("postgres: scan prompt name: %w", err)
		}
		names = append(names, name)
	}
	return names, rows.Err()
}

func (s *Store) SetPromptLabel(ctx context.Context, label *domain.PromptLabel) error {
	now := time.Now().UTC()
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO prompt_labels (project_id, name, label, version, updated_at)
		 VALUES ($1, $2, $3, $4, $5)
		 ON CONFLICT (project_id, name, label) DO UPDATE SET version = $4, updated_at = $5`,
		label.ProjectID, label.Name, label.Label, label.Version, now,
	)
	if err != nil {
		return fmt.Errorf("postgres: set prompt label: %w", err)
	}
	return nil
}

// ---- Eval Rules ----

func (s *Store) CreateEvalRule(ctx context.Context, rule *domain.EvalRule) error {
	filterJSON, err := json.Marshal(rule.Filter)
	if err != nil {
		return fmt.Errorf("postgres: marshal eval filter: %w", err)
	}
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO eval_rules (rule_id, project_id, name, judge_model, prompt_name, prompt_version, filter, sample_rate, enabled, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
		rule.RuleID, rule.ProjectID, rule.Name, rule.JudgeModel, rule.PromptName,
		rule.PromptVersion, string(filterJSON), rule.SampleRate, rule.Enabled, rule.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("postgres: create eval rule: %w", err)
	}
	return nil
}

func (s *Store) GetEvalRule(ctx context.Context, ruleID string) (*domain.EvalRule, error) {
	var rule domain.EvalRule
	var filterJSON string
	var createdAt time.Time
	err := s.db.QueryRowContext(ctx,
		`SELECT project_id, name, judge_model, prompt_name, prompt_version, filter, sample_rate, enabled, created_at
		 FROM eval_rules WHERE rule_id = $1`, ruleID,
	).Scan(&rule.ProjectID, &rule.Name, &rule.JudgeModel, &rule.PromptName, &rule.PromptVersion,
		&filterJSON, &rule.SampleRate, &rule.Enabled, &createdAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("postgres: get eval rule: %w", err)
	}
	rule.CreatedAt = createdAt
	if err := json.Unmarshal([]byte(filterJSON), &rule.Filter); err != nil {
		return nil, fmt.Errorf("postgres: unmarshal eval filter: %w", err)
	}
	return &rule, nil
}

func (s *Store) ListEvalRules(ctx context.Context, projectID string) ([]*domain.EvalRule, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT rule_id, name, judge_model, prompt_name, prompt_version, filter, sample_rate, enabled, created_at
		 FROM eval_rules WHERE project_id = $1 ORDER BY rule_id`, projectID,
	)
	if err != nil {
		return nil, fmt.Errorf("postgres: list eval rules: %w", err)
	}
	defer rows.Close()

	var rules []*domain.EvalRule
	for rows.Next() {
		var r domain.EvalRule
		var filterJSON string
		var createdAt time.Time
		if err := rows.Scan(&r.RuleID, &r.Name, &r.JudgeModel, &r.PromptName, &r.PromptVersion,
			&filterJSON, &r.SampleRate, &r.Enabled, &createdAt); err != nil {
			return nil, fmt.Errorf("postgres: scan eval rule: %w", err)
		}
		r.ProjectID = projectID
		r.CreatedAt = createdAt
		if err := json.Unmarshal([]byte(filterJSON), &r.Filter); err != nil {
			return nil, fmt.Errorf("postgres: unmarshal eval filter: %w", err)
		}
		rules = append(rules, &r)
	}
	return rules, rows.Err()
}

func (s *Store) UpdateEvalRule(ctx context.Context, rule *domain.EvalRule) error {
	filterJSON, err := json.Marshal(rule.Filter)
	if err != nil {
		return fmt.Errorf("postgres: marshal eval filter: %w", err)
	}
	_, err = s.db.ExecContext(ctx,
		`UPDATE eval_rules SET name = $1, judge_model = $2, prompt_name = $3, prompt_version = $4,
		 filter = $5, sample_rate = $6, enabled = $7
		 WHERE rule_id = $8`,
		rule.Name, rule.JudgeModel, rule.PromptName, rule.PromptVersion,
		string(filterJSON), rule.SampleRate, rule.Enabled, rule.RuleID,
	)
	if err != nil {
		return fmt.Errorf("postgres: update eval rule: %w", err)
	}
	return nil
}

func (s *Store) DeleteEvalRule(ctx context.Context, ruleID string) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM eval_rules WHERE rule_id = $1`, ruleID,
	)
	if err != nil {
		return fmt.Errorf("postgres: delete eval rule: %w", err)
	}
	return nil
}

// ---- Datasets ----

func (s *Store) CreateDataset(ctx context.Context, ds *domain.Dataset) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO datasets (dataset_id, project_id, name, created_at) VALUES ($1, $2, $3, $4)`,
		ds.DatasetID, ds.ProjectID, ds.Name, ds.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("postgres: create dataset: %w", err)
	}
	return nil
}

func (s *Store) ListDatasets(ctx context.Context, projectID string) ([]*domain.Dataset, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT dataset_id, project_id, name, created_at FROM datasets WHERE project_id = $1 ORDER BY name`, projectID,
	)
	if err != nil {
		return nil, fmt.Errorf("postgres: list datasets: %w", err)
	}
	defer rows.Close()

	var datasets []*domain.Dataset
	for rows.Next() {
		var d domain.Dataset
		var createdAt time.Time
		if err := rows.Scan(&d.DatasetID, &d.ProjectID, &d.Name, &createdAt); err != nil {
			return nil, fmt.Errorf("postgres: scan dataset: %w", err)
		}
		d.CreatedAt = createdAt
		datasets = append(datasets, &d)
	}
	return datasets, rows.Err()
}

func (s *Store) GetDataset(ctx context.Context, datasetID string) (*domain.Dataset, error) {
	var ds domain.Dataset
	var createdAt time.Time
	err := s.db.QueryRowContext(ctx,
		`SELECT project_id, name, created_at FROM datasets WHERE dataset_id = $1`, datasetID,
	).Scan(&ds.ProjectID, &ds.Name, &createdAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("postgres: get dataset: %w", err)
	}
	return &domain.Dataset{DatasetID: datasetID, ProjectID: ds.ProjectID, Name: ds.Name, CreatedAt: createdAt}, nil
}

func (s *Store) DeleteDataset(ctx context.Context, datasetID string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("postgres: begin tx: %w", err)
	}
	defer tx.Rollback()

	// Delete items first.
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM dataset_items WHERE dataset_id = $1`, datasetID); err != nil {
		return fmt.Errorf("postgres: delete dataset items: %w", err)
	}

	// Delete the dataset.
	result, err := tx.ExecContext(ctx,
		`DELETE FROM datasets WHERE dataset_id = $1`, datasetID)
	if err != nil {
		return fmt.Errorf("postgres: delete dataset: %w", err)
	}
	if rows, _ := result.RowsAffected(); rows == 0 {
		return domain.ErrNotFound
	}

	return tx.Commit()
}

func (s *Store) CreateDatasetItem(ctx context.Context, item *domain.DatasetItem) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO dataset_items (item_id, dataset_id, source_span_id, input, expected_output, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6)`,
		item.ItemID, item.DatasetID, item.SourceSpanID, item.Input, item.ExpectedOutput, item.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("postgres: create dataset item: %w", err)
	}
	return nil
}

func (s *Store) ListDatasetItems(ctx context.Context, datasetID string) ([]*domain.DatasetItem, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT item_id, dataset_id, source_span_id, input, expected_output, created_at
		 FROM dataset_items WHERE dataset_id = $1 ORDER BY item_id`, datasetID,
	)
	if err != nil {
		return nil, fmt.Errorf("postgres: list dataset items: %w", err)
	}
	defer rows.Close()

	var items []*domain.DatasetItem
	for rows.Next() {
		var item domain.DatasetItem
		var createdAt time.Time
		if err := rows.Scan(&item.ItemID, &item.DatasetID, &item.SourceSpanID, &item.Input, &item.ExpectedOutput, &createdAt); err != nil {
			return nil, fmt.Errorf("postgres: scan dataset item: %w", err)
		}
		item.CreatedAt = createdAt
		items = append(items, &item)
	}
	return items, rows.Err()
}

func (s *Store) ListDatasetItemsPaginated(ctx context.Context, datasetID, cursor string, limit int) ([]*domain.DatasetItem, string, error) {
	if limit <= 0 {
		limit = 50
	}

	var rows *sql.Rows
	var err error
	if cursor != "" {
		rows, err = s.db.QueryContext(ctx,
			`SELECT item_id, dataset_id, source_span_id, input, expected_output, created_at
			 FROM dataset_items WHERE dataset_id = $1
			 AND (created_at, item_id) > (
			   SELECT created_at, item_id FROM dataset_items WHERE item_id = $2 LIMIT 1
			 )
			 ORDER BY created_at ASC, item_id ASC LIMIT $3`,
			datasetID, cursor, limit+1,
		)
	} else {
		rows, err = s.db.QueryContext(ctx,
			`SELECT item_id, dataset_id, source_span_id, input, expected_output, created_at
			 FROM dataset_items WHERE dataset_id = $1
			 ORDER BY created_at ASC, item_id ASC LIMIT $2`,
			datasetID,
			limit+1,
		)
	}
	if err != nil {
		return nil, "", fmt.Errorf("postgres: list dataset items paginated: %w", err)
	}
	defer rows.Close()

	var items []*domain.DatasetItem
	for rows.Next() {
		var item domain.DatasetItem
		var createdAt time.Time
		if err := rows.Scan(&item.ItemID, &item.DatasetID, &item.SourceSpanID, &item.Input, &item.ExpectedOutput, &createdAt); err != nil {
			return nil, "", fmt.Errorf("postgres: scan dataset item: %w", err)
		}
		item.CreatedAt = createdAt
		items = append(items, &item)
	}

	// Check if we have one extra row (means more pages exist).
	hasNext := false
	if len(items) > limit {
		hasNext = true
		items = items[:limit]
	}

	// Compute next cursor.
	var nextCursor string
	if hasNext && len(items) > 0 {
		nextCursor = items[len(items)-1].ItemID
	}

	return items, nextCursor, rows.Err()
}

func (s *Store) CreateDatasetRun(ctx context.Context, run *domain.DatasetRun) error {
	status := run.Status
	if status == "" {
		status = domain.DatasetRunStatusPending
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO dataset_runs (run_id, dataset_id, eval_rule_id, prompt_version, status, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6)`,
		run.RunID, run.DatasetID, run.EvalRuleID, run.PromptVersion, status, run.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("postgres: create dataset run: %w", err)
	}
	return nil
}

func (s *Store) GetDatasetRun(ctx context.Context, runID string) (*domain.DatasetRun, error) {
	var dr domain.DatasetRun
	var createdAt time.Time
	err := s.db.QueryRowContext(ctx,
		`SELECT dataset_id, eval_rule_id, prompt_version, status, created_at FROM dataset_runs WHERE run_id = $1`, runID,
	).Scan(&dr.DatasetID, &dr.EvalRuleID, &dr.PromptVersion, &dr.Status, &createdAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("postgres: get dataset run: %w", err)
	}
	return &domain.DatasetRun{RunID: runID, DatasetID: dr.DatasetID, EvalRuleID: dr.EvalRuleID, PromptVersion: dr.PromptVersion, Status: dr.Status, CreatedAt: createdAt}, nil
}

func (s *Store) UpdateDatasetRun(ctx context.Context, run *domain.DatasetRun) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE dataset_runs SET status = $1 WHERE run_id = $2`,
		run.Status, run.RunID,
	)
	if err != nil {
		return fmt.Errorf("postgres: update dataset run: %w", err)
	}
	return nil
}

func (s *Store) ListDatasetRuns(ctx context.Context, datasetID string) ([]*domain.DatasetRun, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT run_id, dataset_id, eval_rule_id, prompt_version, status, created_at FROM dataset_runs WHERE dataset_id = $1 ORDER BY created_at DESC`, datasetID,
	)
	if err != nil {
		return nil, fmt.Errorf("postgres: list dataset runs: %w", err)
	}
	defer rows.Close()

	var runs []*domain.DatasetRun
	for rows.Next() {
		var run domain.DatasetRun
		var createdAt time.Time
		if err := rows.Scan(&run.RunID, &run.DatasetID, &run.EvalRuleID, &run.PromptVersion, &run.Status, &createdAt); err != nil {
			return nil, fmt.Errorf("postgres: scan dataset run: %w", err)
		}
		run.CreatedAt = createdAt
		runs = append(runs, &run)
	}
	return runs, rows.Err()
}

func (s *Store) CreateDatasetRunItem(ctx context.Context, item *domain.DatasetRunItem) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO dataset_run_items (run_item_id, run_id, item_id, score, reasoning, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6)`,
		item.RunItemID, item.RunID, item.ItemID, item.Score, item.Reasoning, item.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("postgres: create dataset run item: %w", err)
	}
	return nil
}

func (s *Store) GetDatasetRunItem(ctx context.Context, id string) (*domain.DatasetRunItem, error) {
	var item domain.DatasetRunItem
	var createdAt time.Time
	err := s.db.QueryRowContext(ctx,
		`SELECT run_id, item_id, score, reasoning, created_at FROM dataset_run_items WHERE run_item_id = $1`, id,
	).Scan(&item.RunID, &item.ItemID, &item.Score, &item.Reasoning, &createdAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("postgres: get dataset run item: %w", err)
	}
	item.CreatedAt = createdAt
	return &item, nil
}

func (s *Store) UpdateDatasetRunItem(ctx context.Context, item *domain.DatasetRunItem) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE dataset_run_items SET score = $1, reasoning = $2 WHERE run_item_id = $3`,
		item.Score, item.Reasoning, item.RunItemID,
	)
	if err != nil {
		return fmt.Errorf("postgres: update dataset run item: %w", err)
	}
	return nil
}

func (s *Store) ListDatasetRunItems(ctx context.Context, runID string) ([]*domain.DatasetRunItem, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT run_item_id, run_id, item_id, score, reasoning, created_at FROM dataset_run_items WHERE run_id = $1 ORDER BY created_at ASC`, runID,
	)
	if err != nil {
		return nil, fmt.Errorf("postgres: list dataset run items: %w", err)
	}
	defer rows.Close()

	var items []*domain.DatasetRunItem
	for rows.Next() {
		var item domain.DatasetRunItem
		var createdAt time.Time
		if err := rows.Scan(&item.RunItemID, &item.RunID, &item.ItemID, &item.Score, &item.Reasoning, &createdAt); err != nil {
			return nil, fmt.Errorf("postgres: scan dataset run item: %w", err)
		}
		item.CreatedAt = createdAt
		items = append(items, &item)
	}
	return items, rows.Err()
}

// ---- Helper functions ----

// derefOr returns the value pointed to by p, or def if p is nil.
func derefOr[T any](p *T, def T) T {
	if p != nil {
		return *p
	}
	return def
}
