package handler

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/omneval/omneval/internal/domain"
	"github.com/omneval/omneval/internal/idgen"
	"github.com/omneval/omneval/internal/lakeclient"
	"github.com/omneval/omneval/services/query/internal/routes"
)

// Re-export shared types for backward compatibility.
type (
	SessionStore = routes.SessionStore
)


// writeJSONError writes a JSON error response with the given status code.
func writeJSONError(w http.ResponseWriter, message string, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": message})
}


// ScoreHandler handles POST /api/v1/scores — the public-facing endpoint
// that allows manual score writes from the UI or API consumers. Scores are
// committed directly to the Lake (lake.scores) through a writable Lake
// attachment (ADR-0004/#91) — the Query API has no other durable write path.
type ScoreHandler struct {
	// Lake is a writable Lake attachment (deps.AdminLake) used to commit
	// scores via InsertScores.
	Lake lakeclient.Client
	// SpanDB is used to look up the annotated span's start_time so the score
	// partitions alongside its span (ADR-0002). Falls back to CreatedAt if
	// the lookup fails.
	SpanDB DBHandle
}

// NewScoreHandler creates a new ScoreHandler backed by a writable Lake
// attachment. spanDB is used to resolve span_start_time for partitioning.
func NewScoreHandler(lakeWriter lakeclient.Client, spanDB DBHandle) http.Handler {
	h := &ScoreHandler{Lake: lakeWriter, SpanDB: spanDB}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/scores", h.HandleScores)
	return mux
}

// HandleScores writes a score to the Lake (lake.scores).
func (h *ScoreHandler) HandleScores(w http.ResponseWriter, r *http.Request) {
	var req domain.ScoreRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, "invalid JSON", http.StatusBadRequest)
		return
	}

	if req.SpanID == "" || req.TraceID == "" || req.ProjectID == "" {
		writeJSONError(w, "span_id, trace_id, and project_id are required", http.StatusBadRequest)
		return
	}

	if h.Lake == nil {
		writeJSONError(w, "score writes are unavailable: Lake is not configured", http.StatusServiceUnavailable)
		return
	}

	scoreID := idgen.Generate()
	score := &domain.Score{
		ScoreID:       scoreID,
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

	// Resolve the annotated span's start_time so the score partitions
	// alongside its span (ADR-0002). Best-effort: InsertScores falls back to
	// CreatedAt when SpanStartTime is zero.
	if h.SpanDB != nil {
		var spanStart time.Time
		err := h.SpanDB.QueryRowContext(r.Context(),
			"SELECT start_time FROM lake.spans WHERE trace_id = ? AND span_id = ?",
			score.TraceID, score.SpanID,
		).Scan(&spanStart)
		if err == nil {
			score.SpanStartTime = spanStart
		}
	}

	if err := h.Lake.InsertScores(r.Context(), []*domain.Score{score}); err != nil {
		writeJSONError(w, "write score: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]string{"score_id": scoreID})
}

// Routes returns the score-related API routes as AuthRoute entries with
// AuthPolicyPublic so the Router can use them for policy-based auth dispatch.
func (h *ScoreHandler) Routes() []AuthRoute {
	return []AuthRoute{
		{Method: http.MethodPost, Path: "/api/v1/scores", Handler: h.HandleScores, Policy: AuthPolicyPublic},
	}
}