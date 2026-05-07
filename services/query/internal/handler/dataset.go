package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/zbloss/lantern/internal/domain"
	"github.com/zbloss/lantern/internal/metadata"
)

// DatasetHandler handles dataset CRUD endpoints:
//   POST   /api/v1/datasets                       — create a dataset
//   GET    /api/v1/datasets                       — list all datasets for the project
//   GET    /api/v1/datasets/:id                   — get dataset by ID
//   POST   /api/v1/datasets/:id/items             — add items to a dataset
//   GET    /api/v1/datasets/:id/items             — list items with keyset cursor pagination
//   DELETE /api/v1/datasets/:id                   — delete a dataset and its items

type DatasetHandler struct {
	Store        metadata.Store
	SessionStore SessionStore
}

// ---- Request / Response Types ----

// CreateDatasetRequest is the body accepted by POST /api/v1/datasets.
type CreateDatasetRequest struct {
	Name string `json:"name"`
}

// datasetListItem represents a dataset in the list endpoint response.
// Each entry includes its item count.
type datasetListItem struct {
	DatasetID string `json:"dataset_id"`
	Name      string `json:"name"`
	CreatedAt string `json:"created_at"`
	ItemCount int    `json:"item_count"`
}

// ListDatasetResponse is the response body for the list endpoint.
type ListDatasetResponse struct {
	Datasets []datasetListItem `json:"datasets"`
}

// GetDatasetResponse is returned by GET /api/v1/datasets/:id.
type GetDatasetResponse struct {
	DatasetID string `json:"dataset_id"`
	ProjectID string `json:"project_id"`
	Name      string `json:"name"`
	CreatedAt string `json:"created_at"`
	ItemCount int    `json:"item_count"`
}

// AddDatasetItemRequest is the body accepted by POST /api/v1/datasets/:id/items.
type AddDatasetItemRequest struct {
	Input          string `json:"input"`
	ExpectedOutput string `json:"expected_output,omitempty"`
	SourceSpanID   string `json:"source_span_id,omitempty"`
}

// DatasetItemResponse represents a single dataset item in list responses.
type datasetItemResponse struct {
	ItemID         string `json:"item_id"`
	DatasetID      string `json:"dataset_id"`
	SourceSpanID   string `json:"source_span_id"`
	Input          string `json:"input"`
	ExpectedOutput string `json:"expected_output"`
	CreatedAt      string `json:"created_at"`
}

// ListDatasetItemsResponse is returned by GET /api/v1/datasets/:id/items.
type ListDatasetItemsResponse struct {
	Items      []datasetItemResponse `json:"items"`
	Next       string                `json:"next,omitempty"`
	Limit      int                   `json:"limit"`
	PageCount  int                   `json:"page_count"`
}

// ---- HTTP Handlers ----

// HandleCreate handles POST /api/v1/datasets.
func (h *DatasetHandler) HandleCreate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	projectID, ok := extractProjectID(h.SessionStore, r)
	if !ok || projectID == "" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var req CreateDatasetRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}

	if req.Name == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}

	ds := &domain.Dataset{
		DatasetID: uuid.New().String(),
		ProjectID: projectID,
		Name:      req.Name,
		CreatedAt: time.Now().UTC(),
	}

	if err := h.Store.CreateDataset(r.Context(), ds); err != nil {
		http.Error(w, "store error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(ds)
}

// HandleList handles GET /api/v1/datasets.
func (h *DatasetHandler) HandleList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	projectID, ok := extractProjectID(h.SessionStore, r)
	if !ok || projectID == "" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	datasets, err := h.Store.ListDatasets(r.Context(), projectID)
	if err != nil {
		http.Error(w, "store error", http.StatusInternalServerError)
		return
	}

	result := make([]datasetListItem, 0, len(datasets))
	for _, ds := range datasets {
		result = append(result, datasetListItem{
			DatasetID: ds.DatasetID,
			Name:      ds.Name,
			CreatedAt: ds.CreatedAt.Format(time.RFC3339),
			ItemCount: countItemsForDataset(r.Context(), h.Store, ds.DatasetID),
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(ListDatasetResponse{Datasets: result})
}

// HandleGet handles GET /api/v1/datasets/:id.
func (h *DatasetHandler) HandleGet(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	projectID, ok := extractProjectID(h.SessionStore, r)
	if !ok || projectID == "" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	datasetID := r.PathValue("id")
	if datasetID == "" {
		// Fallback for tests that don't use ServeMux pattern matching.
		datasetID = extractDatasetID(r.URL.Path)
	}
	if datasetID == "" {
		http.Error(w, "dataset ID is required", http.StatusBadRequest)
		return
	}

	ds, err := h.Store.GetDataset(r.Context(), datasetID)
	if err != nil {
		if errors.Is(err, metadata.ErrNotFound) {
			http.Error(w, "dataset not found", http.StatusNotFound)
			return
		}
		http.Error(w, "store error", http.StatusInternalServerError)
		return
	}

	// Verify ownership.
	if ds.ProjectID != projectID {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	resp := GetDatasetResponse{
		DatasetID: ds.DatasetID,
		ProjectID: ds.ProjectID,
		Name:      ds.Name,
		CreatedAt: ds.CreatedAt.Format(time.RFC3339),
		ItemCount: countItemsForDataset(r.Context(), h.Store, ds.DatasetID),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// HandleAddItems handles POST /api/v1/datasets/:id/items.
func (h *DatasetHandler) HandleAddItems(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	projectID, ok := extractProjectID(h.SessionStore, r)
	if !ok || projectID == "" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	datasetID := r.PathValue("id")
	if datasetID == "" {
		datasetID = extractDatasetID(r.URL.Path)
	}
	if datasetID == "" {
		http.Error(w, "dataset ID is required", http.StatusBadRequest)
		return
	}

	// Verify dataset exists and belongs to the project.
	ds, err := h.Store.GetDataset(r.Context(), datasetID)
	if err != nil {
		if errors.Is(err, metadata.ErrNotFound) {
			http.Error(w, "dataset not found", http.StatusNotFound)
			return
		}
		http.Error(w, "store error", http.StatusInternalServerError)
		return
	}
	if ds.ProjectID != projectID {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	var req AddDatasetItemRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}

	if req.Input == "" {
		http.Error(w, "input is required", http.StatusBadRequest)
		return
	}

	item := &domain.DatasetItem{
		ItemID:         uuid.New().String(),
		DatasetID:      datasetID,
		SourceSpanID:   req.SourceSpanID,
		Input:          req.Input,
		ExpectedOutput: req.ExpectedOutput,
		CreatedAt:      time.Now().UTC(),
	}

	if err := h.Store.CreateDatasetItem(r.Context(), item); err != nil {
		http.Error(w, "store error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(item)
}

// HandleListItems handles GET /api/v1/datasets/:id/items.
func (h *DatasetHandler) HandleListItems(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	projectID, ok := extractProjectID(h.SessionStore, r)
	if !ok || projectID == "" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	datasetID := r.PathValue("id")
	if datasetID == "" {
		datasetID = extractDatasetID(r.URL.Path)
	}
	if datasetID == "" {
		http.Error(w, "dataset ID is required", http.StatusBadRequest)
		return
	}

	// Verify dataset exists and belongs to the project.
	ds, err := h.Store.GetDataset(r.Context(), datasetID)
	if err != nil {
		if errors.Is(err, metadata.ErrNotFound) {
			http.Error(w, "dataset not found", http.StatusNotFound)
			return
		}
		http.Error(w, "store error", http.StatusInternalServerError)
		return
	}
	if ds.ProjectID != projectID {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	// Parse query parameters.
	limit := 50
	if lim := r.URL.Query().Get("limit"); lim != "" {
		var l int
		if _, err := fmt.Sscanf(lim, "%d", &l); err == nil && l > 0 && l <= 500 {
			limit = l
		}
	}

	cursorStr := r.URL.Query().Get("cursor")
	items, nextCursor, err := h.Store.ListDatasetItemsPaginated(r.Context(), datasetID, cursorStr, limit)
	if err != nil {
		http.Error(w, "store error", http.StatusInternalServerError)
		return
	}

	resp := ListDatasetItemsResponse{
		Items:      make([]datasetItemResponse, 0, len(items)),
		Next:       nextCursor,
		Limit:      limit,
		PageCount:  len(items),
	}
	for _, item := range items {
		resp.Items = append(resp.Items, datasetItemResponse{
			ItemID:         item.ItemID,
			DatasetID:      item.DatasetID,
			SourceSpanID:   item.SourceSpanID,
			Input:          item.Input,
			ExpectedOutput: item.ExpectedOutput,
			CreatedAt:      item.CreatedAt.Format(time.RFC3339),
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// HandleDelete handles DELETE /api/v1/datasets/:id.
func (h *DatasetHandler) HandleDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	projectID, ok := extractProjectID(h.SessionStore, r)
	if !ok || projectID == "" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	datasetID := r.PathValue("id")
	if datasetID == "" {
		datasetID = extractDatasetID(r.URL.Path)
	}
	if datasetID == "" {
		http.Error(w, "dataset ID is required", http.StatusBadRequest)
		return
	}

	// Verify the dataset exists and belongs to the project.
	existing, err := h.Store.GetDataset(r.Context(), datasetID)
	if err != nil {
		if errors.Is(err, metadata.ErrNotFound) {
			http.Error(w, "dataset not found", http.StatusNotFound)
			return
		}
		http.Error(w, "store error", http.StatusInternalServerError)
		return
	}
	if existing.ProjectID != projectID {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	if err := h.Store.DeleteDataset(r.Context(), datasetID); err != nil {
		if errors.Is(err, metadata.ErrNotFound) {
			http.Error(w, "dataset not found", http.StatusNotFound)
			return
		}
		http.Error(w, "store error", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// ---- Helpers ----

// countItemsForDataset counts the number of dataset items.
func countItemsForDataset(ctx context.Context, store metadata.Store, datasetID string) int {
	items, err := store.ListDatasetItems(ctx, datasetID)
	if err != nil {
		return 0
	}
	return len(items)
}

// extractDatasetID extracts the dataset ID from a URL path like
// "/api/v1/datasets/abc123" or "/api/v1/datasets/abc123/items".
func extractDatasetID(path string) string {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	// Expected: api, v1, datasets, <id>...
	for i, p := range parts {
		if p == "datasets" && i+1 < len(parts) {
			return parts[i+1]
		}
	}
	return ""
}
