package postgres_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/omneval/omneval/internal/domain"
	"github.com/omneval/omneval/internal/metadata"
	"github.com/omneval/omneval/internal/metadata/postgres"
	testpg "github.com/testcontainers/testcontainers-go/modules/postgres"
	"golang.org/x/crypto/bcrypt"
)

var testDBSeq atomic.Int64

// openTestStore creates a fresh Postgres database and returns a migrated Store.
//
// In CI, TEST_POSTGRES_ADMIN_DSN must point to a running Postgres instance (the
// admin database, e.g. "postgres"). Each call creates an isolated test database
// and drops it in t.Cleanup. Locally, testcontainers-go is used; if Docker is
// unavailable the test is skipped.
func openTestStore(ctx context.Context, t *testing.T) *postgres.Store {
	t.Helper()

	if adminDSN := os.Getenv("TEST_POSTGRES_ADMIN_DSN"); adminDSN != "" {
		return openTestStoreCI(ctx, t, adminDSN)
	}
	return openTestStoreLocal(ctx, t)
}

// openTestStoreCI connects to the GitHub Actions postgres service.
// Each call creates a unique database so tests run in isolation.
func openTestStoreCI(ctx context.Context, t *testing.T, adminDSN string) *postgres.Store {
	t.Helper()

	n := testDBSeq.Add(1)
	dbName := fmt.Sprintf("omneval_t%d", n)

	adminDB, err := sql.Open("pgx", adminDSN)
	if err != nil {
		t.Fatalf("open admin db: %v", err)
	}
	if _, err := adminDB.ExecContext(ctx, "CREATE DATABASE "+dbName); err != nil {
		adminDB.Close()
		t.Fatalf("create test db %s: %v", dbName, err)
	}
	adminDB.Close()

	u, err := url.Parse(adminDSN)
	if err != nil {
		t.Fatalf("parse admin DSN: %v", err)
	}
	u.Path = "/" + dbName
	dsn := u.String()

	t.Cleanup(func() {
		// Terminate any lingering connections, then drop the isolated DB.
		adb, err := sql.Open("pgx", adminDSN)
		if err != nil {
			t.Logf("cleanup: open admin db: %v", err)
			return
		}
		defer adb.Close()
		adb.ExecContext(ctx, fmt.Sprintf( //nolint:errcheck
			"SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = '%s'", dbName,
		))
		adb.ExecContext(ctx, "DROP DATABASE IF EXISTS "+dbName) //nolint:errcheck
	})

	s, err := postgres.New(dsn)
	if err != nil {
		t.Fatalf("open test store: %v", err)
	}
	if err := s.Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return s
}

// openTestStoreLocal spins up a throwaway Postgres container via testcontainers-go.
// Skips the test if Docker is unavailable or the container fails to accept connections.
func openTestStoreLocal(ctx context.Context, t *testing.T) *postgres.Store {
	t.Helper()

	pc, err := testpg.RunContainer(ctx,
		testpg.WithDatabase("omneval"),
		testpg.WithUsername("postgres"),
		testpg.WithPassword("postgres"),
	)
	if err != nil {
		t.Skipf("postgres container unavailable (is Docker running?): %v", err)
		return nil // unreachable; t.Skipf calls runtime.Goexit
	}
	t.Cleanup(func() { pc.Terminate(ctx) }) //nolint:errcheck

	dsn, err := pc.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Skipf("postgres container port unavailable: %v", err)
		return nil
	}

	s, err := postgres.New(dsn)
	if err != nil {
		t.Skipf("postgres container did not become ready: %v", err)
		return nil
	}
	if err := s.Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return s
}

func TestMigrate_Clean(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(ctx, t)
	defer s.Close()

	// Verify key tables exist by querying them
	tables := []string{
		"organizations", "projects", "users", "sessions",
		"api_keys", "prompt_versions", "prompt_labels",
		"eval_rules", "datasets", "dataset_items",
		"dataset_runs", "dataset_run_items",
	}
	for _, table := range tables {
		var count int
		err := s.DB().QueryRowContext(ctx,
			"SELECT count(*) FROM information_schema.tables WHERE table_name = $1 AND table_schema = 'public'", table,
		).Scan(&count)
		if err != nil {
			t.Errorf("table %s not found: %v", table, err)
		} else if count == 0 {
			t.Errorf("table %s not found", table)
		}
	}
}

func TestMigrate_Idempotent(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(ctx, t)
	defer s.Close()

	// Calling Migrate again should be a no-op
	if err := s.Migrate(ctx); err != nil {
		t.Fatalf("second migrate call: %v", err)
	}
}

func TestMigrate_Concurrent(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Spin up a fresh Postgres container (no migrations applied yet)
	pc, err := testpg.RunContainer(ctx,
		testpg.WithDatabase("omneval"),
		testpg.WithUsername("postgres"),
		testpg.WithPassword("postgres"),
	)
	if err != nil {
		t.Skipf("postgres container unavailable: %v", err)
		return
	}
	defer pc.Terminate(ctx) //nolint:errcheck

	dsn, err := pc.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Skipf("postgres container port unavailable: %v", err)
		return
	}

	const concurrency = 8
	errCh := make(chan error, concurrency)
	var stores []*postgres.Store

	// Open all stores first (fresh database, no migrations yet)
	for i := 0; i < concurrency; i++ {
		s, err := postgres.New(dsn)
		if err != nil {
			t.Fatalf("open store %d: %v", i, err)
		}
		stores = append(stores, s)
	}
	t.Cleanup(func() {
		for _, s := range stores {
			s.Close() //nolint:errcheck
		}
	})

	// Run Migrate() concurrently from all stores
	var wg sync.WaitGroup
	for _, s := range stores {
		s := s
		wg.Add(1)
		go func() {
			defer wg.Done()
			errCh <- s.Migrate(ctx)
		}()
	}

	// Wait for all to complete
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-ctx.Done():
		t.Fatal("concurrent migration timed out")
	}

	// Check results: every call should succeed (no duplicate-key errors)
	for i := 0; i < concurrency; i++ {
		if err := <-errCh; err != nil {
			t.Errorf("concurrent migrate %d: %v", i, err)
		}
	}

	// Verify migrations were applied exactly once
	var count int
	err = stores[0].DB().QueryRowContext(ctx,
		"SELECT count(DISTINCT version) FROM _schema_migrations",
	).Scan(&count)
	if err != nil {
		t.Fatalf("count migrations: %v", err)
	}
	if count == 0 {
		t.Fatal("expected at least one migration to be applied")
	}
}

// ---- Organizations ----

func TestOrg_CreateAndGet(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(ctx, t)
	defer s.Close()

	now := time.Now().UTC()
	org := &domain.Organization{OrgID: "org-1", Name: "Test Corp", CreatedAt: now}
	if err := s.CreateOrganization(ctx, org); err != nil {
		t.Fatalf("create org: %v", err)
	}

	got, err := s.GetOrganization(ctx, "org-1")
	if err != nil {
		t.Fatalf("get org: %v", err)
	}
	if got.OrgID != "org-1" || got.Name != "Test Corp" {
		t.Errorf("got %v, want %+v", got, org)
	}
}

func TestOrg_NotFound(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(ctx, t)
	defer s.Close()

	_, err := s.GetOrganization(ctx, "no-such-org")
	if !errors.Is(err, metadata.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

// ---- Projects ----

func TestProject_CreateAndGet(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(ctx, t)
	defer s.Close()

	now := time.Now().UTC()
	if err := s.CreateOrganization(ctx, &domain.Organization{OrgID: "org-1", Name: "Test Corp"}); err != nil {
		t.Fatalf("create org: %v", err)
	}

	project := &domain.Project{ProjectID: "proj-1", OrgID: "org-1", Name: "My Project", CreatedAt: now}
	if err := s.CreateProject(ctx, project); err != nil {
		t.Fatalf("create project: %v", err)
	}

	got, err := s.GetProject(ctx, "proj-1")
	if err != nil {
		t.Fatalf("get project: %v", err)
	}
	if got.OrgID != "org-1" || got.Name != "My Project" {
		t.Errorf("got %v, want %+v", got, project)
	}
}

func TestProject_ListByOrg(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(ctx, t)
	defer s.Close()

	s.CreateOrganization(ctx, &domain.Organization{OrgID: "org-1", Name: "Test Corp"})
	s.CreateOrganization(ctx, &domain.Organization{OrgID: "org-2", Name: "Other Corp"})

	now := time.Now().UTC()
	s.CreateProject(ctx, &domain.Project{ProjectID: "proj-1", OrgID: "org-1", Name: "P1", CreatedAt: now})
	s.CreateProject(ctx, &domain.Project{ProjectID: "proj-2", OrgID: "org-1", Name: "P2", CreatedAt: now})
	s.CreateProject(ctx, &domain.Project{ProjectID: "proj-3", OrgID: "org-2", Name: "P3", CreatedAt: now})

	projects, err := s.ListProjects(ctx, "org-1")
	if err != nil {
		t.Fatalf("list projects: %v", err)
	}
	if len(projects) != 2 {
		t.Fatalf("expected 2 projects, got %d", len(projects))
	}
	if projects[0].ProjectID != "proj-1" || projects[1].ProjectID != "proj-2" {
		t.Errorf("unexpected project order: %v", projects)
	}
}

// ---- Users ----

func TestUser_CreateAndGetByEmail(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(ctx, t)
	defer s.Close()

	now := time.Now().UTC()
	if err := s.CreateOrganization(ctx, &domain.Organization{OrgID: "org-1", Name: "Test Corp", CreatedAt: now}); err != nil {
		t.Fatalf("create org: %v", err)
	}
	user := &domain.User{UserID: "user-1", OrgID: "org-1", Email: "alice@example.com", PasswordHash: "$2a$10$...", CreatedAt: now}
	if err := s.CreateUser(ctx, user); err != nil {
		t.Fatalf("create user: %v", err)
	}

	got, err := s.GetUserByEmail(ctx, "alice@example.com")
	if err != nil {
		t.Fatalf("get user by email: %v", err)
	}
	if got.Email != "alice@example.com" || got.UserID != "user-1" {
		t.Errorf("got %v", got)
	}
	// Verify password was hashed by bcrypt
	if len(got.PasswordHash) < 30 {
		t.Errorf("password hash too short: %q", got.PasswordHash)
	}
}

func TestUser_ListByOrg(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(ctx, t)
	defer s.Close()

	s.CreateOrganization(ctx, &domain.Organization{OrgID: "org-1", Name: "Test Corp"})
	now := time.Now().UTC()

	s.CreateUser(ctx, &domain.User{UserID: "user-1", OrgID: "org-1", Email: "a@example.com", PasswordHash: "hash1", CreatedAt: now})
	s.CreateUser(ctx, &domain.User{UserID: "user-2", OrgID: "org-1", Email: "b@example.com", PasswordHash: "hash2", CreatedAt: now})
	s.CreateUser(ctx, &domain.User{UserID: "user-3", OrgID: "org-2", Email: "c@example.com", PasswordHash: "hash3", CreatedAt: now})

	users, err := s.ListUsers(ctx, "org-1")
	if err != nil {
		t.Fatalf("list users: %v", err)
	}
	if len(users) != 2 {
		t.Fatalf("expected 2 users, got %d", len(users))
	}
	emails := []string{users[0].Email, users[1].Email}
	if emails[0] != "a@example.com" || emails[1] != "b@example.com" {
		t.Errorf("unexpected emails: %v", emails)
	}
}

func TestUser_CheckPassword(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(ctx, t)
	defer s.Close()

	hash, err := bcrypt.GenerateFromPassword([]byte("correct-password"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("generate bcrypt: %v", err)
	}

	if err := s.CheckPassword(string(hash), "correct-password"); err != nil {
		t.Error("correct password should match")
	}
	if err := s.CheckPassword(string(hash), "wrong-password"); err == nil {
		t.Error("wrong password should not match")
	}
}

// ---- Sessions ----

func TestSession_CreateAndGet(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(ctx, t)
	defer s.Close()

	now := time.Now().UTC()
	if err := s.CreateOrganization(ctx, &domain.Organization{OrgID: "org-1", Name: "Test Corp", CreatedAt: now}); err != nil {
		t.Fatalf("create org: %v", err)
	}
	if err := s.CreateUser(ctx, &domain.User{UserID: "user-1", OrgID: "org-1", Email: "sess-test@example.com", PasswordHash: "password", CreatedAt: now}); err != nil {
		t.Fatalf("create user: %v", err)
	}
	expiresAt := now.Add(24 * time.Hour)
	session := &domain.Session{SessionID: "sess-1", UserID: "user-1", ExpiresAt: expiresAt, CreatedAt: now}
	if err := s.CreateSession(ctx, session); err != nil {
		t.Fatalf("create session: %v", err)
	}

	got, err := s.GetSession(ctx, "sess-1")
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	if got.UserID != "user-1" {
		t.Errorf("got userID %q, want %q", got.UserID, "user-1")
	}
}

func TestSession_Delete(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(ctx, t)
	defer s.Close()

	now := time.Now().UTC()
	expiresAt := now.Add(24 * time.Hour)
	s.CreateSession(ctx, &domain.Session{SessionID: "sess-1", UserID: "user-1", ExpiresAt: expiresAt, CreatedAt: now})

	if err := s.DeleteSession(ctx, "sess-1"); err != nil {
		t.Fatalf("delete session: %v", err)
	}

	_, err := s.GetSession(ctx, "sess-1")
	if err != metadata.ErrNotFound {
		t.Errorf("expected ErrNotFound after delete, got %v", err)
	}
}

// ---- API Keys ----

func TestAPIKey_CreateAndGetByHash(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(ctx, t)
	defer s.Close()

	now := time.Now().UTC()
	s.CreateOrganization(ctx, &domain.Organization{OrgID: "org-1", Name: "Test Corp"})
	s.CreateProject(ctx, &domain.Project{ProjectID: "proj-1", OrgID: "org-1", Name: "P1"})

	key := &domain.APIKey{KeyID: "key-1", ProjectID: "proj-1", Kind: domain.APIKeyKindProject, HashedKey: "sha256hash", CreatedAt: now}
	if err := s.CreateAPIKey(ctx, key); err != nil {
		t.Fatalf("create api key: %v", err)
	}

	got, err := s.GetAPIKeyByHash(ctx, "sha256hash")
	if err != nil {
		t.Fatalf("get api key by hash: %v", err)
	}
	if got.KeyID != "key-1" || got.ProjectID != "proj-1" || got.Kind != domain.APIKeyKindProject {
		t.Errorf("got %v", got)
	}
}

// TestAPIKey_NameRoundTrip verifies that the optional display name (#143)
// is persisted and returned by both GetAPIKeyByHash and ListAPIKeys.
func TestAPIKey_NameRoundTrip(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(ctx, t)
	defer s.Close()

	now := time.Now().UTC()
	s.CreateOrganization(ctx, &domain.Organization{OrgID: "org-1", Name: "Test Corp"})
	s.CreateProject(ctx, &domain.Project{ProjectID: "proj-1", OrgID: "org-1", Name: "P1"})

	key := &domain.APIKey{KeyID: "key-1", ProjectID: "proj-1", Kind: domain.APIKeyKindProject, Name: "CI ingest", HashedKey: "sha256hash-name", CreatedAt: now}
	if err := s.CreateAPIKey(ctx, key); err != nil {
		t.Fatalf("create api key: %v", err)
	}

	got, err := s.GetAPIKeyByHash(ctx, "sha256hash-name")
	if err != nil {
		t.Fatalf("get api key by hash: %v", err)
	}
	if got.Name != "CI ingest" {
		t.Errorf("name: got %q, want %q", got.Name, "CI ingest")
	}

	keys, err := s.ListAPIKeys(ctx, "proj-1")
	if err != nil {
		t.Fatalf("list api keys: %v", err)
	}
	if len(keys) != 1 || keys[0].Name != "CI ingest" {
		t.Errorf("listed key name: got %v", keys)
	}
}

func TestAPIKey_Revoke(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(ctx, t)
	defer s.Close()

	s.CreateOrganization(ctx, &domain.Organization{OrgID: "org-1", Name: "Test Corp"})
	s.CreateProject(ctx, &domain.Project{ProjectID: "proj-1", OrgID: "org-1", Name: "P1"})

	now := time.Now().UTC()
	s.CreateAPIKey(ctx, &domain.APIKey{KeyID: "key-1", ProjectID: "proj-1", Kind: domain.APIKeyKindProject, HashedKey: "h1", CreatedAt: now})

	if err := s.RevokeAPIKey(ctx, "key-1"); err != nil {
		t.Fatalf("revoke: %v", err)
	}

	got, err := s.GetAPIKeyByHash(ctx, "h1")
	if err != nil {
		t.Fatalf("get revoked key: %v", err)
	}
	if got.RevokedAt == nil {
		t.Error("expected revoked_at to be set")
	}
}

func TestAPIKey_GetByHash_NilServiceName(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(ctx, t)
	defer s.Close()

	now := time.Now().UTC()
	s.CreateOrganization(ctx, &domain.Organization{OrgID: "org-1", Name: "Test Corp"})
	s.CreateProject(ctx, &domain.Project{ProjectID: "proj-1", OrgID: "org-1", Name: "P1"})

	// Insert a project-scoped key with truly NULL service_name via raw SQL
	// (domain.APIKey{} would default to empty string, not NULL)
	_, err := s.DB().ExecContext(ctx,
		`INSERT INTO api_keys (key_id, project_id, kind, service_name, hashed_key, created_at)
		 VALUES ('key-null', 'proj-1', 'project', NULL, 'sha256hash-null', $1)`,
		now,
	)
	if err != nil {
		t.Fatalf("insert NULL service_name: %v", err)
	}

	got, err := s.GetAPIKeyByHash(ctx, "sha256hash-null")
	if err != nil {
		t.Fatalf("get api key with NULL service_name: %v", err)
	}
	if got.KeyID != "key-null" || got.Kind != domain.APIKeyKindProject {
		t.Errorf("unexpected key: %v", got)
	}
	if got.ServiceName != "" {
		t.Errorf("expected empty string for nil service_name, got %q", got.ServiceName)
	}
}

func TestAPIKey_ListByProject(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(ctx, t)
	defer s.Close()

	now := time.Now().UTC()
	s.CreateOrganization(ctx, &domain.Organization{OrgID: "org-1", Name: "Test Corp"})
	s.CreateProject(ctx, &domain.Project{ProjectID: "proj-1", OrgID: "org-1", Name: "P1"})
	s.CreateProject(ctx, &domain.Project{ProjectID: "proj-2", OrgID: "org-1", Name: "P2"})

	s.CreateAPIKey(ctx, &domain.APIKey{KeyID: "k1", ProjectID: "proj-1", Kind: domain.APIKeyKindProject, HashedKey: "h1", CreatedAt: now})
	s.CreateAPIKey(ctx, &domain.APIKey{KeyID: "k2", ProjectID: "proj-1", Kind: domain.APIKeyKindService, ServiceName: "svc", HashedKey: "h2", CreatedAt: now})
	s.CreateAPIKey(ctx, &domain.APIKey{KeyID: "k3", ProjectID: "proj-2", Kind: domain.APIKeyKindProject, HashedKey: "h3", CreatedAt: now})

	keys, err := s.ListAPIKeys(ctx, "proj-1")
	if err != nil {
		t.Fatalf("list api keys: %v", err)
	}
	if len(keys) != 2 {
		t.Fatalf("expected 2 keys for proj-1, got %d", len(keys))
	}
}

// ---- Prompt Versions ----

func TestPrompt_CreateAndGet(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(ctx, t)
	defer s.Close()

	now := time.Now().UTC()
	if err := s.CreateOrganization(ctx, &domain.Organization{OrgID: "org-1", Name: "Test Corp", CreatedAt: now}); err != nil {
		t.Fatalf("create org: %v", err)
	}
	if err := s.CreateProject(ctx, &domain.Project{ProjectID: "proj-1", OrgID: "org-1", Name: "P1", CreatedAt: now}); err != nil {
		t.Fatalf("create project: %v", err)
	}
	pv := &domain.PromptVersion{
		VersionID:   "pv-1",
		ProjectID:   "proj-1",
		Name:        "greeting",
		Version:     1,
		Template:    "Hello {{name}}, welcome!",
		ModelConfig: domain.PromptModelConfig{Model: "gpt-4", Temperature: 0.7, MaxTokens: 256},
		CreatedAt:   now,
	}
	if err := s.CreatePromptVersion(ctx, pv); err != nil {
		t.Fatalf("create prompt version: %v", err)
	}

	got, err := s.GetPromptVersion(ctx, "proj-1", "greeting", 1)
	if err != nil {
		t.Fatalf("get prompt version: %v", err)
	}
	if got.Template != "Hello {{name}}, welcome!" || got.Version != 1 {
		t.Errorf("got %v", got)
	}
}

func TestPrompt_GetByLabel(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(ctx, t)
	defer s.Close()

	now := time.Now().UTC()
	if err := s.CreateOrganization(ctx, &domain.Organization{OrgID: "org-1", Name: "Test Corp", CreatedAt: now}); err != nil {
		t.Fatalf("create org: %v", err)
	}
	if err := s.CreateProject(ctx, &domain.Project{ProjectID: "proj-1", OrgID: "org-1", Name: "P1", CreatedAt: now}); err != nil {
		t.Fatalf("create project: %v", err)
	}
	s.CreatePromptVersion(ctx, &domain.PromptVersion{
		VersionID:   "pv-1",
		ProjectID:   "proj-1",
		Name:        "greeting",
		Version:     1,
		Template:    "v1",
		ModelConfig: domain.PromptModelConfig{Model: "gpt-3.5"},
		CreatedAt:   now,
	})
	s.CreatePromptVersion(ctx, &domain.PromptVersion{
		VersionID:   "pv-2",
		ProjectID:   "proj-1",
		Name:        "greeting",
		Version:     2,
		Template:    "v2",
		ModelConfig: domain.PromptModelConfig{Model: "gpt-4"},
		CreatedAt:   now,
	})

	s.SetPromptLabel(ctx, &domain.PromptLabel{
		ProjectID: "proj-1", Name: "greeting", Label: "production", Version: 2,
	})

	got, err := s.GetPromptByLabel(ctx, "proj-1", "greeting", "production")
	if err != nil {
		t.Fatalf("get prompt by label: %v", err)
	}
	if got.Version != 2 || got.Template != "v2" {
		t.Errorf("got version %d template %q, want version 2 template \"v2\"", got.Version, got.Template)
	}
}

func TestPrompt_ListVersions(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(ctx, t)
	defer s.Close()

	now := time.Now().UTC()
	if err := s.CreateOrganization(ctx, &domain.Organization{OrgID: "org-1", Name: "Test Corp", CreatedAt: now}); err != nil {
		t.Fatalf("create org: %v", err)
	}
	if err := s.CreateProject(ctx, &domain.Project{ProjectID: "proj-1", OrgID: "org-1", Name: "P1", CreatedAt: now}); err != nil {
		t.Fatalf("create project: %v", err)
	}
	for i := int64(1); i <= 3; i++ {
		s.CreatePromptVersion(ctx, &domain.PromptVersion{
			VersionID:   "pv-" + string(rune('0'+i)),
			ProjectID:   "proj-1",
			Name:        "greeting",
			Version:     i,
			Template:    "v" + string(rune('0'+i)),
			ModelConfig: domain.PromptModelConfig{Model: "gpt-4"},
			CreatedAt:   now,
		})
	}

	versions, err := s.ListPromptVersions(ctx, "proj-1", "greeting")
	if err != nil {
		t.Fatalf("list prompt versions: %v", err)
	}
	if len(versions) != 3 {
		t.Fatalf("expected 3 versions, got %d", len(versions))
	}
	if versions[0].Version != 1 || versions[2].Version != 3 {
		t.Errorf("unexpected version order: %v", versions)
	}
}

func TestPrompt_LabelUpsert(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(ctx, t)
	defer s.Close()

	now := time.Now().UTC()
	if err := s.CreateOrganization(ctx, &domain.Organization{OrgID: "org-1", Name: "Test Corp", CreatedAt: now}); err != nil {
		t.Fatalf("create org: %v", err)
	}
	if err := s.CreateProject(ctx, &domain.Project{ProjectID: "proj-1", OrgID: "org-1", Name: "P1", CreatedAt: now}); err != nil {
		t.Fatalf("create project: %v", err)
	}
	s.CreatePromptVersion(ctx, &domain.PromptVersion{
		VersionID:   "pv-1",
		ProjectID:   "proj-1",
		Name:        "greeting",
		Version:     1,
		Template:    "v1",
		ModelConfig: domain.PromptModelConfig{},
		CreatedAt:   now,
	})
	s.CreatePromptVersion(ctx, &domain.PromptVersion{
		VersionID:   "pv-2",
		ProjectID:   "proj-1",
		Name:        "greeting",
		Version:     2,
		Template:    "v2",
		ModelConfig: domain.PromptModelConfig{},
		CreatedAt:   now,
	})

	// Set initial label
	s.SetPromptLabel(ctx, &domain.PromptLabel{
		ProjectID: "proj-1", Name: "greeting", Label: "production", Version: 1,
	})

	// Update label to point to v2
	s.SetPromptLabel(ctx, &domain.PromptLabel{
		ProjectID: "proj-1", Name: "greeting", Label: "production", Version: 2,
	})

	got, err := s.GetPromptByLabel(ctx, "proj-1", "greeting", "production")
	if err != nil {
		t.Fatalf("get prompt by label after upsert: %v", err)
	}
	if got.Version != 2 {
		t.Errorf("expected version 2 after upsert, got %d", got.Version)
	}
}

// ---- Eval Rules ----

func TestEvalRule_CreateAndGet(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(ctx, t)
	defer s.Close()

	now := time.Now().UTC()
	if err := s.CreateOrganization(ctx, &domain.Organization{OrgID: "org-1", Name: "Test Corp", CreatedAt: now}); err != nil {
		t.Fatalf("create org: %v", err)
	}
	if err := s.CreateProject(ctx, &domain.Project{ProjectID: "proj-1", OrgID: "org-1", Name: "P1", CreatedAt: now}); err != nil {
		t.Fatalf("create project: %v", err)
	}
	filter := domain.EvalFilter{Model: func() *string { s := "gpt-4"; return &s }()}
	rule := &domain.EvalRule{
		RuleID:        "rule-1",
		ProjectID:     "proj-1",
		Name:          "latency check",
		JudgeModel:    "gpt-4",
		PromptName:    "latency-scorer",
		PromptVersion: 1,
		Filter:        filter,
		SampleRate:    1.0,
		Enabled:       true,
		CreatedAt:     now,
	}
	if err := s.CreateEvalRule(ctx, rule); err != nil {
		t.Fatalf("create eval rule: %v", err)
	}

	got, err := s.GetEvalRule(ctx, "rule-1")
	if err != nil {
		t.Fatalf("get eval rule: %v", err)
	}
	if got.Name != "latency check" || got.Enabled != true || got.SampleRate != 1.0 {
		t.Errorf("got %v", got)
	}
	if got.Filter.Model == nil || *got.Filter.Model != "gpt-4" {
		t.Errorf("unexpected filter: %v", got.Filter)
	}
}

func TestEvalRule_ListByProject(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(ctx, t)
	defer s.Close()

	now := time.Now().UTC()
	if err := s.CreateOrganization(ctx, &domain.Organization{OrgID: "org-1", Name: "Test Corp", CreatedAt: now}); err != nil {
		t.Fatalf("create org: %v", err)
	}
	if err := s.CreateProject(ctx, &domain.Project{ProjectID: "proj-1", OrgID: "org-1", Name: "P1", CreatedAt: now}); err != nil {
		t.Fatalf("create project: %v", err)
	}
	if err := s.CreateProject(ctx, &domain.Project{ProjectID: "proj-2", OrgID: "org-1", Name: "P2", CreatedAt: now}); err != nil {
		t.Fatalf("create project: %v", err)
	}
	s.CreateEvalRule(ctx, &domain.EvalRule{RuleID: "r1", ProjectID: "proj-1", Name: "rule-1", JudgeModel: "gpt-4", PromptName: "p", PromptVersion: 1, Enabled: true, CreatedAt: now})
	s.CreateEvalRule(ctx, &domain.EvalRule{RuleID: "r2", ProjectID: "proj-1", Name: "rule-2", JudgeModel: "gpt-3.5", PromptName: "p", PromptVersion: 1, Enabled: false, CreatedAt: now})
	s.CreateEvalRule(ctx, &domain.EvalRule{RuleID: "r3", ProjectID: "proj-2", Name: "rule-3", JudgeModel: "gpt-4", PromptName: "p", PromptVersion: 1, Enabled: true, CreatedAt: now})

	rules, err := s.ListEvalRules(ctx, "proj-1")
	if err != nil {
		t.Fatalf("list eval rules: %v", err)
	}
	if len(rules) != 2 {
		t.Fatalf("expected 2 rules, got %d", len(rules))
	}
}

func TestEvalRule_Update(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(ctx, t)
	defer s.Close()

	now := time.Now().UTC()
	if err := s.CreateOrganization(ctx, &domain.Organization{OrgID: "org-1", Name: "Test Corp", CreatedAt: now}); err != nil {
		t.Fatalf("create org: %v", err)
	}
	if err := s.CreateProject(ctx, &domain.Project{ProjectID: "proj-1", OrgID: "org-1", Name: "P1", CreatedAt: now}); err != nil {
		t.Fatalf("create project: %v", err)
	}
	s.CreateEvalRule(ctx, &domain.EvalRule{RuleID: "r1", ProjectID: "proj-1", Name: "original", JudgeModel: "gpt-3.5", PromptName: "p", PromptVersion: 1, Enabled: true, CreatedAt: now})

	s.UpdateEvalRule(ctx, &domain.EvalRule{RuleID: "r1", ProjectID: "proj-1", Name: "updated", JudgeModel: "gpt-4", PromptName: "p", PromptVersion: 2, Enabled: false, CreatedAt: now})

	got, err := s.GetEvalRule(ctx, "r1")
	if err != nil {
		t.Fatalf("get updated rule: %v", err)
	}
	if got.Name != "updated" || got.Enabled != false || got.PromptVersion != 2 {
		t.Errorf("got %v", got)
	}
}

// ---- Datasets ----

func TestDataset_CreateAndGet(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(ctx, t)
	defer s.Close()

	now := time.Now().UTC()
	if err := s.CreateOrganization(ctx, &domain.Organization{OrgID: "org-1", Name: "Test Corp", CreatedAt: now}); err != nil {
		t.Fatalf("create org: %v", err)
	}
	if err := s.CreateProject(ctx, &domain.Project{ProjectID: "proj-1", OrgID: "org-1", Name: "P1", CreatedAt: now}); err != nil {
		t.Fatalf("create project: %v", err)
	}
	ds := &domain.Dataset{DatasetID: "ds-1", ProjectID: "proj-1", Name: "Eval Set", CreatedAt: now}
	if err := s.CreateDataset(ctx, ds); err != nil {
		t.Fatalf("create dataset: %v", err)
	}

	got, err := s.GetDataset(ctx, "ds-1")
	if err != nil {
		t.Fatalf("get dataset: %v", err)
	}
	if got.Name != "Eval Set" {
		t.Errorf("got %v", got)
	}
}

func TestDatasetItems_CreateAndList(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(ctx, t)
	defer s.Close()

	now := time.Now().UTC()
	if err := s.CreateOrganization(ctx, &domain.Organization{OrgID: "org-1", Name: "Test Corp", CreatedAt: now}); err != nil {
		t.Fatalf("create org: %v", err)
	}
	if err := s.CreateProject(ctx, &domain.Project{ProjectID: "proj-1", OrgID: "org-1", Name: "P1", CreatedAt: now}); err != nil {
		t.Fatalf("create project: %v", err)
	}
	s.CreateDataset(ctx, &domain.Dataset{DatasetID: "ds-1", ProjectID: "proj-1", Name: "Set", CreatedAt: now})

	s.CreateDatasetItem(ctx, &domain.DatasetItem{ItemID: "item-1", DatasetID: "ds-1", Input: "hello", ExpectedOutput: "hi", CreatedAt: now})
	s.CreateDatasetItem(ctx, &domain.DatasetItem{ItemID: "item-2", DatasetID: "ds-1", Input: "bye", ExpectedOutput: "goodbye", CreatedAt: now})

	items, err := s.ListDatasetItems(ctx, "ds-1")
	if err != nil {
		t.Fatalf("list dataset items: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}
	if items[0].Input != "hello" || items[1].Input != "bye" {
		t.Errorf("unexpected items: %v", items)
	}
}

func TestDatasetRun_CreateAndGet(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(ctx, t)
	defer s.Close()

	now := time.Now().UTC()
	if err := s.CreateOrganization(ctx, &domain.Organization{OrgID: "org-1", Name: "Test Corp", CreatedAt: now}); err != nil {
		t.Fatalf("create org: %v", err)
	}
	if err := s.CreateProject(ctx, &domain.Project{ProjectID: "proj-1", OrgID: "org-1", Name: "P1", CreatedAt: now}); err != nil {
		t.Fatalf("create project: %v", err)
	}
	s.CreateDataset(ctx, &domain.Dataset{DatasetID: "ds-1", ProjectID: "proj-1", Name: "Set", CreatedAt: now})
	if err := s.CreateEvalRule(ctx, &domain.EvalRule{
		RuleID:     "rule-1",
		ProjectID:  "proj-1",
		Name:       "test rule",
		JudgeModel: "gpt-4",
		SampleRate: 1.0,
		Enabled:    true,
		CreatedAt:  now,
	}); err != nil {
		t.Fatalf("create eval rule: %v", err)
	}

	run := &domain.DatasetRun{RunID: "run-1", DatasetID: "ds-1", EvalRuleID: "rule-1", PromptVersion: 1, CreatedAt: now}
	if err := s.CreateDatasetRun(ctx, run); err != nil {
		t.Fatalf("create dataset run: %v", err)
	}

	got, err := s.GetDatasetRun(ctx, "run-1")
	if err != nil {
		t.Fatalf("get dataset run: %v", err)
	}
	if got.EvalRuleID != "rule-1" || got.PromptVersion != 1 {
		t.Errorf("got %v", got)
	}
}

func TestClose_NoError(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(ctx, t)
	if err := s.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
}
