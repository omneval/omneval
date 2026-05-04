package handler

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/zbloss/lantern/internal/domain"
)

// ScoreHandler handles POST /internal/v1/scores — the internal-only endpoint
// called by Eval Workers to write completed scores back to DuckDB.
// Not exposed outside the cluster.
type ScoreHandler struct {
	db *sql.DB
}

// ScoreRequest is the JSON body for POST /internal/v1/scores.
type ScoreRequest struct {
	ScoreID       string  `json:"score_id"`
	SpanID        string  `json:"span_id"`
	TraceID       string  `json:"trace_id"`
	ProjectID     string  `json:"project_id"`
	EvalName      string  `json:"eval_name"`
	Value         float64 `json:"value"`
	Reasoning     string  `json:"reasoning"`
	JudgeModel    string  `json:"judge_model"`
	PromptName    string  `json:"prompt_name"`
	PromptVersion int64   `json:"prompt_version"`
}

// New creates a new ScoreHandler.
func New(db *sql.DB) http.Handler {
	h := &ScoreHandler{db: db}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /internal/v1/scores", h.HandleScores)
	return mux
}

// HandleScores writes a single score to DuckDB.
func (h *ScoreHandler) HandleScores(w http.ResponseWriter, r *http.Request) {
	var req ScoreRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}

	score := &domain.Score{
		ScoreID:       req.ScoreID,
		SpanID:        req.SpanID,
		TraceID:       req.TraceID,
		ProjectID:     req.ProjectID,
		EvalName:      req.EvalName,
		Value:         req.Value,
		Reasoning:     req.Reasoning,
		JudgeModel:    req.JudgeModel,
		PromptName:    req.PromptName,
		PromptVersion: req.PromptVersion,
		CreatedAt:     time.Now(),
	}

	if err := h.writeScore(r.Context(), score); err != nil {
		http.Error(w, fmt.Sprintf("write score: %v", err), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
}

// writeScore writes a single score to DuckDB.
func (h *ScoreHandler) writeScore(ctx context.Context, score *domain.Score) error {
	_, err := h.db.ExecContext(ctx, `
		INSERT INTO scores (
			score_id, span_id, trace_id, project_id,
			eval_name, value, reasoning, judge_model,
			prompt_name, prompt_version, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		score.ScoreID,
		score.SpanID,
		score.TraceID,
		score.ProjectID,
		score.EvalName,
		score.Value,
		score.Reasoning,
		score.JudgeModel,
		score.PromptName,
		score.PromptVersion,
		score.CreatedAt,
	)
	return err
}
