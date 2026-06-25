package handlers

import (
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"

	coltracev1 "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	"github.com/omneval/omneval/internal/auth"
	"github.com/omneval/omneval/internal/domain"
	"github.com/omneval/omneval/internal/normalizer"
)

// SpanQueue pushes span batches to the ingest queue.
type SpanQueue interface {
	Enqueue(ctx context.Context, spans []*domain.Span) error
}

// Validator authenticates raw API key strings and returns a ValidatedKey.
type Validator interface {
	Validate(ctx context.Context, rawKey string) (*auth.ValidatedKey, error)
}

// OTLPHandler handles POST /v1/traces for OTLP-encoded traces.
// Accepts Content-Type: application/x-protobuf or application/json,
// with optional gzip compression via Content-Encoding: gzip.
type OTLPHandler struct {
	queue      SpanQueue
	validator  Validator
	normalizer domain.SpanNormalizer
}

// NewOTLPHandler creates a new OTLPHandler.
func NewOTLPHandler(queue SpanQueue, validator Validator) *OTLPHandler {
	return &OTLPHandler{queue: queue, validator: validator, normalizer: normalizer.New()}
}

// Router creates an HTTP handler for OTLP trace ingestion.
func (h *OTLPHandler) Router() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/traces", h.handleOTLPTraces)
	return mux
}

func (h *OTLPHandler) handleOTLPTraces(w http.ResponseWriter, r *http.Request) {
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

	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "read body failed", http.StatusBadRequest)
		return
	}

	// Decompress gzip-encoded bodies — OTLP spec allows gzip compression
	// and the Go SDK uses otlptracehttp.GzipCompression by default.
	contentEncoding := r.Header.Get("Content-Encoding")
	bodyBytes, err = decompressIfNeeded(contentEncoding, bodyBytes)
	if err != nil {
		http.Error(w, fmt.Sprintf("decompress body: %v", err), http.StatusBadRequest)
		return
	}

	contentType := r.Header.Get("Content-Type")
	var req *coltracev1.ExportTraceServiceRequest
	switch contentType {
	case "application/x-protobuf":
		req = new(coltracev1.ExportTraceServiceRequest)
		if err := proto.Unmarshal(bodyBytes, req); err != nil {
			http.Error(w, fmt.Sprintf("invalid protobuf: %v", err), http.StatusBadRequest)
			return
		}
	case "application/json":
		req = new(coltracev1.ExportTraceServiceRequest)
		jsonOpts := protojson.UnmarshalOptions{DiscardUnknown: true}
		if err := jsonOpts.Unmarshal(bodyBytes, req); err != nil {
			http.Error(w, fmt.Sprintf("invalid JSON: %v", err), http.StatusBadRequest)
			return
		}
	default:
		http.Error(w, "unsupported content type: use application/json or application/x-protobuf", http.StatusBadRequest)
		return
	}

	normOpts := normalizer.Options{
		ServiceNameOverride: vk.ServiceName,
	}
	domainSpans, err := normalizer.NormalizeOTLP(r.Context(), vk.ProjectID, req.ResourceSpans, normOpts, h.normalizer)
	if err != nil {
		slog.Error("ingest: normalize failed", "error", err.Error())
		http.Error(w, fmt.Sprintf("normalize: %v", err), http.StatusInternalServerError)
		return
	}

	if err := h.queue.Enqueue(r.Context(), domainSpans); err != nil {
		slog.Error("ingest: enqueue failed", "error", err.Error())
		http.Error(w, "service unavailable", http.StatusServiceUnavailable)
		return
	}

	remoteAddr := RemoteHost(r.RemoteAddr)
	slog.Info("ingest: accepted spans", "project_id", vk.ProjectID, "span_count", len(domainSpans), "remote_addr", remoteAddr)

	w.Header().Set("Content-Type", contentType)
	w.WriteHeader(http.StatusAccepted)
}

// RemoteHost extracts the host part from a remote address string,
// stripping the port if present.
func RemoteHost(remoteAddr string) string {
	if host, _, err := net.SplitHostPort(remoteAddr); err == nil {
		return host
	}
	return remoteAddr
}

// decompressIfNeeded checks the Content-Encoding header and decompresses
// gzip-encoded bodies. It returns the original bytes unchanged if no
// Content-Encoding header is set or if it is not gzip.
func decompressIfNeeded(contentEncoding string, data []byte) ([]byte, error) {
	if contentEncoding == "" {
		return data, nil
	}
	if !strings.EqualFold(contentEncoding, "gzip") {
		return data, nil
	}

	gr, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("create gzip reader: %w", err)
	}
	defer gr.Close()

	decompressed, err := io.ReadAll(gr)
	if err != nil {
		return nil, fmt.Errorf("read gzip body: %w", err)
	}
	return decompressed, nil
}
