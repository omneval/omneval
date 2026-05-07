// Package idgen provides simple utilities for generating random IDs.
package idgen

import (
	"crypto/rand"
	"fmt"
)

// Generate creates a 16-character hex string (8 random bytes).
// Uses crypto/rand for cryptographic randomness.
func Generate() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b) // crypto/rand.Read only fails for truly pathological reasons
	return fmt.Sprintf("%x", b)
}
