package judge

import (
	"testing"
)

func TestInterpolate_TextKind(t *testing.T) {
	tests := []struct {
		name        string
		template    string
		variables   map[string]string
		want        string
		wantMissing []string
	}{
		{
			name:      "simple interpolation",
			template:  "Hello {{name}}, welcome to {{place}}!",
			variables: map[string]string{"name": "Alice", "place": "Omneval"},
			want:      "Hello Alice, welcome to Omneval!",
			wantMissing: nil,
		},
		{
			name:      "missing variable",
			template:  "Hello {{name}}, age: {{age}}",
			variables: map[string]string{"name": "Bob"},
			want:      "Hello Bob, age: {{age}}",
			wantMissing: []string{"age"},
		},
		{
			name:      "no variables",
			template:  "Just plain text",
			variables: map[string]string{},
			want:      "Just plain text",
			wantMissing: nil,
		},
		{
			name:      "whitespace inside braces",
			template:  "Hello {{  name  }}!",
			variables: map[string]string{"name": "Charlie"},
			want:      "Hello Charlie!",
			wantMissing: nil,
		},
		{
			name:      "multiple occurrences of same variable",
			template:  "{{greet}} {{name}}. How are you, {{name}}?",
			variables: map[string]string{"greet": "Hi", "name": "Dana"},
			want:      "Hi Dana. How are you, Dana?",
			wantMissing: nil,
		},
		{
			name:      "empty template",
			template:  "",
			variables: map[string]string{},
			want:      "",
			wantMissing: nil,
		},
		{
			name:      "no matches but variable keys provided",
			template:  "Static content only",
			variables: map[string]string{"unused": "value"},
			want:      "Static content only",
			wantMissing: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, missing := Interpolate(tt.template, tt.variables)
			if got != tt.want {
				t.Errorf("Interpolate() got = %q, want %q", got, tt.want)
			}
			// Compare missing variables (allow different order)
			if len(missing) != len(tt.wantMissing) {
				t.Errorf("Interpolate() missing = %v, want %v", missing, tt.wantMissing)
			}
		})
	}
}

func TestInterpolateChat_ChatKind(t *testing.T) {
	messages := []ChatMessage{
		{Role: "system", Content: "You are a helpful assistant. Evaluate {{task}}."},
		{Role: "user", Content: "Input: {{input}}, Output: {{output}}"},
		{Role: "assistant", Content: "Here's my assessment of {{input}}."},
	}
	variables := map[string]string{
		"task":   "toxicity detection",
		"input":  "Hello world",
		"output": "Hi there!",
	}

	rendered, missing := InterpolateChat(messages, variables)

	if len(rendered) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(rendered))
	}

	// Check each message is interpolated correctly
	wantMessages := []string{
		"You are a helpful assistant. Evaluate toxicity detection.",
		"Input: Hello world, Output: Hi there!",
		"Here's my assessment of Hello world.",
	}
	for i, want := range wantMessages {
		if rendered[i].Content != want {
			t.Errorf("message[%d] content = %q, want %q", i, rendered[i].Content, want)
		}
		if rendered[i].Role != messages[i].Role {
			t.Errorf("message[%d] role = %q, want %q", i, rendered[i].Role, messages[i].Role)
		}
	}

	if len(missing) > 0 {
		t.Errorf("expected no missing variables, got %v", missing)
	}
}

func TestInterpolateChat_MissingVariables(t *testing.T) {
	messages := []ChatMessage{
		{Role: "user", Content: "{{known}} and {{unknown}}"},
	}
	variables := map[string]string{"known": "found"}

	rendered, missing := InterpolateChat(messages, variables)

	if len(rendered) != 1 {
		t.Fatalf("expected 1 message, got %d", len(rendered))
	}
	if rendered[0].Content != "found and {{unknown}}" {
		t.Errorf("content = %q, want 'found and {{unknown}}'", rendered[0].Content)
	}
	if len(missing) != 1 || missing[0] != "unknown" {
		t.Errorf("missing = %v, want ['unknown']", missing)
	}
}

func TestInterpolateChat_EmptyMessages(t *testing.T) {
	rendered, missing := InterpolateChat(nil, map[string]string{})
	if len(rendered) != 0 {
		t.Errorf("expected 0 messages, got %d", len(rendered))
	}
	if len(missing) != 0 {
		t.Errorf("expected no missing, got %v", missing)
	}
}

func TestInterpolateChat_MultipleMissingAcrossMessages(t *testing.T) {
	messages := []ChatMessage{
		{Role: "user", Content: "Hello {{name}}"},
		{Role: "assistant", Content: "Bye {{greeting}}"},
	}
	// No variables provided
	rendered, missing := InterpolateChat(messages, map[string]string{})

	if len(missing) != 2 {
		t.Errorf("expected 2 missing vars, got %d", len(missing))
	}
	if rendered[0].Content != "Hello {{name}}" {
		t.Errorf("first message not preserved: %q", rendered[0].Content)
	}
	if rendered[1].Content != "Bye {{greeting}}" {
		t.Errorf("second message not preserved: %q", rendered[1].Content)
	}
}