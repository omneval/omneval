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
	LPush(ctx context.Context, key string, values ...any) *redis.IntCmd
	BLPop(ctx context.Context, timeout time.Duration, keys ...string) *redis.StringSliceCmd
	Ping(ctx context.Context) *redis.StatusCmd
}

// IngestQueue is the Redis-backed implementation of queue.IngestQueue.
// Pushes and pops JSON-encoded span batches on KeyIngestSpans.
type IngestQueue struct {
	client redisClient
}

// NewIngestQueue creates a new Redis-backed IngestQueue.
func NewIngestQueue(client *redis.Client) *IngestQueue {
	return &IngestQueue{client: client}
}

// Enqueue pushes one JSON-encoded span batch to the Redis list.
func (q *IngestQueue) Enqueue(ctx context.Context, spans []*domain.Span) error {
	data, err := json.Marshal(spans)
	if err != nil {
		return fmt.Errorf("redis ingest: marshal spans: %w", err)
	}
	if err := q.client.LPush(ctx, queue.KeyIngestSpans, data).Err(); err != nil {
		return fmt.Errorf("redis ingest: rpush %s: %w", queue.KeyIngestSpans, err)
	}
	return nil
}

// Dequeue blocks for up to 5 seconds waiting for the next span batch.
// Returns nil, nil when the timeout elapses with no entry available.
func (q *IngestQueue) Dequeue(ctx context.Context) ([]*domain.Span, error) {
	result, err := q.client.BLPop(ctx, 5*time.Second, queue.KeyIngestSpans).Result()
	if err != nil {
		if err == redis.Nil {
			return nil, nil // timeout, no entry
		}
		return nil, fmt.Errorf("redis ingest: blpop %s: %w", queue.KeyIngestSpans, err)
	}
	// result[0] = key, result[1] = value
	var spans []*domain.Span
	if err := json.Unmarshal([]byte(result[1]), &spans); err != nil {
		return nil, fmt.Errorf("redis ingest: unmarshal spans: %w", err)
	}
	return spans, nil
}

// EvalQueue is the Redis-backed implementation of queue.EvalQueue.
// Pushes and pops JSON-encoded EvalJob entries on KeyEvalJobs.
type EvalQueue struct {
	client redisClient
}

// NewEvalQueue creates a new Redis-backed EvalQueue.
func NewEvalQueue(client *redis.Client) *EvalQueue {
	return &EvalQueue{client: client}
}

// Enqueue pushes one JSON-encoded EvalJob to the Redis list.
func (q *EvalQueue) Enqueue(ctx context.Context, job *domain.EvalJob) error {
	data, err := json.Marshal(job)
	if err != nil {
		return fmt.Errorf("redis eval: marshal job: %w", err)
	}
	if err := q.client.RPush(ctx, queue.KeyEvalJobs, data).Err(); err != nil {
		return fmt.Errorf("redis eval: rpush %s: %w", queue.KeyEvalJobs, err)
	}
	return nil
}

// Dequeue blocks for up to 5 seconds waiting for the next eval job.
// Returns nil, nil when the timeout elapses with no entry available.
func (q *EvalQueue) Dequeue(ctx context.Context) (*domain.EvalJob, error) {
	result, err := q.client.BLPop(ctx, 5*time.Second, queue.KeyEvalJobs).Result()
	if err != nil {
		if err == redis.Nil {
			return nil, nil // timeout, no entry
		}
		return nil, fmt.Errorf("redis eval: blpop %s: %w", queue.KeyEvalJobs, err)
	}
	var job domain.EvalJob
	if err := json.Unmarshal([]byte(result[1]), &job); err != nil {
		return nil, fmt.Errorf("redis eval: unmarshal job: %w", err)
	}
	return &job, nil
}
