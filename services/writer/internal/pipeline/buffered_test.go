package pipeline

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/omneval/omneval/internal/buffer"
	"github.com/omneval/omneval/internal/domain"
	"github.com/omneval/omneval/internal/duckdb"
	"github.com/omneval/omneval/internal/lake"
	"github.com/omneval/omneval/internal/queue"
)

// fakeReliableQueue records Ack/Requeue/EnqueueRef calls.
type fakeReliableQueue struct {
	acked    []*queue.IngestEntry
	requeued []*queue.IngestEntry
	refs     []queue.BatchRef
}

func (f *fakeReliableQueue) EnqueueRef(_ context.Context, ref queue.BatchRef) error {
	f.refs = append(f.refs, ref)
	return nil
}
func (f *fakeReliableQueue) DequeueEntry(context.Context) (*queue.IngestEntry, error) {
	return nil, nil
}
func (f *fakeReliableQueue) Ack(_ context.Context, e *queue.IngestEntry) error {
	f.acked = append(f.acked, e)
	return nil
}
func (f *fakeReliableQueue) Requeue(_ context.Context, e *queue.IngestEntry) error {
	f.requeued = append(f.requeued, e)
	return nil
}

// fakeFetcher serves staged batches from a map, like the Ingest Buffer.
type fakeFetcher struct {
	batches map[string][]*domain.Span
	err     error
}

func (f *fakeFetcher) Fetch(_ context.Context, batchID string) ([]*domain.Span, error) {
	if f.err != nil {
		return nil, f.err
	}
	spans, ok := f.batches[batchID]
	if !ok {
		return nil, fmt.Errorf("fetch %s: %w", batchID, buffer.ErrNotFound)
	}
	return spans, nil
}

// fakeLedger is an in-memory Batch Ledger.
type fakeLedger struct {
	committed map[string]bool
	markErr   error
}

func newFakeLedger() *fakeLedger { return &fakeLedger{committed: make(map[string]bool)} }

func (f *fakeLedger) MarkBatchCommitted(_ context.Context, batchID string, _ time.Time) error {
	if f.markErr != nil {
		return f.markErr
	}
	f.committed[batchID] = true
	return nil
}

func (f *fakeLedger) IsBatchCommitted(_ context.Context, batchID string) (bool, error) {
	return f.committed[batchID], nil
}

func bufferedTestPipeline(t *testing.T) (*Pipeline, *lake.Lake, *fakeReliableQueue, *fakeFetcher, *fakeLedger) {
	t.Helper()
	ctx := context.Background()

	db, err := duckdb.Open(":memory:")
	if err != nil {
		t.Fatalf("open duckdb: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	dir := t.TempDir()
	lk, err := lake.Open(ctx, lake.Config{
		CatalogDriver: lake.CatalogDriverLocal,
		CatalogDSN:    filepath.Join(dir, "catalog.ducklake"),
		DataPath:      filepath.Join(dir, "data"),
	})
	if err != nil {
		t.Skipf("lake.Open: %v (ducklake extension unavailable)", err)
	}
	t.Cleanup(func() { lk.Close() })

	rq := &fakeReliableQueue{}
	fetcher := &fakeFetcher{batches: make(map[string][]*domain.Span)}
	ledger := newFakeLedger()
	p := New(nil, db, testPricing, nil, nil, nil).
		WithLake(lk).
		WithBuffer(rq, fetcher, ledger)
	return p, lk, rq, fetcher, ledger
}

func lakeSpanCount(t *testing.T, lk *lake.Lake) int {
	t.Helper()
	var n int
	if err := lk.DB().QueryRowContext(context.Background(),
		"SELECT count(*) FROM lake.spans").Scan(&n); err != nil {
		t.Fatalf("count lake spans: %v", err)
	}
	return n
}

// TestProcessEntry_RefCommitsLakeLedgerThenAcks proves the happy path
// ordering: a reference entry is fetched from the buffer, committed to
// both stores, recorded in the ledger, and only then acked.
func TestProcessEntry_RefCommitsLakeLedgerThenAcks(t *testing.T) {
	ctx := context.Background()
	p, lk, rq, fetcher, ledger := bufferedTestPipeline(t)

	fetcher.batches["b1"] = []*domain.Span{dualWriteSpan("s1")}
	entry := &queue.IngestEntry{Ref: &queue.BatchRef{BatchID: "b1"}, Raw: "raw-b1"}

	p.processEntry(ctx, entry)

	if got := lakeSpanCount(t, lk); got != 1 {
		t.Errorf("lake span count: got %d, want 1", got)
	}
	if !ledger.committed["b1"] {
		t.Error("batch not recorded in the ledger")
	}
	if len(rq.acked) != 1 {
		t.Errorf("acked: got %d entries, want 1", len(rq.acked))
	}
	if len(rq.requeued) != 0 {
		t.Errorf("requeued: got %d entries, want 0", len(rq.requeued))
	}
}

// TestProcessEntry_RedeliveryAddsZeroLakeRows is the issue's redelivery
// criterion: re-enqueueing an already-committed Batch ID results in zero
// new rows in the Lake.
func TestProcessEntry_RedeliveryAddsZeroLakeRows(t *testing.T) {
	ctx := context.Background()
	p, lk, rq, fetcher, ledger := bufferedTestPipeline(t)

	fetcher.batches["b1"] = []*domain.Span{dualWriteSpan("s1")}

	p.processEntry(ctx, &queue.IngestEntry{Ref: &queue.BatchRef{BatchID: "b1"}, Raw: "raw-1"})
	if got := lakeSpanCount(t, lk); got != 1 {
		t.Fatalf("lake span count after first delivery: got %d, want 1", got)
	}

	// Redelivery of the same Batch ID (e.g. requeue race, Redis restart).
	p.processEntry(ctx, &queue.IngestEntry{Ref: &queue.BatchRef{BatchID: "b1"}, Raw: "raw-2"})

	if got := lakeSpanCount(t, lk); got != 1 {
		t.Errorf("lake span count after redelivery: got %d, want 1 (zero new rows)", got)
	}
	if len(rq.acked) != 2 {
		t.Errorf("both deliveries must ack, got %d", len(rq.acked))
	}
	if !ledger.committed["b1"] {
		t.Error("ledger lost the batch")
	}
}

// TestProcessEntry_LakeFailureLeavesBatchReplayable simulates a crash
// before commit: the Lake write fails, so the entry is requeued, the
// ledger never records the batch, and the staged object is untouched —
// the batch stays replayable from the buffer.
func TestProcessEntry_LakeFailureLeavesBatchReplayable(t *testing.T) {
	ctx := context.Background()
	p, _, rq, fetcher, ledger := bufferedTestPipeline(t)
	p.lake = failingLake{} // shared fake from lake_dualwrite_test.go

	fetcher.batches["b1"] = []*domain.Span{dualWriteSpan("s1")}
	entry := &queue.IngestEntry{Ref: &queue.BatchRef{BatchID: "b1"}, Raw: "raw-b1"}

	p.processEntry(ctx, entry)

	if len(rq.acked) != 0 {
		t.Error("entry must not be acked when the lake commit fails")
	}
	if len(rq.requeued) != 1 {
		t.Errorf("entry must be requeued, got %d", len(rq.requeued))
	}
	if ledger.committed["b1"] {
		t.Error("ledger must not record a batch whose lake commit failed")
	}
	if _, ok := fetcher.batches["b1"]; !ok {
		t.Error("staged batch must remain in the buffer for replay")
	}
}

// TestProcessEntry_LedgerInsertFailureRequeues covers the crash window
// between Lake commit and ledger insert: the entry is requeued (residual
// duplicates are deduped at read time per ADR-0004).
func TestProcessEntry_LedgerInsertFailureRequeues(t *testing.T) {
	ctx := context.Background()
	p, _, rq, fetcher, ledger := bufferedTestPipeline(t)
	ledger.markErr = errors.New("postgres down")

	fetcher.batches["b1"] = []*domain.Span{dualWriteSpan("s1")}
	p.processEntry(ctx, &queue.IngestEntry{Ref: &queue.BatchRef{BatchID: "b1"}, Raw: "raw-b1"})

	if len(rq.acked) != 0 {
		t.Error("entry must not be acked when the ledger insert fails")
	}
	if len(rq.requeued) != 1 {
		t.Errorf("entry must be requeued, got %d", len(rq.requeued))
	}
}

// TestProcessEntry_MissingBufferObjectIsDropped: an uncommitted batch
// whose staged object vanished can never succeed; the entry is acked so
// it does not poison the queue.
func TestProcessEntry_MissingBufferObjectIsDropped(t *testing.T) {
	ctx := context.Background()
	p, lk, rq, _, ledger := bufferedTestPipeline(t)

	p.processEntry(ctx, &queue.IngestEntry{Ref: &queue.BatchRef{BatchID: "ghost"}, Raw: "raw-ghost"})

	if len(rq.acked) != 1 {
		t.Errorf("missing-object entry must ack, got %d acks", len(rq.acked))
	}
	if len(rq.requeued) != 0 {
		t.Error("missing-object entry must not requeue")
	}
	if ledger.committed["ghost"] {
		t.Error("ledger must not record a dropped batch")
	}
	if got := lakeSpanCount(t, lk); got != 0 {
		t.Errorf("lake span count: got %d, want 0", got)
	}
}

// TestProcessEntry_LegacyPayloadEntry: payload entries on the reliable
// loop keep dual-write semantics (legacy write authoritative, lake best
// effort) and ack after processing without touching the ledger.
func TestProcessEntry_LegacyPayloadEntry(t *testing.T) {
	ctx := context.Background()
	p, lk, rq, _, ledger := bufferedTestPipeline(t)

	entry := &queue.IngestEntry{Spans: []*domain.Span{dualWriteSpan("s9")}, Raw: "raw-legacy"}
	p.processEntry(ctx, entry)

	var legacyCount int
	if err := p.db.QueryRowContext(ctx, "SELECT count(*) FROM spans").Scan(&legacyCount); err != nil {
		t.Fatalf("count legacy spans: %v", err)
	}
	if legacyCount != 1 {
		t.Errorf("legacy span count: got %d, want 1", legacyCount)
	}
	if got := lakeSpanCount(t, lk); got != 1 {
		t.Errorf("lake span count: got %d, want 1 (dual-write)", got)
	}
	if len(rq.acked) != 1 {
		t.Errorf("acked: got %d, want 1", len(rq.acked))
	}
	if len(ledger.committed) != 0 {
		t.Error("legacy payload entries must not touch the ledger")
	}
}
