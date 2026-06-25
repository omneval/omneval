package normalizer

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	commonv1 "go.opentelemetry.io/proto/otlp/common/v1"
	resourcev1 "go.opentelemetry.io/proto/otlp/resource/v1"
	tracev1 "go.opentelemetry.io/proto/otlp/trace/v1"

	"github.com/omneval/omneval/internal/domain"
)

// Options controls normalizer behaviour that varies by deployment config.
type Options struct {
	// LogSystemPrompt controls whether the system prompt is included as the
	// first element of a span's Input array.
	LogSystemPrompt bool
	// ServiceNameOverride is non-empty when the ingest request was
	// authenticated with a service-scoped API key; it takes precedence over
	// the resource-level service.name attribute.
	ServiceNameOverride string
}

// NormalizeOTLP converts OTLP protobuf ResourceSpans into domain.Span values
// by delegating to the provided normalizer. It handles all OTLP-specific
// field extraction, type coercion, and validation so that the OTLP handler
// does not duplicate this logic.
func NormalizeOTLP(ctx context.Context, projectID string, rss []*tracev1.ResourceSpans, opts Options, normalizer domain.SpanNormalizer) ([]*domain.Span, error) {
	spans := make([]*domain.Span, 0)
	for _, rs := range rss {
		resourceAttrs := resourceAttrMap(rs.GetResource())
		serviceName := extractResourceAttributeString(resourceAttrs, "service.name")
		if opts.ServiceNameOverride != "" {
			serviceName = opts.ServiceNameOverride
		}

		for _, ss := range rs.GetScopeSpans() {
			for _, s := range ss.GetSpans() {
				raw := otlpSpanToRawMap(projectID, resourceAttrs, serviceName, s, opts)
				span, err := normalizer.Normalize(ctx, raw)
				if err != nil {
					return nil, fmt.Errorf("normalize otlp span %s: %w", hexEncode(s.GetSpanId()), err)
				}
				spans = append(spans, span)
			}
		}
	}
	return spans, nil
}

// --- OTLP protobuf conversion helpers ---

func otlpSpanToRawMap(projectID string, resourceAttrs map[string]any, serviceName string, span *tracev1.Span, opts Options) map[string]any {
	attrs := spanAttrMap(span.GetAttributes())

	model := extractAttributeString(attrs, "gen_ai.request.model")
	if model == "" {
		model = extractAttributeString(attrs, "llm.request.model")
	}

	inputTokens := extractAttributeInt64(attrs, "gen_ai.usage.input_tokens")
	if inputTokens == -1 {
		inputTokens = extractAttributeInt64(attrs, "prompt_tokens")
	}
	outputTokens := extractAttributeInt64(attrs, "gen_ai.usage.output_tokens")
	if outputTokens == -1 {
		outputTokens = extractAttributeInt64(attrs, "completion_tokens")
	}
	if inputTokens < 0 {
		inputTokens = 0
	}
	if outputTokens < 0 {
		outputTokens = 0
	}

	input := buildMessageArray(attrs, "gen_ai.prompt")
	if input == "" {
		input = buildMessageArray(attrs, "llm.prompt")
	}
	if input == "" {
		input = extractMessagesJSON(attrs, "gen_ai.input.messages")
	}
	if input == "" {
		input = extractAttributeString(attrs, "input.value")
	}
	if input == "" {
		input = extractAttributeString(attrs, "omneval.input")
	}

	output := buildMessageArray(attrs, "gen_ai.completion")
	if output == "" {
		output = buildMessageArray(attrs, "llm.completion")
	}
	if output == "" {
		output = extractMessagesJSON(attrs, "gen_ai.output.messages")
	}
	if output == "" {
		output = extractAttributeString(attrs, "output.value")
	}
	if output == "" {
		output = extractAttributeString(attrs, "omneval.output")
	}

	kind := deriveKind(attrs, span.GetName())

	overflow := buildOverflowAttributes(attrs, model, inputTokens, outputTokens, input, output, kind, serviceName)

	promptName, promptVersion := resolvePromptInfo(attrs)

	conversationID := extractAttributeString(attrs, "gen_ai.conversation.id")
	if conversationID == "" {
		conversationID = extractAttributeString(attrs, "omneval.conversation.id")
	}

	raw := map[string]any{
		"span_id":        hexEncode(span.GetSpanId()),
		"trace_id":       hexEncode(span.GetTraceId()),
		"name":           span.GetName(),
		"project_id":     projectID,
		"service_name":   serviceName,
		"model":          model,
		"input":          input,
		"output":         output,
		"input_tokens":   inputTokens,
		"output_tokens":  outputTokens,
		"prompt_name":    promptName,
		"prompt_version": promptVersion,
		"kind":           kind,
	}
	if parentID := hexEncode(span.GetParentSpanId()); parentID != "" {
		raw["parent_id"] = parentID
	}
	if conversationID != "" {
		raw["conversation_id"] = conversationID
	}
	if startTime := unixNanoToTime(span.GetStartTimeUnixNano()); !startTime.IsZero() {
		raw["start_time"] = startTime
	}
	if endTime := unixNanoToTime(span.GetEndTimeUnixNano()); !endTime.IsZero() {
		raw["end_time"] = endTime
	}
	if statusCode := statusToCode(span.GetStatus()); statusCode != "" {
		raw["status_code"] = statusCode
	}
	if statusMsg := statusToMessage(span.GetStatus()); statusMsg != "" {
		raw["status_message"] = statusMsg
	}
	if len(overflow) > 0 {
		raw["attributes"] = overflow
	}
	return raw
}

func hexEncode(b []byte) string {
	return fmt.Sprintf("%x", b)
}

func unixNanoToTime(nano uint64) time.Time {
	if nano == 0 {
		return time.Time{}
	}
	sec := nano / 1_000_000_000
	nsec := nano % 1_000_000_000
	return time.Unix(int64(sec), int64(nsec)).UTC()
}

func statusToCode(status *tracev1.Status) string {
	if status == nil {
		return ""
	}
	switch status.GetCode() {
	case tracev1.Status_STATUS_CODE_UNSET:
		return "UNSET"
	case tracev1.Status_STATUS_CODE_OK:
		return "OK"
	case tracev1.Status_STATUS_CODE_ERROR:
		return "ERROR"
	default:
		return fmt.Sprintf("%d", status.GetCode())
	}
}

func statusToMessage(status *tracev1.Status) string {
	if status == nil {
		return ""
	}
	return status.GetMessage()
}

func resourceAttrMap(res *resourcev1.Resource) map[string]any {
	if res == nil {
		return nil
	}
	return kvListToMap(res.GetAttributes())
}

func spanAttrMap(attrs []*commonv1.KeyValue) map[string]any {
	return kvListToMap(attrs)
}

func kvListToMap(attrs []*commonv1.KeyValue) map[string]any {
	if len(attrs) == 0 {
		return nil
	}
	result := make(map[string]any, len(attrs))
	for _, kv := range attrs {
		result[kv.GetKey()] = anyValue(kv.GetValue())
	}
	return result
}

func anyValue(v *commonv1.AnyValue) any {
	if v == nil {
		return nil
	}
	switch val := v.Value.(type) {
	case *commonv1.AnyValue_StringValue:
		return val.StringValue
	case *commonv1.AnyValue_BoolValue:
		return val.BoolValue
	case *commonv1.AnyValue_IntValue:
		return val.IntValue
	case *commonv1.AnyValue_DoubleValue:
		return val.DoubleValue
	case *commonv1.AnyValue_BytesValue:
		return val.BytesValue
	case *commonv1.AnyValue_ArrayValue:
		arr := make([]any, 0, len(val.ArrayValue.Values))
		for _, item := range val.ArrayValue.Values {
			arr = append(arr, anyValue(item))
		}
		return arr
	case *commonv1.AnyValue_KvlistValue:
		kv := make(map[string]any, len(val.KvlistValue.Values))
		for _, item := range val.KvlistValue.Values {
			kv[item.Key] = anyValue(item.Value)
		}
		return kv
	default:
		return nil
	}
}

// --- Domain conversion helpers (from otlp/translator.go) ---

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

func extractAttributeInt64(attrs map[string]any, key string) int64 {
	v, ok := attrs[key]
	if !ok {
		return -1
	}
	switch val := v.(type) {
	case int64:
		return val
	case float64:
		return int64(val)
	case int:
		return int64(val)
	default:
		return -1
	}
}

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

func buildMessageArray(attrs map[string]any, prefix string) string {
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

func extractMessagesJSON(attrs map[string]any, key string) string {
	v, ok := attrs[key]
	if !ok {
		return ""
	}
	switch val := v.(type) {
	case string:
		trimmed := val
		if len(trimmed) == 0 {
			return ""
		}
		var arr []any
		if err := json.Unmarshal([]byte(trimmed), &arr); err != nil || len(arr) == 0 {
			return ""
		}
		return trimmed
	case []any:
		if len(val) == 0 {
			return ""
		}
		data, err := json.Marshal(val)
		if err != nil {
			return ""
		}
		return string(data)
	default:
		return ""
	}
}

func extractNumberedIndex(key, prefix string) (int, bool) {
	if len(key) <= len(prefix)+1 || key[:len(prefix)+1] != prefix+"." {
		return 0, false
	}
	rest := key[len(prefix)+1:]
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

func deriveKind(attrs map[string]any, name string) domain.SpanKind {
	if kind := extractAttributeString(attrs, "omneval.kind"); kind != "" {
		if dk := domain.SpanKind(kind); dk == domain.SpanKindLLM || dk == domain.SpanKindTool ||
			dk == domain.SpanKindAgent || dk == domain.SpanKindChain || dk == domain.SpanKindInternal {
			return dk
		}
	}

	if oiKind := extractAttributeString(attrs, "openinference.span.kind"); oiKind != "" {
		switch strings.ToUpper(oiKind) {
		case "AGENT":
			return domain.SpanKindAgent
		case "LLM":
			return domain.SpanKindLLM
		case "TOOL":
			return domain.SpanKindTool
		case "CHAIN", "RETRIEVER", "EMBEDDING", "RERANKER", "GUARDRAIL":
			return domain.SpanKindChain
		}
	}

	if op := extractAttributeString(attrs, "gen_ai.operation.name"); op != "" {
		switch strings.ToLower(op) {
		case "invoke_agent", "create_agent":
			return domain.SpanKindAgent
		case "execute_tool":
			return domain.SpanKindTool
		case "chat", "text_completion", "embeddings", "generate_content":
			return domain.SpanKindLLM
		}
	}

	if _, hasGenAI := attrs["gen_ai.request.model"]; hasGenAI {
		return domain.SpanKindLLM
	}
	if _, hasGenAIPrompt := attrs["gen_ai.prompt"]; hasGenAIPrompt {
		return domain.SpanKindLLM
	}

	if _, hasLLMModel := attrs["llm.request.model"]; hasLLMModel {
		return domain.SpanKindLLM
	}

	if _, hasToolCall := attrs["tool_call"]; hasToolCall {
		return domain.SpanKindTool
	}
	if _, hasToolName := attrs["tool.name"]; hasToolName {
		return domain.SpanKindTool
	}

	lowerName := strings.ToLower(name)
	switch {
	case strings.HasSuffix(lowerName, ".step") || strings.Contains(lowerName, "agent"):
		return domain.SpanKindAgent
	case strings.HasSuffix(name, "Action") || strings.Contains(lowerName, "tool"):
		return domain.SpanKindTool
	}

	return domain.SpanKindInternal
}

func buildOverflowAttributes(attrs map[string]any, model string, inputTokens, outputTokens int64, input, output string, kind domain.SpanKind, serviceName string) map[string]any {
	remove := make(map[string]bool)

	for k := range attrs {
		if k == "gen_ai.request.model" || k == "llm.request.model" {
			remove[k] = true
		}
	}

	for k := range attrs {
		if k == "gen_ai.usage.input_tokens" || k == "prompt_tokens" {
			remove[k] = true
		}
		if k == "gen_ai.usage.output_tokens" || k == "completion_tokens" {
			remove[k] = true
		}
	}

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

	if _, ok := attrs["omneval.input"]; ok {
		remove["omneval.input"] = true
	}
	if _, ok := attrs["omneval.output"]; ok {
		remove["omneval.output"] = true
	}

	if _, ok := attrs["gen_ai.input.messages"]; ok {
		remove["gen_ai.input.messages"] = true
	}
	if _, ok := attrs["gen_ai.output.messages"]; ok {
		remove["gen_ai.output.messages"] = true
	}

	if _, ok := attrs["input.value"]; ok {
		remove["input.value"] = true
	}
	if _, ok := attrs["output.value"]; ok {
		remove["output.value"] = true
	}

	if _, hasKind := attrs["omneval.kind"]; hasKind {
		remove["omneval.kind"] = true
	}

	if _, hasServiceName := attrs["service.name"]; hasServiceName {
		remove["service.name"] = true
	}

	if _, hasConvID := attrs["gen_ai.conversation.id"]; hasConvID {
		remove["gen_ai.conversation.id"] = true
	}
	if _, hasOmnevalConvID := attrs["omneval.conversation.id"]; hasOmnevalConvID {
		remove["omneval.conversation.id"] = true
	}

	overflow := make(map[string]any, len(attrs)-len(remove))
	for k, v := range attrs {
		if !remove[k] {
			overflow[k] = v
		}
	}
	return overflow
}

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