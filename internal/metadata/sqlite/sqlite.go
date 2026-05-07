package sqlite

import (
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/zbloss/lantern/internal/domain"
	"github.com/zbloss/lantern/internal/metadata"
	_ "modernc.org/sqlite"
	"golang.org/x/crypto/bcrypt"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Store is the SQLite implementation of metadata.Store.
// Used for demo deployments (docker compose) with zero cloud dependencies.
type Store struct {
	db       *sql.DB
	migrated bool
}

// New opens (or creates) a SQLite database at the given path with WAL mode
// and a 5-second busy timeout. Call Migrate() to apply pending migrations,
// then Close() when done.
func New(path string) (*Store, error) {
	// Ensure parent directory exists (skip for bare filenames like "test.db").
	dir := filepath.Dir(path)
	if dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, fmt.Errorf("sqlite: create dir %s: %w", path, err)
		}
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("sqlite: open %s: %w", path, err)
	}

	// Set WAL mode for better concurrency
	if _, err := db.ExecContext(context.Background(), "PRAGMA journal_mode=WAL"); err != nil {
		db.Close()
		return nil, fmt.Errorf("sqlite: set WAL: %w", err)
	}

	// Set busy timeout (5 seconds)
	if _, err := db.ExecContext(context.Background(), "PRAGMA busy_timeout=5000"); err != nil {
		db.Close()
		return nil, fmt.Errorf("sqlite: set busy_timeout: %w", err)
	}

	s := &Store{db: db}
	return s, nil
}

// Migrate applies pending SQL migrations in order.
func (s *Store) Migrate(ctx context.Context) error {
	if s.migrated {
		return nil
	}

	// Create schema_migrations table
	if _, err := s.db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS _schema_migrations (
			version INTEGER PRIMARY KEY,
			applied_at TEXT NOT NULL DEFAULT (datetime('now'))
		)
	`); err != nil {
		return fmt.Errorf("sqlite: create migrations table: %w", err)
	}

	// Get already-applied versions
	rows, err := s.db.QueryContext(ctx, "SELECT version FROM _schema_migrations ORDER BY version")
	if err != nil {
		return fmt.Errorf("sqlite: list applied migrations: %w", err)
	}

	applied := make(map[int]bool)
	for rows.Next() {
		var v int
		if err := rows.Scan(&v); err != nil {
			rows.Close()
			return fmt.Errorf("sqlite: scan migration version: %w", err)
		}
		applied[v] = true
	}
	rows.Close()

	// Collect migration files
	files, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		return fmt.Errorf("sqlite: read migrations: %w", err)
	}

	// Sort files by version
	sort.Slice(files, func(i, j int) bool {
		return fileVersion(files[i].Name()) < fileVersion(files[j].Name())
	})

	// Apply pending migrations (only .up.sql files)
	for _, f := range files {
		if !strings.HasSuffix(f.Name(), ".up.sql") {
			continue
		}
		v := fileVersion(f.Name())
		if applied[v] {
			continue
		}

		data, err := migrationsFS.ReadFile("migrations/" + f.Name())
		if err != nil {
			return fmt.Errorf("sqlite: read migration %s: %w", f.Name(), err)
		}

		// Wrap each statement in a transaction
		if err := s.execMigration(ctx, v, string(data)); err != nil {
			return fmt.Errorf("sqlite: apply migration %s: %w", f.Name(), err)
		}
	}

	s.migrated = true
	return nil
}

func (s *Store) execMigration(ctx context.Context, version int, sql string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("sqlite: begin migration tx: %w", err)
	}

	// Execute each statement separately (SQLite driver may not support multi-statement)
	statements := splitStatements(sql)
	for _, stmt := range statements {
		stmt = strings.TrimSpace(stmt)
		if stmt == "" {
			continue
		}
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			tx.Rollback()
			return fmt.Errorf("sqlite: execute stmt: %w", err)
		}
	}

	if _, err := tx.ExecContext(ctx, "INSERT INTO _schema_migrations (version) VALUES (?)", version); err != nil {
		tx.Rollback()
		return fmt.Errorf("sqlite: record migration version: %w", err)
	}

	return tx.Commit()
}

// splitStatements splits SQL on semicolons, respecting quoted strings.
// It does not handle SQL comments (-- or /* */); callers must strip those first.
func splitStatements(sql string) []string {
	var parts []string
	var current strings.Builder
	inQuote := false
	quoteChar := rune(0)

	for _, r := range sql {
		if inQuote {
			current.WriteRune(r)
			if r == quoteChar {
				inQuote = false
			}
			continue
		}
		switch r {
		case '"', '\'', '`':
			current.WriteRune(r)
			inQuote = true
			quoteChar = r
		case ';':
			parts = append(parts, current.String())
			current.Reset()
		default:
			current.WriteRune(r)
		}
	}
	if s := strings.TrimSpace(current.String()); s != "" {
		parts = append(parts, s)
	}
	return parts
}

func fileVersion(name string) int {
	// e.g. "0001_init.up.sql" → 1
	s := strings.TrimSuffix(name, ".up.sql")
	s = strings.TrimSuffix(s, ".down.sql")
	s = strings.TrimLeft(s, "0")
	if s == "" {
		return 0
	}
	v, _ := strconv.Atoi(s)
	return v
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
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO organizations (org_id, name, created_at) VALUES (?, ?, ?)`,
		org.OrgID, org.Name, now,
	)
	if err != nil {
		return fmt.Errorf("sqlite: create org: %w", err)
	}
	return nil
}

func (s *Store) GetOrganization(ctx context.Context, orgID string) (*domain.Organization, error) {
	var name string
	var createdAt string
	err := s.db.QueryRowContext(ctx,
		`SELECT name, created_at FROM organizations WHERE org_id = ?`, orgID,
	).Scan(&name, &createdAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, metadata.ErrNotFound
		}
		return nil, fmt.Errorf("sqlite: get org %s: %w", orgID, err)
	}
	t, _ := time.Parse(time.RFC3339, createdAt)
	return &domain.Organization{OrgID: orgID, Name: name, CreatedAt: t}, nil
}

// ---- Projects ----

func (s *Store) CreateProject(ctx context.Context, project *domain.Project) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO projects (project_id, org_id, name, created_at) VALUES (?, ?, ?, ?)`,
		project.ProjectID, project.OrgID, project.Name, now,
	)
	if err != nil {
		return fmt.Errorf("sqlite: create project: %w", err)
	}
	return nil
}

func (s *Store) GetProject(ctx context.Context, projectID string) (*domain.Project, error) {
	var orgID, name, createdAt string
	err := s.db.QueryRowContext(ctx,
		`SELECT org_id, name, created_at FROM projects WHERE project_id = ?`, projectID,
	).Scan(&orgID, &name, &createdAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, metadata.ErrNotFound
		}
		return nil, fmt.Errorf("sqlite: get project %s: %w", projectID, err)
	}
	t, _ := time.Parse(time.RFC3339, createdAt)
	return &domain.Project{ProjectID: projectID, OrgID: orgID, Name: name, CreatedAt: t}, nil
}

func (s *Store) ListProjects(ctx context.Context, orgID string) ([]*domain.Project, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT project_id, org_id, name, created_at FROM projects WHERE org_id = ? ORDER BY project_id`, orgID,
	)
	if err != nil {
		return nil, fmt.Errorf("sqlite: list projects: %w", err)
	}
	defer rows.Close()

	var projects []*domain.Project
	for rows.Next() {
		var p domain.Project
		var createdAt string
		if err := rows.Scan(&p.ProjectID, &p.OrgID, &p.Name, &createdAt); err != nil {
			return nil, fmt.Errorf("sqlite: scan project: %w", err)
		}
		p.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		projects = append(projects, &p)
	}
	return projects, rows.Err()
}

// ---- Users ----

func (s *Store) CreateUser(ctx context.Context, user *domain.User) error {
	hashed, err := bcrypt.GenerateFromPassword([]byte(user.PasswordHash), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("sqlite: bcrypt hash: %w", err)
	}
	user.PasswordHash = string(hashed)

	now := time.Now().UTC().Format(time.RFC3339)
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO users (user_id, org_id, email, password_hash, created_at) VALUES (?, ?, ?, ?, ?)`,
		user.UserID, user.OrgID, user.Email, user.PasswordHash, now,
	)
	if err != nil {
		return fmt.Errorf("sqlite: create user: %w", err)
	}
	return nil
}

func (s *Store) GetUserByEmail(ctx context.Context, email string) (*domain.User, error) {
	var userID, orgID, passwordHash, createdAt string
	err := s.db.QueryRowContext(ctx,
		`SELECT user_id, org_id, password_hash, created_at FROM users WHERE email = ?`, email,
	).Scan(&userID, &orgID, &passwordHash, &createdAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, metadata.ErrNotFound
		}
		return nil, fmt.Errorf("sqlite: get user by email: %w", err)
	}
	t, _ := time.Parse(time.RFC3339, createdAt)
	return &domain.User{UserID: userID, OrgID: orgID, Email: email, PasswordHash: passwordHash, CreatedAt: t}, nil
}

func (s *Store) ListUsers(ctx context.Context, orgID string) ([]*domain.User, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT user_id, org_id, email, password_hash, created_at FROM users WHERE org_id = ? ORDER BY user_id`, orgID,
	)
	if err != nil {
		return nil, fmt.Errorf("sqlite: list users: %w", err)
	}
	defer rows.Close()

	var users []*domain.User
	for rows.Next() {
		var u domain.User
		var createdAt string
		if err := rows.Scan(&u.UserID, &u.OrgID, &u.Email, &u.PasswordHash, &createdAt); err != nil {
			return nil, fmt.Errorf("sqlite: scan user: %w", err)
		}
		u.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		users = append(users, &u)
	}
	return users, rows.Err()
}

func (s *Store) CountUsers(ctx context.Context) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM users`).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("sqlite: count users: %w", err)
	}
	return count, nil
}

func (s *Store) UpdateUserPassword(ctx context.Context, userID, passwordHash string) error {
	// Hash the password before storing
	hashed, err := bcrypt.GenerateFromPassword([]byte(passwordHash), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("sqlite: bcrypt hash: %w", err)
	}

	result, err := s.db.ExecContext(ctx,
		`UPDATE users SET password_hash = ? WHERE user_id = ?`, string(hashed), userID,
	)
	if err != nil {
		return fmt.Errorf("sqlite: update user password: %w", err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("sqlite: check rows affected: %w", err)
	}
	if n == 0 {
		return metadata.ErrNotFound
	}
	return nil
}

func (s *Store) GetUserByID(ctx context.Context, userID string) (*domain.User, error) {
	var email, orgID, passwordHash, createdAt string
	err := s.db.QueryRowContext(ctx,
		`SELECT user_id, org_id, email, password_hash, created_at FROM users WHERE user_id = ?`, userID,
	).Scan(&userID, &orgID, &email, &passwordHash, &createdAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, metadata.ErrNotFound
		}
		return nil, fmt.Errorf("sqlite: get user by id: %w", err)
	}
	t, _ := time.Parse(time.RFC3339, createdAt)
	return &domain.User{UserID: userID, OrgID: orgID, Email: email, PasswordHash: passwordHash, CreatedAt: t}, nil
}

// CheckPassword compares a plaintext password against a stored bcrypt hash.
func (s *Store) CheckPassword(hashed, plaintext string) error {
	return bcrypt.CompareHashAndPassword([]byte(hashed), []byte(plaintext))
}

// ---- Sessions ----

func (s *Store) CreateSession(ctx context.Context, session *domain.Session) error {
	now := time.Now().UTC().Format(time.RFC3339)
	expiresAt := session.ExpiresAt.Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO sessions (session_id, user_id, expires_at, created_at) VALUES (?, ?, ?, ?)`,
		session.SessionID, session.UserID, expiresAt, now,
	)
	if err != nil {
		return fmt.Errorf("sqlite: create session: %w", err)
	}
	return nil
}

func (s *Store) GetSession(ctx context.Context, sessionID string) (*domain.Session, error) {
	var userID, expiresAtStr, createdAtStr string
	err := s.db.QueryRowContext(ctx,
		`SELECT user_id, expires_at, created_at FROM sessions WHERE session_id = ?`, sessionID,
	).Scan(&userID, &expiresAtStr, &createdAtStr)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, metadata.ErrNotFound
		}
		return nil, fmt.Errorf("sqlite: get session: %w", err)
	}
	expiresAt, _ := time.Parse(time.RFC3339, expiresAtStr)
	createdAt, _ := time.Parse(time.RFC3339, createdAtStr)
	return &domain.Session{SessionID: sessionID, UserID: userID, ExpiresAt: expiresAt, CreatedAt: createdAt}, nil
}

func (s *Store) DeleteSession(ctx context.Context, sessionID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE session_id = ?`, sessionID)
	if err != nil {
		return fmt.Errorf("sqlite: delete session: %w", err)
	}
	return nil
}

// ---- API Keys ----

func (s *Store) CreateAPIKey(ctx context.Context, key *domain.APIKey) error {
	now := time.Now().UTC().Format(time.RFC3339)
	var revokedAt *string
	if key.RevokedAt != nil {
		s := key.RevokedAt.Format(time.RFC3339)
		revokedAt = &s
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO api_keys (key_id, project_id, kind, service_name, hashed_key, created_at, revoked_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		key.KeyID, key.ProjectID, string(key.Kind), key.ServiceName, key.HashedKey, now, revokedAt,
	)
	if err != nil {
		return fmt.Errorf("sqlite: create api key: %w", err)
	}
	return nil
}

func (s *Store) GetAPIKeyByHash(ctx context.Context, hashedKey string) (*domain.APIKey, error) {
	var keyID, projectID, kind, serviceName, createdAt string
	var revokedAtStr *string
	err := s.db.QueryRowContext(ctx,
		`SELECT key_id, project_id, kind, service_name, created_at, revoked_at
		 FROM api_keys WHERE hashed_key = ?`, hashedKey,
	).Scan(&keyID, &projectID, &kind, &serviceName, &createdAt, &revokedAtStr)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, metadata.ErrNotFound
		}
		return nil, fmt.Errorf("sqlite: get api key by hash: %w", err)
	}
	t, _ := time.Parse(time.RFC3339, createdAt)
	var revokedAt *time.Time
	if revokedAtStr != nil {
		rt, _ := time.Parse(time.RFC3339, *revokedAtStr)
		revokedAt = &rt
	}
	return &domain.APIKey{
		KeyID:       keyID,
		ProjectID:   projectID,
		Kind:        domain.APIKeyKind(kind),
		ServiceName: serviceName,
		HashedKey:   hashedKey,
		CreatedAt:   t,
		RevokedAt:   revokedAt,
	}, nil
}

func (s *Store) RevokeAPIKey(ctx context.Context, keyID string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx,
		`UPDATE api_keys SET revoked_at = ? WHERE key_id = ?`, now, keyID,
	)
	if err != nil {
		return fmt.Errorf("sqlite: revoke api key: %w", err)
	}
	return nil
}

func (s *Store) ListAPIKeys(ctx context.Context, projectID string) ([]*domain.APIKey, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT key_id, project_id, kind, service_name, hashed_key, created_at, revoked_at
		 FROM api_keys WHERE project_id = ? ORDER BY key_id`, projectID,
	)
	if err != nil {
		return nil, fmt.Errorf("sqlite: list api keys: %w", err)
	}
	defer rows.Close()

	var keys []*domain.APIKey
	for rows.Next() {
		var k domain.APIKey
		var kind, createdAt string
		var revokedAtStr *string
		if err := rows.Scan(&k.KeyID, &k.ProjectID, &kind, &k.ServiceName, &k.HashedKey, &createdAt, &revokedAtStr); err != nil {
			return nil, fmt.Errorf("sqlite: scan api key: %w", err)
		}
		k.Kind = domain.APIKeyKind(kind)
		k.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		if revokedAtStr != nil {
			rt, _ := time.Parse(time.RFC3339, *revokedAtStr)
			k.RevokedAt = &rt
		}
		keys = append(keys, &k)
	}
	return keys, rows.Err()
}

// ---- Prompt Registry ----

func (s *Store) CreatePromptVersion(ctx context.Context, pv *domain.PromptVersion) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO prompt_versions (version_id, project_id, name, version, template, model, temperature, max_tokens, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		pv.VersionID, pv.ProjectID, pv.Name, pv.Version, pv.Template,
		pv.ModelConfig.Model, pv.ModelConfig.Temperature, pv.ModelConfig.MaxTokens, now,
	)
	if err != nil {
		return fmt.Errorf("sqlite: create prompt version: %w", err)
	}
	return nil
}

func (s *Store) GetPromptVersion(ctx context.Context, projectID, name string, version int64) (*domain.PromptVersion, error) {
	var versionID, template, model, createdAt string
	var temperature *float64
	var maxTokens *int
	err := s.db.QueryRowContext(ctx,
		`SELECT version_id, template, model, temperature, max_tokens, created_at
		 FROM prompt_versions WHERE project_id = ? AND name = ? AND version = ?`,
		projectID, name, version,
	).Scan(&versionID, &template, &model, &temperature, &maxTokens, &createdAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, metadata.ErrNotFound
		}
		return nil, fmt.Errorf("sqlite: get prompt version: %w", err)
	}
	t, _ := time.Parse(time.RFC3339, createdAt)
	pv := &domain.PromptVersion{
		VersionID: versionID,
		ProjectID: projectID,
		Name:      name,
		Version:   version,
		Template:  template,
		ModelConfig: domain.PromptModelConfig{
			Model:       model,
			Temperature: derefOr(temperature, 0.7),
			MaxTokens:   derefOr(maxTokens, 0),
		},
		CreatedAt: t,
	}
	return pv, nil
}

func (s *Store) GetPromptByLabel(ctx context.Context, projectID, name, label string) (*domain.PromptVersion, error) {
	var version int64
	var updatedAtStr string
	err := s.db.QueryRowContext(ctx,
		`SELECT version, updated_at FROM prompt_labels WHERE project_id = ? AND name = ? AND label = ?`,
		projectID, name, label,
	).Scan(&version, &updatedAtStr)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, metadata.ErrNotFound
		}
		return nil, fmt.Errorf("sqlite: get prompt by label: %w", err)
	}
	return s.GetPromptVersion(ctx, projectID, name, version)
}

func (s *Store) ListPromptVersions(ctx context.Context, projectID, name string) ([]*domain.PromptVersion, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT version_id, name, version, template, model, temperature, max_tokens, created_at
		 FROM prompt_versions WHERE project_id = ? AND name = ? ORDER BY version`, projectID, name,
	)
	if err != nil {
		return nil, fmt.Errorf("sqlite: list prompt versions: %w", err)
	}
	defer rows.Close()

	var versions []*domain.PromptVersion
	for rows.Next() {
		var pv domain.PromptVersion
		var model, createdAt string
		var temperature *float64
		var maxTokens *int
		if err := rows.Scan(&pv.VersionID, &pv.Name, &pv.Version, &pv.Template, &model, &temperature, &maxTokens, &createdAt); err != nil {
			return nil, fmt.Errorf("sqlite: scan prompt version: %w", err)
		}
		pv.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
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
		`SELECT DISTINCT name FROM prompt_versions WHERE project_id = ? ORDER BY name`, projectID,
	)
	if err != nil {
		return nil, fmt.Errorf("sqlite: list prompt names: %w", err)
	}
	defer rows.Close()

	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("sqlite: scan prompt name: %w", err)
		}
		names = append(names, name)
	}
	return names, rows.Err()
}

func (s *Store) SetPromptLabel(ctx context.Context, label *domain.PromptLabel) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO prompt_labels (project_id, name, label, version, updated_at)
		 VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT (project_id, name, label) DO UPDATE SET version = ?, updated_at = ?`,
		label.ProjectID, label.Name, label.Label, label.Version, now,
		label.Version, now,
	)
	if err != nil {
		return fmt.Errorf("sqlite: set prompt label: %w", err)
	}
	return nil
}

// ---- Eval Rules ----

func (s *Store) CreateEvalRule(ctx context.Context, rule *domain.EvalRule) error {
	now := time.Now().UTC().Format(time.RFC3339)
	filterJSON, err := json.Marshal(rule.Filter)
	if err != nil {
		return fmt.Errorf("sqlite: marshal eval filter: %w", err)
	}
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO eval_rules (rule_id, project_id, name, judge_model, prompt_name, prompt_version, filter, sample_rate, enabled, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		rule.RuleID, rule.ProjectID, rule.Name, rule.JudgeModel, rule.PromptName,
		rule.PromptVersion, string(filterJSON), rule.SampleRate, boolToInt(rule.Enabled), now,
	)
	if err != nil {
		return fmt.Errorf("sqlite: create eval rule: %w", err)
	}
	return nil
}

func (s *Store) GetEvalRule(ctx context.Context, ruleID string) (*domain.EvalRule, error) {
	var rule domain.EvalRule
	var filterJSON string
	var enabled int
	var createdAt string
	err := s.db.QueryRowContext(ctx,
		`SELECT project_id, name, judge_model, prompt_name, prompt_version, filter, sample_rate, enabled, created_at
		 FROM eval_rules WHERE rule_id = ?`, ruleID,
	).Scan(&rule.ProjectID, &rule.Name, &rule.JudgeModel, &rule.PromptName, &rule.PromptVersion,
		&filterJSON, &rule.SampleRate, &enabled, &createdAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, metadata.ErrNotFound
		}
		return nil, fmt.Errorf("sqlite: get eval rule: %w", err)
	}
	rule.Enabled = enabled != 0
	rule.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	if err := json.Unmarshal([]byte(filterJSON), &rule.Filter); err != nil {
		return nil, fmt.Errorf("sqlite: unmarshal eval filter: %w", err)
	}
	return &rule, nil
}

func (s *Store) ListEvalRules(ctx context.Context, projectID string) ([]*domain.EvalRule, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT rule_id, name, judge_model, prompt_name, prompt_version, filter, sample_rate, enabled, created_at
		 FROM eval_rules WHERE project_id = ? ORDER BY rule_id`, projectID,
	)
	if err != nil {
		return nil, fmt.Errorf("sqlite: list eval rules: %w", err)
	}
	defer rows.Close()

	var rules []*domain.EvalRule
	for rows.Next() {
		var r domain.EvalRule
		var filterJSON string
		var enabled int
		var createdAt string
		if err := rows.Scan(&r.RuleID, &r.Name, &r.JudgeModel, &r.PromptName, &r.PromptVersion,
			&filterJSON, &r.SampleRate, &enabled, &createdAt); err != nil {
			return nil, fmt.Errorf("sqlite: scan eval rule: %w", err)
		}
		r.ProjectID = projectID
		r.Enabled = enabled != 0
		r.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		if err := json.Unmarshal([]byte(filterJSON), &r.Filter); err != nil {
			return nil, fmt.Errorf("sqlite: unmarshal eval filter: %w", err)
		}
		rules = append(rules, &r)
	}
	return rules, rows.Err()
}

func (s *Store) UpdateEvalRule(ctx context.Context, rule *domain.EvalRule) error {
	filterJSON, err := json.Marshal(rule.Filter)
	if err != nil {
		return fmt.Errorf("sqlite: marshal eval filter: %w", err)
	}
	_, err = s.db.ExecContext(ctx,
		`UPDATE eval_rules SET name = ?, judge_model = ?, prompt_name = ?, prompt_version = ?,
		 filter = ?, sample_rate = ?, enabled = ?
		 WHERE rule_id = ?`,
		rule.Name, rule.JudgeModel, rule.PromptName, rule.PromptVersion,
		string(filterJSON), rule.SampleRate, boolToInt(rule.Enabled), rule.RuleID,
	)
	if err != nil {
		return fmt.Errorf("sqlite: update eval rule: %w", err)
	}
	return nil
}

func (s *Store) DeleteEvalRule(ctx context.Context, ruleID string) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM eval_rules WHERE rule_id = ?`, ruleID,
	)
	if err != nil {
		return fmt.Errorf("sqlite: delete eval rule: %w", err)
	}
	return nil
}

// ---- Datasets ----

func (s *Store) CreateDataset(ctx context.Context, ds *domain.Dataset) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO datasets (dataset_id, project_id, name, created_at) VALUES (?, ?, ?, ?)`,
		ds.DatasetID, ds.ProjectID, ds.Name, now,
	)
	if err != nil {
		return fmt.Errorf("sqlite: create dataset: %w", err)
	}
	return nil
}

func (s *Store) ListDatasets(ctx context.Context, projectID string) ([]*domain.Dataset, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT dataset_id, project_id, name, created_at FROM datasets WHERE project_id = ? ORDER BY name`, projectID,
	)
	if err != nil {
		return nil, fmt.Errorf("sqlite: list datasets: %w", err)
	}
	defer rows.Close()

	var datasets []*domain.Dataset
	for rows.Next() {
		var d domain.Dataset
		var createdAt string
		if err := rows.Scan(&d.DatasetID, &d.ProjectID, &d.Name, &createdAt); err != nil {
			return nil, fmt.Errorf("sqlite: scan dataset: %w", err)
		}
		createdAtTime, err := time.Parse(time.RFC3339, createdAt)
		if err != nil {
			return nil, fmt.Errorf("sqlite: parse created_at: %w", err)
		}
		d.CreatedAt = createdAtTime
		datasets = append(datasets, &d)
	}
	return datasets, rows.Err()
}

func (s *Store) GetDataset(ctx context.Context, datasetID string) (*domain.Dataset, error) {
	var projectID, name, createdAt string
	err := s.db.QueryRowContext(ctx,
		`SELECT project_id, name, created_at FROM datasets WHERE dataset_id = ?`, datasetID,
	).Scan(&projectID, &name, &createdAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, metadata.ErrNotFound
		}
		return nil, fmt.Errorf("sqlite: get dataset: %w", err)
	}
	t, _ := time.Parse(time.RFC3339, createdAt)
	return &domain.Dataset{DatasetID: datasetID, ProjectID: projectID, Name: name, CreatedAt: t}, nil
}

func (s *Store) DeleteDataset(ctx context.Context, datasetID string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("sqlite: begin tx: %w", err)
	}
	defer tx.Rollback()

	// Delete items first.
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM dataset_items WHERE dataset_id = ?`, datasetID); err != nil {
		return fmt.Errorf("sqlite: delete dataset items: %w", err)
	}

	// Delete the dataset.
	result, err := tx.ExecContext(ctx,
		`DELETE FROM datasets WHERE dataset_id = ?`, datasetID)
	if err != nil {
		return fmt.Errorf("sqlite: delete dataset: %w", err)
	}
	if rows, _ := result.RowsAffected(); rows == 0 {
		return metadata.ErrNotFound
	}

	return tx.Commit()
}

func (s *Store) CreateDatasetItem(ctx context.Context, item *domain.DatasetItem) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO dataset_items (item_id, dataset_id, source_span_id, input, expected_output, created_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		item.ItemID, item.DatasetID, item.SourceSpanID, item.Input, item.ExpectedOutput, now,
	)
	if err != nil {
		return fmt.Errorf("sqlite: create dataset item: %w", err)
	}
	return nil
}

func (s *Store) ListDatasetItems(ctx context.Context, datasetID string) ([]*domain.DatasetItem, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT item_id, dataset_id, source_span_id, input, expected_output, created_at
		 FROM dataset_items WHERE dataset_id = ? ORDER BY item_id`, datasetID,
	)
	if err != nil {
		return nil, fmt.Errorf("sqlite: list dataset items: %w", err)
	}
	defer rows.Close()

	var items []*domain.DatasetItem
	for rows.Next() {
		var item domain.DatasetItem
		var createdAt string
		if err := rows.Scan(&item.ItemID, &item.DatasetID, &item.SourceSpanID, &item.Input, &item.ExpectedOutput, &createdAt); err != nil {
			return nil, fmt.Errorf("sqlite: scan dataset item: %w", err)
		}
		item.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
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
			 FROM dataset_items WHERE dataset_id = ?
			 AND (created_at, item_id) > (
			   SELECT created_at, item_id FROM dataset_items WHERE item_id = ? LIMIT 1
			 )
			 ORDER BY created_at ASC, item_id ASC LIMIT ?`,
			datasetID, cursor, limit+1,
		)
	} else {
		rows, err = s.db.QueryContext(ctx,
			`SELECT item_id, dataset_id, source_span_id, input, expected_output, created_at
			 FROM dataset_items WHERE dataset_id = ?
			 ORDER BY created_at ASC, item_id ASC LIMIT ?`,
			datasetID,
			limit+1,
		)
	}
	if err != nil {
		return nil, "", fmt.Errorf("sqlite: list dataset items paginated: %w", err)
	}
	defer rows.Close()

	var items []*domain.DatasetItem
	for rows.Next() {
		var item domain.DatasetItem
		var createdAt string
		if err := rows.Scan(&item.ItemID, &item.DatasetID, &item.SourceSpanID, &item.Input, &item.ExpectedOutput, &createdAt); err != nil {
			return nil, "", fmt.Errorf("sqlite: scan dataset item: %w", err)
		}
		itemCreatedAt, err := time.Parse(time.RFC3339, createdAt)
		if err != nil {
			return nil, "", fmt.Errorf("sqlite: parse created_at: %w", err)
		}
		item.CreatedAt = itemCreatedAt
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
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO dataset_runs (run_id, dataset_id, eval_rule_id, prompt_version, created_at)
		 VALUES (?, ?, ?, ?, ?)`,
		run.RunID, run.DatasetID, run.EvalRuleID, run.PromptVersion, now,
	)
	if err != nil {
		return fmt.Errorf("sqlite: create dataset run: %w", err)
	}
	return nil
}

func (s *Store) GetDatasetRun(ctx context.Context, runID string) (*domain.DatasetRun, error) {
	var datasetID, evalRuleID string
	var promptVersion int64
	var createdAt string
	err := s.db.QueryRowContext(ctx,
		`SELECT dataset_id, eval_rule_id, prompt_version, created_at FROM dataset_runs WHERE run_id = ?`, runID,
	).Scan(&datasetID, &evalRuleID, &promptVersion, &createdAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, metadata.ErrNotFound
		}
		return nil, fmt.Errorf("sqlite: get dataset run: %w", err)
	}
	t, _ := time.Parse(time.RFC3339, createdAt)
	return &domain.DatasetRun{
		RunID:         runID,
		DatasetID:     datasetID,
		EvalRuleID:    evalRuleID,
		PromptVersion: promptVersion,
		CreatedAt:     t,
	}, nil
}

// ---- Helper functions ----

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// derefOr returns the value pointed to by p, or def if p is nil.
func derefOr[T any](p *T, def T) T {
	if p != nil {
		return *p
	}
	return def
}
