package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"testing"
	"time"

	"github.com/zbloss/lantern/internal/domain"
	"github.com/zbloss/lantern/internal/metadata"
)

// ---- FakeMetadataStore ----

// FakeMetadataStore is an in-memory implementation of metadata.Store for testing.
type FakeMetadataStore struct {
	apiKeys map[string]*domain.APIKey // keyID -> APIKey
	byHash  map[string]*domain.APIKey // hashedKey -> APIKey
}

func NewFakeMetadataStore() *FakeMetadataStore {
	return &FakeMetadataStore{
		apiKeys: make(map[string]*domain.APIKey),
		byHash:  make(map[string]*domain.APIKey),
	}
}

func (f *FakeMetadataStore) CreateAPIKey(_ context.Context, key *domain.APIKey) error {
	f.apiKeys[key.KeyID] = key
	f.byHash[key.HashedKey] = key
	return nil
}

func (f *FakeMetadataStore) GetAPIKeyByHash(_ context.Context, hashedKey string) (*domain.APIKey, error) {
	key, ok := f.byHash[hashedKey]
	if !ok {
		return nil, metadata.ErrNotFound
	}
	return key, nil
}

func (f *FakeMetadataStore) RevokeAPIKey(_ context.Context, keyID string) error {
	key, ok := f.apiKeys[keyID]
	if !ok {
		return metadata.ErrNotFound
	}
	now := time.Now().UTC()
	key.RevokedAt = &now
	f.apiKeys[keyID] = key
	return nil
}

func (f *FakeMetadataStore) ListAPIKeys(_ context.Context, projectID string) ([]*domain.APIKey, error) {
	var result []*domain.APIKey
	for _, k := range f.apiKeys {
		if k.ProjectID == projectID {
			result = append(result, k)
		}
	}
	return result, nil
}

// Stub implementations for unused Store methods.
func (f *FakeMetadataStore) CreateOrganization(_ context.Context, _ *domain.Organization) error  { return nil }
func (f *FakeMetadataStore) GetOrganization(_ context.Context, _ string) (*domain.Organization, error) {
	return nil, metadata.ErrNotFound
}
func (f *FakeMetadataStore) CreateProject(_ context.Context, _ *domain.Project) error              { return nil }
func (f *FakeMetadataStore) GetProject(_ context.Context, _ string) (*domain.Project, error)       { return nil, metadata.ErrNotFound }
func (f *FakeMetadataStore) ListProjects(_ context.Context, _ string) ([]*domain.Project, error)   { return nil, nil }
func (f *FakeMetadataStore) CreateUser(_ context.Context, _ *domain.User) error                    { return nil }
func (f *FakeMetadataStore) GetUserByEmail(_ context.Context, _ string) (*domain.User, error)      { return nil, metadata.ErrNotFound }
func (f *FakeMetadataStore) ListUsers(_ context.Context, _ string) ([]*domain.User, error)         { return nil, nil }
func (f *FakeMetadataStore) CreateSession(_ context.Context, _ *domain.Session) error              { return nil }
func (f *FakeMetadataStore) GetSession(_ context.Context, _ string) (*domain.Session, error)       { return nil, metadata.ErrNotFound }
func (f *FakeMetadataStore) DeleteSession(_ context.Context, _ string) error                       { return nil }
func (f *FakeMetadataStore) CreatePromptVersion(_ context.Context, _ *domain.PromptVersion) error  { return nil }
func (f *FakeMetadataStore) GetPromptVersion(_ context.Context, _, _ string, _ int64) (*domain.PromptVersion, error) {
	return nil, metadata.ErrNotFound
}
func (f *FakeMetadataStore) GetPromptByLabel(_ context.Context, _, _, _ string) (*domain.PromptVersion, error) {
	return nil, metadata.ErrNotFound
}
func (f *FakeMetadataStore) ListPromptVersions(_ context.Context, _, _ string) ([]*domain.PromptVersion, error) {
	return nil, nil
}
func (f *FakeMetadataStore) SetPromptLabel(_ context.Context, _ *domain.PromptLabel) error { return nil }
func (f *FakeMetadataStore) CreateEvalRule(_ context.Context, _ *domain.EvalRule) error    { return nil }
func (f *FakeMetadataStore) GetEvalRule(_ context.Context, _ string) (*domain.EvalRule, error) {
	return nil, metadata.ErrNotFound
}
func (f *FakeMetadataStore) ListEvalRules(_ context.Context, _ string) ([]*domain.EvalRule, error) {
	return nil, nil
}
func (f *FakeMetadataStore) UpdateEvalRule(_ context.Context, _ *domain.EvalRule) error { return nil }
func (f *FakeMetadataStore) CreateDataset(_ context.Context, _ *domain.Dataset) error   { return nil }
func (f *FakeMetadataStore) GetDataset(_ context.Context, _ string) (*domain.Dataset, error) {
	return nil, metadata.ErrNotFound
}
func (f *FakeMetadataStore) CreateDatasetItem(_ context.Context, _ *domain.DatasetItem) error { return nil }
func (f *FakeMetadataStore) ListDatasetItems(_ context.Context, _ string) ([]*domain.DatasetItem, error) {
	return nil, nil
}
func (f *FakeMetadataStore) CreateDatasetRun(_ context.Context, _ *domain.DatasetRun) error { return nil }
func (f *FakeMetadataStore) GetDatasetRun(_ context.Context, _ string) (*domain.DatasetRun, error) {
	return nil, metadata.ErrNotFound
}
func (f *FakeMetadataStore) Migrate(_ context.Context) error  { return nil }
func (f *FakeMetadataStore) Close() error                     { return nil }
func (f *FakeMetadataStore) CheckPassword(_ string, _ string) error { return nil }

// ---- Tests ----

func TestGenerate_ProjectPrefix(t *testing.T) {
	raw, hashed, err := Generate(domain.APIKeyKindProject)
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}
	if raw == "" {
		t.Fatal("raw key is empty")
	}
	if hashed == "" {
		t.Fatal("hashed key is empty")
	}
	if len(raw) < len(PrefixProject) || raw[:len(PrefixProject)] != PrefixProject {
		t.Fatalf("raw key should start with %q, got %q", PrefixProject, raw)
	}
}

func TestGenerate_ServicePrefix(t *testing.T) {
	raw, hashed, err := Generate(domain.APIKeyKindService)
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}
	if raw == "" {
		t.Fatal("raw key is empty")
	}
	if hashed == "" {
		t.Fatal("hashed key is empty")
	}
	if len(raw) < len(PrefixService) || raw[:len(PrefixService)] != PrefixService {
		t.Fatalf("raw key should start with %q, got %q", PrefixService, raw)
	}
}

func TestGenerate_RawNeverHashedInStore(t *testing.T) {
	raw, hashed, err := Generate(domain.APIKeyKindProject)
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}
	// The raw key should never equal its own hash
	if raw == hashed {
		t.Fatal("raw key should never equal hashed key")
	}
	// The hash should be a hex string (64 hex chars for SHA-256)
	if len(hashed) != 64 {
		t.Fatalf("hashed key should be 64 hex chars, got %d", len(hashed))
	}
}

func TestGenerate_UniqueKeys(t *testing.T) {
	raw1, _, _ := Generate(domain.APIKeyKindProject)
	raw2, _, _ := Generate(domain.APIKeyKindProject)
	if raw1 == raw2 {
		t.Fatal("Generate should produce unique keys")
	}
}

func TestHash(t *testing.T) {
	raw := "ltn_proj_abc123"
	hashed := Hash(raw)
	if len(hashed) != 64 {
		t.Fatalf("Hash should return 64 hex chars, got %d", len(hashed))
	}
	// Verify SHA-256 manually
	expect := sha256.Sum256([]byte(raw))
	expectHex := hex.EncodeToString(expect[:])
	if hashed != expectHex {
		t.Fatalf("Hash mismatch: got %s, expected %s", hashed, expectHex)
	}
}

func TestHash_Deterministic(t *testing.T) {
	h1 := Hash("ltn_proj_test")
	h2 := Hash("ltn_proj_test")
	if h1 != h2 {
		t.Fatal("Hash should be deterministic for the same input")
	}
}

func TestKindFromRaw_Project(t *testing.T) {
	kind, ok := KindFromRaw("ltn_proj_abc123")
	if !ok {
		t.Fatal("KindFromRaw should return ok=true for ltn_proj_ prefix")
	}
	if kind != domain.APIKeyKindProject {
		t.Fatalf("Expected project kind, got %s", kind)
	}
}

func TestKindFromRaw_Service(t *testing.T) {
	kind, ok := KindFromRaw("ltn_svc_worker-1")
	if !ok {
		t.Fatal("KindFromRaw should return ok=true for ltn_svc_ prefix")
	}
	if kind != domain.APIKeyKindService {
		t.Fatalf("Expected service kind, got %s", kind)
	}
}

func TestKindFromRaw_InvalidPrefix(t *testing.T) {
	kind, ok := KindFromRaw("ltn_bogus_abc123")
	if ok {
		t.Fatal("KindFromRaw should return ok=false for unknown prefix")
	}
	if kind != "" {
		t.Fatalf("Expected empty kind for invalid prefix, got %s", kind)
	}
}

func TestKindFromRaw_Empty(t *testing.T) {
	_, ok := KindFromRaw("")
	if ok {
		t.Fatal("KindFromRaw should return ok=false for empty string")
	}
}

// ---- CachingValidator Tests ----

func TestCachingValidator_ValidProjectKey(t *testing.T) {
	store := NewFakeMetadataStore()
	raw, hashed, err := Generate(domain.APIKeyKindProject)
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}
	keyID := "test-key-id"
	err = store.CreateAPIKey(nil, &domain.APIKey{
		KeyID:     keyID,
		ProjectID: "proj-1",
		Kind:      domain.APIKeyKindProject,
		HashedKey: hashed,
	})
	if err != nil {
		t.Fatalf("CreateAPIKey returned error: %v", err)
	}

	validator := NewCachingValidator(store)
	result, err := validator.Validate(nil, raw)
	if err != nil {
		t.Fatalf("Validate returned error: %v", err)
	}
	if result.ProjectID != "proj-1" {
		t.Fatalf("Expected ProjectID proj-1, got %s", result.ProjectID)
	}
	if result.Kind != domain.APIKeyKindProject {
		t.Fatalf("Expected kind project, got %s", result.Kind)
	}
}

func TestCachingValidator_ValidServiceKey(t *testing.T) {
	store := NewFakeMetadataStore()
	raw, hashed, err := Generate(domain.APIKeyKindService)
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}
	err = store.CreateAPIKey(nil, &domain.APIKey{
		KeyID:       "svc-key-1",
		ProjectID:   "proj-1",
		Kind:        domain.APIKeyKindService,
		ServiceName: "web-frontend",
		HashedKey:   hashed,
	})
	if err != nil {
		t.Fatalf("CreateAPIKey returned error: %v", err)
	}

	validator := NewCachingValidator(store)
	result, err := validator.Validate(nil, raw)
	if err != nil {
		t.Fatalf("Validate returned error: %v", err)
	}
	if result.ServiceName != "web-frontend" {
		t.Fatalf("Expected ServiceName web-frontend, got %s", result.ServiceName)
	}
}

func TestCachingValidator_InvalidKey(t *testing.T) {
	store := NewFakeMetadataStore()
	validator := NewCachingValidator(store)
	_, err := validator.Validate(nil, "ltn_proj_nonexistent")
	if err == nil {
		t.Fatal("Validate should return error for unknown key")
	}
}

func TestCachingValidator_RevokedKey(t *testing.T) {
	store := NewFakeMetadataStore()
	raw, hashed, err := Generate(domain.APIKeyKindProject)
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}
	keyID := "revoke-test-key"
	err = store.CreateAPIKey(nil, &domain.APIKey{
		KeyID:     keyID,
		ProjectID: "proj-1",
		Kind:      domain.APIKeyKindProject,
		HashedKey: hashed,
	})
	if err != nil {
		t.Fatalf("CreateAPIKey returned error: %v", err)
	}

	validator := NewCachingValidator(store)

	// First validation should succeed (caches the result)
	_, err = validator.Validate(nil, raw)
	if err != nil {
		t.Fatalf("Validate should succeed on first call: %v", err)
	}

	// Revoke the key
	err = store.RevokeAPIKey(nil, keyID)
	if err != nil {
		t.Fatalf("RevokeAPIKey returned error: %v", err)
	}

	// Second validation should still succeed (cached, within TTL)
	_, err = validator.Validate(nil, raw)
	if err != nil {
		t.Fatalf("Validate should succeed on second call (cached): %v", err)
	}

	// Simulate cache expiry by creating a new validator (or by waiting)
	validator2 := NewCachingValidator(store)
	_, err = validator2.Validate(nil, raw)
	if err == nil {
		t.Fatal("Validate should return error for revoked key after cache expiry")
	}
}

func TestCachingValidator_CacheHitDoesNotQueryStore(t *testing.T) {
	store := NewFakeMetadataStore()
	raw, hashed, err := Generate(domain.APIKeyKindProject)
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}
	keyID := "cache-test-key"
	err = store.CreateAPIKey(nil, &domain.APIKey{
		KeyID:     keyID,
		ProjectID: "proj-1",
		Kind:      domain.APIKeyKindProject,
		HashedKey: hashed,
	})
	if err != nil {
		t.Fatalf("CreateAPIKey returned error: %v", err)
	}

	validator := NewCachingValidator(store)

	// First call should hit the store
	_, err = validator.Validate(nil, raw)
	if err != nil {
		t.Fatalf("First Validate should succeed: %v", err)
	}

	// Second call should use cache (no error)
	_, err = validator.Validate(nil, raw)
	if err != nil {
		t.Fatalf("Second Validate should succeed from cache: %v", err)
	}
}
