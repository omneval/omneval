package lantern

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zbloss/lantern/internal/domain"
)

// TestClient_GetPrompt_Label verifies GetPrompt fetches a prompt by label
// and returns the template.
func TestClient_GetPrompt_Label(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		label := r.URL.Query().Get("label")
		if label == "" {
			label = "production"
		}
		pv := domain.PromptVersion{
			Name:      "greeting",
			Version:   1,
			Template:  "Hello, {{.Name}}!",
			ModelConfig: domain.PromptModelConfig{
				Model:       "gpt-4",
				Temperature: 0.7,
				MaxTokens:   100,
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(pv)
	}))
	defer ts.Close()

	client := NewClient(ts.URL, "ltn_proj_test")

	template, err := client.GetPrompt("greeting", "production")
	if err != nil {
		t.Fatalf("GetPrompt: %v", err)
	}
	if template != "Hello, {{.Name}}!" {
		t.Errorf("template: got %q, want %q", template, "Hello, {{.Name}}!")
	}
}

// TestClient_GetPrompt_DefaultLabel verifies label defaults to "production".
func TestClient_GetPrompt_DefaultLabel(t *testing.T) {
	var receivedLabel string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedLabel = r.URL.Query().Get("label")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(domain.PromptVersion{
			Name:     "test",
			Version:  1,
			Template: "test",
		})
	}))
	defer ts.Close()

	client := NewClient(ts.URL, "ltn_proj_test")
	_, err := client.GetPrompt("test", "")
	if err != nil {
		t.Fatalf("GetPrompt: %v", err)
	}
	if receivedLabel != "production" {
		t.Errorf("expected default label 'production', got %q", receivedLabel)
	}
}

// TestClient_GetPrompt_Version verifies GetPromptVersion fetches by version.
func TestClient_GetPrompt_Version(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		version := r.URL.Query().Get("version")
		if version == "" {
			http.Error(w, "version required", http.StatusBadRequest)
			return
		}
		pv := domain.PromptVersion{
			Name:     "greeting",
			Version:  2,
			Template: "Welcome, {{.Name}}!",
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(pv)
	}))
	defer ts.Close()

	client := NewClient(ts.URL, "ltn_proj_test")

	template, err := client.GetPromptVersion("greeting", 2)
	if err != nil {
		t.Fatalf("GetPromptVersion: %v", err)
	}
	if template != "Welcome, {{.Name}}!" {
		t.Errorf("template: got %q, want %q", template, "Welcome, {{.Name}}!")
	}
}

// TestClient_GetPrompt_VersionNotFound verifies 404 returns an error.
func TestClient_GetPrompt_VersionNotFound(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer ts.Close()

	client := NewClient(ts.URL, "ltn_proj_test")
	_, err := client.GetPromptVersion("nonexistent", 99)
	if err == nil {
		t.Error("expected error for nonexistent prompt, got nil")
	}
}

// TestClient_GetPrompt_Caching verifies label cache uses 30-second TTL.
func TestClient_GetPrompt_Caching(t *testing.T) {
	var requestCount atomic.Int64
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount.Add(1)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(domain.PromptVersion{
			Name:     "cached",
			Version:  1,
			Template: "cached content",
		})
	}))
	defer ts.Close()

	client := NewClient(ts.URL, "ltn_proj_test")

	// First call — should hit the server.
	_, err := client.GetPrompt("cached", "production")
	if err != nil {
		t.Fatalf("GetPrompt: %v", err)
	}

	// Second call — should use cache (no additional request within TTL).
	_, err = client.GetPrompt("cached", "production")
	if err != nil {
		t.Fatalf("GetPrompt: %v", err)
	}

	// Within TTL, only 1 request.
	if requestCount.Load() != 1 {
		t.Errorf("expected 1 request (cached), got %d", requestCount.Load())
	}
}

// TestClient_GetPromptVersion_NoTTL verifies version cache has no TTL.
func TestClient_GetPromptVersion_NoTTL(t *testing.T) {
	var requestCount atomic.Int64
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount.Add(1)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(domain.PromptVersion{
			Name:     "immutable",
			Version:  1,
			Template: "immutable content",
		})
	}))
	defer ts.Close()

	client := NewClient(ts.URL, "ltn_proj_test")

	// First call — should hit the server.
	_, err := client.GetPromptVersion("immutable", 1)
	if err != nil {
		t.Fatalf("GetPromptVersion: %v", err)
	}

	// Second call — should use cache.
	_, err = client.GetPromptVersion("immutable", 1)
	if err != nil {
		t.Fatalf("GetPromptVersion: %v", err)
	}

	if requestCount.Load() != 1 {
		t.Errorf("expected 1 request (version cache, no TTL), got %d", requestCount.Load())
	}
}

// TestClient_GetPrompt_NotFound verifies 404 returns an error.
func TestClient_GetPrompt_NotFound(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer ts.Close()

	client := NewClient(ts.URL, "ltn_proj_test")
	_, err := client.GetPrompt("nonexistent", "production")
	if err == nil {
		t.Error("expected error for nonexistent prompt, got nil")
	}
}

// TestClient_WriteScore verifies WriteScore sends a score to the server.
func TestClient_WriteScore(t *testing.T) {
	var receivedScore domain.ScoreRequest

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if err := json.NewDecoder(r.Body).Decode(&receivedScore); err != nil {
			http.Error(w, "invalid JSON", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]string{"score_id": "score-123"})
	}))
	defer ts.Close()

	client := NewClient(ts.URL, "ltn_proj_test")

	err := client.WriteScore("span-abc", "helpfulness", 0.8, "Great answer")
	if err != nil {
		t.Fatalf("WriteScore: %v", err)
	}

	if receivedScore.SpanID != "span-abc" {
		t.Errorf("span_id: got %q, want %q", receivedScore.SpanID, "span-abc")
	}
	if receivedScore.EvalName != "helpfulness" {
		t.Errorf("eval_name: got %q, want %q", receivedScore.EvalName, "helpfulness")
	}
	if receivedScore.Value != 0.8 {
		t.Errorf("value: got %f, want 0.8", receivedScore.Value)
	}
	if receivedScore.Reasoning != "Great answer" {
		t.Errorf("reasoning: got %q, want %q", receivedScore.Reasoning, "Great answer")
	}
}

// TestClient_WriteScore_RequiresSpanID verifies WriteScore fails for empty span ID.
func TestClient_WriteScore_RequiresSpanID(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "span_id is required", http.StatusBadRequest)
	}))
	defer ts.Close()

	client := NewClient(ts.URL, "ltn_proj_test")
	err := client.WriteScore("", "eval", 1.0, "no span")
	if err == nil {
		t.Error("expected error for empty span ID")
	}
}

// TestClient_GetPrompt_ServerError verifies HTTP errors are returned.
func TestClient_GetPrompt_ServerError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal error", http.StatusInternalServerError)
	}))
	defer ts.Close()

	client := NewClient(ts.URL, "ltn_proj_test")
	_, err := client.GetPrompt("test", "production")
	if err == nil {
		t.Error("expected error for server error, got nil")
	}
}

// TestClient_BaseURL verifies the client sends requests to the correct baseURL.
func TestClient_BaseURL(t *testing.T) {
	var receivedPath string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(domain.PromptVersion{
			Name:     "test",
			Version:  1,
			Template: "test",
		})
	}))
	defer ts.Close()

	client := NewClient(ts.URL, "ltn_proj_test")
	_, err := client.GetPrompt("test", "production")
	if err != nil {
		t.Fatalf("GetPrompt: %v", err)
	}
	if receivedPath != "/api/v1/prompts/test" {
		t.Errorf("path: got %q, want %q", receivedPath, "/api/v1/prompts/test")
	}
}

// TestClient_GetPromptVersion_BaseURL verifies version endpoint path.
func TestClient_GetPromptVersion_BaseURL(t *testing.T) {
	var receivedPath string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(domain.PromptVersion{
			Name:     "test",
			Version:  3,
			Template: "version 3",
		})
	}))
	defer ts.Close()

	client := NewClient(ts.URL, "ltn_proj_test")
	_, err := client.GetPromptVersion("test", 3)
	if err != nil {
		t.Fatalf("GetPromptVersion: %v", err)
	}
	if receivedPath != "/api/v1/prompts/test" {
		t.Errorf("path: got %q, want %q", receivedPath, "/api/v1/prompts/test")
	}
}

// TestClient_SlowResponse verifies the client handles slightly slow responses
// without error (the 10-second timeout is generous enough for this 100ms delay).
func TestClient_SlowResponse(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(domain.PromptVersion{
			Name:     "test",
			Version:  1,
			Template: "test",
		})
	}))
	defer ts.Close()

	client := NewClient(ts.URL, "ltn_proj_test")
	_, err := client.GetPrompt("test", "production")
	if err != nil {
		t.Errorf("expected no error, got: %v", err)
	}
}
