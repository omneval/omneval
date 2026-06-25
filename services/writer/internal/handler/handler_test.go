package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/omneval/omneval/internal/domain"
	"github.com/omneval/omneval/internal/lake"
	"github.com/omneval/omneval/internal/laketest"
)

const scoreBody = `{
	"score_id": "score-1",
	"span_id": "s1",
	"trace_id": "t1",
	"project_id": "p1",
	"eval_name": "helpfulness",
	"value": 0.8,
	"reasoning": "ok",
	"judge_model": "gpt-4o"
}`

func postScore(t *testing.T, h http.Handler) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("POST", "/internal/v1/scores", strings.NewReader(scoreBody))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// TestScoreWriteToLake proves a written-back score lands in the Lake,
// partitioned by the annotated span's start_time looked up from
// lake.spans (ADR-0002).
func TestScoreWriteToLake(t *testing.T) {
	ctx := context.Background()
	lk := laketest.NewLocal(t)

	// The span the score annotates, already in the Lake.
	spanStart := time.Date(2026, 6, 4, 11, 0, 0, 0, time.UTC)
	if err := lk.InsertSpans(ctx, []*domain.Span{{
		SpanID: "s1", TraceID: "t1", ProjectID: "p1", StartTime: spanStart,
	}}); err != nil {
		t.Fatalf("seed span: %v", err)
	}

	rec := postScore(t, New(lk))
	if rec.Code != http.StatusCreated {
		t.Fatalf("status: got %d, want 201 (%s)", rec.Code, rec.Body.String())
	}

	var gotSpanStart time.Time
	var gotValue float64
	err = lk.DB().QueryRowContext(ctx,
		"SELECT span_start_time, value FROM lake.scores WHERE score_id = 'score-1'",
	).Scan(&gotSpanStart, &gotValue)
	if err != nil {
		t.Fatalf("read lake score: %v", err)
	}
	if !gotSpanStart.Equal(spanStart) {
		t.Errorf("span_start_time: got %v, want %v", gotSpanStart, spanStart)
	}
	if gotValue != 0.8 {
		t.Errorf("value: got %v, want 0.8", gotValue)
	}
}

// TestScoreWithoutScoreID_GeneratesOne is a regression test: the real Eval
// Worker (services/eval/internal/worker/worker.go's writeScore) never sends
// a score_id in its payload — only job_id, rule_id, span_id, trace_id,
// project_id, score, reasoning. Before this fix, HandleScores passed that
// empty req.ScoreID straight through to domain.Score, so every score
// written by the actual Eval pipeline had an empty score_id stored in the
// Lake. Confirmed live against the production instance during the v0.0.27
// cutover validation.
func TestScoreWithoutScoreID_GeneratesOne(t *testing.T) {
	ctx := context.Background()
	lk := laketest.NewLocal(t)

	if err := lk.InsertSpans(ctx, []*domain.Span{{
		SpanID: "s2", TraceID: "t2", ProjectID: "p1", StartTime: time.Now(),
	}}); err != nil {
		t.Fatalf("seed span: %v", err)
	}

	body := `{
		"span_id": "s2",
		"trace_id": "t2",
		"project_id": "p1",
		"eval_name": "helpfulness",
		"value": 0.5,
		"reasoning": "ok"
	}`
	req := httptest.NewRequest("POST", "/internal/v1/scores", strings.NewReader(body))
	rec := httptest.NewRecorder()
	New(lk).ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status: got %d, want 201 (%s)", rec.Code, rec.Body.String())
	}

	var gotScoreID string
	err = lk.DB().QueryRowContext(ctx,
		"SELECT score_id FROM lake.scores WHERE trace_id = 't2' AND span_id = 's2'",
	).Scan(&gotScoreID)
	if err != nil {
		t.Fatalf("read lake score: %v", err)
	}
	if gotScoreID == "" {
		t.Error("score_id: got empty string, want a generated ID")
	}
}

type failingScoreLake struct{}

func (failingScoreLake) InsertScores(context.Context, []*domain.Score) error {
	return errors.New("lake unavailable")
}

// TestScoreLakeFailureReturnsError proves a lake write failure is surfaced
// to the caller (Lake is now authoritative — there is no legacy fallback).
func TestScoreLakeFailureReturnsError(t *testing.T) {
	rec := postScore(t, New(failingScoreLake{}))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status: got %d, want 500", rec.Code)
	}
}

// TestScoreNilLakeReturnsServiceUnavailable proves a nil lake (misconfigured
// writer) returns 503 instead of silently dropping the score.
func TestScoreNilLakeReturnsServiceUnavailable(t *testing.T) {
	rec := postScore(t, New(nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status: got %d, want 503", rec.Code)
	}
}
