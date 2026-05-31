package handlers_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestDocsIngestionAuthAndProjectModel verifies that docs/ingestion.md exists
// and covers all acceptance criteria from issue #52:
// - "Sending traces" section with both native and OTLP endpoints
// - X-API-Key auth header and OTEL_EXPORTER_OTLP_HEADERS form
// - Project inferred from API key (not supplied by client)
// - Key creation (project vs service, prefixes, raw key shown once) and revocation
// - Worked OTLP env-var example
func TestDocsIngestionAuthAndProjectModel(t *testing.T) {
	// Resolve docs/ingestion.md relative to repo root.
	// The test is run from the internal module, so we go up one level.
	docsPath := filepath.Join("..", "..", "docs", "ingestion.md")

	data, err := os.ReadFile(docsPath)
	if err != nil {
		if os.IsNotExist(err) {
			t.Fatalf("docs/ingestion.md does not exist — create it to document trace-ingestion auth and project model")
		}
		t.Fatalf("failed to read docs/ingestion.md: %v", err)
	}

	content := string(data)

	checks := []struct {
		name string
		text string
	}{
		// Acceptance criterion 1: "Sending traces" section covering both endpoints
		{"sending traces section", "Sending Traces"},
		{"native endpoint path", "/api/v1/spans"},
		{"otlp endpoint path", "/v1/traces"},
		{"native content type", "application/json"},
		{"otlp protobuf content type", "application/x-protobuf"},

		// Acceptance criterion 2: auth header + OTLP env-var form
		{"x-api-key header", "X-API-Key"},
		{"otlp headers env var", "OTEL_EXPORTER_OTLP_HEADERS"},

		// Acceptance criterion 3: project inferred from key
		{"project inferred from key", "inferred"},
		{"client must not supply project_id", "must not"},

		// Acceptance criterion 4: key creation, prefixes, raw key shown once, revocation
		{"project key prefix", "oev_proj_"},
		{"service key prefix", "oev_svc_"},
		{"raw key shown once", "once"},
		{"key revocation", "revoke"},

		// Acceptance criterion 5: worked OTLP env-var example
		{"otlp endpoint env var", "OTEL_EXPORTER_OTLP_ENDPOINT"},
		{"otlp protocol env var", "OTEL_EXPORTER_OTLP_PROTOCOL"},
	}

	for _, c := range checks {
		t.Run(c.name, func(t *testing.T) {
			if !strings.Contains(content, c.text) {
				t.Errorf("docs/ingestion.md must mention %q but does not", c.text)
			}
		})
	}
}

// TestREADMEReferencesIngestionDoc verifies the README links to docs/ingestion.md.
func TestREADMEReferencesIngestionDoc(t *testing.T) {
	readmePath := filepath.Join("..", "..", "README.md")
	data, err := os.ReadFile(readmePath)
	if err != nil {
		if os.IsNotExist(err) {
			t.Fatalf("README.md does not exist")
		}
		t.Fatalf("failed to read README.md: %v", err)
	}

	content := string(data)
	if !strings.Contains(content, "docs/ingestion.md") {
		t.Error("README.md must reference docs/ingestion.md")
	}
}
