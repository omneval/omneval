package handler

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/omneval/omneval/internal/pricing"
)

// ModelsHandler handles the GET /api/v1/models endpoint that returns the
// list of known model names sourced from the pricing table.
type ModelsHandler struct {
	Pricing *pricing.Table
}

// HandleModels returns a sorted list of known model names as JSON.
func (h *ModelsHandler) HandleModels(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSONError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var models []string
	if h.Pricing != nil {
		models = h.Pricing.Models()
	}
	if models == nil {
		models = []string{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(models)
}

// HandleModelsPriced reports, for each model in the comma-separated `models`
// query parameter, whether the pricing table knows a price for it. Model names
// are normalized (provider prefix stripped, lowercased) before lookup; response
// keys echo the models exactly as the client sent them. This lets the UI
// distinguish "$0 because priced at zero" from "$0 because unpriced".
func (h *ModelsHandler) HandleModelsPriced(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSONError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	resp := map[string]bool{}
	for _, model := range strings.Split(r.URL.Query().Get("models"), ",") {
		model = strings.TrimSpace(model)
		if model == "" {
			continue
		}
		priced := false
		if h.Pricing != nil {
			priced = h.Pricing.HasModel(pricing.NormalizeModel(model))
		}
		resp[model] = priced
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// Routes returns the models endpoints as AuthRoute entries with
// AuthPolicyPublic so the Router can use them for policy-based auth dispatch.
// Implements the RouteGroup interface.
func (h *ModelsHandler) Routes() []AuthRoute {
	return []AuthRoute{
		{Method: http.MethodGet, Path: "/api/v1/models", Handler: http.HandlerFunc(h.HandleModels), Policy: AuthPolicyPublic},
		{Method: http.MethodGet, Path: "/api/v1/models/priced", Handler: http.HandlerFunc(h.HandleModelsPriced), Policy: AuthPolicyPublic},
	}
}
