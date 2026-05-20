package domain

// ScoreRequest is the JSON body for POST /api/v1/scores.
type ScoreRequest struct {
	SpanID        string  `json:"span_id"`
	TraceID       string  `json:"trace_id"`
	ProjectID     string  `json:"project_id"`
	EvalName      string  `json:"eval_name"`
	Value         float64 `json:"value"`
	Reasoning     string  `json:"reasoning"`
	JudgeModel    string  `json:"judge_model"`
	PromptName    string  `json:"prompt_name"`
	PromptVersion int64   `json:"prompt_version"`
}
