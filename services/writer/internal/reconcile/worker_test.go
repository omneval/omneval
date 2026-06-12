package reconcile

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/omneval/omneval/internal/config"
	"github.com/omneval/omneval/internal/leader"
	"github.com/omneval/omneval/internal/queue"
	s3pkg "github.com/omneval/omneval/internal/storage/s3"
)

// mockStore implements Store for testing.
type mockStore struct {
	listObjectsFn func(ctx context.Context, prefix string, cutoff time.Time) ([]s3pkg.ObjectInfo, error)
	deleteFn      func(ctx context.Context, bucket string, keys []string) error
}

func (m *mockStore) ListObjectsOlderThan(ctx context.Context, prefix string, cutoff time.Time) ([]s3pkg.ObjectInfo, error) {
	return m.listObjectsFn(ctx, prefix, cutoff)
}

func (m *mockStore) DeleteObjectsBatch(ctx context.Context, bucket string, keys []string) error {
	if m.deleteFn == nil {
		return nil
	}
	return m.deleteFn(ctx, bucket, keys)
}

var _ Store = (*mockStore)(nil)

// mockLedger implements Ledger for testing.
type mockLedger struct {
	committed map[string]bool
	err       error
}

func (m *mockLedger) IsBatchCommitted(_ context.Context, batchID string) (bool, error) {
	if m.err != nil {
		return false, m.err
	}
	return m.committed[batchID], nil
}

var _ Ledger = (*mockLedger)(nil)

// mockRefEnqueuer implements RefEnqueuer for testing.
type mockRefEnqueuer struct {
	refs []queue.BatchRef
	err  error
}

func (m *mockRefEnqueuer) EnqueueRef(_ context.Context, ref queue.BatchRef) error {
	if m.err != nil {
		return m.err
	}
	m.refs = append(m.refs, ref)
	return nil
}

var _ RefEnqueuer = (*mockRefEnqueuer)(nil)

// mockMetrics implements Metrics for testing.
type mockMetrics struct {
	recovered      int
	deleted        int
	durationCalled bool
}

func (m *mockMetrics) RecordReconcileBatchesRecovered(count int) { m.recovered += count }
func (m *mockMetrics) RecordReconcileObjectsDeleted(count int)   { m.deleted += count }
func (m *mockMetrics) RecordReconcileSweepDuration(_ float64)    { m.durationCalled = true }

var _ Metrics = (*mockMetrics)(nil)

func enabledCfg() *config.ReconciliationConfig {
	return &config.ReconciliationConfig{
		Enabled:            true,
		IntervalMinutes:    5,
		GracePeriodMinutes: 10,
		RetentionHours:     168,
	}
}

func TestNew_DisabledReturnsNil(t *testing.T) {
	cfg := &config.ReconciliationConfig{Enabled: false}
	w := New(&mockStore{}, &mockLedger{}, &mockRefEnqueuer{}, nil, cfg)
	if w != nil {
		t.Error("expected nil worker when reconciliation is disabled")
	}
}

func TestNew_EnabledReturnsWorker(t *testing.T) {
	w := New(&mockStore{}, &mockLedger{}, &mockRefEnqueuer{}, nil, enabledCfg())
	if w == nil {
		t.Fatal("expected non-nil worker")
	}
}

func TestRun_RecoversUncommittedBatch(t *testing.T) {
	objects := []s3pkg.ObjectInfo{
		{Key: "buffer/batch-1.json", Bucket: "test-bucket", LastModified: time.Now().Add(-30 * time.Minute), Size: 100},
	}
	store := &mockStore{
		listObjectsFn: func(ctx context.Context, prefix string, cutoff time.Time) ([]s3pkg.ObjectInfo, error) {
			return objects, nil
		},
	}
	ledger := &mockLedger{committed: map[string]bool{}}
	refs := &mockRefEnqueuer{}
	metrics := &mockMetrics{}

	w := New(store, ledger, refs, metrics, enabledCfg())
	result, err := w.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if result.BatchesRecovered != 1 {
		t.Errorf("BatchesRecovered = %d, want 1", result.BatchesRecovered)
	}
	if result.ObjectsDeleted != 0 {
		t.Errorf("ObjectsDeleted = %d, want 0", result.ObjectsDeleted)
	}
	if len(refs.refs) != 1 || refs.refs[0].BatchID != "batch-1" {
		t.Fatalf("unexpected enqueued refs: %+v", refs.refs)
	}
	if metrics.recovered != 1 {
		t.Errorf("metrics.recovered = %d, want 1", metrics.recovered)
	}
	if !metrics.durationCalled {
		t.Error("expected sweep duration to be recorded")
	}
}

func TestRun_NeverReenqueuesCommittedBatch(t *testing.T) {
	objects := []s3pkg.ObjectInfo{
		{Key: "buffer/batch-1.json", Bucket: "test-bucket", LastModified: time.Now().Add(-30 * time.Minute), Size: 100},
	}
	store := &mockStore{
		listObjectsFn: func(ctx context.Context, prefix string, cutoff time.Time) ([]s3pkg.ObjectInfo, error) {
			return objects, nil
		},
	}
	ledger := &mockLedger{committed: map[string]bool{"batch-1": true}}
	refs := &mockRefEnqueuer{}

	w := New(store, ledger, refs, nil, enabledCfg())
	result, err := w.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if result.BatchesRecovered != 0 {
		t.Errorf("BatchesRecovered = %d, want 0", result.BatchesRecovered)
	}
	if len(refs.refs) != 0 {
		t.Fatalf("expected no refs enqueued for a committed batch, got %+v", refs.refs)
	}
}

func TestRun_GCDeletesOnlyCommittedAndAgedObjects(t *testing.T) {
	now := time.Now()
	objects := []s3pkg.ObjectInfo{
		// Committed and older than the retention window: should be deleted.
		{Key: "buffer/old-committed.json", Bucket: "test-bucket", LastModified: now.Add(-200 * time.Hour), Size: 100},
		// Committed but within the retention window: must NOT be deleted.
		{Key: "buffer/fresh-committed.json", Bucket: "test-bucket", LastModified: now.Add(-50 * time.Hour), Size: 100},
		// Uncommitted and very old: must NOT be deleted (recovered instead).
		{Key: "buffer/old-uncommitted.json", Bucket: "test-bucket", LastModified: now.Add(-300 * time.Hour), Size: 100},
	}
	store := &mockStore{
		listObjectsFn: func(ctx context.Context, prefix string, cutoff time.Time) ([]s3pkg.ObjectInfo, error) {
			return objects, nil
		},
	}
	var deletedKeys []string
	var deletedBucket string
	store.deleteFn = func(ctx context.Context, bucket string, keys []string) error {
		deletedBucket = bucket
		deletedKeys = keys
		return nil
	}

	ledger := &mockLedger{committed: map[string]bool{
		"old-committed":   true,
		"fresh-committed": true,
		// old-uncommitted intentionally absent.
	}}
	refs := &mockRefEnqueuer{}
	metrics := &mockMetrics{}

	w := New(store, ledger, refs, metrics, enabledCfg())
	result, err := w.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if result.ObjectsDeleted != 1 {
		t.Errorf("ObjectsDeleted = %d, want 1", result.ObjectsDeleted)
	}
	if len(deletedKeys) != 1 || deletedKeys[0] != "buffer/old-committed.json" {
		t.Fatalf("deletedKeys = %+v, want [buffer/old-committed.json]", deletedKeys)
	}
	if deletedBucket != "test-bucket" {
		t.Errorf("deletedBucket = %q, want test-bucket", deletedBucket)
	}

	// The uncommitted batch should have been recovered, not deleted.
	if result.BatchesRecovered != 1 {
		t.Errorf("BatchesRecovered = %d, want 1", result.BatchesRecovered)
	}
	if len(refs.refs) != 1 || refs.refs[0].BatchID != "old-uncommitted" {
		t.Fatalf("unexpected enqueued refs: %+v", refs.refs)
	}
	if metrics.deleted != 1 {
		t.Errorf("metrics.deleted = %d, want 1", metrics.deleted)
	}
}

func TestRun_LedgerErrorIsRecordedAndDoesNotAbortSweep(t *testing.T) {
	objects := []s3pkg.ObjectInfo{
		{Key: "buffer/batch-bad.json", Bucket: "test-bucket", LastModified: time.Now().Add(-30 * time.Minute), Size: 100},
		{Key: "buffer/batch-good.json", Bucket: "test-bucket", LastModified: time.Now().Add(-30 * time.Minute), Size: 100},
	}
	store := &mockStore{
		listObjectsFn: func(ctx context.Context, prefix string, cutoff time.Time) ([]s3pkg.ObjectInfo, error) {
			return objects, nil
		},
	}

	calls := 0
	ledger := &mockLedger{committed: map[string]bool{}}
	// Wrap IsBatchCommitted via a custom ledger that errors on the first call.
	failOnce := &failingOnceLedger{inner: ledger, failBatchID: "batch-bad"}
	_ = calls

	refs := &mockRefEnqueuer{}
	w := New(store, failOnce, refs, nil, enabledCfg())

	result, err := w.Run(context.Background())
	if err == nil {
		t.Fatal("expected an error to be returned when a ledger lookup fails")
	}
	if len(result.Errors) != 1 {
		t.Fatalf("result.Errors = %+v, want 1 error", result.Errors)
	}
	// The good batch should still have been recovered.
	if result.BatchesRecovered != 1 {
		t.Errorf("BatchesRecovered = %d, want 1", result.BatchesRecovered)
	}
	if len(refs.refs) != 1 || refs.refs[0].BatchID != "batch-good" {
		t.Fatalf("unexpected enqueued refs: %+v", refs.refs)
	}
}

// failingOnceLedger errors for a specific batch ID and delegates otherwise.
type failingOnceLedger struct {
	inner       Ledger
	failBatchID string
}

func (f *failingOnceLedger) IsBatchCommitted(ctx context.Context, batchID string) (bool, error) {
	if batchID == f.failBatchID {
		return false, errors.New("ledger unavailable")
	}
	return f.inner.IsBatchCommitted(ctx, batchID)
}

func TestRunLoop_FollowerSkipsSweep(t *testing.T) {
	var listCalls int
	store := &mockStore{
		listObjectsFn: func(ctx context.Context, prefix string, cutoff time.Time) ([]s3pkg.ObjectInfo, error) {
			listCalls++
			return nil, nil
		},
	}
	ledger := &mockLedger{committed: map[string]bool{}}
	refs := &mockRefEnqueuer{}

	cfg := &config.ReconciliationConfig{
		Enabled:            true,
		IntervalMinutes:    1,
		GracePeriodMinutes: 10,
		RetentionHours:     168,
	}
	w := New(store, ledger, refs, nil, cfg)

	// Build a leader election whose underlying lock is held by another
	// instance, so IsLeader() reports false.
	ops := newFollowerOps("other-instance")
	election, err := leader.NewLeaderElection(ops, "test-lock", "this-instance", 15*time.Second)
	if err != nil {
		t.Fatalf("NewLeaderElection: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()

	_ = w.RunLoop(ctx, election)

	if listCalls != 0 {
		t.Errorf("listCalls = %d, want 0 (follower must skip the sweep)", listCalls)
	}
}

// newFollowerOps builds leader.Ops whose Get always reports the given
// leader value and whose SetNX always fails, simulating a follower.
func newFollowerOps(currentLeader string) leader.Ops {
	return leader.Ops{
		SetNX: func(_ context.Context, _ string, _ string, _ time.Duration) (bool, error) {
			return false, nil
		},
		Get: func(_ context.Context, _ string) (string, error) {
			return currentLeader, nil
		},
		Set: func(_ context.Context, _ string, _ string, _ time.Duration) (bool, error) {
			return true, nil
		},
		Del: func(_ context.Context, _ ...string) (int64, error) {
			return 0, nil
		},
		DelIfMatch: func(_ context.Context, _ string, _ string) (bool, error) {
			return false, nil
		},
	}
}
