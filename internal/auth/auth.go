package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/mr-tron/base58"
	"github.com/omneval/omneval/internal/domain"
	"github.com/omneval/omneval/internal/metadata"
)

const (
	PrefixProject = "oev_proj_"
	PrefixService = "oev_svc_"
	// CacheTTL is the duration a validated key is served from the in-memory
	// cache before a metadata store lookup is required. A revoked key may be
	// accepted for up to this duration after revocation.
	CacheTTL = 60 // seconds
)

// ValidatedKey is the result of successful API key authentication.
type ValidatedKey struct {
	ProjectID   string
	Kind        domain.APIKeyKind
	ServiceName string
}

// Validator authenticates raw API key strings against the metadata store.
type Validator interface {
	Validate(ctx context.Context, rawKey string) (*ValidatedKey, error)
}

// Generate creates a new raw API key with the appropriate prefix.
// Returns the raw key (shown once) and its SHA-256 hex hash (stored).
// Keys are 32 bytes of crypto/rand encoded as base58, giving a 43-char suffix.
func Generate(kind domain.APIKeyKind) (rawKey, hashedKey string, err error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", "", fmt.Errorf("auth: generate random bytes: %w", err)
	}
	suffix := base58.Encode(bytes)
	prefix := PrefixProject
	if kind == domain.APIKeyKindService {
		prefix = PrefixService
	}
	rawKey = prefix + suffix
	hashedKey = Hash(rawKey)
	return rawKey, hashedKey, nil
}

func Hash(rawKey string) string {
	hash := sha256.Sum256([]byte(rawKey))
	return hex.EncodeToString(hash[:])
}

func KindFromRaw(rawKey string) (domain.APIKeyKind, bool) {
	switch {
	case strings.HasPrefix(rawKey, PrefixProject):
		return domain.APIKeyKindProject, true
	case strings.HasPrefix(rawKey, PrefixService):
		return domain.APIKeyKindService, true
	default:
		return "", false
	}
}

// CachingValidator validates raw API keys against the metadata store with a
// 60-second in-memory TTL. It depends on the focused APIKeyStore interface so
// callers can pass any backend (postgres, sqlite, in-memory) through the
// narrowest possible contract.
type CachingValidator struct {
	store   metadata.APIKeyStore
	cache   map[string]*cacheEntry
	cacheMu sync.RWMutex
}

type cacheEntry struct {
	result    *ValidatedKey
	expiresAt time.Time
}

func NewCachingValidator(store metadata.APIKeyStore) *CachingValidator {
	return &CachingValidator{
		store: store,
		cache: make(map[string]*cacheEntry),
	}
}

func (v *CachingValidator) Validate(ctx context.Context, rawKey string) (*ValidatedKey, error) {
	now := time.Now()

	v.cacheMu.RLock()
	entry, cached := v.cache[rawKey]
	v.cacheMu.RUnlock()

	if cached && now.Before(entry.expiresAt) {
		return entry.result, nil
	}

	hashed := Hash(rawKey)
	if _, ok := KindFromRaw(rawKey); !ok {
		return nil, fmt.Errorf("auth: invalid key format")
	}

	key, err := v.store.GetAPIKeyByHash(ctx, hashed)
	if err != nil {
		return nil, fmt.Errorf("auth: invalid API key")
	}

	if key.RevokedAt != nil && !key.RevokedAt.IsZero() {
		return nil, fmt.Errorf("auth: API key revoked")
	}

	result := &ValidatedKey{
		ProjectID:   key.ProjectID,
		Kind:        key.Kind,
		ServiceName: key.ServiceName,
	}

	v.cacheMu.Lock()
	v.cache[rawKey] = &cacheEntry{
		result:    result,
		expiresAt: now.Add(time.Duration(CacheTTL) * time.Second),
	}
	v.cacheMu.Unlock()

	return result, nil
}
