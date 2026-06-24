// Package prompts implements the Judge's PromptResolver seam against the
// Prompt Registry in the metadata store.
package prompts

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/omneval/omneval/internal/domain"
	"github.com/omneval/omneval/internal/metadata"
)

// ProductionLabel is the prompt label resolved when an eval rule does not
// pin an explicit prompt version.
const ProductionLabel = "production"

// cacheTTL bounds how long a resolved template is reused before re-fetching,
// so label moves and config changes propagate without restarting workers.
const cacheTTL = 60 * time.Second

type cacheEntry struct {
	template  string
	fetchedAt time.Time
}

// CachingResolver resolves prompt templates from the metadata store with a
// short TTL cache. It implements judge.PromptResolver.
type CachingResolver struct {
	store metadata.PromptStore

	mu    sync.RWMutex
	cache map[string]cacheEntry
}

// NewCachingResolver creates a CachingResolver backed by the given PromptStore.
func NewCachingResolver(store metadata.PromptStore) *CachingResolver {
	return &CachingResolver{
		store: store,
		cache: make(map[string]cacheEntry),
	}
}

// Resolve returns the template for the given prompt. version > 0 fetches
// that exact version; version <= 0 resolves the "production" label.
func (r *CachingResolver) Resolve(ctx context.Context, projectID, name string, version int64) (string, error) {
	key := projectID + "|" + name + "|" + strconv.FormatInt(version, 10)

	r.mu.RLock()
	entry, ok := r.cache[key]
	r.mu.RUnlock()
	if ok && time.Since(entry.fetchedAt) < cacheTTL {
		return entry.template, nil
	}

	var pv *domain.PromptVersion
	var err error
	if version > 0 {
		pv, err = r.store.GetPromptVersion(ctx, projectID, name, version)
	} else {
		pv, err = r.store.GetPromptByLabel(ctx, projectID, name, ProductionLabel)
	}
	if err != nil {
		return "", fmt.Errorf("prompts: resolve %s v%d for project %s: %w", name, version, projectID, err)
	}

	r.mu.Lock()
	r.cache[key] = cacheEntry{template: pv.Template, fetchedAt: time.Now()}
	r.mu.Unlock()

	return pv.Template, nil
}
