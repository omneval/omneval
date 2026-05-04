package redis_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	redisv9 "github.com/redis/go-redis/v9"
	"github.com/zbloss/lantern/internal/domain"
	"github.com/zbloss/lantern/internal/queue"
	redispkg "github.com/zbloss/lantern/internal/queue/redis"
)

// fakeRedisClient is a minimal mock implementing the redisClient interface
// (the subset of go-redis Client methods used by IngestQueue and EvalQueue).
type fakeRedisClient struct {
	lists map[string][]string // key -> list of values
}

func newFakeRedisClient() *fakeRedisClient {
	return &fakeRedisClient{
		lists: make(map[string][]string),
	}
}

func (f *fakeRedisClient) RPush(_ context.Context, key string, values ...any) *redisv9.IntCmd {
	data := ""
	if len(values) > 0 {
		switch v := values[0].(type) {
		case string:
			data = v
		case []byte:
			data = string(v)
		default:
			data = fmt.Sprintf("%v", v)
		}
	}
	f.lists[key] = append(f.lists[key], data)
	cmd := redisv9.NewIntCmd(context.Background(), nil)
	cmd.SetVal(1)
	return cmd
}

func (f *fakeRedisClient) BRPop(_ context.Context, _ time.Duration, keys ...string) *redisv9.StringSliceCmd {
	key := keys[0]
	list, ok := f.lists[key]
	if !ok || len(list) == 0 {
		cmd := redisv9.NewStringSliceCmd(context.Background(), nil)
		cmd.SetVal(nil)
		cmd.SetErr(redisv9.Nil)
		return cmd
	}
	val := list[0]
	f.lists[key] = list[1:]
	cmd := redisv9.NewStringSliceCmd(context.Background(), nil)
	cmd.SetVal([]string{key, val})
	return cmd
}

func (f *fakeRedisClient) Ping(_ context.Context) *redisv9.StatusCmd {
	cmd := redisv9.NewStatusCmd(context.Background(), nil)
	cmd.SetVal("pong")
	return cmd
}

// --- IngestQueue tests ---

func TestIngestQueue_EnqueueAndDequeue(t *testing.T) {
	fake := newFakeRedisClient()
	q := redispkg.NewIngestQueue(fake)

	ctx := context.Background()
	spans := []*domain.Span{
		{SpanID: "span-1", TraceID: "trace-1", Name: "test"},
		{SpanID: "span-2", TraceID: "trace-1", Name: "child"},
	}

	if err := q.Enqueue(ctx, spans); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	result, err := q.Dequeue(ctx)
	if err != nil {
		t.Fatalf("dequeue: %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("dequeued spans: got %d, want 2", len(result))
	}
	if result[0].SpanID != "span-1" {
		t.Errorf("span[0].id: got %q, want %q", result[0].SpanID, "span-1")
	}
	if result[1].SpanID != "span-2" {
		t.Errorf("span[1].id: got %q, want %q", result[1].SpanID, "span-2")
	}
}

func TestIngestQueue_DequeueEmpty(t *testing.T) {
	fake := newFakeRedisClient()
	q := redispkg.NewIngestQueue(fake)

	ctx := context.Background()
	result, err := q.Dequeue(ctx)
	if err != nil {
		t.Fatalf("dequeue: unexpected error: %v", err)
	}
	if result != nil {
		t.Errorf("dequeue on empty: got %v, want nil", result)
	}
}

func TestIngestQueue_EnqueueJSONMarshalling(t *testing.T) {
	fake := newFakeRedisClient()
	q := redispkg.NewIngestQueue(fake)

	ctx := context.Background()
	spans := []*domain.Span{
		{
			SpanID:     "abcdef0123456789",
			TraceID:    "0123456789abcdef0123456789abcdef",
			Name:       "llm-call",
			Model:      "gpt-4",
			ProjectID:  "proj-1",
			InputTokens: 100,
			OutputTokens: 50,
		},
	}

	if err := q.Enqueue(ctx, spans); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	// Verify the data was pushed
	entries, ok := fake.lists[queueKey]
	if !ok {
		t.Fatal("expected list to exist")
	}
	if len(entries) != 1 {
		t.Fatalf("list entries: got %d, want 1", len(entries))
	}

	// Verify JSON can be unmarshalled back
	var result []*domain.Span
	if err := json.Unmarshal([]byte(entries[0]), &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if result[0].Model != "gpt-4" {
		t.Errorf("model: got %q, want %q", result[0].Model, "gpt-4")
	}
	if result[0].InputTokens != 100 {
		t.Errorf("input_tokens: got %d, want 100", result[0].InputTokens)
	}
}

const queueKey = queue.KeyIngestSpans

// --- EvalQueue tests ---

func TestEvalQueue_EnqueueAndDequeue(t *testing.T) {
	fake := newFakeRedisClient()
	q := redispkg.NewEvalQueue(fake)

	ctx := context.Background()
	job := &domain.EvalJob{
		JobID:   "eval-1",
		RuleID:  "rule-1",
		SpanID:  "span-1",
		TraceID: "trace-1",
		ProjectID: "proj-1",
	}

	if err := q.Enqueue(ctx, job); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	result, err := q.Dequeue(ctx)
	if err != nil {
		t.Fatalf("dequeue: %v", err)
	}
	if result.JobID != "eval-1" {
		t.Errorf("job.id: got %q, want %q", result.JobID, "eval-1")
	}
	if result.SpanID != "span-1" {
		t.Errorf("job.span_id: got %q, want %q", result.SpanID, "span-1")
	}
}

func TestEvalQueue_DequeueEmpty(t *testing.T) {
	fake := newFakeRedisClient()
	q := redispkg.NewEvalQueue(fake)

	ctx := context.Background()
	result, err := q.Dequeue(ctx)
	if err != nil {
		t.Fatalf("dequeue: unexpected error: %v", err)
	}
	if result != nil {
		t.Errorf("dequeue on empty: got %v, want nil", result)
	}
}

func TestEvalQueue_EnqueueJSONMarshalling(t *testing.T) {
	fake := newFakeRedisClient()
	q := redispkg.NewEvalQueue(fake)

	ctx := context.Background()
	job := &domain.EvalJob{
		JobID:     "eval-42",
		RuleID:    "rule-42",
		SpanID:    "span-abc",
		TraceID:   "trace-abc",
		ProjectID: "proj-1",
	}

	if err := q.Enqueue(ctx, job); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	entries, ok := fake.lists[queue.KeyEvalJobs]
	if !ok {
		t.Fatal("expected list to exist")
	}
	if len(entries) != 1 {
		t.Fatalf("list entries: got %d, want 1", len(entries))
	}

	var result domain.EvalJob
	if err := json.Unmarshal([]byte(entries[0]), &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if result.JobID != "eval-42" {
		t.Errorf("job_id: got %q, want %q", result.JobID, "eval-42")
	}
	if result.ProjectID != "proj-1" {
		t.Errorf("project_id: got %q, want %q", result.ProjectID, "proj-1")
	}
}
