package handler

import (
	"context"
	"strconv"
	"sync"
	"time"

	"github.com/omneval/omneval/internal/domain"
	"github.com/omneval/omneval/internal/metadata"
)

// cacheEntry holds a cached prompt version with an optional expiration time.
// A zero ExpiresAt means the entry never expires (used for the version cache).
type cacheEntry struct {
	PromptVersion *domain.PromptVersion
	ExpiresAt     time.Time
}

// versionCacheKey builds the cache key for a prompt version lookup.
func versionCacheKey(projectID, name string, version int64) string {
	return projectID + "|" + name + "|" + strconv.FormatInt(version, 10)
}

// labelCacheKey builds the cache key for a prompt label lookup.
func labelCacheKey(projectID, name, label string) string {
	return projectID + "|" + name + "|" + label
}

// PromptCache provides in-process caching for prompt lookups:
//   - Version cache: unbounded (no eviction)
//   - Label cache: 30-second TTL expiry
type PromptCache struct {
	mu           sync.RWMutex
	PromptStore  metadata.PromptStore
	versionCache map[string]*cacheEntry
	labelCache   map[string]*cacheEntry
}

// NewPromptCache creates a new PromptCache backed by the given PromptStore.
func NewPromptCache(store metadata.PromptStore) *PromptCache {
	return &PromptCache{
		PromptStore:  store,
		versionCache: make(map[string]*cacheEntry),
		labelCache:   make(map[string]*cacheEntry),
	}
}

// GetVersion retrieves a prompt version from the cache or the store.
// The version cache never evicts (unbounded) in Phase 1.
func (c *PromptCache) GetVersion(ctx context.Context, projectID, name string, version int64) (*domain.PromptVersion, error) {
	key := versionCacheKey(projectID, name, version)

	c.mu.RLock()
	if entry, ok := c.versionCache[key]; ok {
		c.mu.RUnlock()
		return entry.PromptVersion, nil
	}
	c.mu.RUnlock()

	// Cache miss — fetch from store.
	pv, err := c.PromptStore.GetPromptVersion(ctx, projectID, name, version)
	if err != nil {
		return nil, err
	}

	c.mu.Lock()
	// Double-check after acquiring write lock.
	if entry, ok := c.versionCache[key]; ok {
		c.mu.Unlock()
		return entry.PromptVersion, nil
	}
	c.versionCache[key] = &cacheEntry{PromptVersion: pv}
	c.mu.Unlock()

	return pv, nil
}

// GetLabel retrieves a prompt version resolved by label from the cache or the store.
// The label cache expires after 30 seconds.
func (c *PromptCache) GetLabel(ctx context.Context, projectID, name, label string) (*domain.PromptVersion, error) {
	key := labelCacheKey(projectID, name, label)

	c.mu.RLock()
	if entry, ok := c.labelCache[key]; ok {
		if time.Now().Before(entry.ExpiresAt) {
			pv := entry.PromptVersion
			c.mu.RUnlock()
			return pv, nil
		}
		// Expired — fall through to store.
	}
	c.mu.RUnlock()

	// Cache miss or expired — fetch from store.
	pv, err := c.PromptStore.GetPromptByLabel(ctx, projectID, name, label)
	if err != nil {
		return nil, err
	}

	c.mu.Lock()
	// Double-check after acquiring write lock.
	if entry, ok := c.labelCache[key]; ok && time.Now().Before(entry.ExpiresAt) {
		pv = entry.PromptVersion
	} else {
		c.labelCache[key] = &cacheEntry{
			PromptVersion: pv,
			ExpiresAt:     time.Now().Add(30 * time.Second),
		}
	}
	c.mu.Unlock()

	return pv, nil
}

// InvalidateLabel removes a label cache entry so the next lookup hits the store.
// Called after label reassignment.
func (c *PromptCache) InvalidateLabel(projectID, name, label string) {
	key := labelCacheKey(projectID, name, label)
	c.mu.Lock()
	delete(c.labelCache, key)
	c.mu.Unlock()
}