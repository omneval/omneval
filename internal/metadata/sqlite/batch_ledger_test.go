package sqlite_test

import (
	"context"
	"testing"
	"time"
)

func TestBatchLedger(t *testing.T) {
	s, _ := openTestStore(t)
	defer s.Close()
	ctx := context.Background()

	// Fresh ledger: not committed.
	got, err := s.IsBatchCommitted(ctx, "batch-1")
	if err != nil {
		t.Fatalf("IsBatchCommitted: %v", err)
	}
	if got {
		t.Error("fresh ledger: expected not committed")
	}

	// Mark, idempotently.
	if err := s.MarkBatchCommitted(ctx, "batch-1", time.Now()); err != nil {
		t.Fatalf("MarkBatchCommitted: %v", err)
	}
	if err := s.MarkBatchCommitted(ctx, "batch-1", time.Now()); err != nil {
		t.Fatalf("MarkBatchCommitted twice: %v", err)
	}

	got, err = s.IsBatchCommitted(ctx, "batch-1")
	if err != nil {
		t.Fatalf("IsBatchCommitted: %v", err)
	}
	if !got {
		t.Error("expected committed after MarkBatchCommitted")
	}

	// Other batch IDs are unaffected.
	got, err = s.IsBatchCommitted(ctx, "batch-2")
	if err != nil {
		t.Fatalf("IsBatchCommitted other: %v", err)
	}
	if got {
		t.Error("unrelated batch must not be committed")
	}

	// Zero committedAt defaults to now without error.
	if err := s.MarkBatchCommitted(ctx, "batch-3", time.Time{}); err != nil {
		t.Fatalf("MarkBatchCommitted zero time: %v", err)
	}
}
