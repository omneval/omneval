package cursor

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"time"
)

// Cursor is the opaque pagination token returned by POST /api/v1/spans/query.
// It encodes the last row's start_time and span_id so the next page picks up
// exactly where the previous page left off.
type Cursor struct {
	StartTime time.Time `json:"start_time"`
	SpanID    string    `json:"span_id"`
}

// Encode serialises the cursor to a base64-encoded JSON string suitable for
// inclusion in the URL or response body.
func Encode(c Cursor) string {
	b, err := json.Marshal(c)
	if err != nil {
		// Should never happen — Cursor is JSON-serialisable.
		return ""
	}
	return base64.URLEncoding.WithPadding(base64.NoPadding).EncodeToString(b)
}

// Decode parses a base64-encoded cursor token back into a Cursor struct.
func Decode(s string) (Cursor, error) {
	var c Cursor
	decoded, err := base64.URLEncoding.WithPadding(base64.NoPadding).DecodeString(s)
	if err != nil {
		return c, fmt.Errorf("cursor: invalid encoding: %w", err)
	}
	if err := json.Unmarshal(decoded, &c); err != nil {
		return c, fmt.Errorf("cursor: invalid JSON: %w", err)
	}
	return c, nil
}
