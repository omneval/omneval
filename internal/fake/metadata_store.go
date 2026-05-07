package fake

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/zbloss/lantern/internal/domain"
	"github.com/zbloss/lantern/internal/metadata"
	"golang.org/x/crypto/bcrypt"
)

// FakeMetadataStore is an in-memory implementation of metadata.Store suitable
// for unit tests. It tracks operations and can be inspected by test code.
type FakeMetadataStore struct {
	mu             sync.RWMutex
	organizations  map[string]*domain.Organization
	projects       map[string]*domain.Project
	users          map[string]*domain.User
	emailsToIDs    map[string]string // email -> user_id
	sessions       map[string]*domain.Session
	apiKeys        map[string]*domain.APIKey
	hashedKeys     map[string]*domain.APIKey
	promptVersions map[string]*domain.PromptVersion // key: projectID:name:version
	promptLabels   map[string]*domain.PromptLabel   // key: projectID:name:label
	evalRules      map[string]*domain.EvalRule
	datasets       map[string]*domain.Dataset
	datasetItems   map[string]*domain.DatasetItem
	datasetRuns    map[string]*domain.DatasetRun

	// Counters for testing
	CreateUserCalls      int
	UpdatePasswordCalls  int
	CountUsersCalls      int
	CreateSessionCalls   int
	DeleteSessionCalls   int
}

func NewFakeMetadataStore() *FakeMetadataStore {
	return &FakeMetadataStore{
		organizations:  make(map[string]*domain.Organization),
		projects:       make(map[string]*domain.Project),
		users:          make(map[string]*domain.User),
		emailsToIDs:    make(map[string]string),
		sessions:       make(map[string]*domain.Session),
		apiKeys:        make(map[string]*domain.APIKey),
		hashedKeys:     make(map[string]*domain.APIKey),
		promptVersions: make(map[string]*domain.PromptVersion),
		promptLabels:   make(map[string]*domain.PromptLabel),
		evalRules:      make(map[string]*domain.EvalRule),
		datasets:       make(map[string]*domain.Dataset),
		datasetItems:   make(map[string]*domain.DatasetItem),
		datasetRuns:    make(map[string]*domain.DatasetRun),
	}
}

func (f *FakeMetadataStore) Close() error                                         { return nil }
func (f *FakeMetadataStore) Migrate(ctx context.Context) error                   { return nil }

func (f *FakeMetadataStore) CreateOrganization(ctx context.Context, o *domain.Organization) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.organizations[o.OrgID] = o
	return nil
}

func (f *FakeMetadataStore) GetOrganization(ctx context.Context, id string) (*domain.Organization, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	o, ok := f.organizations[id]
	if !ok {
		return nil, metadata.ErrNotFound
	}
	return o, nil
}

func (f *FakeMetadataStore) CreateProject(ctx context.Context, p *domain.Project) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.projects[p.ProjectID] = p
	return nil
}

func (f *FakeMetadataStore) GetProject(ctx context.Context, id string) (*domain.Project, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	p, ok := f.projects[id]
	if !ok {
		return nil, metadata.ErrNotFound
	}
	return p, nil
}

func (f *FakeMetadataStore) ListProjects(ctx context.Context, orgID string) ([]*domain.Project, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	var projects []*domain.Project
	for _, p := range f.projects {
		if p.OrgID == orgID {
			projects = append(projects, p)
		}
	}
	return projects, nil
}

func (f *FakeMetadataStore) CreateUser(ctx context.Context, u *domain.User) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	// Check for duplicate email
	if _, exists := f.emailsToIDs[u.Email]; exists {
		return fmt.Errorf("duplicate email: %s", u.Email)
	}

	// Hash password if it looks like plaintext (not a bcrypt hash)
	hash, err := bcrypt.GenerateFromPassword([]byte(u.PasswordHash), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	u.PasswordHash = string(hash)

	f.users[u.UserID] = u
	f.emailsToIDs[u.Email] = u.UserID
	return nil
}

func (f *FakeMetadataStore) GetUserByEmail(ctx context.Context, email string) (*domain.User, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	userID, ok := f.emailsToIDs[email]
	if !ok {
		return nil, metadata.ErrNotFound
	}
	u, ok := f.users[userID]
	if !ok {
		return nil, metadata.ErrNotFound
	}
	return u, nil
}

func (f *FakeMetadataStore) GetUserByID(ctx context.Context, userID string) (*domain.User, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	u, ok := f.users[userID]
	if !ok {
		return nil, metadata.ErrNotFound
	}
	return u, nil
}

func (f *FakeMetadataStore) ListUsers(ctx context.Context, orgID string) ([]*domain.User, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	var users []*domain.User
	for _, u := range f.users {
		if u.OrgID == orgID {
			users = append(users, u)
		}
	}
	return users, nil
}

func (f *FakeMetadataStore) CountUsers(ctx context.Context) (int, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	f.CountUsersCalls++
	return len(f.users), nil
}

func (f *FakeMetadataStore) UpdateUserPassword(ctx context.Context, userID, passwordHash string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.UpdatePasswordCalls++
	u, ok := f.users[userID]
	if !ok {
		return metadata.ErrNotFound
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(passwordHash), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	u.PasswordHash = string(hash)
	return nil
}

func (f *FakeMetadataStore) CreateSession(ctx context.Context, s *domain.Session) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sessions[s.SessionID] = s
	f.CreateSessionCalls++
	return nil
}

func (f *FakeMetadataStore) GetSession(ctx context.Context, id string) (*domain.Session, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	s, ok := f.sessions[id]
	if !ok {
		return nil, metadata.ErrNotFound
	}
	return s, nil
}

func (f *FakeMetadataStore) DeleteSession(ctx context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.sessions, id)
	f.DeleteSessionCalls++
	return nil
}

func (f *FakeMetadataStore) CreateAPIKey(ctx context.Context, k *domain.APIKey) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.apiKeys[k.KeyID] = k
	f.hashedKeys[k.HashedKey] = k
	return nil
}

func (f *FakeMetadataStore) GetAPIKeyByHash(ctx context.Context, hash string) (*domain.APIKey, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	k, ok := f.hashedKeys[hash]
	if !ok {
		return nil, metadata.ErrNotFound
	}
	return k, nil
}

func (f *FakeMetadataStore) RevokeAPIKey(ctx context.Context, keyID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	k, ok := f.apiKeys[keyID]
	if !ok {
		return metadata.ErrNotFound
	}
	now := time.Now().UTC()
	k.RevokedAt = &now
	return nil
}

func (f *FakeMetadataStore) ListAPIKeys(ctx context.Context, projectID string) ([]*domain.APIKey, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	var keys []*domain.APIKey
	for _, k := range f.apiKeys {
		if k.ProjectID == projectID {
			keys = append(keys, k)
		}
	}
	return keys, nil
}

func (f *FakeMetadataStore) CreatePromptVersion(ctx context.Context, pv *domain.PromptVersion) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.promptVersions[pv.VersionID] = pv
	return nil
}

func (f *FakeMetadataStore) GetPromptVersion(ctx context.Context, projectID, name string, version int64) (*domain.PromptVersion, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	for _, pv := range f.promptVersions {
		if pv.ProjectID == projectID && pv.Name == name && pv.Version == version {
			return pv, nil
		}
	}
	return nil, metadata.ErrNotFound
}

func (f *FakeMetadataStore) GetPromptByLabel(ctx context.Context, projectID, name, label string) (*domain.PromptVersion, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	pl, ok := f.promptLabels[projectID+":"+name+":"+label]
	if !ok {
		return nil, metadata.ErrNotFound
	}
	return f.GetPromptVersion(ctx, projectID, name, pl.Version)
}

func (f *FakeMetadataStore) ListPromptVersions(ctx context.Context, projectID, name string) ([]*domain.PromptVersion, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	var versions []*domain.PromptVersion
	for _, pv := range f.promptVersions {
		if pv.ProjectID == projectID && pv.Name == name {
			versions = append(versions, pv)
		}
	}
	return versions, nil
}

func (f *FakeMetadataStore) ListPromptNames(ctx context.Context, projectID string) ([]string, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	nameSet := make(map[string]struct{})
	for _, pv := range f.promptVersions {
		if pv.ProjectID == projectID {
			nameSet[pv.Name] = struct{}{}
		}
	}
	names := make([]string, 0, len(nameSet))
	for name := range nameSet {
		names = append(names, name)
	}
	return names, nil
}

func (f *FakeMetadataStore) SetPromptLabel(ctx context.Context, l *domain.PromptLabel) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.promptLabels[l.ProjectID+":"+l.Name+":"+l.Label] = l
	return nil
}

func (f *FakeMetadataStore) CreateEvalRule(ctx context.Context, r *domain.EvalRule) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.evalRules[r.RuleID] = r
	return nil
}

func (f *FakeMetadataStore) GetEvalRule(ctx context.Context, id string) (*domain.EvalRule, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	r, ok := f.evalRules[id]
	if !ok {
		return nil, metadata.ErrNotFound
	}
	return r, nil
}

func (f *FakeMetadataStore) ListEvalRules(ctx context.Context, projectID string) ([]*domain.EvalRule, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	var rules []*domain.EvalRule
	for _, r := range f.evalRules {
		if r.ProjectID == projectID {
			rules = append(rules, r)
		}
	}
	return rules, nil
}

func (f *FakeMetadataStore) UpdateEvalRule(ctx context.Context, r *domain.EvalRule) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.evalRules[r.RuleID] = r
	return nil
}

func (f *FakeMetadataStore) DeleteEvalRule(ctx context.Context, ruleID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.evalRules[ruleID]; !ok {
		return metadata.ErrNotFound
	}
	delete(f.evalRules, ruleID)
	return nil
}

func (f *FakeMetadataStore) CreateDataset(ctx context.Context, d *domain.Dataset) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.datasets[d.DatasetID] = d
	return nil
}

func (f *FakeMetadataStore) ListDatasets(ctx context.Context, projectID string) ([]*domain.Dataset, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	var datasets []*domain.Dataset
	for _, d := range f.datasets {
		if d.ProjectID == projectID {
			datasets = append(datasets, d)
		}
	}
	return datasets, nil
}

func (f *FakeMetadataStore) GetDataset(ctx context.Context, id string) (*domain.Dataset, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	d, ok := f.datasets[id]
	if !ok {
		return nil, metadata.ErrNotFound
	}
	return d, nil
}

func (f *FakeMetadataStore) DeleteDataset(ctx context.Context, datasetID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.datasets[datasetID]; !ok {
		return metadata.ErrNotFound
	}
	delete(f.datasets, datasetID)
	// Also delete all items belonging to this dataset.
	for id, item := range f.datasetItems {
		if item.DatasetID == datasetID {
			delete(f.datasetItems, id)
		}
	}
	return nil
}

func (f *FakeMetadataStore) CreateDatasetItem(ctx context.Context, item *domain.DatasetItem) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.datasetItems[item.ItemID] = item
	return nil
}

func (f *FakeMetadataStore) ListDatasetItems(ctx context.Context, datasetID string) ([]*domain.DatasetItem, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	var items []*domain.DatasetItem
	for _, item := range f.datasetItems {
		if item.DatasetID == datasetID {
			items = append(items, item)
		}
	}
	return items, nil
}

func (f *FakeMetadataStore) ListDatasetItemsPaginated(ctx context.Context, datasetID, cursor string, limit int) ([]*domain.DatasetItem, string, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()

	// Collect all items for this dataset.
	var items []*domain.DatasetItem
	for _, item := range f.datasetItems {
		if item.DatasetID == datasetID {
			items = append(items, item)
		}
	}

	// Sort by CreatedAt ascending, then ItemID ascending for deterministic ordering.
	for i := 0; i < len(items); i++ {
		for j := i + 1; j < len(items); j++ {
			if items[j].CreatedAt.Before(items[i].CreatedAt) ||
				(items[j].CreatedAt.Equal(items[i].CreatedAt) && items[j].ItemID < items[i].ItemID) {
				items[i], items[j] = items[j], items[i]
			}
		}
	}

	// Apply keyset cursor: skip past the cursor position.
	start := 0
	if cursor != "" {
		for i, item := range items {
			if item.ItemID == cursor {
				start = i + 1
				break
			}
		}
	}

	// Slice the page.
	if start >= len(items) {
		return nil, "", nil
	}
	end := start + limit
	if end > len(items) {
		end = len(items)
	}
	page := items[start:end]

	// Compute next cursor.
	var nextCursor string
	if len(page) == limit && end < len(items) {
		nextCursor = page[len(page)-1].ItemID
	}

	return page, nextCursor, nil
}

func (f *FakeMetadataStore) CreateDatasetRun(ctx context.Context, run *domain.DatasetRun) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.datasetRuns[run.RunID] = run
	return nil
}

func (f *FakeMetadataStore) GetDatasetRun(ctx context.Context, id string) (*domain.DatasetRun, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	run, ok := f.datasetRuns[id]
	if !ok {
		return nil, metadata.ErrNotFound
	}
	return run, nil
}

// CheckPassword compares plaintext password against stored hash (uses bcrypt).
func (f *FakeMetadataStore) CheckPassword(hashed, plaintext string) error {
	return bcrypt.CompareHashAndPassword([]byte(hashed), []byte(plaintext))
}
