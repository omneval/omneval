package handlers

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	coltracev1 "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	commonv1 "go.opentelemetry.io/proto/otlp/common/v1"
	resourcev1 "go.opentelemetry.io/proto/otlp/resource/v1"
	tracev1 "go.opentelemetry.io/proto/otlp/trace/v1"

	"github.com/omneval/omneval/internal/auth"
	"github.com/omneval/omneval/internal/domain"
	"github.com/omneval/omneval/internal/otlp"
	"google.golang.org/protobuf/proto"
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
	queue     SpanQueue
	validator Validator
}

// NewOTLPHandler creates a new OTLPHandler.
func NewOTLPHandler(queue SpanQueue, validator Validator) *OTLPHandler {
	return &OTLPHandler{queue: queue, validator: validator}
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
		if err := json.Unmarshal(bodyBytes, req); err != nil {
			http.Error(w, fmt.Sprintf("invalid JSON: %v", err), http.StatusBadRequest)
			return
		}
	default:
		http.Error(w, "unsupported content type: use application/json or application/x-protobuf", http.StatusBadRequest)
		return
	}

	rss := convertToResourceSpans(req)

	opts := otlp.Options{
		ServiceNameOverride: vk.ServiceName,
	}
	domainSpans, err := otlp.Translate(vk.ProjectID, rss, opts)
	if err != nil {
		slog.Error("ingest: translate failed", "error", err.Error())
		http.Error(w, fmt.Sprintf("translate: %v", err), http.StatusInternalServerError)
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

func convertToResourceSpans(req *coltracev1.ExportTraceServiceRequest) []otlp.ResourceSpans {
	result := make([]otlp.ResourceSpans, 0, len(req.ResourceSpans))
	for _, rs := range req.ResourceSpans {
		resource := otlp.Resource{
			Attributes: resourceAttrs(rs.GetResource()),
		}
		spans := make([]*otlp.Span, 0)
		for _, ss := range rs.GetScopeSpans() {
			for _, s := range ss.GetSpans() {
				spans = append(spans, &otlp.Span{
					SpanID:     fmt.Sprintf("%x", s.GetSpanId()),
					TraceID:    fmt.Sprintf("%x", s.GetTraceId()),
					ParentID:   fmt.Sprintf("%x", s.GetParentSpanId()),
					Name:       s.GetName(),
					StartTime:  unixNanoToTime(s.GetStartTimeUnixNano()),
					EndTime:    unixNanoToTime(s.GetEndTimeUnixNano()),
					StatusCode: statusToCode(s.GetStatus()),
					StatusMsg:  statusToMessage(s.GetStatus()),
					Attributes: spanAttrs(s.GetAttributes()),
				})
			}
		}
		result = append(result, otlp.ResourceSpans{
			Resource: resource,
			Spans:    spans,
		})
	}
	return result
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
		return "unset"
	case tracev1.Status_STATUS_CODE_OK:
		return "ok"
	case tracev1.Status_STATUS_CODE_ERROR:
		return "error"
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

func resourceAttrs(res *resourcev1.Resource) map[string]any {
	if res == nil {
		return nil
	}
	return kvListToMap(res.GetAttributes())
}

func spanAttrs(attrs []*commonv1.KeyValue) map[string]any {
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
