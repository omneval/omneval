package handler

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/zbloss/lantern/internal/auth"
	handlers "github.com/zbloss/lantern/internal/handlers"
	"github.com/zbloss/lantern/internal/domain"
	"github.com/zbloss/lantern/services/ingest/internal/metrics"
)

// --- Types ---

// ValidatedKey holds the result of API key authentication.
type ValidatedKey = auth.ValidatedKey

// NativeSpan is the REST API request body for a single span.
type NativeSpan struct {
	SpanID        string         `json:"span_id,omitempty"`
	TraceID       string         `json:"trace_id,omitempty"`
	ParentID      string         `json:"parent_id,omitempty"`
	Name          string         `json:"name,omitempty"`
	Kind          string         `json:"kind,omitempty"`
	Model         string         `json:"model,omitempty"`
	Input         any            `json:"input,omitempty"`  // string or JSON array
	Output        any            `json:"output,omitempty"` // string or JSON array
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

// Validator authenticates raw API key strings and returns a ValidatedKey.
type Validator interface {
	Validate(ctx context.Context, rawKey string) (*ValidatedKey, error)
}

// SpanQueue pushes and pops span batches to/from the ingest queue.
type SpanQueue interface {
	Enqueue(ctx context.Context, spans []*domain.Span) error
}

// --- NativeHandler ---

// NativeHandler handles POST /api/v1/spans for the native Lantern REST format.
type NativeHandler struct {
	queue       SpanQueue
	validator   Validator
	corsOrigins []string
	metrics     *metrics.IngestMetrics
}

// NewNativeHandler creates a NativeHandler with optional CORS origins.
// Pass a non-empty corsOrigins slice to enable CORS middleware.
func NewNativeHandler(queue SpanQueue, validator Validator, corsOrigins []string, m *metrics.IngestMetrics) *NativeHandler {
	return &NativeHandler{queue: queue, validator: validator, corsOrigins: corsOrigins, metrics: m}
}

func (h *NativeHandler) Router() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/spans", h.handleIngest)
	if len(h.corsOrigins) > 0 {
		return newCORS(h.corsOrigins, mux)
	}
	return mux
}

// newCORS wraps a handler with CORS middleware.
func newCORS(origins []string, handler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodOptions {
			w.Header().Set("Access-Control-Allow-Origin", corsOrigin(origins, r))
			w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-API-Key")
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

func (h *NativeHandler) handleIngest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Authenticate via API key header
	rawKey := r.Header.Get("X-API-Key")
	if rawKey == "" {
		http.Error(w, "missing API key", http.StatusUnauthorized)
		return
	}

	vk, err := h.validator.Validate(r.Context(), rawKey)
	if err != nil {
		http.Error(w, fmt.Sprintf("invalid API key: %v", err), http.StatusUnauthorized)
		return
	}

	// Parse request body
	var req IngestRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("invalid request body: %v", err), http.StatusBadRequest)
		return
	}

	// Validate and normalize each span
	domainSpans := make([]*domain.Span, 0, len(req.Spans))
	for _, ns := range req.Spans {
		if err := h.validateAndTransform(ns, vk); err != nil {
			slog.Warn("ingest: validation error", "span_id", ns.SpanID, "error", err.Error())
			http.Error(w, fmt.Sprintf("invalid span: %v", err), http.StatusBadRequest)
			return
		}
		h.normalizeInputOutput(ns)
		domainSpans = append(domainSpans, nsToDomain(ns, vk))
	}

	// Enqueue to Redis
	if err := h.queue.Enqueue(r.Context(), domainSpans); err != nil {
		if h.metrics != nil {
			h.metrics.RecordEnqueueError()
		}
		slog.Error("ingest: enqueue failed", "error", err.Error())
		http.Error(w, "service unavailable", http.StatusServiceUnavailable)
		return
	}

	if h.metrics != nil {
		h.metrics.RecordSpan(vk.ProjectID, len(domainSpans))
	}

	remoteAddr := handlers.RemoteHost(r.RemoteAddr)
	slog.Info("ingest: accepted spans", "project_id", vk.ProjectID, "span_count", len(domainSpans), "remote_addr", remoteAddr)

	w.WriteHeader(http.StatusAccepted)
}

func (h *NativeHandler) validateAndTransform(ns *NativeSpan, vk *auth.ValidatedKey) error {
	// Validate span_id: exactly 8 hex bytes = 16 hex chars
	if ns.SpanID != "" {
		if len(ns.SpanID) != 16 {
			return fmt.Errorf("span_id must be a 16-character lowercase hex string (0-9, a-f), got %d characters", len(ns.SpanID))
		}
		if _, err := hex.DecodeString(ns.SpanID); err != nil {
			return fmt.Errorf("span_id must be a 16-character lowercase hex string (0-9, a-f)")
		}
	}

	// Validate trace_id: exactly 16 hex bytes = 32 hex chars
	if ns.TraceID != "" {
		if len(ns.TraceID) != 32 {
			return fmt.Errorf("trace_id must be a 32-character lowercase hex string (0-9, a-f), got %d characters", len(ns.TraceID))
		}
		if _, err := hex.DecodeString(ns.TraceID); err != nil {
			return fmt.Errorf("trace_id must be a 32-character lowercase hex string (0-9, a-f)")
		}
	}

	// Validate kind
	if ns.Kind != "" {
		switch domain.SpanKind(ns.Kind) {
		case domain.SpanKindLLM, domain.SpanKindTool, domain.SpanKindAgent,
			domain.SpanKindChain, domain.SpanKindInternal:
			// valid
		default:
			return fmt.Errorf("unknown span kind: %q", ns.Kind)
		}
	}

	return nil
}

func (h *NativeHandler) normalizeInputOutput(ns *NativeSpan) {
	ns.Input = normalizeMessageArray(ns.Input, "user")
	ns.Output = normalizeMessageArray(ns.Output, "assistant")
}

// normalizeMessageArray converts a plain string to a single-element JSON message array.
// If the value is already a JSON string (starts with [ or {), it passes through.
func normalizeMessageArray(v any, role string) any {
	switch val := v.(type) {
	case string:
		trimmed := val
		// If it looks like it's already JSON, return as-is
		if len(trimmed) > 0 && (trimmed[0] == '[' || trimmed[0] == '{') {
			return val
		}
		// Wrap plain string as a message with the given role
		msg := map[string]any{
			"role":    role,
			"content": val,
		}
		enc, _ := json.Marshal([]any{msg})
		return string(enc)
	default:
		// Already JSON or other type, pass through
		return v
	}
}

func nsToDomain(ns *NativeSpan, vk *auth.ValidatedKey) *domain.Span {
	// Normalize input/output before conversion
	var inputJSON, outputJSON string
	if ns.Input != nil {
		switch v := ns.Input.(type) {
		case string:
			inputJSON = v
		case []any:
			data, _ := json.Marshal(v)
			inputJSON = string(data)
		case map[string]any:
			data, _ := json.Marshal(v)
			inputJSON = string(data)
		default:
			inputJSON = fmt.Sprintf("%v", v)
		}
	}
	if ns.Output != nil {
		switch v := ns.Output.(type) {
		case string:
			outputJSON = v
		case []any:
			data, _ := json.Marshal(v)
			outputJSON = string(data)
		case map[string]any:
			data, _ := json.Marshal(v)
			outputJSON = string(data)
		default:
			outputJSON = fmt.Sprintf("%v", v)
		}
	}

	var kind domain.SpanKind
	if ns.Kind != "" {
		kind = domain.SpanKind(ns.Kind)
	}

	span := &domain.Span{
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
		Input:         inputJSON,
		Output:        outputJSON,
		InputTokens:   ns.InputTokens,
		OutputTokens:  ns.OutputTokens,
		PromptName:    ns.PromptName,
		PromptVersion: ns.PromptVersion,
		Attributes:    ns.Attributes,
	}

	return span
}
