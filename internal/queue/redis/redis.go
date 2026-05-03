package redis

import (
	"context"

	"github.com/redis/go-redis/v9"
	"github.com/zbloss/lantern/internal/domain"
)

// IngestQueue is the Redis-backed implementation of queue.IngestQueue.
// Pushes and pops JSON-encoded span batches on KeyIngestSpans.
type IngestQueue struct {
	client *redis.Client
}

func NewIngestQueue(client *redis.Client) *IngestQueue {
	return &IngestQueue{client: client}
}

func (q *IngestQueue) Enqueue(ctx context.Context, spans []*domain.Span) error {
	panic("not implemented")
}

// Dequeue blocks for up to 5 seconds waiting for the next span batch.
// Returns nil, nil when the timeout elapses with no entry available.
func (q *IngestQueue) Dequeue(ctx context.Context) ([]*domain.Span, error) {
	panic("not implemented")
}

// EvalQueue is the Redis-backed implementation of queue.EvalQueue.
// Pushes and pops JSON-encoded EvalJob entries on KeyEvalJobs.
type EvalQueue struct {
	client *redis.Client
}

func NewEvalQueue(client *redis.Client) *EvalQueue {
	return &EvalQueue{client: client}
}

func (q *EvalQueue) Enqueue(ctx context.Context, job *domain.EvalJob) error {
	panic("not implemented")
}

// Dequeue blocks for up to 5 seconds waiting for the next eval job.
// Returns nil, nil when the timeout elapses with no entry available.
func (q *EvalQueue) Dequeue(ctx context.Context) (*domain.EvalJob, error) {
	panic("not implemented")
}
