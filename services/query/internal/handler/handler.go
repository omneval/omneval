package handler

import "time"

// SpanQueryRequest is the body accepted by POST /api/v1/spans/query.
// From and To are required absolute UTC timestamps. Cursor is an opaque
// pagination token returned by the previous page; omit for the first page.
type SpanQueryRequest struct {
	From    time.Time         `json:"from"`
	To      time.Time         `json:"to"`
	Filters []SpanQueryFilter `json:"filters,omitempty"`
	Cursor  string            `json:"cursor,omitempty"`
	Limit   int               `json:"limit,omitempty"`
}

// SpanQueryFilter is a single predicate on a span field.
type SpanQueryFilter struct {
	Field string `json:"field"`
	Op    string `json:"op"`
	Value any    `json:"value"`
}

// SpanHandler handles POST /api/v1/spans/query (paginated span list)
// and GET /api/v1/traces/:traceId (single-trace waterfall detail).
type SpanHandler struct{}

// AnalyticsHandler handles POST /api/v1/analytics/spans (DSL queries).
type AnalyticsHandler struct{}

// PromptHandler handles prompt registry read endpoints.
type PromptHandler struct{}

// ScoreHandler handles manual score write endpoint.
type ScoreHandler struct{}
