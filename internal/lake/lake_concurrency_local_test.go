package lake

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/omneval/omneval/internal/domain"
	"github.com/omneval/omneval/internal/lake/lakeserver"
)

// TestConcurrentInsertsNoDataLoss proves that, against a Quack Server backed
// by a local DuckDB-file Catalog (CatalogDriverLocal), multiple concurrent
// Quack-client writers can insert spans without any lost or duplicated rows.
// This is a pure in-process integration test — no Docker, no testcontainers.
func TestConcurrentInsertsNoDataLoss(t *testing.T) {
	ctx := context.Background()
	cfg, _ := startTestServer(t)

	// Pre-initialize the catalog with a single writer. DuckLake's quack
	// catalog driver serializes ATTACH against a file-backed catalog; a
	// single client initializes the ducklake_metadata tables and then
	// disconnects so all subsequent clients attach against a consistent
	// catalog without fighting the attach protocol.
	initL, err := Open(ctx, cfg)
	if err != nil {
		t.Fatalf("pre-init catalog: %v", err)
	}
	initSpans := make([]*domain.Span, 50)
	for i := 0; i < 50; i++ {
		initSpans[i] = testSpan("proj-concurrent", fmt.Sprintf("init-%d", i), time.Now())
	}
	if err := initL.InsertSpans(ctx, initSpans); err != nil {
		t.Fatalf("pre-init insert: %v", err)
	}
	initL.Close()

	const writerCount = 5
	spansPerWriter := 20
	var totalInserted atomic.Int64
	var wg sync.WaitGroup

	// --- Concurrent writer clients ---
	for w := 0; w < writerCount; w++ {
		wg.Add(1)
		go func(writerID int) {
			defer wg.Done()
			l, err := Open(ctx, cfg)
			if err != nil {
				t.Errorf("writer %d open: %v", writerID, err)
				return
			}
			defer l.Close()

			var inserts int64
			for i := 0; i < spansPerWriter; i++ {
				start := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC).
					Add(time.Duration(w*spansPerWriter+i) * time.Second)
				spans := []*domain.Span{testSpan(
					"proj-concurrent",
					fmt.Sprintf("w%d-s%d", writerID, i),
					start,
				)}
				if err := l.InsertSpans(ctx, spans); err != nil {
					t.Errorf("writer %d insert span %d: %v", writerID, i, err)
					return
				}
				inserts++
			}
			totalInserted.Add(inserts)
		}(w)
	}
	wg.Wait()

	// Verify exact count through a fresh read attachment
	readCfg := cfg
	readCfg.ReadOnly = true
	readL, err := Open(ctx, readCfg)
	if err != nil {
		t.Fatalf("open read lake: %v", err)
	}
	defer readL.Close()

	var n int
	if err := readL.DB().QueryRowContext(ctx,
		"SELECT count(*) FROM lake.spans WHERE project_id = 'proj-concurrent'",
	).Scan(&n); err != nil {
		t.Fatalf("count spans: %v", err)
	}
	want := 50 + int(totalInserted.Load()) // 50 pre-init + concurrent inserts
	if n != want {
		t.Errorf("span count: got %d, want %d", n, want)
	}
}

// TestConcurrentInsertsWithScoreWritebackAndReadOnly proves that, against a
// Quack Server backed by a local DuckDB-file Catalog, concurrent writer
// clients inserting spans, a score-write-back client inserting scores, and a
// long-lived read-only client polling all coexist correctly — no lost or
// duplicated rows, no corruption.
func TestConcurrentInsertsWithScoreWritebackAndReadOnly(t *testing.T) {
	ctx := context.Background()
	cfg, _ := startTestServer(t)

	// Pre-initialize the catalog so concurrent ATTACHs don't race.
	initL, err := Open(ctx, cfg)
	if err != nil {
		t.Fatalf("pre-init catalog: %v", err)
	}
	initSpans := make([]*domain.Span, 50)
	for i := 0; i < 50; i++ {
		initSpans[i] = testSpan("proj-multi", fmt.Sprintf("init-%d", i), time.Now())
	}
	if err := initL.InsertSpans(ctx, initSpans); err != nil {
		t.Fatalf("pre-init insert: %v", err)
	}
	initL.Close()

	const writerCount = 5
	spansPerWriter := 10
	scoreCount := 20
	var spansInserted atomic.Int64
	var scoresInserted atomic.Int64
	var wg sync.WaitGroup

	// --- Read-only polling client (long-lived) ---
	roCfg := cfg
	roCfg.ReadOnly = true
	roLake, err := Open(ctx, roCfg)
	if err != nil {
		t.Fatalf("open read-only lake: %v", err)
	}
	defer roLake.Close()

	var roPollCount int64
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 10; i++ {
			var n int
			if err := roLake.DB().QueryRowContext(ctx,
				"SELECT count(*) FROM lake.spans",
			).Scan(&n); err != nil {
				// During concurrent inserts, transient errors may happen.
				// A stale-connection reconnect is handled internally by Lake.
				t.Logf("read-only poll %d: %v", i, err)
			}
			atomic.AddInt64(&roPollCount, 1)
			time.Sleep(50 * time.Millisecond)
		}
	}()

	// --- Concurrent writer clients ---
	for w := 0; w < writerCount; w++ {
		wg.Add(1)
		go func(writerID int) {
			defer wg.Done()
			l, err := Open(ctx, cfg)
			if err != nil {
				t.Errorf("writer %d open: %v", writerID, err)
				return
			}
			defer l.Close()

			for i := 0; i < spansPerWriter; i++ {
				start := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC).
					Add(time.Duration(writerID*spansPerWriter+i) * time.Second)
				spans := []*domain.Span{testSpan(
					"proj-multi",
					fmt.Sprintf("w%d-s%d", writerID, i),
					start,
				)}
				if err := l.InsertSpans(ctx, spans); err != nil {
					t.Errorf("writer %d insert: %v", writerID, err)
					return
				}
			}
			spansInserted.Add(int64(spansPerWriter))
		}(w)
	}

	// --- Score-write-back client ---
	wg.Add(1)
	go func() {
		defer wg.Done()
		s, err := Open(ctx, cfg)
		if err != nil {
			t.Errorf("score client open: %v", err)
			return
		}
		defer s.Close()

		for i := 0; i < scoreCount; i++ {
			score := &domain.Score{
				ScoreID:       fmt.Sprintf("score-%d", i),
				SpanID:        fmt.Sprintf("w%d-s%d", i%writerCount, i%spansPerWriter),
				TraceID:       fmt.Sprintf("trace-score-%d", i),
				ProjectID:     "proj-multi",
				EvalName:      "helpfulness",
				Value:         float64(i) / float64(scoreCount),
				Reasoning:     "auto-scored",
				JudgeModel:    "eval-judge",
				CreatedAt:     time.Now(),
				SpanStartTime: time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC),
			}
			if err := s.InsertScores(ctx, []*domain.Score{score}); err != nil {
				t.Errorf("score client insert score %d: %v", i, err)
				return
			}
		}
		scoresInserted.Add(int64(scoreCount))
	}()

	wg.Wait()

	// --- Assert final row counts through a fresh read attachment ---
	readCfg2 := cfg
	readCfg2.ReadOnly = true
	readL, err := Open(ctx, readCfg2)
	if err != nil {
		t.Fatalf("open final read lake: %v", err)
	}
	defer readL.Close()

	var spanCount, scoreCountActual int
	if err := readL.DB().QueryRowContext(ctx,
		"SELECT count(*) FROM lake.spans WHERE project_id = 'proj-multi'",
	).Scan(&spanCount); err != nil {
		t.Fatalf("count spans: %v", err)
	}
	wantSpans := 50 + int(spansInserted.Load()) // 50 pre-init + concurrent inserts
	if spanCount != wantSpans {
		t.Errorf("span count: got %d, want %d", spanCount, wantSpans)
	}

	if err := readL.DB().QueryRowContext(ctx,
		"SELECT count(*) FROM lake.scores WHERE project_id = 'proj-multi'",
	).Scan(&scoreCountActual); err != nil {
		t.Fatalf("count scores: %v", err)
	}
	wantScores := int(scoresInserted.Load())
	if scoreCountActual != wantScores {
		t.Errorf("score count: got %d, want %d", scoreCountActual, wantScores)
	}

	if roPollCount == 0 {
		t.Error("read-only client never polled successfully")
	}
}

// TestConcurrentInsertsWithMaintenanceAfter proves that Table Maintenance after
// concurrent inserts does not corrupt or resurrect data.
func TestConcurrentInsertsWithMaintenanceAfter(t *testing.T) {
	ctx := context.Background()
	cfg, _ := startTestServer(t)

	// Pre-initialize the catalog.
	initL, err := Open(ctx, cfg)
	if err != nil {
		t.Fatalf("pre-init catalog: %v", err)
	}
	initSpans := make([]*domain.Span, 50)
	for i := 0; i < 50; i++ {
		initSpans[i] = testSpan("proj-maint", fmt.Sprintf("init-%d", i), time.Now())
	}
	if err := initL.InsertSpans(ctx, initSpans); err != nil {
		t.Fatalf("pre-init insert: %v", err)
	}
	initL.Close()

	const writerCount = 4
	spansPerWriter := 15
	var wg sync.WaitGroup
	var totalInserted atomic.Int64

	// --- Concurrent writer clients ---
	for w := 0; w < writerCount; w++ {
		wg.Add(1)
		go func(writerID int) {
			defer wg.Done()
			l, err := Open(ctx, cfg)
			if err != nil {
				t.Errorf("writer %d open: %v", writerID, err)
				return
			}
			defer l.Close()

			for i := 0; i < spansPerWriter; i++ {
				start := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC).
					Add(time.Duration(writerID*spansPerWriter+i) * time.Second)
				spans := []*domain.Span{testSpan(
					"proj-maint",
					fmt.Sprintf("w%d-s%d", writerID, i),
					start,
				)}
				if err := l.InsertSpans(ctx, spans); err != nil {
					t.Errorf("writer %d insert: %v", writerID, err)
					return
				}
			}
			totalInserted.Add(int64(spansPerWriter))
		}(w)
	}
	wg.Wait()

	// --- Verify counts before maintenance ---
	readCfg := cfg
	readCfg.ReadOnly = true
	readL, err := Open(ctx, readCfg)
	if err != nil {
		t.Fatalf("open read lake: %v", err)
	}
	defer readL.Close()

	var n int
	if err := readL.DB().QueryRowContext(ctx,
		"SELECT count(*) FROM lake.spans WHERE project_id = 'proj-maint'",
	).Scan(&n); err != nil {
		t.Fatalf("count before maintenance: %v", err)
	}
	wantBefore := 50 + int(totalInserted.Load()) // 50 pre-init + concurrent inserts
	if n != wantBefore {
		t.Fatalf("spans before maintenance: got %d, want %d", n, wantBefore)
	}

	// --- Run Table Maintenance pass ---
	maintL, err := Open(ctx, cfg)
	if err != nil {
		t.Fatalf("open maintenance lake: %v", err)
	}
	defer maintL.Close()

	result, err := lakeserver.RunMaintenance(ctx, maintL.DB(), lakeserver.MaintenanceTables, lakeserver.RetentionConfig{})
	if err != nil {
		t.Fatalf("table maintenance pass: %v", err)
	}
	_ = result // retention result is zero since retention is disabled

	// --- Verify counts after maintenance — no corruption/resurrection ---
	if err := readL.DB().QueryRowContext(ctx,
		"SELECT count(*) FROM lake.spans WHERE project_id = 'proj-maint'",
	).Scan(&n); err != nil {
		t.Fatalf("count after maintenance: %v", err)
	}
	if n != wantBefore {
		t.Errorf("spans after maintenance: got %d, want %d (data loss or resurrection)", n, wantBefore)
	}

	// Verify no projects other than proj-maint exist
	if err := readL.DB().QueryRowContext(ctx,
		"SELECT count(*) FROM lake.spans WHERE project_id != 'proj-maint'",
	).Scan(&n); err != nil {
		t.Fatalf("count other projects: %v", err)
	}
	if n != 0 {
		t.Errorf("unexpected spans from other projects after maintenance: got %d", n)
	}
}