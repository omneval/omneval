package handlers

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/omneval/omneval/internal/auth"
	"github.com/omneval/omneval/internal/domain"
)

// NativeSpan is the REST API request body for a single span.
type NativeSpan struct {
	SpanID        string         `json:"span_id,omitempty"`
	TraceID       string         `json:"trace_id,omitempty"`
	ParentID      string         `json:"parent_id,omitempty"`
	Name          string         `json:"name,omitempty"`
	Kind          string         `json:"kind,omitempty"`
	Model         string         `json:"model,omitempty"`
	Input         any            `json:"input,omitempty"`
	Output        any            `json:"output,omitempty"`
	InputTokens   int64          `json:"input_tokens,omitempty"`
	OutputTokens  int64          `json:"output_tokens,omitempty"`
	PromptName    string         `json:"prompt_name,omitempty"`
	PromptVersion int64          `json:"prompt_version,omitempty"`
	Attributes    map[string]any `json:"attributes,omitempty"`
}

// IngestRequest is the JSON body of POST /api/v1/spans.
type IngestRequest struct {
	Spans []*NativeSpan `json:"spans"`
}

// NativeHandler handles POST /api/v1/spans for the native Omneval REST format.
type NativeHandler struct {
	queue       SpanQueue
	validator   Validator
	corsOrigins []string
}

// NewNativeHandler creates a NativeHandler with optional CORS origins.
func NewNativeHandler(queue SpanQueue, validator Validator, corsOrigins []string) *NativeHandler {
	return &NativeHandler{queue: queue, validator: validator, corsOrigins: corsOrigins}
}

func (h *NativeHandler) Router() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/spans", h.handleIngest)
	if len(h.corsOrigins) > 0 {
		return newCORS(h.corsOrigins, mux)
	}
	return mux
}

func (h *NativeHandler) handleIngest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	rawKey := ExtractAPIKey(r)
	if rawKey == "" {
		http.Error(w, "missing API key", http.StatusUnauthorized)
		return
	}

	vk, err := h.validator.Validate(r.Context(), rawKey)
	if err != nil {
		http.Error(w, fmt.Sprintf("invalid API key: %v", err), http.StatusUnauthorized)
		return
	}

	var req IngestRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("invalid request body: %v", err), http.StatusBadRequest)
		return
	}

	domainSpans := make([]*domain.Span, 0, len(req.Spans))
	for _, ns := range req.Spans {
		if err := h.validateAndTransform(ns, vk); err != nil {
			http.Error(w, fmt.Sprintf("invalid span: %v", err), http.StatusBadRequest)
			return
		}
		h.normalizeInputOutput(ns)
		domainSpans = append(domainSpans, nsToDomain(ns, vk))
	}

	if err := h.queue.Enqueue(r.Context(), domainSpans); err != nil {
		http.Error(w, "service unavailable", http.StatusServiceUnavailable)
		return
	}

	w.WriteHeader(http.StatusAccepted)
}

func (h *NativeHandler) validateAndTransform(ns *NativeSpan, vk *auth.ValidatedKey) error {
	if err := validateHexID("span_id", ns.SpanID, 16); err != nil {
		return err
	}
	if err := validateHexID("trace_id", ns.TraceID, 32); err != nil {
		return err
	}
	if ns.Kind != "" && !isValidSpanKind(ns.Kind) {
		return fmt.Errorf("unknown span kind: %q", ns.Kind)
	}
	return nil
}

func validateHexID(fieldName, value string, expectedLen int) error {
	if value == "" {
		return nil
	}

	if len(value) != expectedLen {
		return fmt.Errorf("%s must be %d hex characters, got %d", fieldName, expectedLen, len(value))
	}
	if _, err := hex.DecodeString(value); err != nil {
		return fmt.Errorf("%s is not valid hex: %v", fieldName, err)
	}
	return nil
}

func isValidSpanKind(kind string) bool {
	switch domain.SpanKind(kind) {
	case domain.SpanKindLLM, domain.SpanKindTool, domain.SpanKindAgent,
		domain.SpanKindChain, domain.SpanKindInternal:
		return true
	}
	return false
}

func (h *NativeHandler) normalizeInputOutput(ns *NativeSpan) {
	ns.Input = normalizeMessageArray(ns.Input, "user")
	ns.Output = normalizeMessageArray(ns.Output, "assistant")
}

func normalizeMessageArray(v any, role string) any {
	switch val := v.(type) {
	case string:
		trimmed := val
		if len(trimmed) > 0 && (trimmed[0] == '[' || trimmed[0] == '{') {
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

func nsToDomain(ns *NativeSpan, vk *auth.ValidatedKey) *domain.Span {
	var kind domain.SpanKind
	if ns.Kind != "" {
		kind = domain.SpanKind(ns.Kind)
	}

	return &domain.Span{
		SpanID:        ns.SpanID,
		TraceID:       ns.TraceID,
		ParentID:      ns.ParentID,
		ProjectID:     vk.ProjectID,
		ServiceName:   vk.ServiceName,
		Name:          ns.Name,
		Kind:          kind,
		StartTime:     time.Now(),
		EndTime:       time.Now(),
		Model:         ns.Model,
		Input:         spanValueToString(ns.Input),
		Output:        spanValueToString(ns.Output),
		InputTokens:   ns.InputTokens,
		OutputTokens:  ns.OutputTokens,
		PromptName:    ns.PromptName,
		PromptVersion: ns.PromptVersion,
		Attributes:    ns.Attributes,
	}
}

// Simple CORS middleware (extracted from ingest service for reuse).
func newCORS(origins []string, handler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodOptions {
			w.Header().Set("Access-Control-Allow-Origin", corsOrigin(origins, r))
			w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-API-Key, Authorization")
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.Header().Set("Access-Control-Allow-Origin", corsOrigin(origins, r))
		handler.ServeHTTP(w, r)
	})
}

func corsOrigin(origins []string, r *http.Request) string {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return ""
	}
	for _, o := range origins {
		if o == "*" {
			return "*"
		}
		if o == origin {
			return origin
		}
	}
	return ""
}

// spanValueToString converts a span input or output value to a JSON string.
// Strings pass through directly; slices and maps are marshaled; everything else
// is converted via fmt.Sprintf.
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
