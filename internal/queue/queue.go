package queue

import (
	"context"
	"errors"

	"github.com/omneval/omneval/internal/domain"
)

var (
	// ErrQueueUnreachable signals that the underlying queue system is unavailable.
	ErrQueueUnreachable = errors.New("queue: unreachable")
)

const (
	// KeyIngestSpans is the Redis list key for the ingest queue. Legacy
	// entries are JSON-encoded span batches; with the Ingest Buffer
	// (ADR-0004) entries are JSON-encoded Batch ID references instead.
	// Ingest API pushes; Writer Service pops.
	KeyIngestSpans = "omneval:ingest:spans"

	// KeyIngestProcessing is the Redis list holding ingest entries a writer
	// has dequeued but not yet acked. Entries move here atomically on
	// dequeue and are removed by Ack only after the Lake commit and Batch
	// Ledger insert succeed.
	KeyIngestProcessing = "omneval:ingest:spans:processing"

	// KeyEvalJobs is the Redis list key for eval job entries.
	// Writer Service pushes; Eval Workers pop.
	KeyEvalJobs = "omneval:eval:jobs"
)

// IngestQueue is the durability buffer between the Ingest API and the Writer.
// Each Enqueue call pushes one JSON-encoded batch (all spans from one request)
// as a single Redis list entry.
type IngestQueue interface {
	Enqueue(ctx context.Context, spans []*domain.Span) error
	Dequeue(ctx context.Context) ([]*domain.Span, error)
}

// BatchRef is a queue entry referencing a batch staged in the Ingest
// Buffer (ADR-0004). The queue carries only this reference; the span
// payload lives in the buffer until the reconciliation GC reclaims it.
type BatchRef struct {
	BatchID string `json:"batch_id"`
}

// IngestEntry is one dequeued ingest queue entry. Exactly one of Spans or
// Ref is set: Spans for a legacy payload entry, Ref for an Ingest Buffer
// reference. Raw is the opaque queue token used by Ack and Requeue.
type IngestEntry struct {
	Spans []*domain.Span
	Ref   *BatchRef
	Raw   string
}

// ReliableIngestQueue dequeues ingest entries with explicit
// acknowledgement: an entry stays on a processing list until Ack removes
// it (after the Lake commit and ledger insert) or Requeue returns it to
// the queue for another attempt.
type ReliableIngestQueue interface {
	EnqueueRef(ctx context.Context, ref BatchRef) error
	DequeueEntry(ctx context.Context) (*IngestEntry, error)
	Ack(ctx context.Context, entry *IngestEntry) error
	Requeue(ctx context.Context, entry *IngestEntry) error
}

// EvalQueue carries eval jobs from the Writer Service to the Eval Workers.
// Each Enqueue call pushes one JSON-encoded EvalJob per entry.
type EvalQueue interface {
	Enqueue(ctx context.Context, job *domain.EvalJob) error
	Dequeue(ctx context.Context) (*domain.EvalJob, error)
}
