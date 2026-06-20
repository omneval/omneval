package lakeserver_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/omneval/omneval/internal/domain"
	"github.com/omneval/omneval/internal/lake"
	"github.com/omneval/omneval/internal/lake/lakeserver"
	"github.com/omneval/omneval/internal/lake/lakeservertest"
)

// TestRunMaintenanceLoopSkipsWhenNoNewSnapshot proves that RunMaintenanceLoop
// skips its compaction statements (rewrite/merge/expire/cleanup) on ticks
// where nothing has changed since the last completed pass, instead of
// unconditionally writing a fresh (often near-empty) merged Parquet file
// every interval regardless of whether any new spans/scores arrived. The
// first tick always runs (there is no prior pass to compare against);
// subsequent ticks with no new writes report Skipped: true.
func TestRunMaintenanceLoopSkipsWhenNoNewSnapshot(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cfg, _ := lakeservertest.NewLocal(t)

	l, err := lake.Open(ctx, cfg)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer l.Close()

	if err := l.InsertSpans(ctx, []*domain.Span{testSpan("proj-a", "s1", time.Now())}); err != nil {
		t.Fatalf("insert: %v", err)
	}

	var (
		mu      sync.Mutex
		results []lakeserver.MaintenanceResult
	)
	onResult := func(r lakeserver.MaintenanceResult) {
		mu.Lock()
		results = append(results, r)
		mu.Unlock()
	}

	loopErr := make(chan error, 1)
	go func() {
		loopErr <- lakeserver.RunMaintenanceLoop(ctx, l.DB(), lakeserver.MaintenanceTables, 200*time.Millisecond, lakeserver.RetentionConfig{}, onResult)
	}()

	// Let at least two ticks complete with no further writes in between:
	// one real pass (nothing to compare against yet) and one skipped pass
	// (no new snapshot since the first). Real DuckLake compaction passes
	// in this environment can take several seconds apiece (extension
	// load/attach overhead), so this waits generously rather than assuming
	// the configured interval is hit on a tight schedule.
	deadline := time.Now().Add(60 * time.Second)
	for {
		mu.Lock()
		n := len(results)
		mu.Unlock()
		if n >= 2 || time.Now().After(deadline) {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	cancel()
	<-loopErr

	mu.Lock()
	defer mu.Unlock()

	if len(results) < 2 {
		t.Fatalf("expected at least 2 maintenance passes within 60s at a 200ms interval, got %d", len(results))
	}
	if results[0].Skipped {
		t.Errorf("first pass: got Skipped=true, want false (no prior pass to compare against)")
	}
	for i, r := range results[1:] {
		if !r.Skipped {
			t.Errorf("pass %d: got Skipped=false, want true (no writes happened since the first pass)", i+1)
		}
	}
}
