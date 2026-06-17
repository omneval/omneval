package metadata

import (
	"context"
	"time"
)

// BatchLedgerStore is the domain interface for batch commit dedupe operations.
// The batch ledger tracks which ingest batches have been committed to the Lake,
// making queue redelivery idempotent (ADR-0004 / #94).
type BatchLedgerStore interface {
	MarkBatchCommitted(ctx context.Context, batchID string, committedAt time.Time) error
	IsBatchCommitted(ctx context.Context, batchID string) (bool, error)
}