package domain

import "time"

// EvalFilter is a conjunction (AND) of conditions matched against a Span
// in-process by the Writer Service. Nil pointer fields are ignored.
type EvalFilter struct {
	Kind        *SpanKind
	Model       *string
	ServiceName *string
	PromptName  *string
	StatusCode  *string
	MinCostUSD  *float64
	MaxCostUSD  *float64
	MinDurationMS *int64
	MaxDurationMS *int64
}

// EvalRule defines when and how to run LLM-as-a-Judge evaluations.
type EvalRule struct {
	RuleID        string
	ProjectID     string
	Name          string
	JudgeModel    string
	PromptName    string
	PromptVersion int64
	Filter        EvalFilter
	SampleRate    float64 // 0.0–1.0; 1.0 = score every matching span
	Enabled       bool
	CreatedAt     time.Time
}

// EvalJob is a unit of work placed on the eval Redis queue.
type EvalJob struct {
	JobID         string
	RuleID        string
	SpanID        string
	TraceID       string
	ProjectID     string
	EnqueuedAt    time.Time
	PromptName    string
	PromptVersion int64
}
