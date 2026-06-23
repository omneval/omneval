package domain

import "time"

// PromptKind is the kind of a PromptVersion: "text" for a single template
// string, or "chat" for an ordered list of role-tagged messages.
type PromptKind string

const (
	PromptKindText PromptKind = "text"
	PromptKindChat PromptKind = "chat"
)

// PromptMessage is a single role-tagged message in a chat-type PromptVersion.
type PromptMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// PromptVersion is an immutable versioned prompt template.
type PromptVersion struct {
	VersionID   string            `json:"version_id"`
	ProjectID   string            `json:"project_id"`
	Name        string            `json:"name"`
	Version     int64             `json:"version"`
	Kind        PromptKind        `json:"kind"`         // "text" or "chat"
	Template    string            `json:"template"`     // used when Kind=="text"
	Messages    []PromptMessage   `json:"messages"`     // used when Kind=="chat"
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
	VersionID   string          `json:"version_id"`
	ProjectID   string          `json:"project_id"`
	Name        string          `json:"name"`
	Version     int64           `json:"version"`
	Kind        PromptKind      `json:"kind"`
	Template    string          `json:"template"`
	Messages    []PromptMessage `json:"messages"`
	Model       string          `json:"model"`
	Temperature float64         `json:"temperature"`
	MaxTokens   int             `json:"max_tokens"`
	CreatedAt   string          `json:"created_at"`
}

// ToJSON converts a domain PromptVersion into the flattened API response
// representation (PromptVersionJSON) with model/temperature/max_tokens at
// the top level, matching the frontend PromptVersion interface.
func (pv *PromptVersion) ToJSON() PromptVersionJSON {
	return PromptVersionJSON{
		VersionID:   pv.VersionID,
		ProjectID:   pv.ProjectID,
		Name:        pv.Name,
		Version:     pv.Version,
		Kind:        pv.Kind,
		Template:    pv.Template,
		Messages:    pv.Messages,
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
