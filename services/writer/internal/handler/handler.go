package handler

import (
	"context"
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

// SpanStartTimeLookup resolves the start_time of the span a score annotates,
// so the score lands in the correct lake.scores partition (ADR-0002).
// Implemented by *lake.Lake.
type SpanStartTimeLookup interface {
	SpanStartTime(ctx context.Context, traceID, spanID string) (time.Time, error)
}

// ScoreMux handles POST /internal/v1/scores — the internal-only endpoint
// called by Eval Workers to write completed scores back to the Lake.
// Not exposed outside the cluster.
type ScoreMux struct {
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

// New creates a new ScoreMux that handles POST /internal/v1/scores. lake is
// required (writer.lake.enabled defaults true); a nil lake is treated as a
// misconfiguration and every request returns 503.
func New(lake ScoreLakeWriter) http.Handler {
	h := &ScoreMux{lake: lake}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /internal/v1/scores", h.HandleScores)
	return mux
}

// HandleScores writes a single score to the Lake, the sole storage tier
// for scores (ADR-0004).
func (h *ScoreMux) HandleScores(w http.ResponseWriter, r *http.Request) {
	if h.lake == nil {
		slog.ErrorContext(r.Context(), "score handler misconfigured: lake is nil")
		http.Error(w, "lake not configured", http.StatusServiceUnavailable)
		return
	}

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

	// The score partitions by its span's start_time (ADR-0002). Look it up
	// from the Lake; if the span isn't found (or the lookup fails),
	// InsertScores falls back to score.CreatedAt.
	if lookup, ok := h.lake.(SpanStartTimeLookup); ok {
		spanStart, err := lookup.SpanStartTime(r.Context(), score.TraceID, score.SpanID)
		if err != nil {
			slog.WarnContext(r.Context(), "span start time lookup failed, falling back to created_at",
				"score_id", score.ScoreID, "err", err)
		} else if !spanStart.IsZero() {
			score.SpanStartTime = spanStart
		}
	}

	ctx := r.Context()
	if err := h.lake.InsertScores(ctx, []*domain.Score{score}); err != nil {
		metrics.LakeWriteErrors.WithLabelValues("scores").Inc()
		slog.ErrorContext(ctx, "lake score write failed",
			"score_id", score.ScoreID, "err", err)
		http.Error(w, fmt.Sprintf("write score: %v", err), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
}
