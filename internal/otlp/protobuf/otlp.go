// Package protobuf contains the OTLP wire format types and the
// two-step decoder: wire bytes → []ResourceSpans.
//
// The types below mirror the protobuf definitions from
// go.opentelemetry.io/proto/otlp/collector/trace/v1 and
// go.opentelemetry.io/proto/otlp/trace/v1, but are hand-written
// to avoid the protobuf toolchain dependency.
package protobuf

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"
)

// -----------------------------------------------------------------------
// ExportTraceServiceRequest / Response
// -----------------------------------------------------------------------

// ExportTraceServiceRequest is the wire-format payload for POST /v1/traces.
type ExportTraceServiceRequest struct {
	ResourceSpans []*WireResourceSpans `json:"resource_spans,omitempty"`
}

// ExportTraceServiceResponse is the empty response for OTLP.
type ExportTraceServiceResponse struct{}

// -----------------------------------------------------------------------
// WireResourceSpans — the nested OTLP wire format
// -----------------------------------------------------------------------

// WireResourceSpans mirrors the protobuf resourceSpans message with
// its nested ScopeSpans structure.
type WireResourceSpans struct {
	Resource        *Resource   `json:"resource,omitempty"`
	ScopeSpans      []*ScopeSpans `json:"scope_spans,omitempty"`
	SchemaUrl       string      `json:"schema_url,omitempty"`
}

// -----------------------------------------------------------------------
// Resource
// -----------------------------------------------------------------------

type Resource struct {
	Attributes           []*KeyValue `json:"attributes,omitempty"`
	DroppedAttributesCount uint32    `json:"dropped_attributes_count,omitempty"`
}

// -----------------------------------------------------------------------
// ScopeSpans
// -----------------------------------------------------------------------

type ScopeSpans struct {
	Scope     *Scope    `json:"scope,omitempty"`
	Spans     []*Span   `json:"spans,omitempty"`
	SchemaUrl string    `json:"schema_url,omitempty"`
}

// -----------------------------------------------------------------------
// Scope
// -----------------------------------------------------------------------

type Scope struct {
	Name    string `json:"name,omitempty"`
	Version string `json:"version,omitempty"`
}

// -----------------------------------------------------------------------
// Span
// -----------------------------------------------------------------------

type Span struct {
	TraceId                []byte        `json:"trace_id,omitempty"`
	SpanId                 []byte        `json:"span_id,omitempty"`
	TraceState             string        `json:"trace_state,omitempty"`
	ParentSpanId           []byte        `json:"parent_span_id,omitempty"`
	Name                   string        `json:"name,omitempty"`
	Kind                   SpanKind      `json:"kind,omitempty"`
	StartTimeUnixNano      uint64        `json:"start_time_unix_nano,omitempty"`
	EndTimeUnixNano        uint64        `json:"end_time_unix_nano,omitempty"`
	Attributes             []*KeyValue   `json:"attributes,omitempty"`
	Events                 []*SpanEvent  `json:"events,omitempty"`
	Links                  []*SpanLink   `json:"links,omitempty"`
	Flags                  uint32        `json:"flags,omitempty"`
	DroppedAttributesCount uint32        `json:"dropped_attributes_count,omitempty"`
	DroppedEventsCount     uint32        `json:"dropped_events_count,omitempty"`
	DroppedLinksCount      uint32        `json:"dropped_links_count,omitempty"`
}

// SpanKind mirrors otlp.trace.v1.Span_SpanKind
type SpanKind int32

const (
	SpanKindUnspecified SpanKind = 0
	SpanKindInternal    SpanKind = 1
	SpanKindServer      SpanKind = 2
	SpanKindClient      SpanKind = 3
	SpanKindProducer    SpanKind = 4
	SpanKindConsumer    SpanKind = 5
)

// -----------------------------------------------------------------------
// SpanEvent / SpanLink
// -----------------------------------------------------------------------

type SpanEvent struct {
	TimeUnixNano           uint64      `json:"time_unix_nano,omitempty"`
	Name                   string      `json:"name,omitempty"`
	Attributes             []*KeyValue `json:"attributes,omitempty"`
	DroppedAttributesCount uint32      `json:"dropped_attributes_count,omitempty"`
}

type SpanLink struct {
	TraceId                []byte      `json:"trace_id,omitempty"`
	SpanId                 []byte      `json:"span_id,omitempty"`
	TraceState             string      `json:"trace_state,omitempty"`
	Flags                  uint32      `json:"flags,omitempty"`
	Attributes             []*KeyValue `json:"attributes,omitempty"`
	DroppedAttributesCount uint32      `json:"dropped_attributes_count,omitempty"`
}

// -----------------------------------------------------------------------
// KeyValue / AnyValue (the attribute system)
// -----------------------------------------------------------------------

type KeyValue struct {
	Key   string    `json:"key,omitempty"`
	Value *AnyValue `json:"value,omitempty"`
}

type AnyValue struct {
	StringValue  *string      `json:"string_value,omitempty"`
	BoolValue    *bool        `json:"bool_value,omitempty"`
	IntValue     *int64       `json:"int_value,omitempty"`
	DoubleValue  *float64     `json:"double_value,omitempty"`
	ArrayValue   *ArrayValue  `json:"array_value,omitempty"`
	KeyValueList *KeyValueList `json:"kvlist_value,omitempty"`
	BytesValue   []byte       `json:"bytes_value,omitempty"`
}

type ArrayValue struct {
	Values []*AnyValue `json:"values,omitempty"`
}

type KeyValueList struct {
	Values []*KeyValue `json:"values,omitempty"`
}

// -----------------------------------------------------------------------
// Flat ResourceSpans — output of the decoder (used by the translator)
// -----------------------------------------------------------------------

// FlatResourceSpans is the flat form: a Resource with all its Spans.
// This is what the decoder produces from the nested wire format.
type FlatResourceSpans struct {
	Resource *Resource
	Spans    []*Span
}

// -----------------------------------------------------------------------
// Wire Format Decoding
// -----------------------------------------------------------------------

// DecodeJSON parses a JSON-encoded ExportTraceServiceRequest and returns
// a flat []FlatResourceSpans.
func DecodeJSON(data []byte) ([]FlatResourceSpans, error) {
	var req ExportTraceServiceRequest
	if err := json.Unmarshal(data, &req); err != nil {
		return nil, fmt.Errorf("otlp: decode JSON: %w", err)
	}
	return expandResourceSpans(req.ResourceSpans), nil
}

// expandResourceSpans converts the nested wire format into flat
// []FlatResourceSpans (Resource + all its spans). Spans from the same
// resource are grouped together.
func expandResourceSpans(wrss []*WireResourceSpans) []FlatResourceSpans {
	out := make([]FlatResourceSpans, 0, len(wrss))
	for _, w := range wrss {
		res := normalizeResource(w.Resource)
		allSpans := make([]*Span, 0)
		for _, ss := range w.ScopeSpans {
			allSpans = append(allSpans, ss.Spans...)
		}
		if len(allSpans) > 0 {
			out = append(out, FlatResourceSpans{
				Resource: &res,
				Spans:    allSpans,
			})
		}
	}
	return out
}

// normalizeResource deduplicates resource attributes into a map,
// giving priority to the first occurrence of each key.
func normalizeResource(r *Resource) Resource {
	if r == nil {
		return Resource{Attributes: make([]*KeyValue, 0)}
	}
	seen := make(map[string]bool)
	out := make([]*KeyValue, 0, len(r.Attributes))
	for _, kv := range r.Attributes {
		if !seen[kv.Key] {
			seen[kv.Key] = true
			out = append(out, kv)
		}
	}
	return Resource{Attributes: out}
}

// -----------------------------------------------------------------------
// Wire Format Encoding (for response)
// -----------------------------------------------------------------------

// EncodeJSON encodes an ExportTraceServiceResponse as JSON.
func EncodeJSON(resp *ExportTraceServiceResponse) ([]byte, error) {
	return json.Marshal(resp)
}

// EncodeProtobuf encodes an ExportTraceServiceResponse as protobuf.
// OTLP specifies this as an empty message, so we return an empty byte slice.
func EncodeProtobuf(resp *ExportTraceServiceResponse) ([]byte, error) {
	return []byte{}, nil
}

// -----------------------------------------------------------------------
// Time helpers
// -----------------------------------------------------------------------

// UnixNano converts a uint64 nanosecond timestamp (as used in OTLP) to time.Time.
func UnixNano(ts uint64) time.Time {
	if ts == 0 {
		return time.Time{}
	}
	sec := ts / 1e9
	nsec := ts % 1e9
	// Handle overflow: ts > math.MaxInt64 / 1e9
	if sec > math.MaxInt64 {
		sec = math.MaxInt64
		nsec = 0
	}
	return time.Unix(int64(sec), int64(nsec))
}

// AnyValueToAny converts a protobuf-style AnyValue to a Go any.
func AnyValueToAny(v *AnyValue) any {
	if v == nil {
		return nil
	}
	switch {
	case v.StringValue != nil:
		return *v.StringValue
	case v.BoolValue != nil:
		return *v.BoolValue
	case v.IntValue != nil:
		return *v.IntValue
	case v.DoubleValue != nil:
		return *v.DoubleValue
	case v.BytesValue != nil:
		return base64.StdEncoding.EncodeToString(v.BytesValue)
	case v.ArrayValue != nil:
		arr := make([]any, 0, len(v.ArrayValue.Values))
		for _, av := range v.ArrayValue.Values {
			arr = append(arr, AnyValueToAny(av))
		}
		return arr
	case v.KeyValueList != nil:
		m := make(map[string]any)
		for _, kv := range v.KeyValueList.Values {
			m[kv.Key] = AnyValueToAny(kv.Value)
		}
		return m
	default:
		return nil
	}
}

// -----------------------------------------------------------------------
// Attribute helpers
// -----------------------------------------------------------------------

// GetStringAttribute retrieves a string attribute from a slice of KeyValue.
// Returns the value and true if found, or "" and false.
func GetStringAttribute(attrs []*KeyValue, key string) (string, bool) {
	for _, kv := range attrs {
		if kv.Key == key && kv.Value != nil && kv.Value.StringValue != nil {
			return *kv.Value.StringValue, true
		}
	}
	return "", false
}

// GetInt64Attribute retrieves an int64 attribute.
func GetInt64Attribute(attrs []*KeyValue, key string) (int64, bool) {
	for _, kv := range attrs {
		if kv.Key == key && kv.Value != nil && kv.Value.IntValue != nil {
			return *kv.Value.IntValue, true
		}
	}
	return 0, false
}

// GetDoubleAttribute retrieves a double/float64 attribute.
func GetDoubleAttribute(attrs []*KeyValue, key string) (float64, bool) {
	for _, kv := range attrs {
		if kv.Key == key && kv.Value != nil && kv.Value.DoubleValue != nil {
			return *kv.Value.DoubleValue, true
		}
	}
	return 0, false
}

// GetStringMap retrieves a map[string]string from a kvlist_value attribute.
// Returns nil if the key is not found or is not a kvlist.
func GetStringMap(attrs []*KeyValue, key string) map[string]string {
	for _, kv := range attrs {
		if kv.Key == key && kv.Value != nil && kv.Value.KeyValueList != nil {
			m := make(map[string]string)
			for _, sub := range kv.Value.KeyValueList.Values {
				if sub.Value != nil && sub.Value.StringValue != nil {
					m[sub.Key] = *sub.Value.StringValue
				}
			}
			return m
		}
	}
	return nil
}

// -----------------------------------------------------------------------
// Span ID helpers
// -----------------------------------------------------------------------

// DecodeTraceID decodes a hex-encoded trace ID from protobuf bytes.
func DecodeTraceID(b []byte) string {
	return fmt.Sprintf("%x", b)
}

// DecodeSpanID decodes a hex-encoded span ID from protobuf bytes.
func DecodeSpanID(b []byte) string {
	return fmt.Sprintf("%x", b)
}

// -----------------------------------------------------------------------
// Content-Type detection
// -----------------------------------------------------------------------

// ContentType returns the Content-Type string from an HTTP request header.
// Falls back to "application/x-protobuf" (the OTLP default).
func ContentType(contentType string) string {
	if contentType == "" {
		return "application/x-protobuf"
	}
	// Strip parameters like charset
	if idx := strings.Index(contentType, ";"); idx != -1 {
		contentType = contentType[:idx]
	}
	contentType = strings.TrimSpace(contentType)
	if contentType == "" {
		return "application/x-protobuf"
	}
	return contentType
}

// IsProtobuf checks if the content type is protobuf.
func IsProtobuf(contentType string) bool {
	ct := ContentType(contentType)
	return ct == "application/x-protobuf"
}

// IsJSON checks if the content type is JSON.
func IsJSON(contentType string) bool {
	ct := ContentType(contentType)
	return ct == "application/json"
}
