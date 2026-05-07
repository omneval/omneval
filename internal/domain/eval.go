package domain

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// EvalFilter is a conjunction (AND) of conditions matched against a Span
// in-process by the Writer Service. Nil pointer fields are ignored.
type EvalFilter struct {
	Kind            *SpanKind            `json:"kind"`
	Model           *string              `json:"model"`
	ServiceName     *string              `json:"service_name"`
	PromptName      *string              `json:"prompt_name"`
	StatusCode      *string              `json:"status_code"`
	MinCostUSD      *float64             `json:"min_cost_usd"`
	MaxCostUSD      *float64             `json:"max_cost_usd"`
	MinDurationMS   *int64               `json:"min_duration_ms"`
	MaxDurationMS   *int64               `json:"max_duration_ms"`
	AttributesMatch []AttributeRegexFilter `json:"attributes_match"`
}

// AttributeRegexFilter matches a span attribute (from the overflow Attributes map)
// against a compiled regex pattern. Key supports dot-notation for nested paths,
// e.g. "metadata.user_id" traverses span.Attributes["metadata"] as JSON.
type AttributeRegexFilter struct {
	Key      string
	Pattern  string
	compiled *regexp.Regexp
}

// CompilePatterns validates and compiles all regex patterns in the filter.
// Call this at rule-load time (not per-span) for performance.
func (f *EvalFilter) CompilePatterns() error {
	for i, af := range f.AttributesMatch {
		if af.Pattern == "" {
			continue // empty pattern matches nothing; skip
		}
		compiled, err := regexp.Compile(af.Pattern)
		if err != nil {
			return fmt.Errorf("attributes_match[%d] invalid pattern %q: %w", i, af.Pattern, err)
		}
		f.AttributesMatch[i] = AttributeRegexFilter{
			Key:      af.Key,
			Pattern:  af.Pattern,
			compiled: compiled,
		}
	}
	return nil
}

// ValidateDotPaths checks that no dot-separated key exceeds the depth limit (5).
// Returns an error if any path is too deep.
func (f *EvalFilter) ValidateDotPaths() error {
	maxDepth := 5
	for _, af := range f.AttributesMatch {
		parts := strings.Split(af.Key, ".")
		if len(parts) > maxDepth {
			return fmt.Errorf("attributes_match key %q exceeds max depth of %d (got %d)", af.Key, maxDepth, len(parts))
		}
	}
	return nil
}

// attributeValue resolves a dot-separated path in the span's Attributes map.
// Top-level keys are looked up directly. Dotted keys like "metadata.user_id"
// parse span.Attributes["metadata"] as JSON and traverse into the result.
// Returns "" when the path cannot be resolved (missing key, non-JSON, non-object).
func attributeValue(span *Span, key string) string {
	attrs := span.Attributes
	if attrs == nil {
		return ""
	}

	parts := strings.Split(key, ".")
	if len(parts) == 1 {
		val := attrs[parts[0]]
		if val == nil {
			return ""
		}
		return fmt.Sprintf("%v", val)
	}

	// Multi-segment path — traverse via JSON objects.
	var current any = attrs[parts[0]]

	for _, seg := range parts[1:] {
		// Try to treat current as a map to look up the next segment.
		obj, ok := current.(map[string]any)
		if !ok {
			// current is not a JSON object — try to parse it as JSON string.
			strVal, ok := current.(string)
			if !ok {
				return ""
			}
			if err := json.Unmarshal([]byte(strVal), &obj); err != nil {
				return ""
			}
		}

		next, exists := obj[seg]
		if !exists {
			return ""
		}
		current = next
	}

	// Final value — if it's a string, return it; otherwise stringify.
	switch v := current.(type) {
	case string:
		return v
	case float64, bool, nil:
		return fmt.Sprintf("%v", v)
	default:
		// Complex type (array, nested object) — stringify.
		data, err := json.Marshal(current)
		if err != nil {
			return ""
		}
		return string(data)
	}
}

// MatchesFilterAttributeRegex checks a single AttributeRegexFilter against a span.
func MatchesFilterAttributeRegex(span *Span, af AttributeRegexFilter) bool {
	val := attributeValue(span, af.Key)
	if val == "" {
		return false
	}
	if af.compiled != nil {
		return af.compiled.MatchString(val)
	}
	// Pattern not compiled yet (e.g. from JSON deserialization) — raw match.
	p, err := regexp.Compile(af.Pattern)
	if err != nil {
		return false
	}
	return p.MatchString(val)
}

// EvalRule defines when and how to run LLM-as-a-Judge evaluations.
type EvalRule struct {
	RuleID        string
	ProjectID     string
	Name          string
	JudgeModel    string
	PromptName    string
	PromptVersion int64
	Filter        EvalFilter
	SampleRate    float64 // 0.0–1.0; 1.0 = score every matching span
	Enabled       bool
	CreatedAt     time.Time
}

// EvalJob is a unit of work placed on the eval Redis queue.
type EvalJob struct {
	JobID         string
	RuleID        string
	SpanID        string
	TraceID       string
	ProjectID     string
	EnqueuedAt    time.Time
	PromptName    string
	PromptVersion int64
}
