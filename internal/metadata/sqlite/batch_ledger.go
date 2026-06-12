package sqlite

import (
	"context"
	"fmt"
	"time"
)

// ---- Batch Ledger (ADR-0004) ----

// MarkBatchCommitted records a Batch ID as committed to the Lake.
// Idempotent: re-marking a committed batch keeps the original committed_at.
func (s *Store) MarkBatchCommitted(ctx context.Context, batchID string, committedAt time.Time) error {
	if committedAt.IsZero() {
		committedAt = time.Now()
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO committed_batches (batch_id, committed_at)
		 VALUES (?, ?)`,
		batchID, committedAt.UTC().Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("sqlite: mark batch committed: %w", err)
	}
	return nil
}

// IsBatchCommitted reports whether the Batch ID is in the ledger.
func (s *Store) IsBatchCommitted(ctx context.Context, batchID string) (bool, error) {
	var count int
	err := s.db.QueryRowContext(ctx,
		`SELECT count(*) FROM committed_batches WHERE batch_id = ?`,
		batchID,
	).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("sqlite: is batch committed: %w", err)
	}
	return count > 0, nil
}
