package otlp

import "github.com/zbloss/lantern/internal/domain"

// ExportTraceServiceRequest is a minimal stand-in for the protobuf type.
// The real type will come from go.opentelemetry.io/proto/otlp once added.
type ExportTraceServiceRequest = map[string]any

// Translate maps an OTLP ExportTraceServiceRequest to Lantern Span values.
// GenAI semantic convention attributes are promoted to typed fields; all
// remaining attributes fall into each Span's Attributes overflow map.
func Translate(projectID string, req ExportTraceServiceRequest) ([]*domain.Span, error) {
	panic("not implemented")
}
