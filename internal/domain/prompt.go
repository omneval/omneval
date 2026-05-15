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

// PromptVersionJSON is the flattened JSON representation for API responses.
// Model, temperature, and max_tokens are promoted to the top level so the
// frontend PromptVersion interface (flat fields) matches the API.
type PromptVersionJSON struct {
	VersionID   string  `json:"version_id"`
	ProjectID   string  `json:"project_id"`
	Name        string  `json:"name"`
	Version     int64   `json:"version"`
	Template    string  `json:"template"`
	Model       string  `json:"model"`
	Temperature float64 `json:"temperature"`
	MaxTokens   int     `json:"max_tokens"`
	CreatedAt   string  `json:"created_at"`
}

// ToJSON flattens PromptVersion into the API response format.
// Model/temperature/max_tokens are top-level keys, not nested.
func (pv *PromptVersion) ToJSON() PromptVersionJSON {
	return PromptVersionJSON{
		VersionID:   pv.VersionID,
		ProjectID:   pv.ProjectID,
		Name:        pv.Name,
		Version:     pv.Version,
		Template:    pv.Template,
		Model:       pv.ModelConfig.Model,
		Temperature: pv.ModelConfig.Temperature,
		MaxTokens:   pv.ModelConfig.MaxTokens,
		CreatedAt:   pv.CreatedAt.Format(time.RFC3339),
	}
}

// PromptLabel maps a named label (e.g. "production") to a specific version.
type PromptLabel struct {
	ProjectID string    `json:"project_id"`
	Name      string    `json:"name"`
	Label     string    `json:"label"`
	Version   int64     `json:"version"`
	UpdatedAt time.Time `json:"updated_at"`
}
