package domain

import "time"

// EvalFilter is a conjunction (AND) of conditions matched against a Span
// in-process by the Writer Service. Nil pointer fields are ignored.
type EvalFilter struct {
	Kind        *SpanKind `json:"kind"`
	Model       *string   `json:"model"`
	ServiceName *string   `json:"service_name"`
	PromptName  *string   `json:"prompt_name"`
	StatusCode  *string   `json:"status_code"`
	MinCostUSD  *float64  `json:"min_cost_usd"`
	MaxCostUSD  *float64  `json:"max_cost_usd"`
	MinDurationMS *int64  `json:"min_duration_ms"`
	MaxDurationMS *int64  `json:"max_duration_ms"`
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
