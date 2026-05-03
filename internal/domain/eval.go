package domain

import "time"

// EvalRule defines when and how to run LLM-as-a-Judge evaluations.
type EvalRule struct {
	RuleID        string
	ProjectID     string
	Name          string
	JudgeModel    string
	PromptName    string
	PromptVersion int64
	// Filter determines which spans trigger this rule.
	Filter     map[string]any
	SampleRate float64 // 0.0–1.0; 1.0 = score every matching span
	Enabled    bool
	CreatedAt  time.Time
}

// EvalJob is a unit of work placed on the eval Redis queue.
type EvalJob struct {
	JobID      string
	RuleID     string
	SpanID     string
	TraceID    string
	ProjectID  string
	EnqueuedAt time.Time
}
