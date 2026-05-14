package domain

import "time"

// PromptVersion is an immutable versioned prompt template.
type PromptVersion struct {
	VersionID   string            `json:"version_id"`
	ProjectID   string            `json:"project_id"`
	Name        string            `json:"name"`
	Version     int64             `json:"version"`
	Template    string            `json:"template"` // supports {{variable}} interpolation
	ModelConfig PromptModelConfig `json:"model_config"`
	CreatedAt   time.Time         `json:"created_at"`
}

// PromptModelConfig captures the generation config tied to a prompt version.
type PromptModelConfig struct {
	Model       string  `json:"model"`
	Temperature float64 `json:"temperature"`
	MaxTokens   int     `json:"max_tokens"`
}

// PromptLabel maps a named label (e.g. "production") to a specific version.
type PromptLabel struct {
	ProjectID string    `json:"project_id"`
	Name      string    `json:"name"`
	Label     string    `json:"label"`
	Version   int64     `json:"version"`
	UpdatedAt time.Time `json:"updated_at"`
}
