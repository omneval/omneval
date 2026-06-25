package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/omneval/omneval/internal/auth"
	"github.com/omneval/omneval/internal/domain"
	"github.com/omneval/omneval/internal/normalizer"
)

// IngestAdapter is the unified interface for native ingest handling.
// Both the standalone ingest service and other consumers converge on
// this single implementation in internal/handlers.
type IngestAdapter interface {
	// Translate processes an incoming HTTP request: extracts and validates the
	// API key, parses the request body, validates the batch, and normalises
	// every span through the normalizer seam.
	// The caller does not need to know about keys, requests, or validation —
	// Translate absorbs the entire ingress pipeline.
	Translate(ctx context.Context, r *http.Request) ([]*domain.Span, error)
	// Route returns the HTTP handler that serves POST /api/v1/spans.
	Route() http.Handler
}

// NativeSpan is the REST API request body for a single span.
type NativeSpan struct {
	SpanID         string         `json:"span_id,omitempty"`
	TraceID        string         `json:"trace_id,omitempty"`
	ParentID       string         `json:"parent_id,omitempty"`
	ConversationID string         `json:"conversation_id,omitempty"`
	Name           string         `json:"name,omitempty"`
	Kind           string         `json:"kind,omitempty"`
	Model          string         `json:"model,omitempty"`
	Input          any            `json:"input,omitempty"`
	Output         any            `json:"output,omitempty"`
	InputTokens    int64          `json:"input_tokens,omitempty"`
	OutputTokens   int64          `json:"output_tokens,omitempty"`
	PromptName     string         `json:"prompt_name,omitempty"`
	PromptVersion  int64          `json:"prompt_version,omitempty"`
	Attributes     map[string]any `json:"attributes,omitempty"`
}

// IngestRequest is the JSON body of POST /api/v1/spans.
type IngestRequest struct {
	Spans []*NativeSpan `json:"spans"`
}

// IngestMetrics is an optional callback for recording ingest metrics.
type IngestMetrics interface {
	RecordSpan(projectID string, count int)
	RecordEnqueueError()
}

// IngestLogger is an optional callback for emitting structured ingest logs.
type IngestLogger interface {
	// LogAccepted is called after a batch is successfully enqueued.
	LogAccepted(projectID string, spanCount int, remoteAddr string)
	// LogValidationError is called when a span fails validation.
	LogValidationError(spanID string, err error)
	// LogEnqueueError is called when the queue rejects a batch.
	LogEnqueueError(err error)
}

// NativeHandler handles POST /api/v1/spans for the native Omneval REST format.
type NativeHandler struct {
	queue       SpanQueue
	validator   Validator
	corsOrigins []string
	normalizer  domain.SpanNormalizer
	metrics     IngestMetrics
	logger      IngestLogger
}

// NewNativeHandler creates a NativeHandler with optional CORS origins.
func NewNativeHandler(queue SpanQueue, validator Validator, corsOrigins []string) *NativeHandler {
	return &NativeHandler{queue: queue, validator: validator, corsOrigins: corsOrigins, normalizer: normalizer.New()}
}

// NewNativeHandlerWithMetrics creates a NativeHandler with optional CORS origins,
// metrics, and structured logging. Pass nil for metrics/logger to disable.
func NewNativeHandlerWithMetrics(queue SpanQueue, validator Validator, corsOrigins []string, m IngestMetrics, logger IngestLogger) *NativeHandler {
	return &NativeHandler{queue: queue, validator: validator, corsOrigins: corsOrigins, normalizer: normalizer.New(), metrics: m, logger: logger}
}

// Translate processes an incoming HTTP request: extracts and validates the API
// key, parses the request body, validates the batch, and normalises every span.
// Callers do not need to know about keys, requests, or validation — Translate
// absorbs the entire ingress pipeline and returns the normalised domain spans.
func (h *NativeHandler) Translate(ctx context.Context, r *http.Request) ([]*domain.Span, error) {
	rawKey := ExtractAPIKey(r)
	if rawKey == "" {
		return nil, fmt.Errorf("missing API key")
	}

	vk, err := h.validator.Validate(ctx, rawKey)
	if err != nil {
		return nil, fmt.Errorf("invalid API key: %w", err)
	}

	var req IngestRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return nil, fmt.Errorf("invalid request body: %w", err)
	}

	if len(req.Spans) == 0 {
		return nil, fmt.Errorf("spans array must not be empty")
	}

	domainSpans := make([]*domain.Span, 0, len(req.Spans))
	for _, ns := range req.Spans {
		raw := toRawMap(ns, vk)
		span, err := h.normalizer.Normalize(ctx, raw)
		if err != nil {
			return nil, fmt.Errorf("invalid span: %w", err)
		}
		domainSpans = append(domainSpans, span)
	}

	return domainSpans, nil
}

// Router returns the HTTP handler that serves POST /api/v1/spans.
// Kept for backward compatibility; use Route() to satisfy IngestAdapter.
func (h *NativeHandler) Router() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/spans", h.handleIngest)
	if len(h.corsOrigins) > 0 {
		return newCORS(h.corsOrigins, mux)
	}
	return mux
}

// Route satisfies the IngestAdapter interface.
func (h *NativeHandler) Route() http.Handler {
	return h.Router()
}

func (h *NativeHandler) handleIngest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	domainSpans, err := h.Translate(r.Context(), r)
	if err != nil {
		if strings.Contains(err.Error(), "API key") {
			http.Error(w, fmt.Sprintf("unauthorized: %v", err), http.StatusUnauthorized)
		} else {
			if h.logger != nil {
				h.logger.LogValidationError("", err)
			}
			http.Error(w, fmt.Sprintf("bad request: %v", err), http.StatusBadRequest)
		}
		return
	}

	now := time.Now()
	for _, span := range domainSpans {
		// Native spans don't carry client-supplied timestamps, so use
		// ingestion time for both StartTime and EndTime, overriding
		// whatever the normalizer derived from the (empty) raw map.
		span.StartTime = now
		span.EndTime = now
	}

	if err := h.queue.Enqueue(r.Context(), domainSpans); err != nil {
		if h.metrics != nil {
			h.metrics.RecordEnqueueError()
		}
		if h.logger != nil {
			h.logger.LogEnqueueError(err)
		}
		http.Error(w, "service unavailable", http.StatusServiceUnavailable)
		return
	}

	if h.metrics != nil {
		// Extract project ID from the first span.
		if len(domainSpans) > 0 {
			h.metrics.RecordSpan(domainSpans[0].ProjectID, len(domainSpans))
		}
	}

	if h.logger != nil {
		if len(domainSpans) > 0 {
			h.logger.LogAccepted(domainSpans[0].ProjectID, len(domainSpans), r.RemoteAddr)
		}
	}

	w.WriteHeader(http.StatusAccepted)
}

// toRawMap converts a NativeSpan to a raw map for the SpanNormalizer,
// injecting project_id and service_name from the validated key.
func toRawMap(ns *NativeSpan, vk *auth.ValidatedKey) map[string]any {
	raw := map[string]any{
		"span_id":        ns.SpanID,
		"trace_id":       ns.TraceID,
		"project_id":     vk.ProjectID,
		"service_name":   vk.ServiceName,
		"name":           ns.Name,
		"model":          ns.Model,
		"input_tokens":   ns.InputTokens,
		"output_tokens":  ns.OutputTokens,
		"prompt_name":    ns.PromptName,
		"prompt_version": ns.PromptVersion,
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
