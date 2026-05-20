package handler

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/marcboeker/go-duckdb/v2"
	"github.com/omneval/omneval/internal/domain"
)

func TestHandleScores_AuthRequired(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/scores", strings.NewReader(`{}`))
	w := httptest.NewRecorder()

	// ScoreHandler doesn't check auth in this handler — it's a public endpoint
	// that accepts scores from the UI/API. This test verifies the request
	// doesn't get rejected for missing auth.
	h := NewScoreHandler(nil)
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

	h := NewScoreHandler(nil)
	h.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandleScores_InvalidJSON(t *testing.T) {
	body := strings.NewReader(`not json`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/scores", body)
	w := httptest.NewRecorder()

	h := NewScoreHandler(nil)
	h.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandleScores_WritesToDB(t *testing.T) {
	// Create a temp DuckDB file.
	tmpDir, err := os.MkdirTemp("", "omneval-score-test")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)
	tmpPath := tmpDir + "/test.duckdb"

	db, err := sql.Open("duckdb", tmpPath)
	if err != nil {
		t.Fatalf("open duckdb: %v", err)
	}
	defer db.Close()

	// Create the scores table.
	if _, err := db.ExecContext(context.Background(), `
		CREATE TABLE IF NOT EXISTS scores (
			score_id   VARCHAR NOT NULL PRIMARY KEY,
			span_id    VARCHAR NOT NULL,
			trace_id   VARCHAR NOT NULL,
			project_id VARCHAR NOT NULL,
			eval_name  VARCHAR,
			value      DOUBLE,
			reasoning  VARCHAR,
			judge_model VARCHAR,
			prompt_name VARCHAR,
			prompt_version BIGINT,
			created_at TIMESTAMPTZ NOT NULL
		);
	`); err != nil {
		t.Fatalf("create table: %v", err)
	}

	// Also create the spans table for the withScores test.
	if _, err := db.ExecContext(context.Background(), `
		CREATE TABLE IF NOT EXISTS spans (
			span_id        VARCHAR NOT NULL,
			trace_id       VARCHAR NOT NULL,
			parent_id      VARCHAR,
			project_id     VARCHAR NOT NULL,
			service_name   VARCHAR,
			name           VARCHAR,
			kind           VARCHAR,
			start_time     TIMESTAMPTZ NOT NULL,
			end_time       TIMESTAMPTZ,
			model          VARCHAR,
			input          JSON,
			output         JSON,
			input_tokens   BIGINT,
			output_tokens  BIGINT,
			cost_usd       DOUBLE,
			prompt_name    VARCHAR,
			prompt_version BIGINT,
			status_code    VARCHAR,
			status_message VARCHAR,
			attributes     JSON,
			PRIMARY KEY (trace_id, span_id)
		);
	`); err != nil {
		t.Fatalf("create spans table: %v", err)
	}

	h := NewScoreHandler(db)

	// Insert a test span.
	baseTime := time.Date(2025, 1, 1, 10, 0, 0, 0, time.UTC)
	if _, err := db.ExecContext(context.Background(),
		`INSERT INTO spans (span_id, trace_id, project_id, model, start_time, end_time) VALUES (?, ?, ?, ?, ?, ?)`,
		"span-001", "trace-abc", "test-proj", "gpt-4",
		baseTime, baseTime.Add(10*time.Second)); err != nil {
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
	if err := db.QueryRowContext(context.Background(),
		`SELECT score_id, span_id, eval_name, value, reasoning, judge_model
		 FROM scores WHERE score_id = ?`, scoreID).Scan(
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

func TestWithScores_AttachesScoresToSpans(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "omneval-scores-test")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)
	tmpPath := tmpDir + "/test.duckdb"

	db, err := sql.Open("duckdb", tmpPath)
	if err != nil {
		t.Fatalf("open duckdb: %v", err)
	}
	defer db.Close()

	// Create tables.
	if _, err := db.ExecContext(context.Background(), `
		CREATE TABLE IF NOT EXISTS spans (
			span_id        VARCHAR NOT NULL,
			trace_id       VARCHAR NOT NULL,
			parent_id      VARCHAR,
			project_id     VARCHAR NOT NULL,
			service_name   VARCHAR,
			name           VARCHAR,
			kind           VARCHAR,
			start_time     TIMESTAMPTZ NOT NULL,
			end_time       TIMESTAMPTZ,
			model          VARCHAR,
			input          JSON,
			output         JSON,
			input_tokens   BIGINT,
			output_tokens  BIGINT,
			cost_usd       DOUBLE,
			prompt_name    VARCHAR,
			prompt_version BIGINT,
			status_code    VARCHAR,
			status_message VARCHAR,
			attributes     JSON,
			PRIMARY KEY (trace_id, span_id)
		);
		CREATE TABLE IF NOT EXISTS scores (
			score_id       VARCHAR NOT NULL PRIMARY KEY,
			span_id        VARCHAR NOT NULL,
			trace_id       VARCHAR NOT NULL,
			project_id     VARCHAR NOT NULL,
			eval_name      VARCHAR,
			value          DOUBLE,
			reasoning      VARCHAR,
			judge_model    VARCHAR,
			prompt_name    VARCHAR,
			prompt_version BIGINT,
			created_at     TIMESTAMPTZ NOT NULL
		);
	`); err != nil {
		t.Fatalf("create tables: %v", err)
	}

	// Insert spans and scores.
	baseTime := time.Date(2025, 1, 1, 10, 0, 0, 0, time.UTC)
	if _, err := db.ExecContext(context.Background(),
		`INSERT INTO spans (span_id, trace_id, project_id, model, start_time, end_time) VALUES
		 (?, ?, ?, ?, ?, ?),
		 (?, ?, ?, ?, ?, ?)`,
		"span-001", "trace-abc", "test-proj", "gpt-4",
		baseTime, baseTime.Add(10*time.Second),
		"span-002", "trace-abc", "test-proj", "gpt-4",
		baseTime.Add(time.Second), baseTime.Add(20*time.Second)); err != nil {
		t.Fatalf("insert spans: %v", err)
	}

	if _, err := db.ExecContext(context.Background(),
		`INSERT INTO scores (score_id, span_id, trace_id, project_id, eval_name, value, reasoning, judge_model, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"score-1", "span-001", "trace-abc", "test-proj", "helpfulness", 0.8, "Good", "claude-4",
		baseTime); err != nil {
		t.Fatalf("insert score: %v", err)
	}

	// Create spans without scores attached.
	spans := []*domain.Span{
		{SpanID: "span-001", TraceID: "trace-abc", ProjectID: "test-proj"},
		{SpanID: "span-002", TraceID: "trace-abc", ProjectID: "test-proj"},
	}

	// Load scores.
	loaded := withScores(db, spans, "trace-abc", "test-proj")

	// span-001 should have scores, span-002 should not.
	if len(loaded[0].Scores) != 1 {
		t.Errorf("span-001: got %d scores, want 1", len(loaded[0].Scores))
	} else {
		if loaded[0].Scores[0].EvalName != "helpfulness" {
			t.Errorf("span-001 score eval_name: got %q, want %q", loaded[0].Scores[0].EvalName, "helpfulness")
		}
		if loaded[0].Scores[0].Value != 0.8 {
			t.Errorf("span-001 score value: got %f, want 0.8", loaded[0].Scores[0].Value)
		}
	}

	if len(loaded[1].Scores) != 0 {
		t.Errorf("span-002: got %d scores, want 0", len(loaded[1].Scores))
	}
}
