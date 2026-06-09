package omneval

import (
	"crypto/rand"
	"fmt"
)

// generateTraceID produces a 32-character hex string suitable as a trace_id.
func generateTraceID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%x", b)
}

// GenerateConversationID produces a 32-character hex string suitable as a
// gen_ai.conversation.id per OTel GenAI semantic conventions.
func GenerateConversationID() string {
	return generateTraceID()
}
