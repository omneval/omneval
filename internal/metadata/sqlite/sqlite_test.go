package sqlite_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/zbloss/lantern/internal/domain"
	"github.com/zbloss/lantern/internal/metadata"
	"github.com/zbloss/lantern/internal/metadata/sqlite"
	"golang.org/x/crypto/bcrypt"
)

// helper creates a temporary SQLite file and a Store bound to it.
// The caller is responsible for cleanup.
func openTestStore(t *testing.T) (*sqlite.Store, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "test.db")
	s, err := sqlite.New(path)
	if err != nil {
		t.Fatalf("open test store: %v", err)
	}
	// Run migrations immediately
	if err := s.Migrate(context.Background()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return s, path
}

func TestMigrate_Clean(t *testing.T) {
	s, path := openTestStore(t)
	defer s.Close()

	// Verify the database file exists
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Fatal("database file was not created")
	}

	// Verify key tables exist by querying them
	tables := []string{
		"organizations", "projects", "users", "sessions",
		"api_keys", "prompt_versions", "prompt_labels",
		"eval_rules", "datasets", "dataset_items",
		"dataset_runs", "dataset_run_items",
	}
	for _, table := range tables {
		var count int
		err := s.DB().QueryRow("SELECT count(*) FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&count)
		if err != nil {
			t.Errorf("table %s not found: %v", table, err)
		}
		_ = count
	}
}

func TestMigrate_Idempotent(t *testing.T) {
	s, _ := openTestStore(t)
	defer s.Close()

	// Calling Migrate again should be a no-op
	if err := s.Migrate(context.Background()); err != nil {
		t.Fatalf("second migrate call: %v", err)
	}
}

// ---- Organizations ----

func TestOrg_CreateAndGet(t *testing.T) {
	s, _ := openTestStore(t)
	defer s.Close()

	org := &domain.Organization{OrgID: "org-1", Name: "Test Corp"}
	if err := s.CreateOrganization(context.Background(), org); err != nil {
		t.Fatalf("create org: %v", err)
	}

	got, err := s.GetOrganization(context.Background(), "org-1")
	if err != nil {
		t.Fatalf("get org: %v", err)
	}
	if got.OrgID != "org-1" || got.Name != "Test Corp" {
		t.Errorf("got %v, want %+v", got, org)
	}
}

func TestOrg_NotFound(t *testing.T) {
	s, _ := openTestStore(t)
	defer s.Close()

	_, err := s.GetOrganization(context.Background(), "no-such-org")
	if !errors.Is(err, metadata.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

// ---- Projects ----

func TestProject_CreateAndGet(t *testing.T) {
	s, _ := openTestStore(t)
	defer s.Close()

	// Create parent org first
	if err := s.CreateOrganization(context.Background(), &domain.Organization{OrgID: "org-1", Name: "Test Corp"}); err != nil {
		t.Fatalf("create org: %v", err)
	}

	project := &domain.Project{ProjectID: "proj-1", OrgID: "org-1", Name: "My Project"}
	if err := s.CreateProject(context.Background(), project); err != nil {
		t.Fatalf("create project: %v", err)
	}

	got, err := s.GetProject(context.Background(), "proj-1")
	if err != nil {
		t.Fatalf("get project: %v", err)
	}
	if got.OrgID != "org-1" || got.Name != "My Project" {
		t.Errorf("got %v, want %+v", got, project)
	}
}

func TestProject_ListByOrg(t *testing.T) {
	s, _ := openTestStore(t)
	defer s.Close()

	s.CreateOrganization(context.Background(), &domain.Organization{OrgID: "org-1", Name: "Test Corp"})
	s.CreateOrganization(context.Background(), &domain.Organization{OrgID: "org-2", Name: "Other Corp"})

	s.CreateProject(context.Background(), &domain.Project{ProjectID: "proj-1", OrgID: "org-1", Name: "P1"})
	s.CreateProject(context.Background(), &domain.Project{ProjectID: "proj-2", OrgID: "org-1", Name: "P2"})
	s.CreateProject(context.Background(), &domain.Project{ProjectID: "proj-3", OrgID: "org-2", Name: "P3"})

	projects, err := s.ListProjects(context.Background(), "org-1")
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
	s, _ := openTestStore(t)
	defer s.Close()

	user := &domain.User{UserID: "user-1", OrgID: "org-1", Email: "alice@example.com", PasswordHash: "$2a$10$..."}
	if err := s.CreateUser(context.Background(), user); err != nil {
		t.Fatalf("create user: %v", err)
	}

	got, err := s.GetUserByEmail(context.Background(), "alice@example.com")
	if err != nil {
		t.Fatalf("get user: %v", err)
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
	s, _ := openTestStore(t)
	defer s.Close()

	s.CreateOrganization(context.Background(), &domain.Organization{OrgID: "org-1", Name: "Test Corp"})

	s.CreateUser(context.Background(), &domain.User{UserID: "user-1", OrgID: "org-1", Email: "a@example.com", PasswordHash: "hash1"})
	s.CreateUser(context.Background(), &domain.User{UserID: "user-2", OrgID: "org-1", Email: "b@example.com", PasswordHash: "hash2"})
	s.CreateUser(context.Background(), &domain.User{UserID: "user-3", OrgID: "org-2", Email: "c@example.com", PasswordHash: "hash3"})

	users, err := s.ListUsers(context.Background(), "org-1")
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
	s, _ := openTestStore(t)
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
	s, _ := openTestStore(t)
	defer s.Close()

	expiresAt := time.Now().Add(24 * time.Hour)
	session := &domain.Session{SessionID: "sess-1", UserID: "user-1", ExpiresAt: expiresAt}
	if err := s.CreateSession(context.Background(), session); err != nil {
		t.Fatalf("create session: %v", err)
	}

	got, err := s.GetSession(context.Background(), "sess-1")
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	if got.UserID != "user-1" {
		t.Errorf("got userID %q, want %q", got.UserID, "user-1")
	}
}

func TestSession_Delete(t *testing.T) {
	s, _ := openTestStore(t)
	defer s.Close()

	expiresAt := time.Now().Add(24 * time.Hour)
	s.CreateSession(context.Background(), &domain.Session{SessionID: "sess-1", UserID: "user-1", ExpiresAt: expiresAt})

	if err := s.DeleteSession(context.Background(), "sess-1"); err != nil {
		t.Fatalf("delete session: %v", err)
	}

	_, err := s.GetSession(context.Background(), "sess-1")
	if err != metadata.ErrNotFound {
		t.Errorf("expected ErrNotFound after delete, got %v", err)
	}
}

// ---- API Keys ----

func TestAPIKey_CreateAndGetByHash(t *testing.T) {
	s, _ := openTestStore(t)
	defer s.Close()

	s.CreateOrganization(context.Background(), &domain.Organization{OrgID: "org-1", Name: "Test Corp"})
	s.CreateProject(context.Background(), &domain.Project{ProjectID: "proj-1", OrgID: "org-1", Name: "P1"})

	key := &domain.APIKey{KeyID: "key-1", ProjectID: "proj-1", Kind: domain.APIKeyKindProject, HashedKey: "sha256hash"}
	if err := s.CreateAPIKey(context.Background(), key); err != nil {
		t.Fatalf("create api key: %v", err)
	}

	got, err := s.GetAPIKeyByHash(context.Background(), "sha256hash")
	if err != nil {
		t.Fatalf("get api key by hash: %v", err)
	}
	if got.KeyID != "key-1" || got.ProjectID != "proj-1" || got.Kind != domain.APIKeyKindProject {
		t.Errorf("got %v", got)
	}
}

func TestAPIKey_Revoke(t *testing.T) {
	s, _ := openTestStore(t)
	defer s.Close()

	s.CreateOrganization(context.Background(), &domain.Organization{OrgID: "org-1", Name: "Test Corp"})
	s.CreateProject(context.Background(), &domain.Project{ProjectID: "proj-1", OrgID: "org-1", Name: "P1"})

	s.CreateAPIKey(context.Background(), &domain.APIKey{KeyID: "key-1", ProjectID: "proj-1", Kind: domain.APIKeyKindProject, HashedKey: "h1"})

	if err := s.RevokeAPIKey(context.Background(), "key-1"); err != nil {
		t.Fatalf("revoke: %v", err)
	}

	got, err := s.GetAPIKeyByHash(context.Background(), "h1")
	if err != nil {
		t.Fatalf("get revoked key: %v", err)
	}
	if got.RevokedAt == nil {
		t.Error("expected revoked_at to be set")
	}
}

func TestAPIKey_ListByProject(t *testing.T) {
	s, _ := openTestStore(t)
	defer s.Close()

	s.CreateOrganization(context.Background(), &domain.Organization{OrgID: "org-1", Name: "Test Corp"})
	s.CreateProject(context.Background(), &domain.Project{ProjectID: "proj-1", OrgID: "org-1", Name: "P1"})
	s.CreateProject(context.Background(), &domain.Project{ProjectID: "proj-2", OrgID: "org-1", Name: "P2"})

	s.CreateAPIKey(context.Background(), &domain.APIKey{KeyID: "k1", ProjectID: "proj-1", Kind: domain.APIKeyKindProject, HashedKey: "h1"})
	s.CreateAPIKey(context.Background(), &domain.APIKey{KeyID: "k2", ProjectID: "proj-1", Kind: domain.APIKeyKindService, ServiceName: "svc", HashedKey: "h2"})
	s.CreateAPIKey(context.Background(), &domain.APIKey{KeyID: "k3", ProjectID: "proj-2", Kind: domain.APIKeyKindProject, HashedKey: "h3"})

	keys, err := s.ListAPIKeys(context.Background(), "proj-1")
	if err != nil {
		t.Fatalf("list api keys: %v", err)
	}
	if len(keys) != 2 {
		t.Fatalf("expected 2 keys for proj-1, got %d", len(keys))
	}
}

// ---- Prompt Versions ----

func TestPrompt_CreateAndGet(t *testing.T) {
	s, _ := openTestStore(t)
	defer s.Close()

	pv := &domain.PromptVersion{
		VersionID:   "pv-1",
		ProjectID:   "proj-1",
		Name:        "greeting",
		Version:     1,
		Template:    "Hello {{name}}, welcome!",
		ModelConfig: domain.PromptModelConfig{Model: "gpt-4", Temperature: 0.7, MaxTokens: 256},
	}
	if err := s.CreatePromptVersion(context.Background(), pv); err != nil {
		t.Fatalf("create prompt version: %v", err)
	}

	got, err := s.GetPromptVersion(context.Background(), "proj-1", "greeting", 1)
	if err != nil {
		t.Fatalf("get prompt version: %v", err)
	}
	if got.Template != "Hello {{name}}, welcome!" || got.Version != 1 {
		t.Errorf("got %v", got)
	}
}

func TestPrompt_GetByLabel(t *testing.T) {
	s, _ := openTestStore(t)
	defer s.Close()

	s.CreatePromptVersion(context.Background(), &domain.PromptVersion{
		VersionID:   "pv-1",
		ProjectID:   "proj-1",
		Name:        "greeting",
		Version:     1,
		Template:    "v1",
		ModelConfig: domain.PromptModelConfig{Model: "gpt-3.5"},
	})
	s.CreatePromptVersion(context.Background(), &domain.PromptVersion{
		VersionID:   "pv-2",
		ProjectID:   "proj-1",
		Name:        "greeting",
		Version:     2,
		Template:    "v2",
		ModelConfig: domain.PromptModelConfig{Model: "gpt-4"},
	})

	s.SetPromptLabel(context.Background(), &domain.PromptLabel{
		ProjectID: "proj-1", Name: "greeting", Label: "production", Version: 2,
	})

	got, err := s.GetPromptByLabel(context.Background(), "proj-1", "greeting", "production")
	if err != nil {
		t.Fatalf("get prompt by label: %v", err)
	}
	if got.Version != 2 || got.Template != "v2" {
		t.Errorf("got version %d template %q, want version 2 template \"v2\"", got.Version, got.Template)
	}
}

func TestPrompt_ListVersions(t *testing.T) {
	s, _ := openTestStore(t)
	defer s.Close()

	for i := int64(1); i <= 3; i++ {
		s.CreatePromptVersion(context.Background(), &domain.PromptVersion{
			VersionID:   "pv-" + string(rune('0'+i)),
			ProjectID:   "proj-1",
			Name:        "greeting",
			Version:     i,
			Template:    "v" + string(rune('0'+i)),
			ModelConfig: domain.PromptModelConfig{Model: "gpt-4"},
		})
	}

	versions, err := s.ListPromptVersions(context.Background(), "proj-1", "greeting")
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
	s, _ := openTestStore(t)
	defer s.Close()

	s.CreatePromptVersion(context.Background(), &domain.PromptVersion{
		VersionID:   "pv-1",
		ProjectID:   "proj-1",
		Name:        "greeting",
		Version:     1,
		Template:    "v1",
		ModelConfig: domain.PromptModelConfig{},
	})
	s.CreatePromptVersion(context.Background(), &domain.PromptVersion{
		VersionID:   "pv-2",
		ProjectID:   "proj-1",
		Name:        "greeting",
		Version:     2,
		Template:    "v2",
		ModelConfig: domain.PromptModelConfig{},
	})

	// Set initial label
	s.SetPromptLabel(context.Background(), &domain.PromptLabel{
		ProjectID: "proj-1", Name: "greeting", Label: "production", Version: 1,
	})

	// Update label to point to v2
	s.SetPromptLabel(context.Background(), &domain.PromptLabel{
		ProjectID: "proj-1", Name: "greeting", Label: "production", Version: 2,
	})

	got, err := s.GetPromptByLabel(context.Background(), "proj-1", "greeting", "production")
	if err != nil {
		t.Fatalf("get prompt by label after upsert: %v", err)
	}
	if got.Version != 2 {
		t.Errorf("expected version 2 after upsert, got %d", got.Version)
	}
}

// ---- Eval Rules ----

func TestEvalRule_CreateAndGet(t *testing.T) {
	s, _ := openTestStore(t)
	defer s.Close()

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
	}
	if err := s.CreateEvalRule(context.Background(), rule); err != nil {
		t.Fatalf("create eval rule: %v", err)
	}

	got, err := s.GetEvalRule(context.Background(), "rule-1")
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
	s, _ := openTestStore(t)
	defer s.Close()

	s.CreateEvalRule(context.Background(), &domain.EvalRule{RuleID: "r1", ProjectID: "proj-1", Name: "rule-1", JudgeModel: "gpt-4", PromptName: "p", PromptVersion: 1, Enabled: true})
	s.CreateEvalRule(context.Background(), &domain.EvalRule{RuleID: "r2", ProjectID: "proj-1", Name: "rule-2", JudgeModel: "gpt-3.5", PromptName: "p", PromptVersion: 1, Enabled: false})
	s.CreateEvalRule(context.Background(), &domain.EvalRule{RuleID: "r3", ProjectID: "proj-2", Name: "rule-3", JudgeModel: "gpt-4", PromptName: "p", PromptVersion: 1, Enabled: true})

	rules, err := s.ListEvalRules(context.Background(), "proj-1")
	if err != nil {
		t.Fatalf("list eval rules: %v", err)
	}
	if len(rules) != 2 {
		t.Fatalf("expected 2 rules, got %d", len(rules))
	}
}

func TestEvalRule_Update(t *testing.T) {
	s, _ := openTestStore(t)
	defer s.Close()

	s.CreateEvalRule(context.Background(), &domain.EvalRule{RuleID: "r1", ProjectID: "proj-1", Name: "original", JudgeModel: "gpt-3.5", PromptName: "p", PromptVersion: 1, Enabled: true})

	s.UpdateEvalRule(context.Background(), &domain.EvalRule{RuleID: "r1", ProjectID: "proj-1", Name: "updated", JudgeModel: "gpt-4", PromptName: "p", PromptVersion: 2, Enabled: false})

	got, err := s.GetEvalRule(context.Background(), "r1")
	if err != nil {
		t.Fatalf("get updated rule: %v", err)
	}
	if got.Name != "updated" || got.Enabled != false || got.PromptVersion != 2 {
		t.Errorf("got %v", got)
	}
}

// ---- Datasets ----

func TestDataset_CreateAndGet(t *testing.T) {
	s, _ := openTestStore(t)
	defer s.Close()

	ds := &domain.Dataset{DatasetID: "ds-1", ProjectID: "proj-1", Name: "Eval Set"}
	if err := s.CreateDataset(context.Background(), ds); err != nil {
		t.Fatalf("create dataset: %v", err)
	}

	got, err := s.GetDataset(context.Background(), "ds-1")
	if err != nil {
		t.Fatalf("get dataset: %v", err)
	}
	if got.Name != "Eval Set" {
		t.Errorf("got %v", got)
	}
}

func TestDatasetItems_CreateAndList(t *testing.T) {
	s, _ := openTestStore(t)
	defer s.Close()

	s.CreateDataset(context.Background(), &domain.Dataset{DatasetID: "ds-1", ProjectID: "proj-1", Name: "Set"})

	s.CreateDatasetItem(context.Background(), &domain.DatasetItem{ItemID: "item-1", DatasetID: "ds-1", Input: "hello", ExpectedOutput: "hi"})
	s.CreateDatasetItem(context.Background(), &domain.DatasetItem{ItemID: "item-2", DatasetID: "ds-1", Input: "bye", ExpectedOutput: "goodbye"})

	items, err := s.ListDatasetItems(context.Background(), "ds-1")
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
	s, _ := openTestStore(t)
	defer s.Close()

	s.CreateDataset(context.Background(), &domain.Dataset{DatasetID: "ds-1", ProjectID: "proj-1", Name: "Set"})

	run := &domain.DatasetRun{RunID: "run-1", DatasetID: "ds-1", EvalRuleID: "rule-1", PromptVersion: 1}
	if err := s.CreateDatasetRun(context.Background(), run); err != nil {
		t.Fatalf("create dataset run: %v", err)
	}

	got, err := s.GetDatasetRun(context.Background(), "run-1")
	if err != nil {
		t.Fatalf("get dataset run: %v", err)
	}
	if got.EvalRuleID != "rule-1" || got.PromptVersion != 1 {
		t.Errorf("got %v", got)
	}
}

func TestClose_NoError(t *testing.T) {
	s, _ := openTestStore(t)
	if err := s.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
}
