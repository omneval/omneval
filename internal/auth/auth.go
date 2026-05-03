package auth

import (
	"context"

	"github.com/zbloss/lantern/internal/domain"
	"github.com/zbloss/lantern/internal/metadata"
)

const (
	PrefixProject = "ltn_proj_"
	PrefixService = "ltn_svc_"
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
	panic("not implemented")
}

// Hash returns the SHA-256 hex digest of a raw API key.
func Hash(rawKey string) string {
	panic("not implemented")
}

// KindFromRaw infers the APIKeyKind from the key's prefix.
func KindFromRaw(rawKey string) (domain.APIKeyKind, bool) {
	panic("not implemented")
}

// CachingValidator validates raw API keys against the metadata store with a
// 60-second in-memory TTL.
type CachingValidator struct {
	store metadata.Store
}

func NewCachingValidator(store metadata.Store) *CachingValidator {
	return &CachingValidator{store: store}
}

func (v *CachingValidator) Validate(ctx context.Context, rawKey string) (*ValidatedKey, error) {
	panic("not implemented")
}
