package otlp

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/omneval/omneval/internal/domain"
)

// Resource represents a normalized OTLP Resource (resource-level attributes).
type Resource struct {
	Attributes map[string]any
}

// Span represents a normalized OTLP span before translation to domain.Span.
type Span struct {
	SpanID     string
	TraceID    string
	ParentID   string
	Name       string
	StartTime  time.Time
	EndTime    time.Time
	StatusCode string
	StatusMsg  string
	Attributes map[string]any
}

// ResourceSpans groups a Resource with the spans it produced.
type ResourceSpans struct {
	Resource Resource
	Spans    []*Span
}

// Options controls translator behaviour that varies by deployment config.
type Options struct {
	// LogSystemPrompt controls whether the system prompt is included as the
	// first element of a span's Input array.
	LogSystemPrompt bool
	// ServiceNameOverride is non-empty when the ingest request was
	// authenticated with a service-scoped API key; it takes precedence over
	// the resource-level service.name attribute.
	ServiceNameOverride string
}

// Translate converts a slice of ResourceSpans into domain.Span values.
// projectID is attached to every span. opts controls system-prompt logging
// and service-name override for service-scoped API keys.
// normalizer validates and normalizes each translated span.
func Translate(ctx context.Context, projectID string, rss []ResourceSpans, opts Options, normalizer domain.SpanNormalizer) ([]*domain.Span, error) {
	spans := make([]*domain.Span, 0, totalSpanCount(rss))
	for _, rs := range rss {
		for _, s := range rs.Spans {
			raw := toRawMap(projectID, rs.Resource, *s, opts)
			span, err := normalizer.Normalize(ctx, raw)
			if err != nil {
				return nil, fmt.Errorf("normalize otlp span %s: %w", s.SpanID, err)
			}
			// Restore OTLP-specific fields that the normalizer doesn't set
			span.StatusCode = s.StatusCode
			span.StatusMessage = s.StatusMsg
			spans = append(spans, span)
		}
	}
	return spans, nil
}

// toRawMap extracts OTLP-specific fields from an OTLP Span and produces
// a raw map suitable for the SpanNormalizer.
func toRawMap(projectID string, resource Resource, span Span, opts Options) map[string]any {
	// Derive model from GenAI attributes.
	model := extractAttributeString(span.Attributes, "gen_ai.request.model")
	if model == "" {
		model = extractAttributeString(span.Attributes, "llm.request.model")
	}

	// Derive token counts (prefer GenAI conventions, fall back to legacy).
	inputTokens := extractAttributeInt64(span.Attributes, "gen_ai.usage.input_tokens")
	if inputTokens == -1 {
		inputTokens = extractAttributeInt64(span.Attributes, "prompt_tokens")
	}
	outputTokens := extractAttributeInt64(span.Attributes, "gen_ai.usage.output_tokens")
	if outputTokens == -1 {
		outputTokens = extractAttributeInt64(span.Attributes, "completion_tokens")
	}
	if inputTokens < 0 {
		inputTokens = 0
	}
	if outputTokens < 0 {
		outputTokens = 0
	}

	// Build Input from gen_ai.prompt.N.
	input := buildMessageArray(span.Attributes, "gen_ai.prompt")
	if input == "" {
		input = buildMessageArray(span.Attributes, "llm.prompt")
	}

	// Build Output from gen_ai.completion.N.
	output := buildMessageArray(span.Attributes, "gen_ai.completion")
	if output == "" {
		output = buildMessageArray(span.Attributes, "llm.completion")
	}

	// Derive Kind: explicit omneval.kind wins, then heuristic.
	kind := deriveKind(span.Attributes)

	// Derive ServiceName from Resource attributes.
	serviceName := extractResourceAttributeString(resource.Attributes, "service.name")
	if opts.ServiceNameOverride != "" {
		serviceName = opts.ServiceNameOverride
	}

	// Build attributes overflow map (remove GenAI/LLM attributes already mapped).
	overflow := buildOverflowAttributes(span.Attributes, model, inputTokens, outputTokens, input, output, kind, serviceName)

	// Extract prompt linkage.
	promptName, promptVersion := resolvePromptInfo(span.Attributes)

	// Extract conversation_id.
	conversationID := extractAttributeString(span.Attributes, "gen_ai.conversation.id")
	if conversationID == "" {
		conversationID = extractAttributeString(span.Attributes, "omneval.conversation.id")
	}

	raw := map[string]any{
		"span_id":         span.SpanID,
		"trace_id":        span.TraceID,
		"name":            span.Name,
		"project_id":      projectID,
		"service_name":    serviceName,
		"model":           model,
		"input":           input,
		"output":          output,
		"input_tokens":    inputTokens,
		"output_tokens":   outputTokens,
		"prompt_name":     promptName,
		"prompt_version":  promptVersion,
		"kind":            kind,
	}
	if span.ParentID != "" {
		raw["parent_id"] = span.ParentID
	}
	if conversationID != "" {
		raw["conversation_id"] = conversationID
	}
	if !span.StartTime.IsZero() {
		raw["start_time"] = span.StartTime
	}
	if !span.EndTime.IsZero() {
		raw["end_time"] = span.EndTime
	}
	if len(overflow) > 0 {
		raw["attributes"] = overflow
	}
	return raw
}

func totalSpanCount(rss []ResourceSpans) int {
	total := 0
	for _, rs := range rss {
		total += len(rs.Spans)
	}
	return total
}



// resolvePromptInfo extracts prompt linkage from omneval.* attributes.
func resolvePromptInfo(attrs map[string]any) (string, int64) {
	var promptName string
	if name, ok := attrs["omneval.prompt.name"]; ok {
		if s, ok := name.(string); ok {
			promptName = s
		}
	}
	var promptVersion int64
	if _, ok := attrs["omneval.prompt.version"]; ok {
		promptVersion = extractAttributeInt64(attrs, "omneval.prompt.version")
	}
	return promptName, promptVersion
}

// extractAttributeString looks up a string attribute from the overflow map.
func extractAttributeString(attrs map[string]any, key string) string {
	v, ok := attrs[key]
	if !ok {
		return ""
	}
	switch val := v.(type) {
	case string:
		return val
	case int64:
		return fmt.Sprintf("%d", val)
	default:
		return fmt.Sprintf("%v", val)
	}
}

// extractAttributeInt64 looks up an integer attribute, returns -1 on missing/error.
func extractAttributeInt64(attrs map[string]any, key string) int64 {
	v, ok := attrs[key]
	if !ok {
		return -1
	}
	switch val := v.(type) {
	case int64:
		return val
	case float64:
		// JSON numbers are decoded as float64.
		return int64(val)
	case int:
		return int64(val)
	default:
		return -1
	}
}

// extractResourceAttributeString looks up a string attribute from Resource-level attributes.
func extractResourceAttributeString(resourceAttrs map[string]any, key string) string {
	v, ok := resourceAttrs[key]
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

// buildMessageArray constructs a JSON-serialized message array from
// numbered attributes like gen_ai.prompt.0.role, gen_ai.prompt.0.content.
func buildMessageArray(attrs map[string]any, prefix string) string {
	// Find max index for this prefix.
	maxIdx := -1
	for k := range attrs {
		if idx, ok := extractNumberedIndex(k, prefix); ok && idx > maxIdx {
			maxIdx = idx
		}
	}
	if maxIdx < 0 {
		return ""
	}

	messages := make([]map[string]any, 0, maxIdx+1)
	for i := 0; i <= maxIdx; i++ {
		role := extractAttributeString(attrs, fmt.Sprintf("%s.%d.role", prefix, i))
		content := extractAttributeString(attrs, fmt.Sprintf("%s.%d.content", prefix, i))
		if role != "" && content != "" {
			messages = append(messages, map[string]any{
				"role":    role,
				"content": content,
			})
		}
	}

	if len(messages) == 0 {
		return ""
	}

	data, err := json.Marshal(messages)
	if err != nil {
		return ""
	}
	return string(data)
}

// extractNumberedIndex checks if a key matches pattern "prefix.N.suffix"
// and returns N. Returns false if no match.
func extractNumberedIndex(key, prefix string) (int, bool) {
	// Key must start with prefix.
	if len(key) <= len(prefix)+1 || key[:len(prefix)+1] != prefix+"." {
		return 0, false
	}
	rest := key[len(prefix)+1:]
	// Find the first dot after the number.
	dotIdx := -1
	for i, r := range rest {
		if r == '.' {
			dotIdx = i
			break
		}
		if r < '0' || r > '9' {
			return 0, false
		}
	}
	if dotIdx < 0 {
		return 0, false
	}
	numStr := rest[:dotIdx]
	var n int
	for _, c := range numStr {
		if c < '0' || c > '9' {
			return 0, false
		}
		n = n*10 + int(c-'0')
	}
	return n, true
}

// deriveKind determines the SpanKind from attribute heuristics.
func deriveKind(attrs map[string]any) domain.SpanKind {
	// Explicit omneval.kind wins.
	if kind := extractAttributeString(attrs, "omneval.kind"); kind != "" {
		if dk := domain.SpanKind(kind); dk == domain.SpanKindLLM || dk == domain.SpanKindTool ||
			dk == domain.SpanKindAgent || dk == domain.SpanKindChain || dk == domain.SpanKindInternal {
			return dk
		}
	}

	// Check for GenAI attributes → llm.
	if _, hasGenAI := attrs["gen_ai.request.model"]; hasGenAI {
		return domain.SpanKindLLM
	}
	if _, hasGenAIPrompt := attrs["gen_ai.prompt"]; hasGenAIPrompt {
		return domain.SpanKindLLM
	}

	// Check for LLM attributes → llm.
	if _, hasLLMModel := attrs["llm.request.model"]; hasLLMModel {
		return domain.SpanKindLLM
	}

	// Check for tool attributes → tool.
	if _, hasToolCall := attrs["tool_call"]; hasToolCall {
		return domain.SpanKindTool
	}
	if _, hasToolName := attrs["tool.name"]; hasToolName {
		return domain.SpanKindTool
	}

	// Default: internal.
	return domain.SpanKindInternal
}

// buildOverflowAttributes creates an overflow map without the GenAI/LLM attributes
// that have been extracted into typed columns.
func buildOverflowAttributes(attrs map[string]any, model string, inputTokens, outputTokens int64, input, output string, kind domain.SpanKind, serviceName string) map[string]any {
	// Collect all keys to remove.
	remove := make(map[string]bool)

	// GenAI model.
	for k := range attrs {
		if k == "gen_ai.request.model" || k == "llm.request.model" {
			remove[k] = true
		}
	}

	// GenAI tokens.
	for k := range attrs {
		if k == "gen_ai.usage.input_tokens" || k == "prompt_tokens" {
			remove[k] = true
		}
		if k == "gen_ai.usage.output_tokens" || k == "completion_tokens" {
			remove[k] = true
		}
	}

	// GenAI prompt/completion numbered attributes.
	for k := range attrs {
		if _, ok := extractNumberedIndex(k, "gen_ai.prompt"); ok {
			remove[k] = true
		} else if _, ok := extractNumberedIndex(k, "llm.prompt"); ok {
			remove[k] = true
		}
		if _, ok := extractNumberedIndex(k, "gen_ai.completion"); ok {
			remove[k] = true
		} else if _, ok := extractNumberedIndex(k, "llm.completion"); ok {
			remove[k] = true
		}
	}

	// Kind.
	if _, hasKind := attrs["omneval.kind"]; hasKind {
		remove["omneval.kind"] = true
	}

	// Service name.
	if _, hasServiceName := attrs["service.name"]; hasServiceName {
		remove["service.name"] = true
	}

	// Conversation ID — remove both gen_ai and omneval variants.
	if _, hasConvID := attrs["gen_ai.conversation.id"]; hasConvID {
		remove["gen_ai.conversation.id"] = true
	}
	if _, hasOmnevalConvID := attrs["omneval.conversation.id"]; hasOmnevalConvID {
		remove["omneval.conversation.id"] = true
	}

	// Build overflow without removed keys.
	overflow := make(map[string]any, len(attrs)-len(remove))
	for k, v := range attrs {
		if !remove[k] {
			overflow[k] = v
		}
	}
	return overflow
}
