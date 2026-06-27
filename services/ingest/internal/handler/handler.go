package handler

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/omneval/omneval/internal/auth"
	"github.com/omneval/omneval/internal/domain"
	"github.com/omneval/omneval/internal/handlers"
	"github.com/omneval/omneval/services/ingest/internal/metrics"
)

// Re-export canonical types from internal/handlers so existing consumers do
// not need to change their import paths.
type (
	NativeSpan    = handlers.NativeSpan
	IngestRequest = handlers.IngestRequest
	SpanQueue     = handlers.SpanQueue
)

// --- Thin dispatcher that adds metrics and logging ---

// NativeHandler is a thin dispatcher around handlers.NativeHandler that adds
// metrics recording and structured logging.  All span normalisation logic is
// delegated to the canonical implementation in internal/handlers.
type NativeHandler struct {
	base  *handlers.NativeHandler
	m     *metrics.IngestMetrics
}

// NewNativeHandler creates a NativeHandler backed by the canonical internal
// implementation with metrics and logging added on top.
func NewNativeHandler(queue SpanQueue, validator auth.Validator, corsOrigins []string, m *metrics.IngestMetrics) *NativeHandler {
	slogAdapter := &slogAdapter{}
	base := handlers.NewNativeHandlerWithMetrics(queue, validator, corsOrigins, metricsAdapter{m}, slogAdapter)
	return &NativeHandler{base: base, m: m}
}

// slogAdapter bridges slog to handlers.IngestLogger.
type slogAdapter struct{}

func (s *slogAdapter) LogAccepted(projectID string, spanCount int, remoteAddr string) {
	slog.Default().Info("ingest: accepted spans", "project_id", projectID, "span_count", spanCount, "remote_addr", remoteAddr)
}

func (s *slogAdapter) LogValidationError(spanID string, err error) {
	slog.Default().Warn("ingest: validation error", "span_id", spanID, "error", err.Error())
}

func (s *slogAdapter) LogEnqueueError(err error) {
	slog.Default().Error("ingest: enqueue failed", "error", err.Error())
}

// metricsAdapter bridges metrics.IngestMetrics to handlers.IngestMetrics.
type metricsAdapter struct {
	m *metrics.IngestMetrics
}

func (ma metricsAdapter) RecordSpan(projectID string, count int) {
	if ma.m != nil {
		ma.m.RecordSpan(projectID, count)
	}
}

func (ma metricsAdapter) RecordEnqueueError() {
	if ma.m != nil {
		ma.m.RecordEnqueueError()
	}
}

// Router returns the HTTP handler with CORS middleware and metrics.
// Structured logging is handled by the IngestLogger injected into the base.
func (h *NativeHandler) Router() http.Handler {
	return h.base.Router()
}

// Translate delegates to the canonical implementation.
func (h *NativeHandler) Translate(ctx context.Context, r *http.Request) (*domain.Span, error) {
	return h.base.Translate(ctx, r)
}

// Route satisfies the IngestAdapter interface by delegating to the base handler.
func (h *NativeHandler) Route() http.Handler {
	return h.base.Route()
}