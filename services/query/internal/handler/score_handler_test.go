package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	_ "github.com/duckdb/duckdb-go/v2"
	"github.com/omneval/omneval/internal/domain"
	_ "github.com/omneval/omneval/internal/duckdbfix"
)

func TestHandleScores_AuthRequired(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/scores", strings.NewReader(`{}`))
	w := httptest.NewRecorder()

	// ScoreHandler doesn't check auth in this handler — it's a public endpoint
	// that accepts scores from the UI/API. This test verifies the request
	// doesn't get rejected for missing auth.
	h := NewScoreHandler(nil, nil)
	h.ServeHTTP(w, req)

	// It should proceed to validation (missing required fields).
	if w.Code == http.StatusUnauthorized {
		t.Errorf("score handler should not require auth for POST /api/v1/scores, got %d", w.Code)
	}
}

func TestHandleScores_MissingFields(t *testing.T) {
	body := strings.NewReader(`{"eval_name": "test"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/scores", body)
	w := httptest.NewRecorder()

	h := NewScoreHandler(nil, nil)
	h.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandleScores_InvalidJSON(t *testing.T) {
	body := strings.NewReader(`not json`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/scores", body)
	w := httptest.NewRecorder()

	h := NewScoreHandler(nil, nil)
	h.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandleScores_WritesToDB(t *testing.T) {
	lk := setupTestLake(t)

	h := NewScoreHandler(lk, lk.DB())

	// Insert a test span (proj-1/trace-1 already seeded by setupTestLake;
	// add a dedicated span for this test's project).
	baseTime := time.Date(2025, 1, 1, 10, 0, 0, 0, time.UTC)
	span := &domain.Span{
		SpanID:    "span-001",
		TraceID:   "trace-abc",
		ProjectID: "test-proj",
		Model:     "gpt-4",
		StartTime: baseTime,
		EndTime:   baseTime.Add(10 * time.Second),
	}
	if err := lk.InsertSpans(context.Background(), []*domain.Span{span}); err != nil {
		t.Fatalf("insert span: %v", err)
	}

	// Make score write request.
	scoreReq := domain.ScoreRequest{
		SpanID:        "span-001",
		TraceID:       "trace-abc",
		ProjectID:     "test-proj",
		EvalName:      "helpfulness",
		Value:         0.8,
		Reasoning:     "Good response",
		JudgeModel:    "claude-sonnet-4-6",
		PromptName:    "helpfulness-judge",
		PromptVersion: 1,
	}
	body, _ := json.Marshal(scoreReq)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/scores", strings.NewReader(string(body)))
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("status: got %d, want %d\nbody: %s", w.Code, http.StatusCreated, w.Body.String())
		return
	}

	// Parse response to get score_id.
	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	scoreID := resp["score_id"]
	if scoreID == "" {
		t.Fatal("expected score_id in response")
	}

	// Verify the score was written to the database.
	var stored domain.Score
	if err := lk.DB().QueryRowContext(context.Background(),
		`SELECT score_id, span_id, eval_name, value, reasoning, judge_model
		 FROM lake.scores WHERE score_id = ?`, scoreID).Scan(
		&stored.ScoreID, &stored.SpanID, &stored.EvalName, &stored.Value, &stored.Reasoning, &stored.JudgeModel,
	); err != nil {
		t.Fatalf("query score: %v", err)
	}

	if stored.SpanID != "span-001" {
		t.Errorf("span_id: got %q, want %q", stored.SpanID, "span-001")
	}
	if stored.EvalName != "helpfulness" {
		t.Errorf("eval_name: got %q, want %q", stored.EvalName, "helpfulness")
	}
	if stored.Value != 0.8 {
		t.Errorf("value: got %f, want 0.8", stored.Value)
	}
	if stored.Reasoning != "Good response" {
		t.Errorf("reasoning: got %q, want %q", stored.Reasoning, "Good response")
	}
	if stored.JudgeModel != "claude-sonnet-4-6" {
		t.Errorf("judge_model: got %q, want %q", stored.JudgeModel, "claude-sonnet-4-6")
	}
}

// Stub: withScores moved to spansegment package.
