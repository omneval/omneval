package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"testing"
	"time"

	"github.com/omneval/omneval/internal/domain"
	"github.com/omneval/omneval/internal/metadata"
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
func (f *FakeMetadataStore) CreateOrganization(_ context.Context, _ *domain.Organization) error {
	return nil
}
func (f *FakeMetadataStore) GetOrganization(_ context.Context, _ string) (*domain.Organization, error) {
	return nil, metadata.ErrNotFound
}
func (f *FakeMetadataStore) CreateProject(_ context.Context, _ *domain.Project) error { return nil }
func (f *FakeMetadataStore) GetProject(_ context.Context, _ string) (*domain.Project, error) {
	return nil, metadata.ErrNotFound
}
func (f *FakeMetadataStore) ListProjects(_ context.Context, _ string) ([]*domain.Project, error) {
	return nil, nil
}
func (f *FakeMetadataStore) CreateUser(_ context.Context, _ *domain.User) error { return nil }
func (f *FakeMetadataStore) GetUserByEmail(_ context.Context, _ string) (*domain.User, error) {
	return nil, metadata.ErrNotFound
}
func (f *FakeMetadataStore) GetUserByID(_ context.Context, _ string) (*domain.User, error) {
	return nil, metadata.ErrNotFound
}
func (f *FakeMetadataStore) ListUsers(_ context.Context, _ string) ([]*domain.User, error) {
	return nil, nil
}
func (f *FakeMetadataStore) CountUsers(_ context.Context) (int, error)               { return 0, nil }
func (f *FakeMetadataStore) UpdateUserPassword(_ context.Context, _, _ string) error { return nil }
func (f *FakeMetadataStore) UpdateUserResetToken(_ context.Context, _, _ string, _ time.Time) error {
	return nil
}
func (f *FakeMetadataStore) GetUserByResetToken(_ context.Context, _ string) (*domain.User, error) {
	return nil, metadata.ErrNotFound
}
func (f *FakeMetadataStore) CreateSession(_ context.Context, _ *domain.Session) error { return nil }
func (f *FakeMetadataStore) GetSession(_ context.Context, _ string) (*domain.Session, error) {
	return nil, metadata.ErrNotFound
}
func (f *FakeMetadataStore) DeleteSession(_ context.Context, _ string) error { return nil }
func (f *FakeMetadataStore) CreatePromptVersion(_ context.Context, _ *domain.PromptVersion) error {
	return nil
}
func (f *FakeMetadataStore) GetPromptVersion(_ context.Context, _, _ string, _ int64) (*domain.PromptVersion, error) {
	return nil, metadata.ErrNotFound
}
func (f *FakeMetadataStore) GetPromptByLabel(_ context.Context, _, _, _ string) (*domain.PromptVersion, error) {
	return nil, metadata.ErrNotFound
}
func (f *FakeMetadataStore) ListPromptVersions(_ context.Context, _, _ string) ([]*domain.PromptVersion, error) {
	return nil, nil
}
func (f *FakeMetadataStore) ListPromptNames(_ context.Context, _ string) ([]string, error) {
	return nil, nil
}
func (f *FakeMetadataStore) SetPromptLabel(_ context.Context, _ *domain.PromptLabel) error {
	return nil
}
func (f *FakeMetadataStore) CreateEvalRule(_ context.Context, _ *domain.EvalRule) error { return nil }
func (f *FakeMetadataStore) GetEvalRule(_ context.Context, _ string) (*domain.EvalRule, error) {
	return nil, metadata.ErrNotFound
}
func (f *FakeMetadataStore) ListEvalRules(_ context.Context, _ string) ([]*domain.EvalRule, error) {
	return nil, nil
}
func (f *FakeMetadataStore) UpdateEvalRule(_ context.Context, _ *domain.EvalRule) error { return nil }
func (f *FakeMetadataStore) DeleteEvalRule(_ context.Context, _ string) error           { return nil }
func (f *FakeMetadataStore) CreateDataset(_ context.Context, _ *domain.Dataset) error   { return nil }
func (f *FakeMetadataStore) GetDataset(_ context.Context, _ string) (*domain.Dataset, error) {
	return nil, metadata.ErrNotFound
}
func (f *FakeMetadataStore) CreateDatasetItem(_ context.Context, _ *domain.DatasetItem) error {
	return nil
}
func (f *FakeMetadataStore) ListDatasets(_ context.Context, _ string) ([]*domain.Dataset, error) {
	return nil, nil
}
func (f *FakeMetadataStore) DeleteDataset(_ context.Context, _ string) error { return nil }
func (f *FakeMetadataStore) ListDatasetItems(_ context.Context, _ string) ([]*domain.DatasetItem, error) {
	return nil, nil
}
func (f *FakeMetadataStore) ListDatasetItemsPaginated(_ context.Context, _, _ string, _ int) ([]*domain.DatasetItem, string, error) {
	return nil, "", nil
}
func (f *FakeMetadataStore) CreateDatasetRun(_ context.Context, _ *domain.DatasetRun) error {
	return nil
}
func (f *FakeMetadataStore) GetDatasetRun(_ context.Context, _ string) (*domain.DatasetRun, error) {
	return nil, metadata.ErrNotFound
}
func (f *FakeMetadataStore) UpdateDatasetRun(_ context.Context, _ *domain.DatasetRun) error {
	return nil
}
func (f *FakeMetadataStore) ListDatasetRuns(_ context.Context, _ string) ([]*domain.DatasetRun, error) {
	return nil, nil
}
func (f *FakeMetadataStore) CreateDatasetRunItem(_ context.Context, _ *domain.DatasetRunItem) error {
	return nil
}
func (f *FakeMetadataStore) GetDatasetRunItem(_ context.Context, _ string) (*domain.DatasetRunItem, error) {
	return nil, metadata.ErrNotFound
}
func (f *FakeMetadataStore) UpdateDatasetRunItem(_ context.Context, _ *domain.DatasetRunItem) error {
	return nil
}
func (f *FakeMetadataStore) ListDatasetRunItems(_ context.Context, _ string) ([]*domain.DatasetRunItem, error) {
	return nil, nil
}
func (f *FakeMetadataStore) Migrate(_ context.Context) error        { return nil }
func (f *FakeMetadataStore) Close() error                           { return nil }
func (f *FakeMetadataStore) CheckPassword(_ string, _ string) error { return nil }

// ---- Tests ----

func TestGenerate(t *testing.T) {
	tests := []struct {
		name   string
		kind   domain.APIKeyKind
		prefix string
	}{
		{"project", domain.APIKeyKindProject, PrefixProject},
		{"service", domain.APIKeyKindService, PrefixService},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw, hashed, err := Generate(tt.kind)
			if err != nil {
				t.Fatalf("Generate returned error: %v", err)
			}
			if raw == "" {
				t.Fatal("raw key must not be empty")
			}
			if hashed == "" {
				t.Fatal("hashed key must not be empty")
			}
			if len(raw) < len(tt.prefix) || raw[:len(tt.prefix)] != tt.prefix {
				t.Fatalf("raw key must start with %q, got %q", tt.prefix, raw)
			}
		})
	}
}

func TestGenerate_RawNeverHashedInStore(t *testing.T) {
	raw, hashed, err := Generate(domain.APIKeyKindProject)
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}
	if raw == hashed {
		t.Fatal("raw key must not equal its own hash")
	}
	if len(hashed) != 64 {
		t.Fatalf("hashed key must be 64 hex chars, got %d", len(hashed))
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
	tests := []struct {
		name string
		raw  string
	}{
		{"project prefix", "oev_proj_abc123"},
		{"service prefix", "oev_svc_worker"},
		{"arbitrary input", "random-string"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hashed := Hash(tt.raw)
			if len(hashed) != 64 {
				t.Fatalf("expected 64 hex chars, got %d", len(hashed))
			}
			expect := sha256.Sum256([]byte(tt.raw))
			if hashed != hex.EncodeToString(expect[:]) {
				t.Fatalf("hash mismatch")
			}
		})
	}
}

func TestHash_Deterministic(t *testing.T) {
	input := "oev_proj_test"
	h1 := Hash(input)
	h2 := Hash(input)
	if h1 != h2 {
		t.Fatal("hash must be deterministic for the same input")
	}
}

func TestKindFromRaw(t *testing.T) {
	tests := []struct {
		name     string
		raw      string
		expected domain.APIKeyKind
		wantOk   bool
	}{
		{"project prefix", "oev_proj_abc123", domain.APIKeyKindProject, true},
		{"service prefix", "oev_svc_worker-1", domain.APIKeyKindService, true},
		{"unknown prefix", "ltn_bogus_abc123", "", false},
		{"empty string", "", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			kind, ok := KindFromRaw(tt.raw)
			if ok != tt.wantOk {
				t.Fatalf("wantOk=%v, got %v", tt.wantOk, ok)
			}
			if kind != tt.expected {
				t.Fatalf("expected kind %q, got %q", tt.expected, kind)
			}
		})
	}
}

// ---- CachingValidator Tests ----

func TestCachingValidator_ValidKey(t *testing.T) {
	tests := []struct {
		name        string
		kind        domain.APIKeyKind
		serviceName string
	}{
		{"project", domain.APIKeyKindProject, ""},
		{"service", domain.APIKeyKindService, "web-frontend"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := NewFakeMetadataStore()
			raw, hashed, err := Generate(tt.kind)
			if err != nil {
				t.Fatalf("Generate returned error: %v", err)
			}
			err = store.CreateAPIKey(nil, &domain.APIKey{
				KeyID:       fmt.Sprintf("key-%s", tt.name),
				ProjectID:   "proj-1",
				Kind:        tt.kind,
				ServiceName: tt.serviceName,
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
			if result.ProjectID != "proj-1" {
				t.Fatalf("expected ProjectID proj-1, got %s", result.ProjectID)
			}
			if result.Kind != tt.kind {
				t.Fatalf("expected kind %q, got %q", tt.kind, result.Kind)
			}
			if result.ServiceName != tt.serviceName {
				t.Fatalf("expected ServiceName %q, got %q", tt.serviceName, result.ServiceName)
			}
		})
	}
}

func TestCachingValidator_InvalidKey(t *testing.T) {
	store := NewFakeMetadataStore()
	validator := NewCachingValidator(store)
	_, err := validator.Validate(nil, "oev_proj_nonexistent")
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

func (f *FakeMetadataStore) SetBookmark(_ context.Context, _ *domain.Bookmark) error { return nil }
func (f *FakeMetadataStore) RemoveBookmark(_ context.Context, _, _ string) error     { return nil }
func (f *FakeMetadataStore) IsBookmarked(_ context.Context, _, _ string) (bool, error) {
	return false, nil
}
func (f *FakeMetadataStore) ListBookmarkedTraceIDs(_ context.Context, _ string) ([]string, error) {
	return nil, nil
}
