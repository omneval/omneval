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