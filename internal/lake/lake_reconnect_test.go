package lake

// Reconnection tests for the self-healing Lake connection introduced to
// survive Quack Server OOM restarts (ADR-0005).
//
// # Why we don't restart the Quack Server in tests
//
// DuckDB's quack extension has process-level global state; calling
// quack_serve() a second time inside the same OS process crashes with
// "libc++abi: terminating / signal: abort trap". The full production
// scenario (server restarts, client gets "Invalid connection id", client
// self-heals) therefore cannot be driven end-to-end in a single test binary.
//
// What these tests prove instead:
//
//  1. isStaleConn correctly recognises the "Invalid connection id" error text
//     (TestIsStaleConn — pure unit test, no server needed).
//
//  2. reconnect() opens a fresh DuckDB connection, re-attaches to the running
//     Quack Server using the stored Config, and replaces the stale l.db so
//     that all subsequent operations succeed (TestReconnect_Direct).
//
//  3. Every public method that wraps reconnect logic (InsertSpans, InsertScores,
//     Ping, QueryContext, ExecContext, QueryRowContext) works correctly on the
//     new connection immediately after reconnect() is called
//     (TestReconnect_<Method>_AfterReconnect). In production the call to
//     reconnect() is triggered by isStaleConn(err) inside each method; here we
//     call it directly to test the reconnect machinery independently of the
//     error-detection path, which is already covered by TestIsStaleConn.

import (
	"context"
	"testing"
	"time"

	"github.com/omneval/omneval/internal/domain"
)

// simulateStaleDB closes l.db while holding the mutex, simulating what
// happens to the Lake connection when the Quack Server restarts. After this
// call the caller must drive reconnect() before using l again.
func simulateStaleDB(l *Lake) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.db.Close()
}

// reconnectLocked acquires the Lake mutex and calls reconnect. Use this in
// tests instead of calling reconnect directly, so the mutex invariants match
// what the production code observes.
func reconnectLocked(ctx context.Context, l *Lake) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.reconnect(ctx)
}

// --- isStaleConn ---

func TestIsStaleConn(t *testing.T) {
	tests := []struct {
		name string
		err  string
		want bool
	}{
		{"nil", "", false},
		{"unrelated IO error", "IO Error: no such file", false},
		{
			"Could not connect to server (Quack down)",
			"HTTP Error: Could not connect to server",
			true,
		},
		{
			"Authentication failed (token mismatch on restart)",
			"HTTP Error: Authentication failed",
			true,
		},
		{
			"context canceled (interrupted DuckDB op)",
			"context canceled",
			true,
		},
		{
			"LOAD httpfs: context canceled",
			"LOAD httpfs: context canceled",
			true,
		},
		{
			"ATTACH IF NOT EXISTS: context canceled",
			"ATTACH IF NOT EXISTS: context canceled",
			true,
		},
		{
			"INTERRUPT Error: Interrupted!",
			"INTERRUPT Error: Interrupted!",
			true,
		},
		{
			"exact DuckLake stale connection error",
			"Invalid Input Error: Failed to query most recent snapshot for DuckLake: Invalid connection id",
			true,
		},
		{
			"stale error wrapped by lake methods",
			"lake: prepare: Invalid Input Error: Failed to query most recent snapshot for DuckLake: Invalid connection id",
			true,
		},
		{
			"malformed SQL does not trigger reconnect",
			"Binder Error: Unknown column: nonexistent_column",
			false,
		},
		{
			"missing table does not trigger reconnect",
			"Catalog Error: Table 'lake.spans' doesn't exist",
			false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var err error
			if tt.err != "" {
				err = errStr(tt.err)
			}
			if got := isStaleConn(err); got != tt.want {
				t.Errorf("isStaleConn(%q) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

// errStr is a minimal error type for table-driven isStaleConn tests.
type errStr string

func (e errStr) Error() string { return string(e) }

// --- reconnect() directly ---

// TestReconnect_Direct verifies that reconnect() replaces a closed l.db with
// a fresh, working connection to the running Quack Server. This is the core
// invariant: after the server restarts and the client's connection becomes
// stale, reconnect() makes the Lake operational again.
func TestReconnect_Direct(t *testing.T) {
	ctx := context.Background()
	cfg, _ := startTestServer(t)

	l, err := Open(ctx, cfg)
	if err != nil {
		t.Fatalf("open lake: %v", err)
	}
	defer l.Close()

	start := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	if err := l.InsertSpans(ctx, []*domain.Span{testSpan("p", "s1", start)}); err != nil {
		t.Fatalf("insert: %v", err)
	}

	// Simulate the Quack Server restarting: close the underlying connection.
	simulateStaleDB(l)

	// reconnect() must open a fresh connection to the still-running server.
	if err := reconnectLocked(ctx, l); err != nil {
		t.Fatalf("reconnect: %v", err)
	}

	// The new connection sees the committed data.
	var n int
	if err := l.db.QueryRowContext(ctx, "SELECT count(*) FROM lake.spans").Scan(&n); err != nil {
		t.Fatalf("query after reconnect: %v", err)
	}
	if n != 1 {
		t.Errorf("span count after reconnect: got %d, want 1", n)
	}
}

// TestReconnect_Direct_ReadOnly verifies that reconnect() works for a
// read-only Lake (the Query API's attachment).
func TestReconnect_Direct_ReadOnly(t *testing.T) {
	ctx := context.Background()
	cfg, _ := startTestServer(t)

	// Seed data with a read-write lake first.
	rw, err := Open(ctx, cfg)
	if err != nil {
		t.Fatalf("open rw: %v", err)
	}
	start := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	if err := rw.InsertSpans(ctx, []*domain.Span{testSpan("p", "s1", start)}); err != nil {
		t.Fatalf("insert: %v", err)
	}
	rw.Close()

	roCfg := cfg
	roCfg.ReadOnly = true
	ro, err := Open(ctx, roCfg)
	if err != nil {
		t.Fatalf("open ro: %v", err)
	}
	defer ro.Close()

	simulateStaleDB(ro)
	if err := reconnectLocked(ctx, ro); err != nil {
		t.Fatalf("reconnect ro: %v", err)
	}

	var n int
	if err := ro.db.QueryRowContext(ctx, "SELECT count(*) FROM lake.spans").Scan(&n); err != nil {
		t.Fatalf("query after reconnect: %v", err)
	}
	if n != 1 {
		t.Errorf("span count: got %d, want 1", n)
	}
}

// --- Method-level tests: verify each method works on the reconnected DB ---
//
// In production these methods call reconnect() internally when isStaleConn(err)
// is true. Here we drive reconnect() directly and then call the method to
// verify it operates correctly on the new connection.

func TestReconnect_InsertSpans_AfterReconnect(t *testing.T) {
	ctx := context.Background()
	cfg, _ := startTestServer(t)

	l, err := Open(ctx, cfg)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer l.Close()

	start := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	if err := l.InsertSpans(ctx, []*domain.Span{testSpan("p", "s1", start)}); err != nil {
		t.Fatalf("insert before reconnect: %v", err)
	}

	simulateStaleDB(l)
	if err := reconnectLocked(ctx, l); err != nil {
		t.Fatalf("reconnect: %v", err)
	}

	// InsertSpans must commit via the fresh connection.
	if err := l.InsertSpans(ctx, []*domain.Span{testSpan("p", "s2", start.Add(time.Hour))}); err != nil {
		t.Fatalf("insert after reconnect: %v", err)
	}

	var n int
	if err := l.db.QueryRowContext(ctx, "SELECT count(*) FROM lake.spans").Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 2 {
		t.Errorf("span count: got %d, want 2", n)
	}
}

func TestReconnect_InsertScores_AfterReconnect(t *testing.T) {
	ctx := context.Background()
	cfg, _ := startTestServer(t)

	l, err := Open(ctx, cfg)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer l.Close()

	start := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	score := func(id string) *domain.Score {
		return &domain.Score{
			ScoreID: id, SpanID: "s1", TraceID: "t1", ProjectID: "p",
			EvalName: "e", Value: 1, CreatedAt: start, SpanStartTime: start,
		}
	}

	if err := l.InsertScores(ctx, []*domain.Score{score("sc1")}); err != nil {
		t.Fatalf("insert before reconnect: %v", err)
	}

	simulateStaleDB(l)
	if err := reconnectLocked(ctx, l); err != nil {
		t.Fatalf("reconnect: %v", err)
	}

	if err := l.InsertScores(ctx, []*domain.Score{score("sc2")}); err != nil {
		t.Fatalf("insert after reconnect: %v", err)
	}

	var n int
	if err := l.db.QueryRowContext(ctx, "SELECT count(*) FROM lake.scores").Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 2 {
		t.Errorf("score count: got %d, want 2", n)
	}
}

func TestReconnect_Ping_AfterReconnect(t *testing.T) {
	ctx := context.Background()
	cfg, _ := startTestServer(t)

	l, err := Open(ctx, cfg)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer l.Close()

	start := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	if err := l.InsertSpans(ctx, []*domain.Span{testSpan("p", "s1", start)}); err != nil {
		t.Fatalf("insert: %v", err)
	}

	simulateStaleDB(l)
	if err := reconnectLocked(ctx, l); err != nil {
		t.Fatalf("reconnect: %v", err)
	}

	// Ping (the readiness-probe handler) must succeed on the new connection.
	if err := l.Ping(ctx); err != nil {
		t.Fatalf("Ping after reconnect: %v", err)
	}
}

func TestReconnect_QueryContext_AfterReconnect(t *testing.T) {
	ctx := context.Background()
	cfg, _ := startTestServer(t)

	l, err := Open(ctx, cfg)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer l.Close()

	start := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	if err := l.InsertSpans(ctx, []*domain.Span{
		testSpan("p", "s1", start),
		testSpan("p", "s2", start.Add(time.Hour)),
	}); err != nil {
		t.Fatalf("insert: %v", err)
	}

	simulateStaleDB(l)
	if err := reconnectLocked(ctx, l); err != nil {
		t.Fatalf("reconnect: %v", err)
	}

	// QueryContext is the Query API's primary read path (handler.DBHandle).
	rows, err := l.QueryContext(ctx, "SELECT span_id FROM lake.spans ORDER BY span_id")
	if err != nil {
		t.Fatalf("QueryContext after reconnect: %v", err)
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("scan: %v", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows err: %v", err)
	}
	if len(ids) != 2 || ids[0] != "s1" || ids[1] != "s2" {
		t.Errorf("QueryContext: got %v, want [s1 s2]", ids)
	}
}

func TestReconnect_QueryContext_ReadOnly_AfterReconnect(t *testing.T) {
	ctx := context.Background()
	cfg, _ := startTestServer(t)

	rw, err := Open(ctx, cfg)
	if err != nil {
		t.Fatalf("open rw: %v", err)
	}
	start := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	if err := rw.InsertSpans(ctx, []*domain.Span{testSpan("p", "s1", start)}); err != nil {
		t.Fatalf("insert: %v", err)
	}
	rw.Close()

	roCfg := cfg
	roCfg.ReadOnly = true
	ro, err := Open(ctx, roCfg)
	if err != nil {
		t.Fatalf("open ro: %v", err)
	}
	defer ro.Close()

	simulateStaleDB(ro)
	if err := reconnectLocked(ctx, ro); err != nil {
		t.Fatalf("reconnect ro: %v", err)
	}

	rows, err := ro.QueryContext(ctx, "SELECT span_id FROM lake.spans")
	if err != nil {
		t.Fatalf("QueryContext after reconnect: %v", err)
	}
	defer rows.Close()
	if !rows.Next() {
		t.Fatal("expected one row, got none")
	}
	var id string
	if err := rows.Scan(&id); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if id != "s1" {
		t.Errorf("span_id: got %q, want %q", id, "s1")
	}
}

func TestReconnect_ExecContext_AfterReconnect(t *testing.T) {
	ctx := context.Background()
	cfg, _ := startTestServer(t)

	l, err := Open(ctx, cfg)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer l.Close()

	start := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	if err := l.InsertSpans(ctx, []*domain.Span{testSpan("p", "s1", start)}); err != nil {
		t.Fatalf("insert: %v", err)
	}

	simulateStaleDB(l)
	if err := reconnectLocked(ctx, l); err != nil {
		t.Fatalf("reconnect: %v", err)
	}

	// ExecContext is used by the query wire to create views (openLakeDB).
	if _, err := l.ExecContext(ctx,
		"CREATE OR REPLACE VIEW spans AS SELECT * FROM lake.spans",
	); err != nil {
		t.Fatalf("ExecContext after reconnect: %v", err)
	}
}

// TestReconnect_QueryRowContext_AfterReconnect verifies QueryRowContext works
// on the reconnected DB. In production, a concurrent Ping reconnect makes the
// new connection visible to a subsequent QueryRowContext call via mutex
// serialisation.
// TestPing_SucceedsAfterReconnect_EvenIfCallerContextAlreadyExpired proves
// that Ping's documented self-heal retry ("Ping reconnects and retries once
// so the readiness probe self-heals") actually succeeds once the underlying
// connection is healthy again — even when the caller's context (e.g. the
// readiness probe's 3-second budget, internal/probe/probe.go) has already
// expired by the time reconnect() finishes.
//
// This is exactly the failure mode observed in production: reconnect() runs
// under its own decoupled reconnectTimeout budget (up to 10s, deliberately
// independent of the caller's ctx — see reconnect's doc comment) and can
// succeed well after the probe's ctx has already expired. The retry query
// must not reuse that expired ctx, or every reconnect-triggering Ping is
// guaranteed to report failure even though the connection it just repaired
// is perfectly healthy — defeating the self-heal and forcing every
// subsequent probe to pay for another full reconnect, indefinitely.
func TestPing_SucceedsAfterReconnect_EvenIfCallerContextAlreadyExpired(t *testing.T) {
	ctx := context.Background()
	cfg, _ := startTestServer(t)

	l, err := Open(ctx, cfg)
	if err != nil {
		t.Fatalf("open lake: %v", err)
	}
	defer l.Close()

	// An already-canceled context simulates the readiness probe's budget
	// having elapsed by the time Ping's reconnect-and-retry logic runs.
	// database/sql returns ctx.Err() ("context canceled") for a query
	// issued against an already-done context without even reaching the
	// driver, which is exactly the error isStaleConn recognizes and that
	// triggers the reconnect path in production — even though the
	// underlying connection here is perfectly healthy.
	expiredCtx, cancel := context.WithCancel(ctx)
	cancel()

	if err := l.Ping(expiredCtx); err != nil {
		t.Fatalf("Ping: expected self-heal to succeed despite an already-expired caller context, got: %v", err)
	}
}

func TestReconnect_QueryRowContext_AfterReconnect(t *testing.T) {
	ctx := context.Background()
	cfg, _ := startTestServer(t)

	l, err := Open(ctx, cfg)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer l.Close()

	start := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	if err := l.InsertSpans(ctx, []*domain.Span{testSpan("p", "s1", start)}); err != nil {
		t.Fatalf("insert: %v", err)
	}

	simulateStaleDB(l)
	if err := reconnectLocked(ctx, l); err != nil {
		t.Fatalf("reconnect: %v", err)
	}

	var n int
	if err := l.QueryRowContext(ctx, "SELECT count(*) FROM lake.spans").Scan(&n); err != nil {
		t.Fatalf("QueryRowContext after reconnect: %v", err)
	}
	if n != 1 {
		t.Errorf("span count: got %d, want 1", n)
	}
}
