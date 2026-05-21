package domain

import "time"

type Dataset struct {
	DatasetID string    `json:"dataset_id"`
	ProjectID string    `json:"project_id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
}

type DatasetItem struct {
	ItemID         string    `json:"item_id"`
	DatasetID      string    `json:"dataset_id"`
	SourceSpanID   string    `json:"source_span_id"`
	Input          string    `json:"input"`
	ExpectedOutput string    `json:"expected_output"`
	CreatedAt      time.Time `json:"created_at"`
}

const (
	DatasetRunStatusPending  = "pending"
	DatasetRunStatusRunning  = "running"
	DatasetRunStatusComplete = "complete"
	DatasetRunStatusError    = "error"
)

// DatasetRun represents a dataset evaluation run.
type DatasetRun struct {
	RunID         string
	DatasetID     string
	EvalRuleID    string
	PromptVersion int64
	Status        string // pending, running, complete, error
	CreatedAt     time.Time
}

// DatasetRunItem represents a single scored item within a dataset run.
type DatasetRunItem struct {
	RunItemID string
	RunID     string
	ItemID    string
	Score     float64
	Reasoning string
	CreatedAt time.Time
}
