package otlp

import (
	"time"

	"github.com/zbloss/lantern/internal/domain"
)

// Resource represents a normalized OTLP Resource (resource-level attributes).
type Resource struct {
	Attributes map[string]any
}

// Span represents a normalized OTLP span before translation to domain.Span.
type Span struct {
	SpanID     string
	TraceID    string
	ParentID   string
	Name       string
	StartTime  time.Time
	EndTime    time.Time
	StatusCode string
	StatusMsg  string
	Attributes map[string]any
}

// ResourceSpans groups a Resource with the spans it produced.
type ResourceSpans struct {
	Resource Resource
	Spans    []*Span
}

// Options controls translator behaviour that varies by deployment config.
type Options struct {
	// LogSystemPrompt controls whether the system prompt is included as the
	// first element of a span's Input array.
	LogSystemPrompt bool
	// ServiceNameOverride is non-empty when the ingest request was
	// authenticated with a service-scoped API key; it takes precedence over
	// the resource-level service.name attribute.
	ServiceNameOverride string
}

// Translate converts a slice of ResourceSpans into domain.Span values.
// projectID is attached to every span. opts controls system-prompt logging
// and service-name override for service-scoped API keys.
func Translate(projectID string, rss []ResourceSpans, opts Options) ([]*domain.Span, error) {
	panic("not implemented")
}
