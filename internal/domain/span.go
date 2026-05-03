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
	SpanID     string
	TraceID    string
	ParentID   string
	ProjectID  string
	ServiceName string

	Name      string
	Kind      SpanKind
	StartTime time.Time
	EndTime   time.Time

	// LLM-specific fields extracted from OTel GenAI conventions.
	Model           string
	InputTokens     int64
	OutputTokens    int64
	CostUSD         float64

	// Prompt linkage.
	PromptName    string
	PromptVersion int64

	StatusCode    string
	StatusMessage string

	// Overflow bucket for all other OTel attributes.
	Attributes map[string]any
}

// Trace groups a set of spans that share a TraceID.
type Trace struct {
	TraceID   string
	ProjectID string
	RootSpan  *Span
	Spans     []*Span
}

// Score is an evaluation result attached to a span.
type Score struct {
	ScoreID    string
	SpanID     string
	TraceID    string
	ProjectID  string
	EvalName   string
	Value      float64
	Reasoning  string
	JudgeModel string
	PromptName    string
	PromptVersion int64
	CreatedAt  time.Time
}
