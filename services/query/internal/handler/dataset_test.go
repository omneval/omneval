package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/omneval/omneval/internal/domain"
	"github.com/omneval/omneval/internal/fake"
	"github.com/omneval/omneval/internal/metadata"
)

// ---- Tests for HandleCreate ----

func TestDatasetHandler_CreateDataset(t *testing.T) {
	store := fake.NewFakeMetadataStore()
	handler := &DatasetHandler{
		Store:        store,
		SessionStore: &FakeSessionStore{projectID: "test-proj"},
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/datasets", strings.NewReader(`{
		"name": "my-dataset"
	}`))
	w := httptest.NewRecorder()
	handler.HandleCreate(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("status: got %d, want %d\nbody: %s", w.Code, http.StatusCreated, w.Body.String())
	}

	var resp domain.Dataset
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if resp.Name != "my-dataset" {
		t.Errorf("name: got %q, want %q", resp.Name, "my-dataset")
	}
	if resp.ProjectID != "test-proj" {
		t.Errorf("project_id: got %q, want %q", resp.ProjectID, "test-proj")
	}
	if resp.DatasetID == "" {
		t.Error("dataset_id: expected non-empty UUID")
	}
}

func TestDatasetHandler_CreateDataset_MissingName(t *testing.T) {
	store := fake.NewFakeMetadataStore()
	handler := &DatasetHandler{
		Store:        store,
		SessionStore: &FakeSessionStore{projectID: "test-proj"},
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/datasets", strings.NewReader(`{}`))
	w := httptest.NewRecorder()
	handler.HandleCreate(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestDatasetHandler_CreateDataset_AuthRequired(t *testing.T) {
	store := fake.NewFakeMetadataStore()
	handler := &DatasetHandler{
		Store: store,
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/datasets", strings.NewReader(`{
		"name": "my-dataset"
	}`))
	w := httptest.NewRecorder()
	handler.HandleCreate(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestDatasetHandler_CreateDataset_MethodNotAllowed(t *testing.T) {
	store := fake.NewFakeMetadataStore()
	handler := &DatasetHandler{
		Store:        store,
		SessionStore: &FakeSessionStore{projectID: "test-proj"},
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/datasets", nil)
	w := httptest.NewRecorder()
	handler.HandleCreate(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusMethodNotAllowed)
	}
}

func TestDatasetHandler_CreateDataset_InvalidJSON(t *testing.T) {
	store := fake.NewFakeMetadataStore()
	handler := &DatasetHandler{
		Store:        store,
		SessionStore: &FakeSessionStore{projectID: "test-proj"},
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/datasets", strings.NewReader(`{invalid`))
	w := httptest.NewRecorder()
	handler.HandleCreate(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusBadRequest)
	}
}

// ---- Tests for HandleList ----

func TestDatasetHandler_ListDatasets(t *testing.T) {
	store := fake.NewFakeMetadataStore()
	handler := &DatasetHandler{
		Store:        store,
		SessionStore: &FakeSessionStore{projectID: "test-proj"},
	}

	ds1 := &domain.Dataset{DatasetID: uuid.New().String(), ProjectID: "test-proj", Name: "alpha"}
	ds2 := &domain.Dataset{DatasetID: uuid.New().String(), ProjectID: "test-proj", Name: "beta"}
	store.CreateDataset(context.Background(), ds1)
	store.CreateDataset(context.Background(), ds2)

	// Create a dataset for a different project — should not appear.
	otherDS := &domain.Dataset{DatasetID: uuid.New().String(), ProjectID: "other-proj", Name: "gamma"}
	store.CreateDataset(context.Background(), otherDS)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/datasets", nil)
	w := httptest.NewRecorder()
	handler.HandleList(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want %d\nbody: %s", w.Code, http.StatusOK, w.Body.String())
	}

	var resp struct {
		Datasets []struct {
			DatasetID string `json:"dataset_id"`
			Name      string `json:"name"`
			ItemCount int    `json:"item_count"`
		} `json:"datasets"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if len(resp.Datasets) != 2 {
		t.Errorf("datasets count: got %d, want 2", len(resp.Datasets))
	}
}

func TestDatasetHandler_ListDatasets_AuthRequired(t *testing.T) {
	store := fake.NewFakeMetadataStore()
	handler := &DatasetHandler{
		Store: store,
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/datasets", nil)
	w := httptest.NewRecorder()
	handler.HandleList(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestDatasetHandler_ListDatasets_EmptyList(t *testing.T) {
	store := fake.NewFakeMetadataStore()
	handler := &DatasetHandler{
		Store:        store,
		SessionStore: &FakeSessionStore{projectID: "empty-proj"},
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/datasets", nil)
	w := httptest.NewRecorder()
	handler.HandleList(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want %d", w.Code, http.StatusOK)
	}

	var resp struct {
		Datasets []struct {
			DatasetID string `json:"dataset_id"`
		} `json:"datasets"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if len(resp.Datasets) != 0 {
		t.Errorf("datasets count: got %d, want 0", len(resp.Datasets))
	}
}

func TestDatasetHandler_ListDatasets_MethodNotAllowed(t *testing.T) {
	store := fake.NewFakeMetadataStore()
	handler := &DatasetHandler{
		Store:        store,
		SessionStore: &FakeSessionStore{projectID: "test-proj"},
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/datasets", nil)
	w := httptest.NewRecorder()
	handler.HandleList(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusMethodNotAllowed)
	}
}

func TestDatasetHandler_ListDatasets_ItemCount(t *testing.T) {
	store := fake.NewFakeMetadataStore()
	handler := &DatasetHandler{
		Store:        store,
		SessionStore: &FakeSessionStore{projectID: "test-proj"},
	}

	ds := &domain.Dataset{DatasetID: uuid.New().String(), ProjectID: "test-proj", Name: "test"}
	store.CreateDataset(context.Background(), ds)

	// Add 3 items.
	for i := 0; i < 3; i++ {
		item := &domain.DatasetItem{
			ItemID:    uuid.New().String(),
			DatasetID: ds.DatasetID,
			Input:     "test input",
		}
		store.CreateDatasetItem(context.Background(), item)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/datasets", nil)
	w := httptest.NewRecorder()
	handler.HandleList(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d", w.Code)
	}

	var resp struct {
		Datasets []struct {
			DatasetID string `json:"dataset_id"`
			Name      string `json:"name"`
			ItemCount int    `json:"item_count"`
		} `json:"datasets"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if len(resp.Datasets) != 1 {
		t.Fatalf("datasets count: got %d, want 1", len(resp.Datasets))
	}
	if resp.Datasets[0].ItemCount != 3 {
		t.Errorf("item_count: got %d, want 3", resp.Datasets[0].ItemCount)
	}
}

// ---- Tests for HandleGet ----

func TestDatasetHandler_GetDataset(t *testing.T) {
	store := fake.NewFakeMetadataStore()
	handler := &DatasetHandler{
		Store:        store,
		SessionStore: &FakeSessionStore{projectID: "test-proj"},
	}

	ds := &domain.Dataset{DatasetID: uuid.New().String(), ProjectID: "test-proj", Name: "my-ds"}
	store.CreateDataset(context.Background(), ds)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/datasets/"+ds.DatasetID, nil)
	w := httptest.NewRecorder()
	handler.HandleGet(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status: got %d, want %d\nbody: %s", w.Code, http.StatusOK, w.Body.String())
	}

	var resp struct {
		DatasetID string `json:"dataset_id"`
		Name      string `json:"name"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if resp.DatasetID != ds.DatasetID {
		t.Errorf("dataset_id: got %q, want %q", resp.DatasetID, ds.DatasetID)
	}
	if resp.Name != "my-ds" {
		t.Errorf("name: got %q, want %q", resp.Name, "my-ds")
	}
}

func TestDatasetHandler_GetDataset_NotFound(t *testing.T) {
	store := fake.NewFakeMetadataStore()
	handler := &DatasetHandler{
		Store:        store,
		SessionStore: &FakeSessionStore{projectID: "test-proj"},
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/datasets/nonexistent", nil)
	w := httptest.NewRecorder()
	handler.HandleGet(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestDatasetHandler_GetDataset_Forbidden(t *testing.T) {
	store := fake.NewFakeMetadataStore()
	handler := &DatasetHandler{
		Store:        store,
		SessionStore: &FakeSessionStore{projectID: "test-proj"},
	}

	// Create dataset for a different project.
	otherDS := &domain.Dataset{DatasetID: uuid.New().String(), ProjectID: "other-proj", Name: "other"}
	store.CreateDataset(context.Background(), otherDS)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/datasets/"+otherDS.DatasetID, nil)
	w := httptest.NewRecorder()
	handler.HandleGet(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusForbidden)
	}
}

func TestDatasetHandler_GetDataset_AuthRequired(t *testing.T) {
	store := fake.NewFakeMetadataStore()
	handler := &DatasetHandler{
		Store: store,
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/datasets/some-id", nil)
	w := httptest.NewRecorder()
	handler.HandleGet(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestDatasetHandler_GetDataset_MethodNotAllowed(t *testing.T) {
	store := fake.NewFakeMetadataStore()
	handler := &DatasetHandler{
		Store:        store,
		SessionStore: &FakeSessionStore{projectID: "test-proj"},
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/datasets/some-id", nil)
	w := httptest.NewRecorder()
	handler.HandleGet(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusMethodNotAllowed)
	}
}

// ---- Tests for HandleAddItems ----

func TestDatasetHandler_AddItems(t *testing.T) {
	store := fake.NewFakeMetadataStore()
	handler := &DatasetHandler{
		Store:        store,
		SessionStore: &FakeSessionStore{projectID: "test-proj"},
	}

	ds := &domain.Dataset{DatasetID: uuid.New().String(), ProjectID: "test-proj", Name: "my-ds"}
	store.CreateDataset(context.Background(), ds)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/datasets/"+ds.DatasetID+"/items", strings.NewReader(`{
		"input": "hello world",
		"expected_output": "hi there"
	}`))
	w := httptest.NewRecorder()
	handler.HandleAddItems(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("status: got %d, want %d\nbody: %s", w.Code, http.StatusCreated, w.Body.String())
	}

	var resp domain.DatasetItem
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if resp.DatasetID != ds.DatasetID {
		t.Errorf("dataset_id: got %q, want %q", resp.DatasetID, ds.DatasetID)
	}
	if resp.Input != "hello world" {
		t.Errorf("input: got %q, want %q", resp.Input, "hello world")
	}
	if resp.ExpectedOutput != "hi there" {
		t.Errorf("expected_output: got %q, want %q", resp.ExpectedOutput, "hi there")
	}
}

func TestDatasetHandler_AddItems_MissingInput(t *testing.T) {
	store := fake.NewFakeMetadataStore()
	handler := &DatasetHandler{
		Store:        store,
		SessionStore: &FakeSessionStore{projectID: "test-proj"},
	}

	ds := &domain.Dataset{DatasetID: uuid.New().String(), ProjectID: "test-proj", Name: "my-ds"}
	store.CreateDataset(context.Background(), ds)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/datasets/"+ds.DatasetID+"/items", strings.NewReader(`{
		"expected_output": "hi"
	}`))
	w := httptest.NewRecorder()
	handler.HandleAddItems(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestDatasetHandler_AddItems_DatasetNotFound(t *testing.T) {
	store := fake.NewFakeMetadataStore()
	handler := &DatasetHandler{
		Store:        store,
		SessionStore: &FakeSessionStore{projectID: "test-proj"},
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/datasets/nonexistent/items", strings.NewReader(`{
		"input": "hello"
	}`))
	w := httptest.NewRecorder()
	handler.HandleAddItems(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestDatasetHandler_AddItems_Forbidden(t *testing.T) {
	store := fake.NewFakeMetadataStore()
	handler := &DatasetHandler{
		Store:        store,
		SessionStore: &FakeSessionStore{projectID: "test-proj"},
	}

	otherDS := &domain.Dataset{DatasetID: uuid.New().String(), ProjectID: "other-proj", Name: "other"}
	store.CreateDataset(context.Background(), otherDS)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/datasets/"+otherDS.DatasetID+"/items", strings.NewReader(`{
		"input": "hello"
	}`))
	w := httptest.NewRecorder()
	handler.HandleAddItems(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusForbidden)
	}
}

func TestDatasetHandler_AddItems_AuthRequired(t *testing.T) {
	store := fake.NewFakeMetadataStore()
	handler := &DatasetHandler{
		Store: store,
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/datasets/some-id/items", strings.NewReader(`{
		"input": "hello"
	}`))
	w := httptest.NewRecorder()
	handler.HandleAddItems(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestDatasetHandler_AddItems_MethodNotAllowed(t *testing.T) {
	store := fake.NewFakeMetadataStore()
	handler := &DatasetHandler{
		Store:        store,
		SessionStore: &FakeSessionStore{projectID: "test-proj"},
	}

	ds := &domain.Dataset{DatasetID: uuid.New().String(), ProjectID: "test-proj", Name: "my-ds"}
	store.CreateDataset(context.Background(), ds)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/datasets/"+ds.DatasetID+"/items", nil)
	w := httptest.NewRecorder()
	handler.HandleAddItems(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusMethodNotAllowed)
	}
}

func TestDatasetHandler_AddItems_WithSourceSpanID(t *testing.T) {
	store := fake.NewFakeMetadataStore()
	handler := &DatasetHandler{
		Store:        store,
		SessionStore: &FakeSessionStore{projectID: "test-proj"},
	}

	ds := &domain.Dataset{DatasetID: uuid.New().String(), ProjectID: "test-proj", Name: "my-ds"}
	store.CreateDataset(context.Background(), ds)

	spanID := "trace-abc-123"
	req := httptest.NewRequest(http.MethodPost, "/api/v1/datasets/"+ds.DatasetID+"/items", strings.NewReader(`{
		"input": "hello",
		"source_span_id": "`+spanID+`"
	}`))
	w := httptest.NewRecorder()
	handler.HandleAddItems(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("status: got %d, want %d", w.Code, http.StatusCreated)
	}

	var resp domain.DatasetItem
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if resp.SourceSpanID != spanID {
		t.Errorf("source_span_id: got %q, want %q", resp.SourceSpanID, spanID)
	}
}

func TestDatasetHandler_AddItems_InvalidJSON(t *testing.T) {
	store := fake.NewFakeMetadataStore()
	handler := &DatasetHandler{
		Store:        store,
		SessionStore: &FakeSessionStore{projectID: "test-proj"},
	}

	ds := &domain.Dataset{DatasetID: uuid.New().String(), ProjectID: "test-proj", Name: "my-ds"}
	store.CreateDataset(context.Background(), ds)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/datasets/"+ds.DatasetID+"/items", strings.NewReader(`{invalid`))
	w := httptest.NewRecorder()
	handler.HandleAddItems(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusBadRequest)
	}
}

// ---- Tests for HandleAddItemsBatch ----

func TestDatasetHandler_AddItemsBatch(t *testing.T) {
	store := fake.NewFakeMetadataStore()
	handler := &DatasetHandler{
		Store:        store,
		SessionStore: &FakeSessionStore{projectID: "test-proj"},
	}

	ds := &domain.Dataset{DatasetID: uuid.New().String(), ProjectID: "test-proj", Name: "my-ds"}
	store.CreateDataset(context.Background(), ds)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/datasets/"+ds.DatasetID+"/items/batch", strings.NewReader(`{
		"items": [
			{"input": "hello", "expected_output": "hi"},
			{"input": "world", "expected_output": "earth"}
		]
	}`))
	w := httptest.NewRecorder()
	handler.HandleAddItemsBatch(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("status: got %d, want %d\nbody: %s", w.Code, http.StatusCreated, w.Body.String())
	}

	var resp AddDatasetItemsBatchResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if resp.Created != 2 {
		t.Errorf("created count: got %d, want 2", resp.Created)
	}
}

func TestDatasetHandler_AddItemsBatch_EmptyItems(t *testing.T) {
	store := fake.NewFakeMetadataStore()
	handler := &DatasetHandler{
		Store:        store,
		SessionStore: &FakeSessionStore{projectID: "test-proj"},
	}

	ds := &domain.Dataset{DatasetID: uuid.New().String(), ProjectID: "test-proj", Name: "my-ds"}
	store.CreateDataset(context.Background(), ds)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/datasets/"+ds.DatasetID+"/items/batch", strings.NewReader(`{"items": []}`))
	w := httptest.NewRecorder()
	handler.HandleAddItemsBatch(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestDatasetHandler_AddItemsBatch_SkipsEmptyInput(t *testing.T) {
	store := fake.NewFakeMetadataStore()
	handler := &DatasetHandler{
		Store:        store,
		SessionStore: &FakeSessionStore{projectID: "test-proj"},
	}

	ds := &domain.Dataset{DatasetID: uuid.New().String(), ProjectID: "test-proj", Name: "my-ds"}
	store.CreateDataset(context.Background(), ds)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/datasets/"+ds.DatasetID+"/items/batch", strings.NewReader(`{
		"items": [
			{"input": "valid", "expected_output": "yes"},
			{"input": "", "expected_output": "no"},
			{"input": "also valid"}
		]
	}`))
	w := httptest.NewRecorder()
	handler.HandleAddItemsBatch(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("status: got %d, want %d", w.Code, http.StatusCreated)
	}

	var resp AddDatasetItemsBatchResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	// Only 2 items created (empty input skipped)
	if resp.Created != 2 {
		t.Errorf("created count: got %d, want 2", resp.Created)
	}

	// Verify only 2 items exist in store
	items, err := store.ListDatasetItems(context.Background(), ds.DatasetID)
	if err != nil {
		t.Fatalf("ListDatasetItems: %v", err)
	}
	if len(items) != 2 {
		t.Errorf("store items count: got %d, want 2", len(items))
	}
}

func TestDatasetHandler_AddItemsBatch_AuthRequired(t *testing.T) {
	store := fake.NewFakeMetadataStore()
	handler := &DatasetHandler{
		Store: store,
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/datasets/some-id/items/batch", strings.NewReader(`{"items": [{"input": "hello"}]}`))
	w := httptest.NewRecorder()
	handler.HandleAddItemsBatch(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestDatasetHandler_AddItemsBatch_DatasetNotFound(t *testing.T) {
	store := fake.NewFakeMetadataStore()
	handler := &DatasetHandler{
		Store:        store,
		SessionStore: &FakeSessionStore{projectID: "test-proj"},
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/datasets/nonexistent/items/batch", strings.NewReader(`{"items": [{"input": "hello"}]}`))
	w := httptest.NewRecorder()
	handler.HandleAddItemsBatch(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestDatasetHandler_AddItemsBatch_Forbidden(t *testing.T) {
	store := fake.NewFakeMetadataStore()
	handler := &DatasetHandler{
		Store:        store,
		SessionStore: &FakeSessionStore{projectID: "test-proj"},
	}

	otherDS := &domain.Dataset{DatasetID: uuid.New().String(), ProjectID: "other-proj", Name: "other"}
	store.CreateDataset(context.Background(), otherDS)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/datasets/"+otherDS.DatasetID+"/items/batch", strings.NewReader(`{"items": [{"input": "hello"}]}`))
	w := httptest.NewRecorder()
	handler.HandleAddItemsBatch(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusForbidden)
	}
}

func TestDatasetHandler_AddItemsBatch_MethodNotAllowed(t *testing.T) {
	store := fake.NewFakeMetadataStore()
	handler := &DatasetHandler{
		Store:        store,
		SessionStore: &FakeSessionStore{projectID: "test-proj"},
	}

	ds := &domain.Dataset{DatasetID: uuid.New().String(), ProjectID: "test-proj", Name: "my-ds"}
	store.CreateDataset(context.Background(), ds)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/datasets/"+ds.DatasetID+"/items/batch", nil)
	w := httptest.NewRecorder()
	handler.HandleAddItemsBatch(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusMethodNotAllowed)
	}
}

func TestDatasetHandler_AddItemsBatch_WithSourceSpanID(t *testing.T) {
	store := fake.NewFakeMetadataStore()
	handler := &DatasetHandler{
		Store:        store,
		SessionStore: &FakeSessionStore{projectID: "test-proj"},
	}

	ds := &domain.Dataset{DatasetID: uuid.New().String(), ProjectID: "test-proj", Name: "my-ds"}
	store.CreateDataset(context.Background(), ds)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/datasets/"+ds.DatasetID+"/items/batch", strings.NewReader(`{
		"items": [{
			"input": "hello",
			"source_span_id": "span-abc-123"
		}]
	}`))
	w := httptest.NewRecorder()
	handler.HandleAddItemsBatch(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("status: got %d, want %d", w.Code, http.StatusCreated)
	}

	items, err := store.ListDatasetItems(context.Background(), ds.DatasetID)
	if err != nil {
		t.Fatalf("ListDatasetItems: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("items count: got %d, want 1", len(items))
	}
	if items[0].SourceSpanID != "span-abc-123" {
		t.Errorf("source_span_id: got %q, want %q", items[0].SourceSpanID, "span-abc-123")
	}
}

// ---- Tests for HandleListItems ----

func TestDatasetHandler_ListItems(t *testing.T) {
	store := fake.NewFakeMetadataStore()
	handler := &DatasetHandler{
		Store:        store,
		SessionStore: &FakeSessionStore{projectID: "test-proj"},
	}

	ds := &domain.Dataset{DatasetID: uuid.New().String(), ProjectID: "test-proj", Name: "my-ds"}
	store.CreateDataset(context.Background(), ds)

	// Add items.
	for i := 0; i < 3; i++ {
		item := &domain.DatasetItem{
			ItemID:         uuid.New().String(),
			DatasetID:      ds.DatasetID,
			Input:          "input " + string(rune('0'+i)),
			ExpectedOutput: "output " + string(rune('0'+i)),
		}
		store.CreateDatasetItem(context.Background(), item)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/datasets/"+ds.DatasetID+"/items", nil)
	w := httptest.NewRecorder()
	handler.HandleListItems(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want %d\nbody: %s", w.Code, http.StatusOK, w.Body.String())
	}

	var resp struct {
		Items []struct {
			ItemID         string `json:"item_id"`
			Input          string `json:"input"`
			ExpectedOutput string `json:"expected_output"`
		} `json:"items"`
		Limit int `json:"limit"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if len(resp.Items) != 3 {
		t.Errorf("items count: got %d, want 3", len(resp.Items))
	}
	if resp.Limit != 50 {
		t.Errorf("limit: got %d, want 50", resp.Limit)
	}
}

func TestDatasetHandler_ListItems_AuthRequired(t *testing.T) {
	store := fake.NewFakeMetadataStore()
	handler := &DatasetHandler{
		Store: store,
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/datasets/some-id/items", nil)
	w := httptest.NewRecorder()
	handler.HandleListItems(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestDatasetHandler_ListItems_DatasetNotFound(t *testing.T) {
	store := fake.NewFakeMetadataStore()
	handler := &DatasetHandler{
		Store:        store,
		SessionStore: &FakeSessionStore{projectID: "test-proj"},
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/datasets/nonexistent/items", nil)
	w := httptest.NewRecorder()
	handler.HandleListItems(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestDatasetHandler_ListItems_Forbidden(t *testing.T) {
	store := fake.NewFakeMetadataStore()
	handler := &DatasetHandler{
		Store:        store,
		SessionStore: &FakeSessionStore{projectID: "test-proj"},
	}

	otherDS := &domain.Dataset{DatasetID: uuid.New().String(), ProjectID: "other-proj", Name: "other"}
	store.CreateDataset(context.Background(), otherDS)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/datasets/"+otherDS.DatasetID+"/items", nil)
	w := httptest.NewRecorder()
	handler.HandleListItems(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusForbidden)
	}
}

func TestDatasetHandler_ListItems_MethodNotAllowed(t *testing.T) {
	store := fake.NewFakeMetadataStore()
	handler := &DatasetHandler{
		Store:        store,
		SessionStore: &FakeSessionStore{projectID: "test-proj"},
	}

	ds := &domain.Dataset{DatasetID: uuid.New().String(), ProjectID: "test-proj", Name: "my-ds"}
	store.CreateDataset(context.Background(), ds)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/datasets/"+ds.DatasetID+"/items", nil)
	w := httptest.NewRecorder()
	handler.HandleListItems(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusMethodNotAllowed)
	}
}

func TestDatasetHandler_ListItems_WithLimit(t *testing.T) {
	store := fake.NewFakeMetadataStore()
	handler := &DatasetHandler{
		Store:        store,
		SessionStore: &FakeSessionStore{projectID: "test-proj"},
	}

	ds := &domain.Dataset{DatasetID: uuid.New().String(), ProjectID: "test-proj", Name: "my-ds"}
	store.CreateDataset(context.Background(), ds)

	// Add 5 items.
	for i := 0; i < 5; i++ {
		item := &domain.DatasetItem{
			ItemID:    uuid.New().String(),
			DatasetID: ds.DatasetID,
			Input:     "input",
		}
		store.CreateDatasetItem(context.Background(), item)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/datasets/"+ds.DatasetID+"/items?limit=2", nil)
	w := httptest.NewRecorder()
	handler.HandleListItems(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d", w.Code)
	}

	var resp struct {
		Items []struct{ ItemID string } `json:"items"`
		Limit int                       `json:"limit"`
		Next  string                    `json:"next"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if len(resp.Items) != 2 {
		t.Errorf("items count: got %d, want 2", len(resp.Items))
	}
	if resp.Limit != 2 {
		t.Errorf("limit: got %d, want 2", resp.Limit)
	}
	if resp.Next == "" {
		t.Error("next cursor: expected non-empty when more pages exist")
	}
}

func TestDatasetHandler_ListItems_CursorPagination(t *testing.T) {
	store := fake.NewFakeMetadataStore()
	handler := &DatasetHandler{
		Store:        store,
		SessionStore: &FakeSessionStore{projectID: "test-proj"},
	}

	ds := &domain.Dataset{DatasetID: uuid.New().String(), ProjectID: "test-proj", Name: "my-ds"}
	store.CreateDataset(context.Background(), ds)

	// Add 5 items.
	allItemIDs := make([]string, 5)
	for i := 0; i < 5; i++ {
		item := &domain.DatasetItem{
			ItemID:    uuid.New().String(),
			DatasetID: ds.DatasetID,
			Input:     "input",
		}
		store.CreateDatasetItem(context.Background(), item)
		allItemIDs[i] = item.ItemID
	}

	// First page.
	req := httptest.NewRequest(http.MethodGet, "/api/v1/datasets/"+ds.DatasetID+"/items?limit=2", nil)
	w := httptest.NewRecorder()
	handler.HandleListItems(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("first page status: got %d", w.Code)
	}

	var firstResp struct {
		Items []struct{ ItemID string } `json:"items"`
		Next  string                    `json:"next"`
	}
	if err := json.NewDecoder(w.Body).Decode(&firstResp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if len(firstResp.Items) != 2 {
		t.Errorf("first page items: got %d, want 2", len(firstResp.Items))
	}
	if firstResp.Next == "" {
		t.Error("first page next cursor: expected non-empty")
	}

	// Second page.
	req = httptest.NewRequest(http.MethodGet, "/api/v1/datasets/"+ds.DatasetID+"/items?limit=2&cursor="+firstResp.Next, nil)
	w = httptest.NewRecorder()
	handler.HandleListItems(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("second page status: got %d", w.Code)
	}

	var secondResp struct {
		Items []struct{ ItemID string } `json:"items"`
		Next  string                    `json:"next"`
	}
	if err := json.NewDecoder(w.Body).Decode(&secondResp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if len(secondResp.Items) != 2 {
		t.Errorf("second page items: got %d, want 2", len(secondResp.Items))
	}
}

// ---- Tests for HandleDelete ----

func TestDatasetHandler_DeleteDataset(t *testing.T) {
	store := fake.NewFakeMetadataStore()
	handler := &DatasetHandler{
		Store:        store,
		SessionStore: &FakeSessionStore{projectID: "test-proj"},
	}

	ds := &domain.Dataset{DatasetID: uuid.New().String(), ProjectID: "test-proj", Name: "to-delete"}
	store.CreateDataset(context.Background(), ds)

	// Add items to the dataset.
	for i := 0; i < 2; i++ {
		item := &domain.DatasetItem{
			ItemID:    uuid.New().String(),
			DatasetID: ds.DatasetID,
			Input:     "input",
		}
		store.CreateDatasetItem(context.Background(), item)
	}

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/datasets/"+ds.DatasetID, nil)
	w := httptest.NewRecorder()
	handler.HandleDelete(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("status: got %d, want %d\nbody: %s", w.Code, http.StatusNoContent, w.Body.String())
	}

	// Verify dataset is gone.
	_, err := store.GetDataset(context.Background(), ds.DatasetID)
	if err != metadata.ErrNotFound {
		t.Errorf("expected ErrNotFound after delete, got: %v", err)
	}

	// Verify items are gone.
	items, err := store.ListDatasetItems(context.Background(), ds.DatasetID)
	if err != nil {
		t.Fatalf("ListDatasetItems after delete: %v", err)
	}
	if len(items) != 0 {
		t.Errorf("items count after delete: got %d, want 0", len(items))
	}
}

func TestDatasetHandler_DeleteDataset_NotFound(t *testing.T) {
	store := fake.NewFakeMetadataStore()
	handler := &DatasetHandler{
		Store:        store,
		SessionStore: &FakeSessionStore{projectID: "test-proj"},
	}

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/datasets/nonexistent", nil)
	w := httptest.NewRecorder()
	handler.HandleDelete(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestDatasetHandler_DeleteDataset_Forbidden(t *testing.T) {
	store := fake.NewFakeMetadataStore()
	handler := &DatasetHandler{
		Store:        store,
		SessionStore: &FakeSessionStore{projectID: "test-proj"},
	}

	otherDS := &domain.Dataset{DatasetID: uuid.New().String(), ProjectID: "other-proj", Name: "other"}
	store.CreateDataset(context.Background(), otherDS)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/datasets/"+otherDS.DatasetID, nil)
	w := httptest.NewRecorder()
	handler.HandleDelete(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusForbidden)
	}
}

func TestDatasetHandler_DeleteDataset_AuthRequired(t *testing.T) {
	store := fake.NewFakeMetadataStore()
	handler := &DatasetHandler{
		Store: store,
	}

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/datasets/some-id", nil)
	w := httptest.NewRecorder()
	handler.HandleDelete(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestDatasetHandler_DeleteDataset_MethodNotAllowed(t *testing.T) {
	store := fake.NewFakeMetadataStore()
	handler := &DatasetHandler{
		Store:        store,
		SessionStore: &FakeSessionStore{projectID: "test-proj"},
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/datasets/some-id", nil)
	w := httptest.NewRecorder()
	handler.HandleDelete(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusMethodNotAllowed)
	}
}
