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

type DatasetRun struct {
	RunID         string
	DatasetID     string
	EvalRuleID    string
	PromptVersion int64
	CreatedAt     time.Time
}

type DatasetRunItem struct {
	RunItemID string
	RunID     string
	ItemID    string
	ScoreID   string
}
