package lake

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/omneval/omneval/internal/domain"
	"github.com/omneval/omneval/internal/lake/lakeserver"
)

// TestCrashRecoveryLocalCatalog proves that a Quack Server backed by a local
// DuckDB-file Catalog (CatalogDriverLocal) survives an ungraceful shutdown
// (simulating a pod eviction or OOM-kill where the process is terminated
// without a clean DB.Close()) and recovers correctly when restarted against
// the same on-disk catalog file — no manual intervention required.
//
// This is the Docker-free integration test that exercises the recovery path
// documented in ADR-0007: Kubernetes reattaches the same PVC to the
// rescheduled pod, lakeserver.Serve reopens the catalog file, and DuckDB's
// engine replays its WAL for crash consistency.
//
// Test structure:
//  1. Start a Quack Server on CatalogDriverLocal against a temp-dir catalog.
//  2. Insert committed pre-crash data via a Lake client.
//  3. Launch a goroutine that begins an in-flight batch insert (the write
//     is active but may not have committed before the crash).
//  4. Forcefully terminate the server by closing the underlying DuckDB
//     handle directly — this simulates an ungraceful kill (not a clean
//     srv.Close()), leaving DuckDB's .wal file unflushed.
//  5. Wait for the in-flight goroutine to fail (expected, since the server
//     is down).
//  6. Restart the server against the same catalog file path (simulating the
//     rescheduled pod reattaching the same PVC).
//  7. Assert: the server starts successfully, previously committed data is
//     intact, and the interrupted write is either fully present or fully
//     absent (no partial/corrupted rows).
//  8. Assert: the restarted server is usable — a fresh Lake client can
//     read/write without any manual recovery step.
func TestCrashRecoveryLocalCatalog(t *testing.T) {
	ctx := context.Background()

	// ---- Phase 1: Start server, insert committed data ----
	dir := t.TempDir()
	catalogPath := filepath.Join(dir, "catalog", "lake.ducklake")

	port1 := freePort(t)
	srv1, err := lakeserver.Serve(ctx, lakeserver.Config{
		ListenAddr:    fmt.Sprintf(":%d", port1),
		CatalogDriver: lakeserver.CatalogDriverLocal,
		CatalogDSN:    catalogPath,
	})
	if err != nil {
		t.Fatalf("start quack server (pre-crash): %v", err)
	}

	addr := fmt.Sprintf("localhost:%d", port1)
	cfg := Config{
		QuackAddr:  addr,
		QuackToken: srv1.Token(),
		DataPath:   filepath.Join(dir, "data"),
	}

	// Insert pre-crash committed spans.
	l1, err := Open(ctx, cfg)
	if err != nil {
		t.Fatalf("open lake (pre-crash): %v", err)
	}
	start := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	preCrashSpans := make([]*domain.Span, 10)
	for i := 0; i < 10; i++ {
		preCrashSpans[i] = testSpan("proj-crash", fmt.Sprintf("pre-%d", i), start.Add(time.Duration(i)*time.Minute))
	}
	if err := l1.InsertSpans(ctx, preCrashSpans); err != nil {
		t.Fatalf("insert pre-crash spans: %v", err)
	}
	l1.Close()

	// Verify pre-crash data is visible through a fresh read attachment.
	roCfg := cfg
	roCfg.ReadOnly = true
	lRO, err := Open(ctx, roCfg)
	if err != nil {
		t.Fatalf("open read lake (pre-crash): %v", err)
	}
	var n int
	if err := lRO.DB().QueryRowContext(ctx,
		"SELECT count(*) FROM lake.spans WHERE project_id = 'proj-crash'",
	).Scan(&n); err != nil {
		t.Fatalf("count pre-crash spans: %v", err)
	}
	if n != 10 {
		t.Fatalf("pre-crash span count: got %d, want 10", n)
	}
	lRO.Close()

	// ---- Phase 2: Launch in-flight write, then crash the server ----
	// Start a goroutine that inserts spans in a loop — this is the "in-flight"
	// write that is active when the server is killed.
	var inFlightDone atomic.Bool
	var inFlightInserted atomic.Int64
	inFlightWg := sync.WaitGroup{}
	inFlightWg.Add(1)
	go func() {
		defer inFlightWg.Done()
		l, err := Open(ctx, cfg)
		if err != nil {
			return
		}
		defer l.Close()
		for i := 0; i < 20; i++ {
			if inFlightDone.Load() {
				return // server crashed, stop inserting
			}
			spans := []*domain.Span{testSpan(
				"proj-crash",
				fmt.Sprintf("inflight-%d", i),
				start.Add(time.Duration(10+i)*time.Minute),
			)}
			if err := l.InsertSpans(ctx, spans); err != nil {
				// Expected: the write will fail once the server is down.
				return
			}
			inFlightInserted.Add(1)
			time.Sleep(2 * time.Millisecond)
		}
	}()

	// Briefly wait for the in-flight goroutine to start.
	time.Sleep(20 * time.Millisecond)

	// Crash the server: close the underlying DuckDB handle directly.
	// This simulates an ungraceful kill (pod eviction / OOM-kill) where
	// the process is terminated without calling srv.Close() — DuckDB's
	// WAL is left unflushed exactly as it would be in production.
	if err := srv1.DB().Close(); err != nil {
		// Close on a busy connection can return an error; the point is
		// the underlying DB is now gone, simulating the crash.
		t.Logf("crash-close DB (expected non-clean): %v", err)
	}

	// Close the server struct so t.Cleanup doesn't try to double-close.
	srv1 = nil

	// Wait for the in-flight goroutine to finish (it will fail with a DB
	// error since the server is down).
	inFlightDone.Store(true)
	inFlightWg.Wait()

	// ---- Phase 3: Restart server against the same catalog file ----
	// This simulates the rescheduled pod reattaching the same PVC and
	// lakeserver.Serve reopening the catalog file.
	port2 := freePort(t)
	srv2, err := lakeserver.Serve(ctx, lakeserver.Config{
		ListenAddr:    fmt.Sprintf(":%d", port2),
		CatalogDriver: lakeserver.CatalogDriverLocal,
		CatalogDSN:    catalogPath,
	})
	if err != nil {
		t.Fatalf("start quack server (post-crash): %v", err)
	}
	defer srv2.Close()

	addr2 := fmt.Sprintf("localhost:%d", port2)
	cfg2 := Config{
		QuackAddr:  addr2,
		QuackToken: srv2.Token(),
		DataPath:   filepath.Join(dir, "data"),
	}

	// ---- Phase 4: Assert recovery ----
	// 4a. The server started successfully with no manual intervention.
	// This is verified by reaching here — Serve returned nil error.

	// 4b. Previously committed (pre-crash) data is intact.
	lPost, err := Open(ctx, cfg2)
	if err != nil {
		t.Fatalf("open lake (post-crash): %v", err)
	}
	defer lPost.Close()

	if err := lPost.Ping(ctx); err != nil {
		t.Fatalf("ping post-crash: %v — server not usable", err)
	}

	if err := lPost.DB().QueryRowContext(ctx,
		"SELECT count(*) FROM lake.spans WHERE project_id = 'proj-crash'",
	).Scan(&n); err != nil {
		t.Fatalf("count post-crash spans: %v", err)
	}
	// All 10 pre-crash spans must be present. The in-flight insert that was
	// interrupted may or may not have committed (DuckDB's WAL replay), but
	// what remains must be consistent (no partial/corrupted rows).
	if n < 10 {
		t.Errorf("post-crash span count: got %d, want >= 10 (pre-crash data loss)", n)
	}

	// 4c. Assert no corruption: read every pre-crash span and verify its
	// fields are intact.
	rows, err := lPost.DB().QueryContext(ctx,
		"SELECT span_id, trace_id, model, input, output, input_tokens, output_tokens FROM lake.spans WHERE span_id LIKE 'pre-%' ORDER BY span_id",
	)
	if err != nil {
		t.Fatalf("query pre-crash spans: %v — possible corruption", err)
	}
	var preCrashRecovered int
	for rows.Next() {
		var spanID, traceID, model string
		var input, output *string
		var inTok, outTok int64
		if err := rows.Scan(&spanID, &traceID, &model, &input, &output, &inTok, &outTok); err != nil {
			t.Fatalf("scan pre-crash span: %v — possible corruption", err)
		}
		if spanID == "" || traceID == "" || model == "" {
			t.Errorf("corrupted row detected: span_id=%q trace_id=%q model=%q", spanID, traceID, model)
		}
		if inTok != 10 || outTok != 5 {
			t.Errorf("corrupted token counts for %s: input_tokens=%d output_tokens=%d", spanID, inTok, outTok)
		}
		preCrashRecovered++
	}
	rows.Close()
	if preCrashRecovered != 10 {
		t.Errorf("pre-crash spans recovered: got %d, want 10", preCrashRecovered)
	}

	// 4d. Assert the restarted server is usable for new writes.
	newSpan := testSpan("proj-crash", "post-crash-new", start.Add(40*time.Minute))
	if err := lPost.InsertSpans(ctx, []*domain.Span{newSpan}); err != nil {
		t.Fatalf("insert post-crash span: %v — server not writable", err)
	}

	// Verify the new span is visible through a fresh read attachment.
	roCfg2 := cfg2
	roCfg2.ReadOnly = true
	lRO2, err := Open(ctx, roCfg2)
	if err != nil {
		t.Fatalf("open read lake (post-crash verify): %v", err)
	}
	defer lRO2.Close()

	if err := lRO2.DB().QueryRowContext(ctx,
		"SELECT count(*) FROM lake.spans WHERE span_id = 'post-crash-new'",
	).Scan(&n); err != nil {
		t.Fatalf("verify post-crash span: %v", err)
	}
	if n != 1 {
		t.Errorf("post-crash span visibility: got %d, want 1", n)
	}

	// 4e. Assert no partial/corrupted rows from the interrupted write.
	// The in-flight write that was active at crash time is either fully
	// committed or fully absent (DuckDB WAL replay guarantees atomicity).
	// Check that all in-flight spans that exist have valid, non-corrupt data.
	if err := lPost.DB().QueryRowContext(ctx,
		`SELECT count(*) FROM lake.spans
		 WHERE span_id LIKE 'inflight-%'
		   AND (span_id = '' OR trace_id = '' OR model = ''
		        OR input_tokens IS NULL OR output_tokens IS NULL)`,
	).Scan(&n); err != nil {
		t.Fatalf("check for corrupted in-flight rows: %v", err)
	}
	if n > 0 {
		t.Errorf("corrupted in-flight rows: got %d, want 0", n)
	}
}