package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/omneval/omneval/internal/pricing"
)

func TestHandleModels_NilPricingReturnsEmptyList(t *testing.T) {
	h := &ModelsHandler{Pricing: nil}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/models", nil)
	w := httptest.NewRecorder()

	h.HandleModels(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusOK)
		return
	}

	contentType := w.Header().Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("Content-Type: got %q, want %q", contentType, "application/json")
		return
	}

	var models []string
	if err := json.NewDecoder(w.Body).Decode(&models); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if len(models) != 0 {
		t.Errorf("models: got %d entries, want 0 (nil pricing table should return empty list)", len(models))
	}
}

func TestHandleModels_PopulatedPricingReturnsModels(t *testing.T) {
	pricingTable := pricing.NewTableFromBytes([]byte(`{
		"gpt-4o": {
			"input_cost_per_token": 0.0000025,
			"output_cost_per_token": 0.000010
		},
		"claude-sonnet-4-6": {
			"input_cost_per_token": 0.000003,
			"output_cost_per_token": 0.000015
		}
	}`))

	h := &ModelsHandler{Pricing: pricingTable}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/models", nil)
	w := httptest.NewRecorder()

	h.HandleModels(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status: got %d, want %d\nbody: %s", w.Code, http.StatusOK, w.Body.String())
		return
	}

	contentType := w.Header().Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("Content-Type: got %q, want %q", contentType, "application/json")
		return
	}

	var models []string
	if err := json.NewDecoder(w.Body).Decode(&models); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if len(models) != 2 {
		t.Errorf("models: got %d entries, want 2", len(models))
		return
	}

	// Models should be sorted alphabetically.
	if models[0] != "claude-sonnet-4-6" || models[1] != "gpt-4o" {
		t.Errorf("models: got %v, want [claude-sonnet-4-6 gpt-4o]", models)
	}
}

func TestHandleModelsPriced_ReportsPricedStatusPerModel(t *testing.T) {
	pricingTable := pricing.NewTableFromBytes([]byte(`{
		"gpt-4o": {
			"input_cost_per_token": 0.0000025,
			"output_cost_per_token": 0.000010
		}
	}`))
	h := &ModelsHandler{Pricing: pricingTable}

	// Provider prefixes and case must be normalized before lookup; response keys
	// echo the models exactly as the client sent them.
	req := httptest.NewRequest(http.MethodGet, "/api/v1/models/priced?models=openai/GPT-4o,my-local-llama", nil)
	w := httptest.NewRecorder()

	h.HandleModelsPriced(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want %d\nbody: %s", w.Code, http.StatusOK, w.Body.String())
	}

	var resp map[string]bool
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if got, ok := resp["openai/GPT-4o"]; !ok || !got {
		t.Errorf("openai/GPT-4o: got %v (present=%v), want true", got, ok)
	}
	if got, ok := resp["my-local-llama"]; !ok || got {
		t.Errorf("my-local-llama: got %v (present=%v), want false", got, ok)
	}
}

func TestHandleModelsPriced_NilPricingReportsAllUnpriced(t *testing.T) {
	h := &ModelsHandler{Pricing: nil}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/models/priced?models=gpt-4o", nil)
	w := httptest.NewRecorder()

	h.HandleModelsPriced(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want %d", w.Code, http.StatusOK)
	}
	var resp map[string]bool
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["gpt-4o"] {
		t.Errorf("gpt-4o: got true, want false with nil pricing table")
	}
}

func TestHandleModels_MethodNotAllowed(t *testing.T) {
	pricingTable := pricing.NewTableFromBytes([]byte(`{}`))
	h := &ModelsHandler{Pricing: pricingTable}

	methods := []string{http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch}
	for _, method := range methods {
		t.Run(method, func(t *testing.T) {
			req := httptest.NewRequest(method, "/api/v1/models", nil)
			w := httptest.NewRecorder()

			h.HandleModels(w, req)

			if w.Code != http.StatusMethodNotAllowed {
				t.Errorf("status for %s: got %d, want %d", method, w.Code, http.StatusMethodNotAllowed)
			}
		})
	}
}