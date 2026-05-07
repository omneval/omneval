package domain

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// EvalFilter is a recursive boolean filter matched against a Span
// in-process by the Writer Service. It supports AND, OR, and NOT
// boolean operators in addition to leaf conditions.
//
// The structure is:
//
//	{
//	  "and": [/* EvalFilter[] - all must match */],
//	  "or":  [/* EvalFilter[] - any must match */],
//	  "not": /* EvalFilter - must not match */,
//	  ...leaf conditions (kind, model, etc.)
//	}
//
// When and/or/not are absent/nil/empty, the filter behaves as a
// flat AND of leaf conditions (backward-compatible with Phase 1).
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

	// Boolean operators (recursive).
	And []*EvalFilter `json:"and"`
	Or  []*EvalFilter `json:"or"`
	Not *EvalFilter   `json:"not"`
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
// Recursively compiles patterns in nested AND/OR/NOT filters.
func (f *EvalFilter) CompilePatterns() error {
	for i, af := range f.AttributesMatch {
		if af.Pattern == "" {
			continue
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
	// Recurse into AND/OR/NOT groups.
	for _, sub := range f.And {
		if err := sub.CompilePatterns(); err != nil {
			return err
		}
	}
	for _, sub := range f.Or {
		if err := sub.CompilePatterns(); err != nil {
			return err
		}
	}
	if f.Not != nil {
		if err := f.Not.CompilePatterns(); err != nil {
			return err
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
	// Recurse into AND/OR/NOT groups.
	for _, sub := range f.And {
		if err := sub.ValidateDotPaths(); err != nil {
			return fmt.Errorf("and[%v]: %w", sub, err)
		}
	}
	for _, sub := range f.Or {
		if err := sub.ValidateDotPaths(); err != nil {
			return fmt.Errorf("or[%v]: %w", sub, err)
		}
	}
	if f.Not != nil {
		if err := f.Not.ValidateDotPaths(); err != nil {
			return fmt.Errorf("not: %w", err)
		}
	}
	return nil
}

// Validate checks the structural validity of the filter.
// Returns an error for invalid configurations such as empty and/or arrays,
// invalid regex patterns, or dot-path depth violations.
func (f *EvalFilter) Validate() error {
	if len(f.And) == 0 && f.And != nil {
		return fmt.Errorf("empty and array")
	}
	if len(f.Or) == 0 && f.Or != nil {
		return fmt.Errorf("empty or array")
	}
	if err := f.ValidateDotPaths(); err != nil {
		return err
	}
	if err := f.CompilePatterns(); err != nil {
		return err
	}
	// Recursively validate nested filters.
	for i, sub := range f.And {
		if err := sub.Validate(); err != nil {
			return fmt.Errorf("and[%d]: %w", i, err)
		}
	}
	for i, sub := range f.Or {
		if err := sub.Validate(); err != nil {
			return fmt.Errorf("or[%d]: %w", i, err)
		}
	}
	if f.Not != nil {
		if err := f.Not.Validate(); err != nil {
			return fmt.Errorf("not: %w", err)
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

	var current any = attrs[parts[0]]

	for _, seg := range parts[1:] {
		obj, ok := current.(map[string]any)
		if !ok {
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

	switch v := current.(type) {
	case string:
		return v
	case float64, bool, nil:
		return fmt.Sprintf("%v", v)
	default:
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

// Matches checks if a span matches this EvalFilter.
// The evaluation order is:
//
//	1. AND group: all sub-filters must match
//	2. OR group: at least one sub-filter must match
//	3. NOT group: sub-filter must NOT match
//	4. Leaf conditions: all must match
//
// All groups are combined with AND logic.
// When AND/OR/NOT are absent/nil/empty, only leaf conditions are evaluated
// (backward-compatible with the flat AND-only Phase 1 structure).
func (f *EvalFilter) Matches(span *Span) bool {
	// 1. AND: all sub-filters must match.
	if len(f.And) > 0 {
		for _, sub := range f.And {
			if !sub.Matches(span) {
				return false
			}
		}
	}

	// 2. OR: at least one sub-filter must match.
	if len(f.Or) > 0 {
		anyMatch := false
		for _, sub := range f.Or {
			if sub.Matches(span) {
				anyMatch = true
				break
			}
		}
		if !anyMatch {
			return false
		}
	}

	// 3. NOT: sub-filter must NOT match.
	if f.Not != nil {
		if f.Not.Matches(span) {
			return false
		}
	}

	// 4. Leaf conditions (AND of all).
	if f.Kind != nil && span.Kind != *f.Kind {
		return false
	}
	if f.Model != nil && span.Model != *f.Model {
		return false
	}
	if f.ServiceName != nil && span.ServiceName != *f.ServiceName {
		return false
	}
	if f.PromptName != nil && span.PromptName != *f.PromptName {
		return false
	}
	if f.StatusCode != nil && span.StatusCode != *f.StatusCode {
		return false
	}
	if f.MinCostUSD != nil && span.CostUSD < *f.MinCostUSD {
		return false
	}
	if f.MaxCostUSD != nil && span.CostUSD > *f.MaxCostUSD {
		return false
	}
	durationMS := int64(0)
	if !span.StartTime.IsZero() && !span.EndTime.IsZero() {
		durationMS = span.EndTime.Sub(span.StartTime).Milliseconds()
	}
	if f.MinDurationMS != nil && durationMS < *f.MinDurationMS {
		return false
	}
	if f.MaxDurationMS != nil && durationMS > *f.MaxDurationMS {
		return false
	}
	for _, af := range f.AttributesMatch {
		if !MatchesFilterAttributeRegex(span, af) {
			return false
		}
	}

	return true
}
