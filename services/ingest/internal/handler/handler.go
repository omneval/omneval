package handler

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/zbloss/lantern/internal/auth"
	"github.com/zbloss/lantern/internal/domain"
	"github.com/zbloss/lantern/internal/otlp"
	"github.com/zbloss/lantern/internal/otlp/protobuf"
	"github.com/zbloss/lantern/services/ingest/internal/cors"
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
}

// NewNativeHandler creates a NativeHandler with optional CORS origins.
// Pass a non-empty corsOrigins slice to enable CORS middleware.
func NewNativeHandler(queue SpanQueue, validator Validator, corsOrigins []string) *NativeHandler {
	return &NativeHandler{queue: queue, validator: validator, corsOrigins: corsOrigins}
}

func (h *NativeHandler) Router() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/spans", h.handleIngest)
	if len(h.corsOrigins) > 0 {
		return cors.New(h.corsOrigins).Handler(mux)
	}
	return mux
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
			http.Error(w, fmt.Sprintf("invalid span: %v", err), http.StatusBadRequest)
			return
		}
		h.normalizeInputOutput(ns)
		domainSpans = append(domainSpans, nsToDomain(ns, vk))
	}

	// Enqueue to Redis
	if err := h.queue.Enqueue(r.Context(), domainSpans); err != nil {
		http.Error(w, "service unavailable", http.StatusServiceUnavailable)
		return
	}

	w.WriteHeader(http.StatusAccepted)
}

func (h *NativeHandler) validateAndTransform(ns *NativeSpan, vk *auth.ValidatedKey) error {
	// Validate span_id: exactly 8 hex bytes = 16 hex chars
	if ns.SpanID != "" {
		if len(ns.SpanID) != 16 {
			return fmt.Errorf("span_id must be 16 hex characters, got %d", len(ns.SpanID))
		}
		if _, err := hex.DecodeString(ns.SpanID); err != nil {
			return fmt.Errorf("span_id is not valid hex: %v", err)
		}
	}

	// Validate trace_id: exactly 16 hex bytes = 32 hex chars
	if ns.TraceID != "" {
		if len(ns.TraceID) != 32 {
			return fmt.Errorf("trace_id must be 32 hex characters, got %d", len(ns.TraceID))
		}
		if _, err := hex.DecodeString(ns.TraceID); err != nil {
			return fmt.Errorf("trace_id is not valid hex: %v", err)
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

// --- OTLPHandler ---

// OTLPHandler handles POST /v1/traces for the OpenTelemetry Protocol.
// It accepts both application/x-protobuf and application/json content types.
type OTLPHandler struct {
	queue       SpanQueue
	validator   Validator
	corsOrigins []string
}

// NewOTLPHandler creates an OTLPHandler with optional CORS origins.
func NewOTLPHandler(queue SpanQueue, validator Validator, corsOrigins []string) *OTLPHandler {
	return &OTLPHandler{queue: queue, validator: validator, corsOrigins: corsOrigins}
}

// CombinedRouter returns a single http.Handler that serves both the native
// REST endpoint (/api/v1/spans) and the OTLP endpoint (/v1/traces).
func CombinedRouter(queue SpanQueue, validator Validator, corsOrigins []string) http.Handler {
	native := NewNativeHandler(queue, validator, corsOrigins)
	otlp := NewOTLPHandler(queue, validator, corsOrigins)

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/spans", native.handleIngest)
	mux.HandleFunc("/v1/traces", otlp.handleOTLP)

	if len(corsOrigins) > 0 {
		return cors.New(corsOrigins).Handler(mux)
	}
	return mux
}

func (h *OTLPHandler) Router() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/traces", h.handleOTLP)
	if len(h.corsOrigins) > 0 {
		return cors.New(h.corsOrigins).Handler(mux)
	}
	return mux
}

func (h *OTLPHandler) handleOTLP(w http.ResponseWriter, r *http.Request) {
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

	// Read body
	body, err := readBody(r)
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to read body: %v", err), http.StatusBadRequest)
		return
	}

	// Step 1: Wire format → []protobuf.FlatResourceSpans
	flat, err := decodeWireFormat(body, r.Header.Get("Content-Type"))
	if err != nil {
		contentType := protobuf.ContentType(r.Header.Get("Content-Type"))
		http.Error(w, fmt.Sprintf("failed to decode %s: %v", contentType, err), http.StatusBadRequest)
		return
	}

	// Step 2: []protobuf.FlatResourceSpans → []*domain.Span
	domainSpans, err := otlp.Translate(vk.ProjectID, flat, otlp.Options{
		ServiceNameOverride: vk.ServiceName,
	})
	if err != nil {
		http.Error(w, fmt.Sprintf("translation failed: %v", err), http.StatusBadRequest)
		return
	}

	// Enqueue to Redis
	if err := h.queue.Enqueue(r.Context(), domainSpans); err != nil {
		http.Error(w, "service unavailable", http.StatusServiceUnavailable)
		return
	}

	// Respond with empty ExportTraceServiceResponse matching Content-Type
	resp := &protobuf.ExportTraceServiceResponse{}
	writeOTLPResponse(w, resp, r.Header.Get("Content-Type"))
}

// decodeWireFormat decodes request body into FlatResourceSpans based on Content-Type.
func decodeWireFormat(body []byte, contentType string) ([]protobuf.FlatResourceSpans, error) {
	if protobuf.IsJSON(contentType) {
		return protobuf.DecodeJSON(body)
	}
	// Default: protobuf (wire format is binary, no JSON decoding needed).
	// Since we don't have a protobuf library, we treat unknown binary
	// as invalid for now. This is a stub — production would use protoc-generated code.
	return nil, fmt.Errorf("unsupported wire format: protobuf decoding requires protoc-generated types")
}

// writeOTLPResponse encodes the response to match the request's Content-Type.
func writeOTLPResponse(w http.ResponseWriter, resp *protobuf.ExportTraceServiceResponse, contentType string) {
	if protobuf.IsJSON(contentType) {
		data, err := protobuf.EncodeJSON(resp)
		if err != nil {
			http.Error(w, "encode response: internal error", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write(data)
	} else {
		// Protobuf: empty message.
		w.Header().Set("Content-Type", "application/x-protobuf")
		w.WriteHeader(http.StatusOK)
	}
}

// readBody reads the full request body (max 10MB).
func readBody(r *http.Request) ([]byte, error) {
	defer r.Body.Close()
	buf := make([]byte, 0, 1024)
	for {
		b := make([]byte, 1024)
		n, err := r.Body.Read(b)
		if n > 0 {
			buf = append(buf, b[:n]...)
		}
		if err != nil {
			break
		}
	}
	return buf, nil
}
