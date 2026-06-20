package lake

import (
	"context"
	"sync"
	"testing"
	"time"
)

// TestLakeMutexAllowsConcurrentReaders proves l.mu is a sync.RWMutex (not a
// plain sync.Mutex): two goroutines must both be able to hold the lock in
// its shared (RLock) mode at the same time. This is the precondition for
// Query/Exec/Ping/InsertSpans/InsertScores to run concurrently rather than
// fully serializing every Lake call within one process — the root cause of
// a single slow UI query freezing an entire query/writer pod, including its
// own health checks (issue: query pod wedged while quack-server was healthy).
func TestLakeMutexAllowsConcurrentReaders(t *testing.T) {
	l := &Lake{}
	l.mu.RLock()
	defer l.mu.RUnlock()

	acquired := make(chan struct{})
	go func() {
		l.mu.RLock()
		defer l.mu.RUnlock()
		close(acquired)
	}()

	select {
	case <-acquired:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("a second RLock did not acquire while the first RLock was held — l.mu does not allow concurrent readers")
	}
}

// TestOpenConfiguresConnectionPoolLargerThanOne proves Open honors
// cfg.MaxOpenConns instead of hard-pinning the pool to a single connection.
// A single physical connection means every Lake call — including the
// readiness/liveness Ping — serializes behind whatever query is currently
// running, which is what let one slow UI query freeze the whole pod.
func TestOpenConfiguresConnectionPoolLargerThanOne(t *testing.T) {
	ctx := context.Background()
	cfg, _ := startTestServer(t)
	cfg.MaxOpenConns = 4

	l, err := Open(ctx, cfg)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer l.Close()

	if got := l.DB().Stats().MaxOpenConnections; got != 4 {
		t.Fatalf("MaxOpenConnections = %d, want 4", got)
	}
}

// TestOpenDefaultsMaxOpenConnsWhenUnset proves a zero-value MaxOpenConns
// (e.g. a Config built without ConfigFromApp, or an older config file
// missing the new field) still gets a usable pool instead of silently
// reverting to the single-connection wedge risk.
func TestOpenDefaultsMaxOpenConnsWhenUnset(t *testing.T) {
	ctx := context.Background()
	cfg, _ := startTestServer(t)
	// cfg.MaxOpenConns left at its zero value.

	l, err := Open(ctx, cfg)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer l.Close()

	if got := l.DB().Stats().MaxOpenConnections; got != defaultMaxOpenConns {
		t.Fatalf("MaxOpenConnections = %d, want default %d", got, defaultMaxOpenConns)
	}
}

// TestPingDoesNotBlockOnConcurrentSlowOperation reproduces the production
// incident directly: a long-running operation holding one pool connection
// must not block a concurrent Ping waiting behind it. Before this fix,
// MaxOpenConns(1) plus a single sync.Mutex meant Ping always queued behind
// any in-flight call on the same Lake instance, so kubelet's healthz check
// hung for as long as the slow query did — exactly what made the query pod
// look wedged while quack-server itself was healthy.
//
// Compares Ping's concurrent-run duration against its own solo baseline
// (rather than an absolute threshold) because a single local Ping round
// trip through quack_serve is itself slow and variable across machines —
// what matters is whether the held connection inflates that latency, not
// its absolute value.
func TestPingDoesNotBlockOnConcurrentSlowOperation(t *testing.T) {
	ctx := context.Background()
	cfg, _ := startTestServer(t)
	cfg.MaxOpenConns = 4

	l, err := Open(ctx, cfg)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer l.Close()

	if err := l.Ping(ctx); err != nil { // warm up
		t.Fatalf("warmup ping: %v", err)
	}
	baselineStart := time.Now()
	if err := l.Ping(ctx); err != nil {
		t.Fatalf("baseline ping: %v", err)
	}
	baseline := time.Since(baselineStart)

	holdDuration := baseline * 4
	if holdDuration < 200*time.Millisecond {
		holdDuration = 200 * time.Millisecond
	}
	holdStarted := make(chan struct{})
	go func() {
		conn, err := l.DB().Conn(ctx)
		if err != nil {
			close(holdStarted)
			return
		}
		defer conn.Close()
		close(holdStarted)
		time.Sleep(holdDuration)
	}()
	<-holdStarted
	time.Sleep(20 * time.Millisecond) // let the goroutine settle into its hold

	start := time.Now()
	if err := l.Ping(ctx); err != nil {
		t.Fatalf("ping: %v", err)
	}
	elapsed := time.Since(start)
	// If Ping queued behind the held connection (the pre-fix bug), it would
	// take roughly holdDuration (4x baseline). Concurrency should keep it
	// close to baseline instead.
	if threshold := baseline*3 + 50*time.Millisecond; elapsed > threshold {
		t.Fatalf("Ping took %v (solo baseline %v) while a concurrent operation held a separate connection for %v — expected it to return close to baseline instead of queuing behind the hold", elapsed, baseline, holdDuration)
	}
}

// TestConcurrentQueriesRunInParallelNotSerialized proves two real queries
// issued concurrently through the public Lake API run in parallel rather
// than serializing end-to-end, comparing wall-clock time for one query
// against two run concurrently (robust to absolute hardware speed).
func TestConcurrentQueriesRunInParallelNotSerialized(t *testing.T) {
	ctx := context.Background()
	cfg, _ := startTestServer(t)
	cfg.MaxOpenConns = 4

	l, err := Open(ctx, cfg)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer l.Close()

	const slowQuery = "SELECT count(*) FROM range(20000) a, range(20000) b"
	run := func() {
		var n int64
		if err := l.QueryRow(slowQuery).Scan(&n); err != nil {
			t.Errorf("slow query: %v", err)
		}
	}

	soloStart := time.Now()
	run()
	soloDuration := time.Since(soloStart)

	var wg sync.WaitGroup
	concurrentStart := time.Now()
	wg.Add(2)
	go func() { defer wg.Done(); run() }()
	go func() { defer wg.Done(); run() }()
	wg.Wait()
	concurrentDuration := time.Since(concurrentStart)

	// Fully serialized (mutex/pool of 1) would take ~2x the solo duration;
	// running in parallel should land much closer to 1x. 1.6x leaves
	// headroom for scheduling noise while still clearly distinguishing the
	// two cases.
	if threshold := soloDuration * 16 / 10; concurrentDuration > threshold {
		t.Fatalf("two concurrent queries took %v (solo: %v) — expected them to overlap instead of serializing", concurrentDuration, soloDuration)
	}
}
