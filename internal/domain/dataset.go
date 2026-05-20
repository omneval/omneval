package domain

import "time"

type Dataset struct {
	DatasetID string
	ProjectID string
	Name      string
	CreatedAt time.Time
}

type DatasetItem struct {
	ItemID         string
	DatasetID      string
	SourceSpanID   string
	Input          string
	ExpectedOutput string
	CreatedAt      time.Time
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
