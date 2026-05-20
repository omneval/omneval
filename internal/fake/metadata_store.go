package fake

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/omneval/omneval/internal/domain"
	"github.com/omneval/omneval/internal/metadata"
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
	tokensToIDs    map[string]string // reset token -> user_id
	sessions       map[string]*domain.Session
	apiKeys        map[string]*domain.APIKey
	hashedKeys     map[string]*domain.APIKey
	promptVersions map[string]*domain.PromptVersion // key: projectID:name:version
	promptLabels   map[string]*domain.PromptLabel   // key: projectID:name:label
	evalRules      map[string]*domain.EvalRule
	datasets         map[string]*domain.Dataset
	datasetItems     map[string]*domain.DatasetItem
	datasetRuns      map[string]*domain.DatasetRun
	datasetRunItems  map[string]*domain.DatasetRunItem

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
		tokensToIDs:    make(map[string]string),
		sessions:       make(map[string]*domain.Session),
		apiKeys:        make(map[string]*domain.APIKey),
		hashedKeys:     make(map[string]*domain.APIKey),
		promptVersions: make(map[string]*domain.PromptVersion),
		promptLabels:   make(map[string]*domain.PromptLabel),
		evalRules:      make(map[string]*domain.EvalRule),
		datasets:         make(map[string]*domain.Dataset),
		datasetItems:     make(map[string]*domain.DatasetItem),
		datasetRuns:      make(map[string]*domain.DatasetRun),
		datasetRunItems:  make(map[string]*domain.DatasetRunItem),
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

	// Hash password only if it's provided (non-empty).
	// Invite flow creates users without an initial password; the user
	// sets their own via the reset token endpoint.
	if u.PasswordHash != "" {
		hash, err := bcrypt.GenerateFromPassword([]byte(u.PasswordHash), bcrypt.DefaultCost)
		if err != nil {
			return err
		}
		u.PasswordHash = string(hash)
	}

	f.users[u.UserID] = u
	f.emailsToIDs[u.Email] = u.UserID

	// Register reset token mapping if provided (mirrors DB INSERT).
	if u.PasswordResetToken != "" {
		f.tokensToIDs[u.PasswordResetToken] = u.UserID
	}

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

// UpdateUserResetToken sets (or clears) the password reset token for a user.
// Passing an empty token and zero time clears the token.
func (f *FakeMetadataStore) UpdateUserResetToken(ctx context.Context, userID, token string, expiry time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	u, ok := f.users[userID]
	if !ok {
		return metadata.ErrNotFound
	}
	// Remove old token mapping if present
	if u.PasswordResetToken != "" {
		delete(f.tokensToIDs, u.PasswordResetToken)
	}
	u.PasswordResetToken = token
	u.ResetTokenExpiry = expiry
	if token != "" {
		f.tokensToIDs[token] = userID
	}
	return nil
}

// GetUserByResetToken returns the user associated with a reset token.
// Returns ErrNotFound if the token doesn't exist or has expired.
func (f *FakeMetadataStore) GetUserByResetToken(ctx context.Context, token string) (*domain.User, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	userID, ok := f.tokensToIDs[token]
	if !ok {
		return nil, metadata.ErrNotFound
	}
	u, ok := f.users[userID]
	if !ok {
		return nil, metadata.ErrNotFound
	}
	// Check expiry to mirror production DB behavior (WHERE reset_token_expiry > now())
	if !u.ResetTokenExpiry.IsZero() && time.Now().After(u.ResetTokenExpiry) {
		return nil, metadata.ErrNotFound
	}
	// Return a copy so callers can't mutate the store
	userCopy := *u
	return &userCopy, nil
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
	sort.Slice(items, func(i, j int) bool {
		if items[i].CreatedAt.Equal(items[j].CreatedAt) {
			return items[i].ItemID < items[j].ItemID
		}
		return items[i].CreatedAt.Before(items[j].CreatedAt)
	})

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
	r := *run
	return &r, nil
}

func (f *FakeMetadataStore) UpdateDatasetRun(ctx context.Context, run *domain.DatasetRun) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.datasetRuns[run.RunID]; !ok {
		return metadata.ErrNotFound
	}
	f.datasetRuns[run.RunID] = run
	return nil
}

func (f *FakeMetadataStore) ListDatasetRuns(ctx context.Context, datasetID string) ([]*domain.DatasetRun, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	var runs []*domain.DatasetRun
	for _, run := range f.datasetRuns {
		if run.DatasetID == datasetID {
			cp := *run
			runs = append(runs, &cp)
		}
	}
	return runs, nil
}

func (f *FakeMetadataStore) CreateDatasetRunItem(ctx context.Context, item *domain.DatasetRunItem) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.datasetRunItems[item.RunItemID] = item
	return nil
}

func (f *FakeMetadataStore) GetDatasetRunItem(ctx context.Context, id string) (*domain.DatasetRunItem, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	item, ok := f.datasetRunItems[id]
	if !ok {
		return nil, metadata.ErrNotFound
	}
	cp := *item
	return &cp, nil
}

func (f *FakeMetadataStore) UpdateDatasetRunItem(ctx context.Context, item *domain.DatasetRunItem) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.datasetRunItems[item.RunItemID]; !ok {
		return metadata.ErrNotFound
	}
	f.datasetRunItems[item.RunItemID] = item
	return nil
}

func (f *FakeMetadataStore) ListDatasetRunItems(ctx context.Context, runID string) ([]*domain.DatasetRunItem, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	var items []*domain.DatasetRunItem
	for _, item := range f.datasetRunItems {
		if item.RunID == runID {
			cp := *item
			items = append(items, &cp)
		}
	}
	return items, nil
}

// CheckPassword compares plaintext password against stored hash (uses bcrypt).
func (f *FakeMetadataStore) CheckPassword(hashed, plaintext string) error {
	return bcrypt.CompareHashAndPassword([]byte(hashed), []byte(plaintext))
}
