package domain

import "time"

// PromptVersion is an immutable versioned prompt template.
type PromptVersion struct {
	VersionID   string
	ProjectID   string
	Name        string
	Version     int64
	Template    string // supports {{variable}} interpolation
	ModelConfig PromptModelConfig
	CreatedAt   time.Time
}

// PromptModelConfig captures the generation config tied to a prompt version.
type PromptModelConfig struct {
	Model       string
	Temperature float64
	MaxTokens   int
}

// PromptLabel maps a named label (e.g. "production") to a specific version.
type PromptLabel struct {
	ProjectID string
	Name      string
	Label     string
	Version   int64
	UpdatedAt time.Time
}
