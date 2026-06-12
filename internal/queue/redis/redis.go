package redis

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/omneval/omneval/internal/domain"
	"github.com/omneval/omneval/internal/queue"
	"github.com/redis/go-redis/v9"
)

// redisClient defines the subset of go-redis Client methods used by the queue
// implementations. This allows injecting a mock in tests.
type redisClient interface {
	RPush(ctx context.Context, key string, values ...any) *redis.IntCmd
	LPush(ctx context.Context, key string, values ...any) *redis.IntCmd
	BLPop(ctx context.Context, timeout time.Duration, keys ...string) *redis.StringSliceCmd
	BLMove(ctx context.Context, source, destination, srcpos, destpos string, timeout time.Duration) *redis.StringCmd
	LRem(ctx context.Context, key string, count int64, value any) *redis.IntCmd
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

// EnqueueRef pushes one JSON-encoded Batch ID reference to the Redis list.
// Reference entries share the list with legacy payload entries; the entry
// shape (object vs array) tells them apart on dequeue.
func (q *IngestQueue) EnqueueRef(ctx context.Context, ref queue.BatchRef) error {
	data, err := json.Marshal(ref)
	if err != nil {
		return fmt.Errorf("redis ingest: marshal batch ref: %w", err)
	}
	if err := q.client.LPush(ctx, queue.KeyIngestSpans, data).Err(); err != nil {
		return fmt.Errorf("redis ingest: lpush %s: %w", queue.KeyIngestSpans, err)
	}
	return nil
}

// DequeueEntry blocks for up to 5 seconds waiting for the next ingest
// entry, moving it atomically onto the processing list so a writer crash
// never silently drops it. Returns nil, nil when the timeout elapses.
func (q *IngestQueue) DequeueEntry(ctx context.Context) (*queue.IngestEntry, error) {
	raw, err := q.client.BLMove(ctx, queue.KeyIngestSpans, queue.KeyIngestProcessing,
		"RIGHT", "LEFT", 5*time.Second).Result()
	if err != nil {
		if err == redis.Nil {
			return nil, nil // timeout, no entry
		}
		return nil, fmt.Errorf("redis ingest: blmove %s: %w", queue.KeyIngestSpans, err)
	}

	entry := &queue.IngestEntry{Raw: raw}
	trimmed := strings.TrimSpace(raw)
	switch {
	case strings.HasPrefix(trimmed, "{"):
		var ref queue.BatchRef
		if err := json.Unmarshal([]byte(trimmed), &ref); err != nil || ref.BatchID == "" {
			return entry, fmt.Errorf("redis ingest: unmarshal batch ref: %w", errOrMissingID(err))
		}
		entry.Ref = &ref
	default:
		var spans []*domain.Span
		if err := json.Unmarshal([]byte(trimmed), &spans); err != nil {
			return entry, fmt.Errorf("redis ingest: unmarshal spans: %w", err)
		}
		entry.Spans = spans
	}
	return entry, nil
}

// Ack removes a processed entry from the processing list. Call only after
// the batch is durably committed (Lake write + Batch Ledger insert).
func (q *IngestQueue) Ack(ctx context.Context, entry *queue.IngestEntry) error {
	if err := q.client.LRem(ctx, queue.KeyIngestProcessing, 1, entry.Raw).Err(); err != nil {
		return fmt.Errorf("redis ingest: ack lrem: %w", err)
	}
	return nil
}

// Requeue returns a failed entry to the ingest queue for another attempt
// and drops it from the processing list. The push happens first so a crash
// between the two operations duplicates the entry rather than losing it —
// redelivery is idempotent via the Batch Ledger.
func (q *IngestQueue) Requeue(ctx context.Context, entry *queue.IngestEntry) error {
	if err := q.client.LPush(ctx, queue.KeyIngestSpans, entry.Raw).Err(); err != nil {
		return fmt.Errorf("redis ingest: requeue lpush: %w", err)
	}
	if err := q.client.LRem(ctx, queue.KeyIngestProcessing, 1, entry.Raw).Err(); err != nil {
		return fmt.Errorf("redis ingest: requeue lrem: %w", err)
	}
	return nil
}

// errOrMissingID normalizes the two batch-ref parse failures (JSON error or
// an object without batch_id) into one error value.
func errOrMissingID(err error) error {
	if err != nil {
		return err
	}
	return fmt.Errorf("missing batch_id")
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
