package normalizer

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/omneval/omneval/internal/domain"
)

// SpanNormalizer validates, normalizes, and converts raw span maps into
// domain.Span values. It carries all domain rules behind a clean interface
// so that the Native handler and OTLP translator call it through a seam.
type SpanNormalizer interface {
	Normalize(ctx context.Context, raw map[string]any) (*domain.Span, error)
}

// defaultNormalizer is the concrete implementation of SpanNormalizer.
type defaultNormalizer struct{}

// New returns a SpanNormalizer ready for use.
func New() SpanNormalizer {
	return &defaultNormalizer{}
}

// Normalize validates, normalizes, and converts a raw span map into a domain.Span.
func (n *defaultNormalizer) Normalize(_ context.Context, raw map[string]any) (*domain.Span, error) {
	// Validate span_id: required, exactly 16 lowercase hex chars
	spanID := getStringField(raw, "span_id")
	if err := validateHexID("span_id", spanID, 16); err != nil {
		return nil, err
	}

	// Validate trace_id: required, exactly 32 lowercase hex chars
	traceID := getStringField(raw, "trace_id")
	if err := validateHexID("trace_id", traceID, 32); err != nil {
		return nil, err
	}

	// Validate kind
	kindStr := getStringField(raw, "kind")
	var kind domain.SpanKind
	if kindStr != "" {
		kind = domain.SpanKind(kindStr)
		if !isValidSpanKind(kind) {
			return nil, fmt.Errorf("unknown span kind: %q", kindStr)
		}
	}

	// Normalize input/output
	input := normalizeMessageArray(getField(raw, "input"), "user")
	output := normalizeMessageArray(getField(raw, "output"), "assistant")

	// Build domain.Span
	return &domain.Span{
		SpanID:         spanID,
		TraceID:        traceID,
		ParentID:       getStringField(raw, "parent_id"),
		ConversationID: getStringField(raw, "conversation_id"),
		ProjectID:      getStringField(raw, "project_id"),
		ServiceName:    getStringField(raw, "service_name"),
		Name:           getStringField(raw, "name"),
		Kind:           kind,
		Model:          getStringField(raw, "model"),
		Input:          spanValueToString(input),
		Output:         spanValueToString(output),
		InputTokens:    getInt64Field(raw, "input_tokens"),
		OutputTokens:   getInt64Field(raw, "output_tokens"),
		PromptName:     getStringField(raw, "prompt_name"),
		PromptVersion:  getInt64Field(raw, "prompt_version"),
		StatusCode:     getStringField(raw, "status_code"),
		StatusMessage:  getStringField(raw, "status_message"),
		Attributes:     getAttributes(raw),
	}, nil
}

func getStringField(raw map[string]any, key string) string {
	v, ok := raw[key]
	if !ok {
		return ""
	}
	switch val := v.(type) {
	case string:
		return val
	default:
		return fmt.Sprintf("%v", val)
	}
}

func getField(raw map[string]any, key string) any {
	v, ok := raw[key]
	if !ok {
		return nil
	}
	return v
}

func getInt64Field(raw map[string]any, key string) int64 {
	v, ok := raw[key]
	if !ok {
		return 0
	}
	switch val := v.(type) {
	case int64:
		return val
	case float64:
		return int64(val)
	case int:
		return int64(val)
	default:
		return 0
	}
}

func getAttributes(raw map[string]any) map[string]any {
	v, ok := raw["attributes"]
	if !ok {
		return nil
	}
	switch attrs := v.(type) {
	case map[string]any:
		return attrs
	default:
		return nil
	}
}

func isValidSpanKind(kind domain.SpanKind) bool {
	switch kind {
	case domain.SpanKindLLM, domain.SpanKindTool, domain.SpanKindAgent,
		domain.SpanKindChain, domain.SpanKindInternal:
		return true
	}
	return false
}

// validateHexID checks that the given value is a non-empty lowercase hex string
// of the expected length. Returns a user-friendly error if invalid.
func validateHexID(fieldName, value string, expectedLen int) error {
	if value == "" {
		return fmt.Errorf("%s is required", fieldName)
	}
	if len(value) != expectedLen {
		return fmt.Errorf("%s must be a %d-character lowercase hex string (0-9, a-f), got %d characters", fieldName, expectedLen, len(value))
	}
	if value != strings.ToLower(value) {
		return fmt.Errorf("%s must be a %d-character lowercase hex string (0-9, a-f)", fieldName, expectedLen)
	}
	if _, err := hex.DecodeString(value); err != nil {
		return fmt.Errorf("%s must be a %d-character lowercase hex string (0-9, a-f)", fieldName, expectedLen)
	}
	return nil
}

func normalizeMessageArray(v any, role string) any {
	switch val := v.(type) {
	case string:
		if len(val) > 0 && (val[0] == '[' || val[0] == '{') {
			return val
		}
		msg := map[string]any{
			"role":    role,
			"content": val,
		}
		enc, _ := json.Marshal([]any{msg})
		return string(enc)
	default:
		return v
	}
}

func spanValueToString(v any) string {
	if v == nil {
		return ""
	}
	switch val := v.(type) {
	case string:
		return val
	case []any:
		data, _ := json.Marshal(val)
		return string(data)
	case map[string]any:
		data, _ := json.Marshal(val)
		return string(data)
	default:
		return fmt.Sprintf("%v", v)
	}
}
