package handler

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/omneval/omneval/internal/domain"
	"github.com/omneval/omneval/services/writer/internal/metrics"
)

// ScoreLakeWriter commits scores to the Lake (ADR-0004). Implemented by
// *lake.Lake; an interface so tests can fake lake failures.
type ScoreLakeWriter interface {
	InsertScores(ctx context.Context, scores []*domain.Score) error
}

// ScoreMux handles POST /internal/v1/scores — the internal-only endpoint
// called by Eval Workers to write completed scores back to DuckDB.
// Not exposed outside the cluster.
type ScoreMux struct {
	db *sql.DB
	// lake, when non-nil, receives a dual-write of every score after the
	// legacy DuckDB write succeeds (writer.lake.enabled).
	lake ScoreLakeWriter
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

// New creates a new ScoreMux that handles POST /internal/v1/scores.
// lake may be nil (legacy-only writes).
func New(db *sql.DB, lake ScoreLakeWriter) http.Handler {
	h := &ScoreMux{db: db, lake: lake}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /internal/v1/scores", h.HandleScores)
	return mux
}

// HandleScores writes a single score to DuckDB.
func (h *ScoreMux) HandleScores(w http.ResponseWriter, r *http.Request) {
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

	h.dualWriteLake(r.Context(), score)

	w.WriteHeader(http.StatusCreated)
}

// dualWriteLake commits the score to the Lake after a successful legacy
// write. The score partitions by its span's start_time (ADR-0002), looked
// up from the legacy spans table where the span already landed. A
// lake-write failure must never fail the legacy write while dual-writing.
func (h *ScoreMux) dualWriteLake(ctx context.Context, score *domain.Score) {
	if h.lake == nil {
		return
	}

	var spanStart time.Time
	err := h.db.QueryRowContext(ctx,
		"SELECT start_time FROM spans WHERE trace_id = ? AND span_id = ?",
		score.TraceID, score.SpanID,
	).Scan(&spanStart)
	if err == nil {
		score.SpanStartTime = spanStart
	}

	if err := h.lake.InsertScores(ctx, []*domain.Score{score}); err != nil {
		slog.ErrorContext(ctx, "lake score write failed, legacy write kept",
			"score_id", score.ScoreID,
			"err", err)
		metrics.LakeWriteErrors.WithLabelValues("scores").Inc()
	}
}

// writeScore writes a single score to DuckDB.
func (h *ScoreMux) writeScore(ctx context.Context, score *domain.Score) error {
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
