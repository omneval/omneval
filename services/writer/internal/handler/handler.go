package handler

// ScoreHandler handles POST /internal/v1/scores — the internal-only endpoint
// called by Eval Workers to write completed scores back to DuckDB.
// Not exposed outside the cluster.
type ScoreHandler struct{}
