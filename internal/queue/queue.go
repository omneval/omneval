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
	// KeyIngestSpans is the Redis list key for translated span batches.
	// Ingest API pushes; Writer Service pops.
	KeyIngestSpans = "omneval:ingest:spans"

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

// EvalQueue carries eval jobs from the Writer Service to the Eval Workers.
// Each Enqueue call pushes one JSON-encoded EvalJob per entry.
type EvalQueue interface {
	Enqueue(ctx context.Context, job *domain.EvalJob) error
	Dequeue(ctx context.Context) (*domain.EvalJob, error)
}
