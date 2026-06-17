//go:build lake

package pipeline

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/omneval/omneval/internal/domain"
	"github.com/omneval/omneval/internal/lake"
	"github.com/omneval/omneval/internal/lake/lakeservertest"
)

// openErrTestLake opens an embedded local Lake for the pipeline error
// tests, skipping if the DuckLake extension is unavailable.
func openErrTestLake(t *testing.T) *lake.Lake {
	t.Helper()
	ctx := context.Background()
	cfg, _ := lakeservertest.NewLocal(t)
	lk, err := lake.Open(ctx, cfg)
	if err != nil {
		t.Skipf("lake.Open: %v (ducklake extension unavailable)", err)
	}
	t.Cleanup(func() { lk.Close() })
	return lk
}

func lakeSpanCountFor(t *testing.T, lk *lake.Lake, projectID string) int {
	t.Helper()
	var n int
	if err := lk.DB().QueryRowContext(context.Background(),
		"SELECT count(*) FROM lake.spans WHERE project_id = ?", projectID).Scan(&n); err != nil {
		t.Fatalf("count lake spans: %v", err)
	}
	return n
}

// NOTE: testPricing, fakeIngestQueue, fakeEvalQueue, and fakeMetaStore
// are defined in pipeline_test_helpers.go (no build tag).

// TestPipeline_Run_continuesAfterDequeueError verifies that when Dequeue
// returns an error (e.g. Redis unreachable), the pipeline logs the error
// and continues on the next iteration instead of exiting.
func TestPipeline_Run_continuesAfterDequeueError(t *testing.T) {
	lk := openErrTestLake(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	spans := []*domain.Span{{
		TraceID:   "trace-00000000000000001",
		SpanID:    "span-00000000000000001",
		Model:     "gpt-4o",
		ProjectID: "proj-1",
		Input:     `[{"role":"user","content":"test"}]`,
		Output:    `[{"role":"assistant","content":"response"}]`,
	}}

	// Dequeue fails once (simulating a transient Redis issue),
	// then succeeds on the next call.
	ingestQ := &fakeIngestQueue{
		batches:    [][]*domain.Span{spans},
		dequeueErr: fmt.Errorf("redis: connection refused"),
		consumeAll: false,
	}

	pl := New(ingestQ, testPricing, &fakeMetaStore{}, newFakeLedger(), nil, nil).WithLake(lk)

	pipelineDone := make(chan error, 1)
	go func() {
		pipelineDone <- pl.Run(ctx)
	}()

	// Wait for the pipeline to finish (it will loop until context deadline).
	err := <-pipelineDone
	if err == nil {
		t.Fatal("expected pipeline to stop when context canceled")
	}
	t.Logf("pipeline Run returned: %v", err)

	// The dequeue error happened once, then the batch was dequeued successfully.
	// After the error (idx=0) + success (idx=1), idx should be 1.
	if ingestQ.idx != 1 {
		t.Errorf("dequeue called %d times, want 1 (pipeline should not stop on dequeue error)", ingestQ.idx)
	}

	// The span should have been written after the retry.
	if count := lakeSpanCountFor(t, lk, "proj-1"); count != 1 {
		t.Errorf("span count in lake: got %d, want 1", count)
	}
}

// TestPipeline_Run_continuesAfterWriteFailure verifies that when the DuckDB
// write fails, the pipeline logs the error but continues processing subsequent
// batches instead of crashing and losing remaining spans.
func TestPipeline_Run_continuesAfterWriteFailure(t *testing.T) {
	lk := openErrTestLake(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	spans1 := []*domain.Span{{
		TraceID:   "trace-00000000000000001",
		SpanID:    "span-00000000000000001",
		Model:     "gpt-4o",
		ProjectID: "proj-1",
		Input:     `[{"role":"user","content":"test1"}]`,
		Output:    `[{"role":"assistant","content":"response1"}]`,
	}}
	spans2 := []*domain.Span{{
		TraceID:   "trace-00000000000000002",
		SpanID:    "span-00000000000000002",
		Model:     "gpt-4o",
		ProjectID: "proj-1",
		Input:     `[{"role":"user","content":"test2"}]`,
		Output:    `[{"role":"assistant","content":"response2"}]`,
	}}

	ingestQ := &fakeIngestQueue{
		batches:    [][]*domain.Span{spans1, spans2},
		dequeueErr: nil,
		consumeAll: false,
	}
	pl := New(ingestQ, testPricing, &fakeMetaStore{}, newFakeLedger(), nil, nil).WithLake(lk)

	pipelineDone := make(chan error, 1)
	go func() {
		pipelineDone <- pl.Run(ctx)
	}()

	err := <-pipelineDone
	if err == nil {
		t.Fatal("expected pipeline to stop when context canceled")
	}
	t.Logf("pipeline Run returned: %v", err)

	// Both batches should have been dequeued and written.
	if ingestQ.idx != 2 {
		t.Errorf("dequeued %d batches, want 2 (pipeline should not stop on any error)", ingestQ.idx)
	}

	// Both spans should be in the Lake.
	if count := lakeSpanCountFor(t, lk, "proj-1"); count != 2 {
		t.Errorf("span count in lake: got %d, want 2", count)
	}
}

// TestPipeline_Run_continuesAfterWriteError verifies that when writeSpans
// fails (simulating a DuckDB error), the pipeline logs the error, skips
// the batch, and continues processing subsequent batches.
func TestPipeline_Run_continuesAfterWriteError(t *testing.T) {
	lk := openErrTestLake(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	spans1 := []*domain.Span{{
		TraceID:   "trace-00000000000000001",
		SpanID:    "span-00000000000000001",
		Model:     "gpt-4o",
		ProjectID: "proj-1",
		Input:     `[{"role":"user","content":"test1"}]`,
		Output:    `[{"role":"assistant","content":"response1"}]`,
	}}
	spans2 := []*domain.Span{{
		TraceID:   "trace-00000000000000002",
		SpanID:    "span-00000000000000002",
		Model:     "gpt-4o",
		ProjectID: "proj-1",
		Input:     `[{"role":"user","content":"test2"}]`,
		Output:    `[{"role":"assistant","content":"response2"}]`,
	}}

	ingestQ := &fakeIngestQueue{
		batches:    [][]*domain.Span{spans1, spans2},
		dequeueErr: nil,
		consumeAll: false,
	}
	errWrite := fmt.Errorf("lake: constraint violation")
	pl := New(ingestQ, testPricing, &fakeMetaStore{}, newFakeLedger(), nil, nil).WithLake(lk)
	pl.writeErr = errWrite // inject write error

	pipelineDone := make(chan error, 1)
	go func() {
		pipelineDone <- pl.Run(ctx)
	}()

	err := <-pipelineDone
	if err == nil {
		t.Fatal("expected pipeline to stop when context canceled")
	}
	t.Logf("pipeline Run returned: %v", err)

	// All batches should have been dequeued (pipeline continued past write errors).
	if ingestQ.idx != 2 {
		t.Errorf("dequeued %d batches, want 2 (pipeline should not stop on write error)", ingestQ.idx)
	}

	// No spans should be in the lake (all writes failed).
	if count := lakeSpanCountFor(t, lk, "proj-1"); count != 0 {
		t.Errorf("span count in lake: got %d, want 0 (writes should have failed)", count)
	}
}
