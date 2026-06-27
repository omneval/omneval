package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
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

// BookmarkStore returns the focused BookmarkStore interface for callers that only
// need bookmark operations.
func (f *FakeMetadataStore) BookmarkStore() metadata.BookmarkStore { return f }

// ProjectStore returns a focused ProjectStore interface for callers that only
// need project operations.
func (f *FakeMetadataStore) ProjectStore() metadata.ProjectStore { return f }

// PromptStore returns a focused PromptStore interface for callers that only
// need prompt operations.
func (f *FakeMetadataStore) PromptStore() metadata.PromptStore { return f }

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
func (f *FakeMetadataStore) CreateDatasetItemsBatch(_ context.Context, _ []*domain.DatasetItem) error {
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
func (f *FakeMetadataStore) Migrate(_ context.Context) error { return nil }
func (f *FakeMetadataStore) MarkBatchCommitted(ctx context.Context, batchID string, committedAt time.Time) error {
	return nil
}
func (f *FakeMetadataStore) IsBatchCommitted(ctx context.Context, batchID string) (bool, error) {
	return false, nil
}

func (f *FakeMetadataStore) Close() error                           { return nil }
func (f *FakeMetadataStore) CheckPassword(_ string, _ string) error { return nil }

// EvalRuleStore returns the focused EvalRuleStore interface.
func (f *FakeMetadataStore) EvalRuleStore() metadata.EvalRuleStore { return f }

// SessionStore returns the focused SessionStore interface.
func (f *FakeMetadataStore) SessionStore() metadata.SessionStore { return f }

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
func (f *FakeMetadataStore) RemoveBookmarksForProject(_ context.Context, _ string) error {
	return nil
}
func (f *FakeMetadataStore) IsBookmarked(_ context.Context, _, _ string) (bool, error) {
	return false, nil
}
func (f *FakeMetadataStore) ListBookmarkedTraceIDs(_ context.Context, _ string) ([]string, error) {
	return nil, nil
}

// ---- Context Keys Tests ----

func TestContextKeys_NotNil(t *testing.T) {
	if ProjectIDContextKey == nil {
		t.Fatal("ProjectIDContextKey must not be nil")
	}
	if UserIDContextKey == nil {
		t.Fatal("UserIDContextKey must not be nil")
	}
	if EmailContextKey == nil {
		t.Fatal("EmailContextKey must not be nil")
	}
	if AdminEmailContextKey == nil {
		t.Fatal("AdminEmailContextKey must not be nil")
	}
}

func TestContextKeys_Uniqueness(t *testing.T) {
	keys := []any{ProjectIDContextKey, UserIDContextKey, EmailContextKey, AdminEmailContextKey}
	for i := 0; i < len(keys); i++ {
		for j := i + 1; j < len(keys); j++ {
			if keys[i] == keys[j] {
				t.Errorf("context keys %d and %d are the same pointer: %v", i, j, keys[i])
			}
		}
	}
}

func TestContextKeys_StringValues(t *testing.T) {
	if ProjectIDContextKey != "omneval_project_id" {
		t.Errorf("ProjectIDContextKey: got %q, want %q", ProjectIDContextKey, "omneval_project_id")
	}
	if UserIDContextKey != "omneval_user_id" {
		t.Errorf("UserIDContextKey: got %q, want %q", UserIDContextKey, "omneval_user_id")
	}
	if EmailContextKey != "omneval_email" {
		t.Errorf("EmailContextKey: got %q, want %q", EmailContextKey, "omneval_email")
	}
	if AdminEmailContextKey != "omneval_admin_email" {
		t.Errorf("AdminEmailContextKey: got %q, want %q", AdminEmailContextKey, "omneval_admin_email")
	}
	if APIKeyProjectIDContextKey != "omneval_api_key_project_id" {
		t.Errorf("APIKeyProjectIDContextKey: got %q, want %q", APIKeyProjectIDContextKey, "omneval_api_key_project_id")
	}
}

// ---- ResolveProjectID Tests ----

// fakeResolver is a test double that always returns a fixed project ID.
type fakeResolver struct {
	id string
	ok bool
}

func (f *fakeResolver) ProjectID(_ *http.Request) (string, bool) {
	return f.id, f.ok
}

func TestResolveProjectID_APIKeyContext(t *testing.T) {
	ctx := context.WithValue(context.Background(), APIKeyProjectIDContextKey, "apikey-proj")
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r = r.WithContext(ctx)
	resolver := &fakeResolver{id: "other-proj", ok: true}
	proj, ok := ResolveProjectID(r, resolver, "")
	if !ok {
		t.Fatal("expected ok")
	}
	if proj != "apikey-proj" {
		t.Errorf("got %q, want %q", proj, "apikey-proj")
	}
}

func TestResolveProjectID_DelegatesToResolver(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	resolver := &fakeResolver{id: "resolver-proj", ok: true}
	proj, ok := ResolveProjectID(r, resolver, "")
	if !ok {
		t.Fatal("expected ok")
	}
	if proj != "resolver-proj" {
		t.Errorf("got %q, want %q", proj, "resolver-proj")
	}
}

func TestResolveProjectID_ExplicitFallback(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	resolver := &fakeResolver{id: "", ok: false}
	proj, ok := ResolveProjectID(r, resolver, "explicit-proj")
	if !ok {
		t.Fatal("expected ok")
	}
	if proj != "explicit-proj" {
		t.Errorf("got %q, want %q", proj, "explicit-proj")
	}
}

func TestResolveProjectID_QueryParamFallback(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/?project_id=query-proj", nil)
	resolver := &fakeResolver{id: "", ok: false}
	proj, ok := ResolveProjectID(r, resolver, "")
	if !ok {
		t.Fatal("expected ok")
	}
	if proj != "query-proj" {
		t.Errorf("got %q, want %q", proj, "query-proj")
	}
}

func TestResolveProjectID_APIKeyWinsOverExplicit(t *testing.T) {
	ctx := context.WithValue(context.Background(), APIKeyProjectIDContextKey, "apikey-proj")
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r = r.WithContext(ctx)
	resolver := &fakeResolver{id: "resolver-proj", ok: true}
	proj, ok := ResolveProjectID(r, resolver, "explicit-proj")
	if !ok {
		t.Fatal("expected ok")
	}
	if proj != "apikey-proj" {
		t.Errorf("got %q, want %q", proj, "apikey-proj")
	}
}

func TestResolveProjectID_EmptyContextValueIgnoresAPIKey(t *testing.T) {
	ctx := context.WithValue(context.Background(), APIKeyProjectIDContextKey, "")
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r = r.WithContext(ctx)
	resolver := &fakeResolver{id: "resolver-proj", ok: true}
	proj, ok := ResolveProjectID(r, resolver, "")
	if !ok {
		t.Fatal("expected ok")
	}
	if proj != "resolver-proj" {
		t.Errorf("got %q, want %q", proj, "resolver-proj")
	}
}

func TestResolveProjectID_NoFallback(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	resolver := &fakeResolver{id: "", ok: false}
	_, ok := ResolveProjectID(r, resolver, "")
	if ok {
		t.Error("expected not ok when nothing resolves")
	}
}

// ---- fakeKeyValidator ---

// fakeKeyValidator is a test double for auth.Validator.
type fakeKeyValidator struct {
	projectID string
	valid     bool
}

func (v *fakeKeyValidator) Validate(_ context.Context, _ string) (*ValidatedKey, error) {
	if !v.valid {
		return nil, fmt.Errorf("invalid key")
	}
	return &ValidatedKey{ProjectID: v.projectID}, nil
}

// ---- Middleware Factory Tests ----

func TestRequireSessionOrAPIKey_APIKeyValid(t *testing.T) {
	validator := &fakeKeyValidator{projectID: "apikey-proj", valid: true}
	factory := RequireSessionOrAPIKey(nil, validator, false, time.Minute, APIKeyProjectIDContextKey)
	handler := factory(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pid, ok := r.Context().Value(APIKeyProjectIDContextKey).(string)
		if !ok || pid != "apikey-proj" {
			t.Errorf("expected api-key project ID in context, got ok=%v pid=%q", ok, pid)
		}
	}))
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("X-API-Key", "abc123")
	handler.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestRequireSessionOrAPIKey_APIKeyInvalid(t *testing.T) {
	validator := &fakeKeyValidator{projectID: "", valid: false}
	factory := RequireSessionOrAPIKey(nil, validator, false, time.Minute, APIKeyProjectIDContextKey)
	handler := factory(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be called for invalid key")
	}))
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("X-API-Key", "bad")
	handler.ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestRequireAuth_NoAuth(t *testing.T) {
	validator := &fakeKeyValidator{projectID: "", valid: false}
	factory := RequireAuth(nil, validator, false, time.Minute)
	handler := factory(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be called when not authenticated")
	}))
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	handler.ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestRequireAdmin_NoAuth(t *testing.T) {
	factory := RequireAdmin(nil, false, time.Minute, "admin@example.com")
	handler := factory(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be called when not authenticated")
	}))
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	handler.ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

// ---- SessionStoreResolver Tests ----

func TestSessionStoreResolver_ProjectID(t *testing.T) {
	provider := &fakeSessionProvider{id: "resolved-proj", ok: true}
	resolver := NewSessionStoreResolver(provider)
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	proj, ok := resolver.ProjectID(r)
	if !ok {
		t.Fatal("expected ok")
	}
	if proj != "resolved-proj" {
		t.Errorf("got %q, want %q", proj, "resolved-proj")
	}
}

func TestSessionStoreResolver_EmptyProjectID(t *testing.T) {
	provider := &fakeSessionProvider{id: "", ok: false}
	resolver := NewSessionStoreResolver(provider)
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	_, ok := resolver.ProjectID(r)
	if ok {
		t.Error("expected not ok for empty project ID")
	}
}

func TestResolveProjectID_SessionStoreResolver(t *testing.T) {
	provider := &fakeSessionProvider{id: "sess-proj", ok: true}
	resolver := NewSessionStoreResolver(provider)
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	proj, ok := ResolveProjectID(r, resolver, "")
	if !ok {
		t.Fatal("expected ok")
	}
	if proj != "sess-proj" {
		t.Errorf("got %q, want %q", proj, "sess-proj")
	}
}

// fakeSessionProvider implements the ProjectIDProvider interface.
type fakeSessionProvider struct {
	id string
	ok bool
}

func (f *fakeSessionProvider) ProjectID(_ *http.Request) (string, bool) {
	return f.id, f.ok
}


