// Package otlp provides the two-step translation pipeline for OpenTelemetry
// trace data:
//
//  1. Wire format (protobuf or JSON) → []FlatResourceSpans (handled in
//     internal/otlp/protobuf).
//  2. []FlatResourceSpans → []*domain.Span via Translate (pure function).
//
// After translation, spans are enqueued to the Redis ingest queue.
package otlp

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/zbloss/lantern/internal/domain"
	"github.com/zbloss/lantern/internal/otlp/protobuf"
)

// Resource represents a normalized OTLP Resource (resource-level attributes).
type Resource = protobuf.Resource

// Span represents a normalized OTLP span before translation to domain.Span.
type Span = protobuf.Span

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

// Translate converts a slice of FlatResourceSpans into domain.Span values.
// projectID is attached to every span. opts controls system-prompt logging
// and service-name override for service-scoped API keys.
func Translate(projectID string, rss []protobuf.FlatResourceSpans, opts Options) ([]*domain.Span, error) {
	spans := make([]*domain.Span, 0, len(rss)*4 /* heuristic */)
	for _, rs := range rss {
		for _, s := range rs.Spans {
			domainSpan, err := translateOne(rs.Resource, s, opts)
			if err != nil {
				return nil, fmt.Errorf("translate span %q: %w", s.Name, err)
			}
			domainSpan.ProjectID = projectID
			spans = append(spans, domainSpan)
		}
	}
	return spans, nil
}

// translateOne converts a single OTLP span + its resource into a domain.Span.
func translateOne(res *Resource, s *Span, opts Options) (*domain.Span, error) {
	d := &domain.Span{
		SpanID:    protobuf.DecodeSpanID(s.SpanId),
		TraceID:   protobuf.DecodeTraceID(s.TraceId),
		ParentID:  protobuf.DecodeSpanID(s.ParentSpanId),
		Name:      s.Name,
		Kind:      deriveKind(s.Attributes),
		StartTime: protobuf.UnixNano(s.StartTimeUnixNano),
		EndTime:   protobuf.UnixNano(s.EndTimeUnixNano),
	}

	d.ServiceName = resolveServiceName(res, opts)
	d.Model = resolveModel(s.Attributes)
	d.InputTokens, d.OutputTokens = resolveTokenCounts(s.Attributes)
	d.Input = resolveInput(s.Attributes)
	d.Output = resolveOutput(s.Attributes)
	d.PromptName, d.PromptVersion = resolvePromptInfo(s.Attributes)
	d.StatusCode = resolveStatusCode(s.Flags)
	d.Attributes = collectOverflowAttributes(s.Attributes)

	return d, nil
}

// resolveServiceName returns the service name, preferring an API-key override
// over the resource-level service.name attribute.
func resolveServiceName(res *Resource, opts Options) string {
	if opts.ServiceNameOverride != "" {
		return opts.ServiceNameOverride
	}
	return getStringResourceAttr(res, "service.name")
}

// resolveModel extracts the LLM model name from gen_ai.request.model.
func resolveModel(attrs []*protobuf.KeyValue) string {
	model, _ := protobuf.GetStringAttribute(attrs, "gen_ai.request.model")
	return model
}

// resolveTokenCounts extracts input and output token counts, supporting
// both modern (gen_ai.usage.*) and legacy (prompt_tokens, completion_tokens)
// attribute names. Modern names take precedence.
func resolveTokenCounts(attrs []*protobuf.KeyValue) (int64, int64) {
	inputTokens := extractTokenCount(attrs, "gen_ai.usage.input_tokens", "prompt_tokens")
	outputTokens := extractTokenCount(attrs, "gen_ai.usage.output_tokens", "completion_tokens")
	return inputTokens, outputTokens
}

// resolveInput builds a JSON message array from gen_ai.prompt attributes.
func resolveInput(attrs []*protobuf.KeyValue) string {
	return extractPromptCompletion(attrs, "gen_ai.prompt", "user")
}

// resolveOutput builds a JSON message array from gen_ai.completion attributes.
func resolveOutput(attrs []*protobuf.KeyValue) string {
	return extractPromptCompletion(attrs, "gen_ai.completion", "assistant")
}

// resolvePromptInfo extracts prompt linkage from lantern.* attributes.
func resolvePromptInfo(attrs []*protobuf.KeyValue) (string, int64) {
	var promptName string
	if name, ok := protobuf.GetStringAttribute(attrs, "lantern.prompt.name"); ok {
		promptName = name
	}
	var promptVersion int64
	if version, ok := protobuf.GetInt64Attribute(attrs, "lantern.prompt.version"); ok {
		promptVersion = version
	}
	return promptName, promptVersion
}

// resolveStatusCode converts the span flags to an OTLP status code string.
// Bit 0 of the flags indicates whether the span has an explicit status set.
func resolveStatusCode(flags uint32) string {
	if flags&0x01 == 0 {
		return "unset"
	}
	return "ok"
}

// getStringResourceAttr retrieves a string attribute from a Resource.
func getStringResourceAttr(res *Resource, key string) string {
	for _, kv := range res.Attributes {
		if kv.Key == key && kv.Value != nil && kv.Value.StringValue != nil {
			return *kv.Value.StringValue
		}
	}
	return ""
}

// deriveKind determines the SpanKind based on attribute precedence:
// 1. Explicit lantern.kind attribute wins.
// 2. Presence of gen_ai.* attributes → llm.
// 3. Presence of tool.* attributes → tool.
// 4. Otherwise → internal.
func deriveKind(attrs []*protobuf.KeyValue) domain.SpanKind {
	// Check explicit lantern.kind first.
	if lk, ok := protobuf.GetStringAttribute(attrs, "lantern.kind"); ok {
		switch domain.SpanKind(lk) {
		case domain.SpanKindLLM, domain.SpanKindTool, domain.SpanKindAgent,
			domain.SpanKindChain, domain.SpanKindInternal:
			return domain.SpanKind(lk)
		}
		// Unknown lantern.kind value → fall through to next check.
	}

	// Check for gen_ai.* attributes → llm.
	if hasPrefix(attrs, "gen_ai.") {
		return domain.SpanKindLLM
	}

	// Check for tool.* attributes → tool.
	if hasPrefix(attrs, "tool.") {
		return domain.SpanKindTool
	}

	// Default.
	return domain.SpanKindInternal
}

// hasPrefix returns true if any attribute key starts with the given prefix.
func hasPrefix(attrs []*protobuf.KeyValue, prefix string) bool {
	for _, kv := range attrs {
		if strings.HasPrefix(kv.Key, prefix) {
			return true
		}
	}
	return false
}

// extractTokenCount gets token count from the modern or legacy attribute name.
// Returns 0 if neither is present.
func extractTokenCount(attrs []*protobuf.KeyValue, modern, legacy string) int64 {
	if count, ok := protobuf.GetInt64Attribute(attrs, modern); ok {
		return count
	}
	if count, ok := protobuf.GetInt64Attribute(attrs, legacy); ok {
		return count
	}
	return 0
}

// extractPromptCompletion builds a JSON message array from gen_ai.prompt or
// gen_ai.completion attributes. The index N in the attribute name (e.g.
// gen_ai.prompt.0.content) determines the position in the array.
func extractPromptCompletion(attrs []*protobuf.KeyValue, prefix, defaultRole string) string {
	// Collect messages by their numeric index.
	type indexedMsg struct {
		idx     int
		role    string
		roleSet bool
		content string
	}
	msgMap := make(map[int]*indexedMsg)
	hasContent := false

	for _, kv := range attrs {
		if !strings.HasPrefix(kv.Key, prefix+".") {
			continue
		}
		rest := strings.TrimPrefix(kv.Key, prefix+".")
		dot := strings.Index(rest, ".")
		if dot < 0 {
			continue
		}
		idxStr := rest[:dot]
		roleKey := rest[dot+1:]
		if roleKey != "content" && roleKey != "role" {
			continue
		}
		var idx int
		fmt.Sscanf(idxStr, "%d", &idx)

		msg, exists := msgMap[idx]
		if !exists {
			msg = &indexedMsg{idx: idx}
			msgMap[idx] = msg
		}

		if roleKey == "content" {
			if val, ok := protobuf.GetStringAttribute(attrs, kv.Key); ok {
				msg.content = val
				hasContent = true
			}
		} else if roleKey == "role" {
			if val, ok := protobuf.GetStringAttribute(attrs, kv.Key); ok {
				msg.role = val
				msg.roleSet = true
			}
		}
	}

	if !hasContent {
		return ""
	}

	// Sort messages by index.
	type pair struct {
		idx int
		msg *indexedMsg
	}
	pairs := make([]pair, 0, len(msgMap))
	for idx, msg := range msgMap {
		pairs = append(pairs, pair{idx: idx, msg: msg})
	}
	for i := 0; i < len(pairs); i++ {
		for j := i + 1; j < len(pairs); j++ {
			if pairs[i].idx > pairs[j].idx {
				pairs[i], pairs[j] = pairs[j], pairs[i]
			}
		}
	}

	// Build JSON message array.
	messages := make([]map[string]string, 0, len(pairs))
	for _, p := range pairs {
		msg := map[string]string{
			"content": p.msg.content,
		}
		if p.msg.roleSet {
			msg["role"] = p.msg.role
		} else {
			msg["role"] = defaultRole
		}
		messages = append(messages, msg)
	}

	data, err := json.Marshal(messages)
	if err != nil {
		return ""
	}
	return string(data)
}

// collectOverflowAttributes gathers all attributes that are not consumed by
// the translator into the overflow map.
func collectOverflowAttributes(attrs []*protobuf.KeyValue) map[string]any {
	overflow := make(map[string]any)

	// Keys consumed by the translator (to exclude from overflow).
	consumedPrefixes := []string{
		"gen_ai.request.model",
		"gen_ai.usage.input_tokens",
		"gen_ai.usage.output_tokens",
		"prompt_tokens",
		"completion_tokens",
		"gen_ai.prompt.",
		"gen_ai.completion.",
		"lantern.prompt.name",
		"lantern.prompt.version",
		"lantern.kind",
	}

	for _, kv := range attrs {
		skip := false
		for _, prefix := range consumedPrefixes {
			if strings.HasPrefix(kv.Key, prefix) {
				skip = true
				break
			}
		}
		if !skip {
			overflow[kv.Key] = protobuf.AnyValueToAny(kv.Value)
		}
	}

	return overflow
}


