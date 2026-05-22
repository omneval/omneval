package omneval

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/omneval/omneval/internal/domain"
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
			Name:     "greeting",
			Version:  1,
			Template: "Hello, {{.Name}}!",
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

	client := NewClient(ts.URL, "oev_proj_test")

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

	client := NewClient(ts.URL, "oev_proj_test")
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

	client := NewClient(ts.URL, "oev_proj_test")

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

	client := NewClient(ts.URL, "oev_proj_test")
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

	client := NewClient(ts.URL, "oev_proj_test")

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

	client := NewClient(ts.URL, "oev_proj_test")

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

	client := NewClient(ts.URL, "oev_proj_test")
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

	client := NewClient(ts.URL, "oev_proj_test")

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

	client := NewClient(ts.URL, "oev_proj_test")
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

	client := NewClient(ts.URL, "oev_proj_test")
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

	client := NewClient(ts.URL, "oev_proj_test")
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

	client := NewClient(ts.URL, "oev_proj_test")
	_, err := client.GetPromptVersion("test", 3)
	if err != nil {
		t.Fatalf("GetPromptVersion: %v", err)
	}
	if receivedPath != "/api/v1/prompts/test" {
		t.Errorf("path: got %q, want %q", receivedPath, "/api/v1/prompts/test")
	}
}

// TestClient_WriteScore_APIKeyHeader verifies WriteScore sends the X-API-Key header.
func TestClient_WriteScore_APIKeyHeader(t *testing.T) {
	var receivedAPIKey string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAPIKey = r.Header.Get("X-API-Key")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]string{"score_id": "score-123"})
	}))
	defer ts.Close()

	client := NewClient(ts.URL, "oev_proj_test")
	_ = client.WriteScore("span-abc", "helpfulness", 0.8, "Great answer")

	if receivedAPIKey != "oev_proj_test" {
		t.Errorf("X-API-Key header: got %q, want %q", receivedAPIKey, "oev_proj_test")
	}
}

// TestClient_GetPrompt_APIKeyHeader verifies GetPrompt sends the X-API-Key header.
func TestClient_GetPrompt_APIKeyHeader(t *testing.T) {
	var receivedAPIKey string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAPIKey = r.Header.Get("X-API-Key")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(domain.PromptVersion{
			Name:     "test",
			Version:  1,
			Template: "test",
		})
	}))
	defer ts.Close()

	client := NewClient(ts.URL, "oev_proj_test")
	_, _ = client.GetPrompt("test", "production")

	if receivedAPIKey != "oev_proj_test" {
		t.Errorf("X-API-Key header: got %q, want %q", receivedAPIKey, "oev_proj_test")
	}
}

// TestClient_GetPromptVersion_APIKeyHeader verifies GetPromptVersion sends the X-API-Key header.
func TestClient_GetPromptVersion_APIKeyHeader(t *testing.T) {
	var receivedAPIKey string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAPIKey = r.Header.Get("X-API-Key")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(domain.PromptVersion{
			Name:     "test",
			Version:  3,
			Template: "version 3",
		})
	}))
	defer ts.Close()

	client := NewClient(ts.URL, "oev_proj_test")
	_, _ = client.GetPromptVersion("test", 3)

	if receivedAPIKey != "oev_proj_test" {
		t.Errorf("X-API-Key header: got %q, want %q", receivedAPIKey, "oev_proj_test")
	}
}

// TestClient_NoAPIKeyOmitsHeader verifies an empty API key results in no X-API-Key header.
func TestClient_NoAPIKeyOmitsHeader(t *testing.T) {
	var receivedAPIKey string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAPIKey = r.Header.Get("X-API-Key")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(domain.PromptVersion{
			Name:     "test",
			Version:  1,
			Template: "test",
		})
	}))
	defer ts.Close()

	client := NewClient(ts.URL, "")
	_, _ = client.GetPrompt("test", "production")

	if receivedAPIKey != "" {
		t.Errorf("X-API-Key header should be empty when no API key provided: got %q", receivedAPIKey)
	}
}

// TestClient_CreatePrompt verifies CreatePrompt posts to /api/v1/prompts.
func TestClient_CreatePrompt(t *testing.T) {
	var receivedBody map[string]interface{}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if err := json.NewDecoder(r.Body).Decode(&receivedBody); err != nil {
			http.Error(w, "invalid JSON", http.StatusBadRequest)
			return
		}
		pv := domain.PromptVersionJSON{
			Name:     "greeting",
			Version:  1,
			Template: "Hello, {{.Name}}!",
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(pv)
	}))
	defer ts.Close()

	client := NewClient(ts.URL, "oev_proj_test")
	result, err := client.CreatePrompt("greeting", "Hello, {{.Name}}!", nil)
	if err != nil {
		t.Fatalf("CreatePrompt: %v", err)
	}

	if receivedBody["name"] != "greeting" {
		t.Errorf("name: got %v, want %q", receivedBody["name"], "greeting")
	}
	if receivedBody["template"] != "Hello, {{.Name}}!" {
		t.Errorf("template: got %v, want %q", receivedBody["template"], "Hello, {{.Name}}!")
	}
	if result.Name != "greeting" {
		t.Errorf("result.Name: got %q, want %q", result.Name, "greeting")
	}
	if result.Version != 1 {
		t.Errorf("result.Version: got %d, want 1", result.Version)
	}
}

// TestClient_CreatePrompt_RequiresName verifies CreatePrompt fails for empty name.
func TestClient_CreatePrompt_RequiresName(t *testing.T) {
	client := NewClient("http://localhost:8080", "oev_proj_test")
	_, err := client.CreatePrompt("", "template", nil)
	if err == nil {
		t.Error("expected error for empty name")
	}
}

// TestClient_CreatePrompt_RequiresTemplate verifies CreatePrompt fails for empty template.
func TestClient_CreatePrompt_RequiresTemplate(t *testing.T) {
	client := NewClient("http://localhost:8080", "oev_proj_test")
	_, err := client.CreatePrompt("my-prompt", "", nil)
	if err == nil {
		t.Error("expected error for empty template")
	}
}

// TestClient_CreatePrompt_APIKeyHeader verifies CreatePrompt sends X-API-Key header.
func TestClient_CreatePrompt_APIKeyHeader(t *testing.T) {
	var receivedAPIKey string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAPIKey = r.Header.Get("X-API-Key")
		pv := domain.PromptVersionJSON{Name: "test", Version: 1, Template: "t"}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(pv)
	}))
	defer ts.Close()

	client := NewClient(ts.URL, "oev_proj_test")
	_, _ = client.CreatePrompt("test", "template", nil)

	if receivedAPIKey != "oev_proj_test" {
		t.Errorf("X-API-Key header: got %q, want %q", receivedAPIKey, "oev_proj_test")
	}
}

// TestClient_ListPrompts verifies ListPrompts returns prompt summaries.
func TestClient_ListPrompts(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		items := []PromptListItem{
			{Name: "greeting", LatestVersion: 2, Labels: map[string]int64{"production": 2}},
			{Name: "eval", LatestVersion: 1, Labels: map[string]int64{}},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(items)
	}))
	defer ts.Close()

	client := NewClient(ts.URL, "oev_proj_test")
	result, err := client.ListPrompts()
	if err != nil {
		t.Fatalf("ListPrompts: %v", err)
	}
	if len(result) != 2 {
		t.Errorf("len: got %d, want 2", len(result))
	}
	if result[0].Name != "greeting" {
		t.Errorf("result[0].Name: got %q, want %q", result[0].Name, "greeting")
	}
	if result[0].LatestVersion != 2 {
		t.Errorf("result[0].LatestVersion: got %d, want 2", result[0].LatestVersion)
	}
}

// TestClient_ListPrompts_Empty verifies ListPrompts handles empty response.
func TestClient_ListPrompts_Empty(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]PromptListItem{})
	}))
	defer ts.Close()

	client := NewClient(ts.URL, "oev_proj_test")
	result, err := client.ListPrompts()
	if err != nil {
		t.Fatalf("ListPrompts: %v", err)
	}
	if len(result) != 0 {
		t.Errorf("expected empty list, got %d items", len(result))
	}
}

// TestClient_SetPromptLabel verifies SetPromptLabel sends PUT to the correct endpoint.
func TestClient_SetPromptLabel(t *testing.T) {
	var receivedBody map[string]interface{}
	var receivedPath string

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedPath = r.URL.Path
		if r.Method != http.MethodPut {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		json.NewDecoder(r.Body).Decode(&receivedBody)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"name": "greeting", "label": "production", "version": 2,
		})
	}))
	defer ts.Close()

	client := NewClient(ts.URL, "oev_proj_test")
	err := client.SetPromptLabel("greeting", "production", 2)
	if err != nil {
		t.Fatalf("SetPromptLabel: %v", err)
	}

	want := "/api/v1/prompts/greeting/labels/production"
	if receivedPath != want {
		t.Errorf("path: got %q, want %q", receivedPath, want)
	}
	versionVal, _ := receivedBody["version"].(float64)
	if int(versionVal) != 2 {
		t.Errorf("version: got %v, want 2", receivedBody["version"])
	}
}

// TestClient_SetPromptLabel_RequiresName verifies SetPromptLabel fails for empty name.
func TestClient_SetPromptLabel_RequiresName(t *testing.T) {
	client := NewClient("http://localhost:8080", "oev_proj_test")
	err := client.SetPromptLabel("", "production", 1)
	if err == nil {
		t.Error("expected error for empty name")
	}
}

// TestClient_ListEvalRules verifies ListEvalRules returns eval rules.
func TestClient_ListEvalRules(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		resp := map[string]interface{}{
			"rules": []map[string]interface{}{
				{
					"rule_id":        "rule-1",
					"name":           "helpfulness",
					"judge_model":    "gpt-4o-mini",
					"prompt_name":    "eval-prompt",
					"prompt_version": 1,
					"sample_rate":    1.0,
					"enabled":        true,
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	client := NewClient(ts.URL, "oev_proj_test")
	result, err := client.ListEvalRules()
	if err != nil {
		t.Fatalf("ListEvalRules: %v", err)
	}
	if len(result) != 1 {
		t.Errorf("len: got %d, want 1", len(result))
	}
	if result[0].Name != "helpfulness" {
		t.Errorf("Name: got %q, want %q", result[0].Name, "helpfulness")
	}
	if result[0].RuleID != "rule-1" {
		t.Errorf("RuleID: got %q, want %q", result[0].RuleID, "rule-1")
	}
}

// TestClient_CreateEvalRule verifies CreateEvalRule posts to /api/v1/eval-rules.
func TestClient_CreateEvalRule(t *testing.T) {
	var receivedBody map[string]interface{}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		json.NewDecoder(r.Body).Decode(&receivedBody)
		result := map[string]interface{}{
			"rule_id":        "rule-123",
			"name":           "helpfulness",
			"judge_model":    "gpt-4o-mini",
			"prompt_name":    "eval-prompt",
			"prompt_version": 1,
			"sample_rate":    1.0,
			"enabled":        true,
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(result)
	}))
	defer ts.Close()

	client := NewClient(ts.URL, "oev_proj_test")
	rule, err := client.CreateEvalRule("helpfulness", "eval-prompt", nil)
	if err != nil {
		t.Fatalf("CreateEvalRule: %v", err)
	}

	if receivedBody["name"] != "helpfulness" {
		t.Errorf("name: got %v, want %q", receivedBody["name"], "helpfulness")
	}
	if receivedBody["prompt_name"] != "eval-prompt" {
		t.Errorf("prompt_name: got %v, want %q", receivedBody["prompt_name"], "eval-prompt")
	}
	if rule.RuleID != "rule-123" {
		t.Errorf("RuleID: got %q, want %q", rule.RuleID, "rule-123")
	}
}

// TestClient_CreateEvalRule_RequiresName verifies CreateEvalRule fails for empty name.
func TestClient_CreateEvalRule_RequiresName(t *testing.T) {
	client := NewClient("http://localhost:8080", "oev_proj_test")
	_, err := client.CreateEvalRule("", "eval-prompt", nil)
	if err == nil {
		t.Error("expected error for empty name")
	}
}

// TestClient_CreateEvalRule_RequiresPromptName verifies CreateEvalRule fails for empty prompt_name.
func TestClient_CreateEvalRule_RequiresPromptName(t *testing.T) {
	client := NewClient("http://localhost:8080", "oev_proj_test")
	_, err := client.CreateEvalRule("helpfulness", "", nil)
	if err == nil {
		t.Error("expected error for empty prompt_name")
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

	client := NewClient(ts.URL, "oev_proj_test")
	_, err := client.GetPrompt("test", "production")
	if err != nil {
		t.Errorf("expected no error, got: %v", err)
	}
}
