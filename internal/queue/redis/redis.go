package redis

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/zbloss/lantern/internal/domain"
	"github.com/zbloss/lantern/internal/queue"
)

// redisClient defines the subset of go-redis Client methods used by the queue
// implementations. This allows injecting a mock in tests.
type redisClient interface {
	RPush(ctx context.Context, key string, values ...any) *redis.IntCmd
	BRPop(ctx context.Context, timeout time.Duration, keys ...string) *redis.StringSliceCmd
	Ping(ctx context.Context) *redis.StatusCmd
}

// IngestQueue is the Redis-backed implementation of queue.IngestQueue.
// Pushes and pops JSON-encoded span batches on KeyIngestSpans.
type IngestQueue struct {
	client redisClient
}

func NewIngestQueue(client redisClient) *IngestQueue {
	return &IngestQueue{client: client}
}

func (q *IngestQueue) Enqueue(ctx context.Context, spans []*domain.Span) error {
	data, err := json.Marshal(spans)
	if err != nil {
		return fmt.Errorf("marshalling spans: %w", err)
	}
	if err := q.client.RPush(ctx, queue.KeyIngestSpans, data).Err(); err != nil {
		return fmt.Errorf("pushing to ingest queue: %w", err)
	}
	return nil
}

// Dequeue blocks for up to 5 seconds waiting for the next span batch.
// Returns nil, nil when the timeout elapses with no entry available.
func (q *IngestQueue) Dequeue(ctx context.Context) ([]*domain.Span, error) {
	results, err := q.client.BRPop(ctx, 5*time.Second, queue.KeyIngestSpans).Result()
	if err != nil {
		if err == redis.Nil {
			return nil, nil
		}
		return nil, fmt.Errorf("blocking pop from ingest queue: %w", err)
	}
	// results[0] is the key, results[1] is the value
	var spans []*domain.Span
	if err := json.Unmarshal([]byte(results[1]), &spans); err != nil {
		return nil, fmt.Errorf("unmarshalling spans: %w", err)
	}
	return spans, nil
}

// EvalQueue is the Redis-backed implementation of queue.EvalQueue.
// Pushes and pops JSON-encoded EvalJob entries on KeyEvalJobs.
type EvalQueue struct {
	client redisClient
}

func NewEvalQueue(client redisClient) *EvalQueue {
	return &EvalQueue{client: client}
}

func (q *EvalQueue) Enqueue(ctx context.Context, job *domain.EvalJob) error {
	data, err := json.Marshal(job)
	if err != nil {
		return fmt.Errorf("marshalling eval job: %w", err)
	}
	if err := q.client.RPush(ctx, queue.KeyEvalJobs, data).Err(); err != nil {
		return fmt.Errorf("pushing to eval queue: %w", err)
	}
	return nil
}

// Dequeue blocks for up to 5 seconds waiting for the next eval job.
// Returns nil, nil when the timeout elapses with no entry available.
func (q *EvalQueue) Dequeue(ctx context.Context) (*domain.EvalJob, error) {
	results, err := q.client.BRPop(ctx, 5*time.Second, queue.KeyEvalJobs).Result()
	if err != nil {
		if err == redis.Nil {
			return nil, nil
		}
		return nil, fmt.Errorf("blocking pop from eval queue: %w", err)
	}
	var job domain.EvalJob
	if err := json.Unmarshal([]byte(results[1]), &job); err != nil {
		return nil, fmt.Errorf("unmarshalling eval job: %w", err)
	}
	return &job, nil
}
