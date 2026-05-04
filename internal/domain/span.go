package domain

import "time"

// SpanKind classifies a span by its role in the trace.
type SpanKind string

const (
	SpanKindLLM      SpanKind = "llm"
	SpanKindTool     SpanKind = "tool"
	SpanKindAgent    SpanKind = "agent"
	SpanKindChain    SpanKind = "chain"
	SpanKindInternal SpanKind = "internal"
)

// Span is the central fact type stored in DuckDB.
type Span struct {
	SpanID      string           `json:"span_id"`
	TraceID     string           `json:"trace_id"`
	ParentID    string           `json:"parent_id"`
	ProjectID   string           `json:"project_id"`
	ServiceName string           `json:"service_name"`

	Name      string    `json:"name"`
	Kind      SpanKind  `json:"kind"`
	StartTime time.Time `json:"start_time"`
	EndTime   time.Time `json:"end_time"`

	// LLM-specific fields extracted from OTel GenAI conventions.
	Model       string `json:"model"`
	Input       string `json:"input"` // serialized JSON of gen_ai.prompt messages (or raw text)
	Output      string `json:"output"` // serialized JSON of gen_ai.completion
	InputTokens int64  `json:"input_tokens"`
	OutputTokens int64 `json:"output_tokens"`
	CostUSD     float64 `json:"cost_usd"`

	// Prompt linkage.
	PromptName    string `json:"prompt_name"`
	PromptVersion int64  `json:"prompt_version"`

	StatusCode    string `json:"status_code"`
	StatusMessage string `json:"status_message"`

	// Overflow bucket for all other OTel attributes.
	Attributes map[string]any `json:"attributes"`

	// Children populated by buildTraceTree for waterfall rendering.
	Children []*Span `json:"children"`

	// Scores attached to this span from the eval pipeline.
	Scores []*SpanScore `json:"scores"`
}

// Trace groups a set of spans that share a TraceID.
type Trace struct {
	TraceID   string
	ProjectID string
	RootSpan  *Span
	Spans     []*Span
}

// TraceResponse is the JSON body returned by GET /api/v1/traces/:traceId.
type TraceResponse struct {
	TraceID   string  `json:"trace_id"`
	ProjectID string  `json:"project_id"`
	RootSpan  *Span   `json:"root_span"`
	Spans     []*Span `json:"spans"`
}

// Score is an evaluation result attached to a span (used by eval pipeline).
type Score struct {
	ScoreID     string    `json:"score_id"`
	SpanID      string    `json:"span_id"`
	TraceID     string    `json:"trace_id"`
	ProjectID   string    `json:"project_id"`
	EvalName    string    `json:"eval_name"`
	Value       float64   `json:"value"`
	Reasoning   string    `json:"reasoning"`
	JudgeModel  string    `json:"judge_model"`
	PromptName  string    `json:"prompt_name"`
	PromptVersion int64   `json:"prompt_version"`
	CreatedAt   time.Time `json:"created_at"`
}

// SpanScore is a lightweight score attached inline to a Span for waterfall rendering.
type SpanScore struct {
	EvalName  string  `json:"eval_name"`
	Value     float64 `json:"value"`
	Reasoning string  `json:"reasoning"`
}
