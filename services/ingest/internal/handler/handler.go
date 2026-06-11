package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/omneval/omneval/internal/auth"
	"github.com/omneval/omneval/internal/domain"
	"github.com/omneval/omneval/internal/normalizer"
	handlers "github.com/omneval/omneval/internal/handlers"
	"github.com/omneval/omneval/services/ingest/internal/metrics"
)

// --- Types ---

// ValidatedKey holds the result of API key authentication.
type ValidatedKey = auth.ValidatedKey

// NativeSpan is the REST API request body for a single span.
type NativeSpan struct {
	SpanID          string         `json:"span_id,omitempty"`
	TraceID         string         `json:"trace_id,omitempty"`
	ParentID        string         `json:"parent_id,omitempty"`
	ConversationID  string         `json:"conversation_id,omitempty"`
	Name            string         `json:"name,omitempty"`
	Kind            string         `json:"kind,omitempty"`
	Model           string         `json:"model,omitempty"`
	Input           any            `json:"input,omitempty"`  // string or JSON array
	Output          any            `json:"output,omitempty"` // string or JSON array
	InputTokens     int64          `json:"input_tokens,omitempty"`
	OutputTokens    int64          `json:"output_tokens,omitempty"`
	PromptName      string         `json:"prompt_name,omitempty"`
	PromptVersion   int64          `json:"prompt_version,omitempty"`
	Attributes      map[string]any `json:"attributes,omitempty"`
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

// NativeHandler handles POST /api/v1/spans for the native Omneval REST format.
type NativeHandler struct {
	queue       SpanQueue
	validator   Validator
	corsOrigins []string
	metrics     *metrics.IngestMetrics
	normalizer  domain.SpanNormalizer
}

// NewNativeHandler creates a NativeHandler with optional CORS origins.
// Pass a non-empty corsOrigins slice to enable CORS middleware.
func NewNativeHandler(queue SpanQueue, validator Validator, corsOrigins []string, m *metrics.IngestMetrics) *NativeHandler {
	return &NativeHandler{queue: queue, validator: validator, corsOrigins: corsOrigins, metrics: m, normalizer: normalizer.New()}
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

	// Reject empty batches
	if len(req.Spans) == 0 {
		http.Error(w, "spans array must not be empty", http.StatusBadRequest)
		return
	}

	// Normalize each span through the SpanNormalizer seam
	now := time.Now()
	domainSpans := make([]*domain.Span, 0, len(req.Spans))
	for _, ns := range req.Spans {
		raw := toRawMap(ns, vk)
		span, err := h.normalizer.Normalize(r.Context(), raw)
		if err != nil {
			slog.Warn("ingest: validation error", "span_id", ns.SpanID, "error", err.Error())
			http.Error(w, fmt.Sprintf("invalid span: %v", err), http.StatusBadRequest)
			return
		}
		span.StartTime = now
		span.EndTime = now
		domainSpans = append(domainSpans, span)
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

// toRawMap converts a NativeSpan to a raw map for the SpanNormalizer,
// injecting project_id and service_name from the validated key.
func toRawMap(ns *NativeSpan, vk *auth.ValidatedKey) map[string]any {
	raw := map[string]any{
		"span_id":         ns.SpanID,
		"trace_id":        ns.TraceID,
		"project_id":      vk.ProjectID,
		"service_name":    vk.ServiceName,
		"name":            ns.Name,
		"model":           ns.Model,
		"input_tokens":    ns.InputTokens,
		"output_tokens":   ns.OutputTokens,
		"prompt_name":     ns.PromptName,
		"prompt_version":  ns.PromptVersion,
	}
	if ns.ParentID != "" {
		raw["parent_id"] = ns.ParentID
	}
	if ns.ConversationID != "" {
		raw["conversation_id"] = ns.ConversationID
	}
	if ns.Kind != "" {
		raw["kind"] = ns.Kind
	}
	if ns.Input != nil {
		raw["input"] = ns.Input
	}
	if ns.Output != nil {
		raw["output"] = ns.Output
	}
	if len(ns.Attributes) > 0 {
		raw["attributes"] = ns.Attributes
	}
	return raw
}
