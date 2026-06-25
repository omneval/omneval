package handler

import (
	"encoding/json"
	"net/http"

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

// Routes returns the models endpoint as an AuthRoute entry with
// AuthPolicyPublic so the Router can use it for policy-based auth dispatch.
// Implements the RouteGroup interface.
func (h *ModelsHandler) Routes() []AuthRoute {
	return []AuthRoute{
		{Method: http.MethodGet, Path: "/api/v1/models", Handler: http.HandlerFunc(h.HandleModels), Policy: AuthPolicyPublic},
	}
}