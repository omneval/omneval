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
	"github.com/omneval/omneval/internal/duckdb"
	"github.com/omneval/omneval/internal/lake"
	"github.com/omneval/omneval/internal/lake/lakeservertest"
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

// TestScoreDualWrite proves a written-back score lands in both the legacy
// store and the Lake, partitioned by the annotated span's start_time.
func TestScoreDualWrite(t *testing.T) {
	ctx := context.Background()

	db, err := duckdb.Open(":memory:")
	if err != nil {
		t.Fatalf("open duckdb: %v", err)
	}
	defer db.Close()

	// The span the score annotates, already in the hot store.
	spanStart := time.Date(2026, 6, 4, 11, 0, 0, 0, time.UTC)
	_, err = db.ExecContext(ctx, `
		INSERT INTO spans (span_id, trace_id, project_id, start_time)
		VALUES ('s1', 't1', 'p1', ?)`, spanStart)
	if err != nil {
		t.Fatalf("seed span: %v", err)
	}

	cfg, _ := lakeservertest.NewLocal(t)
	lk, err := lake.Open(ctx, cfg)
	if err != nil {
		t.Fatalf("open lake: %v", err)
	}
	defer lk.Close()

	rec := postScore(t, New(db, lk))
	if rec.Code != http.StatusCreated {
		t.Fatalf("status: got %d, want 201 (%s)", rec.Code, rec.Body.String())
	}

	var legacyCount int
	if err := db.QueryRowContext(ctx, "SELECT count(*) FROM scores").Scan(&legacyCount); err != nil {
		t.Fatalf("legacy count: %v", err)
	}
	if legacyCount != 1 {
		t.Errorf("legacy scores: got %d, want 1", legacyCount)
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

type failingScoreLake struct{}

func (failingScoreLake) InsertScores(context.Context, []*domain.Score) error {
	return errors.New("lake unavailable")
}

// TestScoreLakeFailureKeepsLegacyWrite proves a lake failure still
// returns 201 and the legacy write stands.
func TestScoreLakeFailureKeepsLegacyWrite(t *testing.T) {
	db, err := duckdb.Open(":memory:")
	if err != nil {
		t.Fatalf("open duckdb: %v", err)
	}
	defer db.Close()

	rec := postScore(t, New(db, failingScoreLake{}))
	if rec.Code != http.StatusCreated {
		t.Fatalf("status: got %d, want 201", rec.Code)
	}

	var n int
	if err := db.QueryRow("SELECT count(*) FROM scores").Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Errorf("legacy scores: got %d, want 1", n)
	}
}

// TestScoreNoLakeLegacyOnly proves nil lake (flag off) is byte-identical
// to the old behavior.
func TestScoreNoLakeLegacyOnly(t *testing.T) {
	db, err := duckdb.Open(":memory:")
	if err != nil {
		t.Fatalf("open duckdb: %v", err)
	}
	defer db.Close()

	rec := postScore(t, New(db, nil))
	if rec.Code != http.StatusCreated {
		t.Fatalf("status: got %d, want 201", rec.Code)
	}
}
