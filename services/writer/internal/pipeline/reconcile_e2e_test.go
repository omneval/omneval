package pipeline

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/omneval/omneval/internal/buffer"
	"github.com/omneval/omneval/internal/config"
	"github.com/omneval/omneval/internal/domain"
	"github.com/omneval/omneval/internal/lake"
	"github.com/omneval/omneval/internal/laketest"
	"github.com/omneval/omneval/internal/queue"
	s3pkg "github.com/omneval/omneval/internal/storage/s3"
	"github.com/omneval/omneval/services/writer/internal/reconcile"
)

// fakeObjectStore is an in-memory object store that satisfies both
// buffer.ObjectStore (for staging) and reconcile.Store (for the sweep). It
// tracks a LastModified timestamp per key so reconciliation's age-based
// cutoffs can be exercised deterministically in tests.
type fakeObjectStore struct {
	objects      map[string][]byte
	lastModified map[string]time.Time
	bucket       string
}

func newFakeObjectStore() *fakeObjectStore {
	return &fakeObjectStore{
		objects:      make(map[string][]byte),
		lastModified: make(map[string]time.Time),
		bucket:       "test-bucket",
	}
}

func (f *fakeObjectStore) PutSized(_ context.Context, key string, r io.Reader, _ int64) error {
	data, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	f.objects[key] = data
	f.lastModified[key] = time.Now()
	return nil
}

func (f *fakeObjectStore) Get(_ context.Context, key string) (io.ReadCloser, error) {
	data, ok := f.objects[key]
	if !ok {
		return nil, fmt.Errorf("get %s: %w", key, buffer.ErrNotFound)
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

// setAge backdates the LastModified timestamp for key, simulating an
// object staged a while ago.
func (f *fakeObjectStore) setAge(key string, age time.Duration) {
	f.lastModified[key] = time.Now().Add(-age)
}

func (f *fakeObjectStore) ListObjectsOlderThan(_ context.Context, prefix string, cutoff time.Time) ([]s3pkg.ObjectInfo, error) {
	var result []s3pkg.ObjectInfo
	for key, data := range f.objects {
		if len(key) < len(prefix) || key[:len(prefix)] != prefix {
			continue
		}
		lm := f.lastModified[key]
		if lm.Before(cutoff) {
			result = append(result, s3pkg.ObjectInfo{
				Key:          key,
				Bucket:       f.bucket,
				LastModified: lm,
				Size:         int64(len(data)),
			})
		}
	}
	return result, nil
}

func (f *fakeObjectStore) DeleteObjectsBatch(_ context.Context, _ string, keys []string) error {
	for _, key := range keys {
		delete(f.objects, key)
		delete(f.lastModified, key)
	}
	return nil
}

// reconcileTestPipeline wires a buffered pipeline plus a reconciliation
// Worker sharing the same fake object store, ledger and ingest queue, so a
// batch staged without a queue reference can be recovered by the sweep and
// then processed end-to-end into the Lake.
func reconcileTestPipeline(t *testing.T) (*Pipeline, *lake.Lake, *fakeReliableQueue, *fakeObjectStore, *fakeLedger, *reconcile.Worker) {
	t.Helper()

	lk := laketest.NewLocal(t)

	store := newFakeObjectStore()
	rq := &fakeReliableQueue{}
	buf := buffer.New(store)
	ledger := newFakeLedger()

	p := New(nil, testPricing, nil, ledger, nil, nil).
		WithLake(lk).
		WithBuffer(rq, buf, ledger)

	cfg := &config.ReconciliationConfig{
		Enabled:            true,
		IntervalMinutes:    5,
		GracePeriodMinutes: 10,
		RetentionHours:     168,
	}
	w := reconcile.New(store, ledger, rq, nil, cfg)
	if w == nil {
		t.Fatal("expected non-nil reconciliation worker")
	}

	return p, lk, rq, store, ledger, w
}

// TestReconcileRecovery_CrashBeforeCommitIsRecovered is the end-to-end
// loss-proofing criterion from issue #88: a batch staged in the Ingest
// Buffer whose queue reference was lost (writer crashed between dequeue and
// commit) has no ledger entry and no pending queue ref. The reconciliation
// sweep must recover it by re-enqueueing its Batch ID; running that
// reference through the pipeline then lands the span in the Lake.
func TestReconcileRecovery_CrashBeforeCommitIsRecovered(t *testing.T) {
	ctx := context.Background()
	p, lk, rq, store, ledger, w := reconcileTestPipeline(t)

	// Simulate a writer that staged the batch (PutSized succeeded) but
	// crashed before enqueueing the reference or committing to the ledger.
	const batchID = "crashed-batch"
	if err := buffer.New(store).Stage(ctx, batchID, []*domain.Span{bufferedTestSpan("s1")}); err != nil {
		t.Fatalf("stage: %v", err)
	}
	store.setAge(buffer.Key(batchID), 30*time.Minute) // older than the grace period

	// Sanity check: nothing has landed in the Lake or ledger yet, and no
	// reference is queued.
	if got := lakeSpanCount(t, lk); got != 0 {
		t.Fatalf("lake span count before recovery: got %d, want 0", got)
	}
	if ledger.committed[batchID] {
		t.Fatal("batch must not be committed before recovery")
	}
	if len(rq.refs) != 0 {
		t.Fatal("no reference should be queued before recovery")
	}

	// The reconciliation sweep should recover the orphaned batch.
	result, err := w.Run(ctx)
	if err != nil {
		t.Fatalf("reconcile Run: %v", err)
	}
	if result.BatchesRecovered != 1 {
		t.Fatalf("BatchesRecovered = %d, want 1", result.BatchesRecovered)
	}
	if result.ObjectsDeleted != 0 {
		t.Fatalf("ObjectsDeleted = %d, want 0 (uncommitted object must not be deleted)", result.ObjectsDeleted)
	}
	if len(rq.refs) != 1 || rq.refs[0].BatchID != batchID {
		t.Fatalf("unexpected refs after recovery: %+v", rq.refs)
	}

	// Feed the recovered reference through the pipeline, as the writer's
	// dequeue loop would.
	entry := &queue.IngestEntry{Ref: &rq.refs[0], Raw: "raw-" + batchID}
	p.processEntry(ctx, entry)

	if got := lakeSpanCount(t, lk); got != 1 {
		t.Errorf("lake span count after recovery + processing: got %d, want 1", got)
	}
	if !ledger.committed[batchID] {
		t.Error("recovered batch must be recorded in the ledger after processing")
	}
	if len(rq.acked) != 1 {
		t.Errorf("acked: got %d, want 1", len(rq.acked))
	}

	// A second sweep must not recover it again (it's now in the ledger).
	result2, err := w.Run(ctx)
	if err != nil {
		t.Fatalf("reconcile Run (2nd sweep): %v", err)
	}
	if result2.BatchesRecovered != 0 {
		t.Errorf("second sweep BatchesRecovered = %d, want 0", result2.BatchesRecovered)
	}
}

// TestReconcileRecovery_RetentionGCDeletesOnlyCommittedAgedObjects exercises
// the GC half of the sweep against the same fake store/ledger used for
// recovery: a committed object past the retention window is deleted, while
// an uncommitted object of the same age is recovered instead of deleted.
func TestReconcileRecovery_RetentionGCDeletesOnlyCommittedAgedObjects(t *testing.T) {
	ctx := context.Background()
	_, _, rq, store, ledger, w := reconcileTestPipeline(t)

	const committedID = "old-committed-batch"
	const uncommittedID = "old-uncommitted-batch"

	for _, id := range []string{committedID, uncommittedID} {
		if err := buffer.New(store).Stage(ctx, id, []*domain.Span{bufferedTestSpan(id)}); err != nil {
			t.Fatalf("stage %s: %v", id, err)
		}
		// Older than both the grace period and the retention window.
		store.setAge(buffer.Key(id), 200*time.Hour)
	}
	ledger.committed[committedID] = true

	result, err := w.Run(ctx)
	if err != nil {
		t.Fatalf("reconcile Run: %v", err)
	}

	if result.ObjectsDeleted != 1 {
		t.Errorf("ObjectsDeleted = %d, want 1", result.ObjectsDeleted)
	}
	if _, ok := store.objects[buffer.Key(committedID)]; ok {
		t.Error("committed object past retention window should have been deleted")
	}
	if _, ok := store.objects[buffer.Key(uncommittedID)]; !ok {
		t.Error("uncommitted object must never be deleted, regardless of age")
	}

	if result.BatchesRecovered != 1 {
		t.Errorf("BatchesRecovered = %d, want 1", result.BatchesRecovered)
	}
	if len(rq.refs) != 1 || rq.refs[0].BatchID != uncommittedID {
		t.Fatalf("unexpected refs: %+v", rq.refs)
	}
}
