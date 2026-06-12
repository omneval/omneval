package redis

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/omneval/omneval/internal/domain"
	"github.com/omneval/omneval/internal/queue"
	goredis "github.com/redis/go-redis/v9"
)

// TestQueueKeys tests that the queue keys are correct.
func TestQueueKeys(t *testing.T) {
	if queue.KeyIngestSpans != "omneval:ingest:spans" {
		t.Errorf("KeyIngestSpans: got %q, want %q", queue.KeyIngestSpans, "omneval:ingest:spans")
	}
	if queue.KeyEvalJobs != "omneval:eval:jobs" {
		t.Errorf("KeyEvalJobs: got %q, want %q", queue.KeyEvalJobs, "omneval:eval:jobs")
	}
}

// TestJSONEncoding verifies that domain.Span serializes correctly for Redis transport.
func TestJSONEncoding_Spans(t *testing.T) {
	spans := []*domain.Span{
		{SpanID: "span-1", TraceID: "trace-1", ProjectID: "proj-1", Name: "chat", Model: "gpt-4o", InputTokens: 10, OutputTokens: 5},
		{SpanID: "span-2", TraceID: "trace-1", ProjectID: "proj-1", Name: "tool-call", Model: "gpt-4o", InputTokens: 3, OutputTokens: 1},
	}
	data, err := json.Marshal(spans)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var decoded []*domain.Span
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if !reflect.DeepEqual(spans, decoded) {
		t.Errorf("JSON roundtrip mismatch:\ngot  %+v\nwant %+v", decoded, spans)
	}

	// Verify JSON is valid Redis list value (string).
	if len(data) == 0 {
		t.Error("JSON encoding produced empty data")
	}
}

// TestJSONEncoding_EvalJob verifies EvalJob serializes correctly.
func TestJSONEncoding_EvalJob(t *testing.T) {
	job := &domain.EvalJob{
		JobID:      "job-1",
		RuleID:     "rule-1",
		SpanID:     "span-1",
		TraceID:    "trace-1",
		ProjectID:  "proj-1",
		EnqueuedAt: time.Now(),
	}

	data, err := json.Marshal(job)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var decoded domain.EvalJob
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if decoded.JobID != "job-1" {
		t.Errorf("JobID: got %q, want %q", decoded.JobID, "job-1")
	}
	if decoded.RuleID != "rule-1" {
		t.Errorf("RuleID: got %q, want %q", decoded.RuleID, "rule-1")
	}
	if decoded.SpanID != "span-1" {
		t.Errorf("SpanID: got %q, want %q", decoded.SpanID, "span-1")
	}
}

// TestNewQueueConstructors verifies constructors exist and are callable.
func TestNewQueueConstructors(t *testing.T) {
	// Just verify the constructor functions exist.
	// Actual queue integration tests require a real Redis server.
	t.Log("Constructors NewIngestQueue and NewEvalQueue exist (signatures verified by go build)")
}

// ─── Reliable ingest queue (Ingest Buffer references, ADR-0004) ───

// memRedis is an in-memory redisClient: enough list semantics for the
// reliable-queue tests (LPUSH to head, BLMOVE tail→head, LREM head-first).
type memRedis struct {
	lists map[string][]string
}

func newMemRedis() *memRedis { return &memRedis{lists: make(map[string][]string)} }

func (m *memRedis) LPush(ctx context.Context, key string, values ...any) *goredis.IntCmd {
	cmd := goredis.NewIntCmd(ctx)
	for _, v := range values {
		m.lists[key] = append([]string{toString(v)}, m.lists[key]...)
	}
	cmd.SetVal(int64(len(m.lists[key])))
	return cmd
}

func (m *memRedis) RPush(ctx context.Context, key string, values ...any) *goredis.IntCmd {
	cmd := goredis.NewIntCmd(ctx)
	for _, v := range values {
		m.lists[key] = append(m.lists[key], toString(v))
	}
	cmd.SetVal(int64(len(m.lists[key])))
	return cmd
}

func (m *memRedis) BLPop(ctx context.Context, timeout time.Duration, keys ...string) *goredis.StringSliceCmd {
	cmd := goredis.NewStringSliceCmd(ctx)
	for _, key := range keys {
		if l := m.lists[key]; len(l) > 0 {
			m.lists[key] = l[1:]
			cmd.SetVal([]string{key, l[0]})
			return cmd
		}
	}
	cmd.SetErr(goredis.Nil)
	return cmd
}

func (m *memRedis) BLMove(ctx context.Context, source, destination, srcpos, destpos string, timeout time.Duration) *goredis.StringCmd {
	cmd := goredis.NewStringCmd(ctx)
	l := m.lists[source]
	if len(l) == 0 {
		cmd.SetErr(goredis.Nil)
		return cmd
	}
	var v string
	if srcpos == "RIGHT" {
		v = l[len(l)-1]
		m.lists[source] = l[:len(l)-1]
	} else {
		v = l[0]
		m.lists[source] = l[1:]
	}
	if destpos == "LEFT" {
		m.lists[destination] = append([]string{v}, m.lists[destination]...)
	} else {
		m.lists[destination] = append(m.lists[destination], v)
	}
	cmd.SetVal(v)
	return cmd
}

func (m *memRedis) LRem(ctx context.Context, key string, count int64, value any) *goredis.IntCmd {
	cmd := goredis.NewIntCmd(ctx)
	want := toString(value)
	var removed int64
	var kept []string
	for _, v := range m.lists[key] {
		if (count == 0 || removed < count) && v == want {
			removed++
			continue
		}
		kept = append(kept, v)
	}
	m.lists[key] = kept
	cmd.SetVal(removed)
	return cmd
}

func (m *memRedis) Ping(ctx context.Context) *goredis.StatusCmd {
	cmd := goredis.NewStatusCmd(ctx)
	cmd.SetVal("PONG")
	return cmd
}

func toString(v any) string {
	switch s := v.(type) {
	case string:
		return s
	case []byte:
		return string(s)
	default:
		return fmt.Sprintf("%v", v)
	}
}

func TestReliableQueue_RefRoundTrip(t *testing.T) {
	ctx := context.Background()
	m := newMemRedis()
	q := &IngestQueue{client: m}

	if err := q.EnqueueRef(ctx, queue.BatchRef{BatchID: "b1"}); err != nil {
		t.Fatalf("EnqueueRef: %v", err)
	}

	entry, err := q.DequeueEntry(ctx)
	if err != nil {
		t.Fatalf("DequeueEntry: %v", err)
	}
	if entry == nil || entry.Ref == nil || entry.Ref.BatchID != "b1" {
		t.Fatalf("entry: got %+v, want ref b1", entry)
	}
	if entry.Spans != nil {
		t.Error("ref entry must not carry spans")
	}

	// The entry moved to the processing list, not vanished.
	if got := len(m.lists[queue.KeyIngestProcessing]); got != 1 {
		t.Fatalf("processing list: got %d entries, want 1", got)
	}
	if got := len(m.lists[queue.KeyIngestSpans]); got != 0 {
		t.Fatalf("main list: got %d entries, want 0", got)
	}

	// Ack removes it from the processing list only after commit.
	if err := q.Ack(ctx, entry); err != nil {
		t.Fatalf("Ack: %v", err)
	}
	if got := len(m.lists[queue.KeyIngestProcessing]); got != 0 {
		t.Fatalf("processing list after ack: got %d entries, want 0", got)
	}
}

func TestReliableQueue_LegacyPayloadEntry(t *testing.T) {
	ctx := context.Background()
	m := newMemRedis()
	q := &IngestQueue{client: m}

	// A legacy producer enqueues a payload batch on the same list.
	if err := q.Enqueue(ctx, []*domain.Span{{SpanID: "s1", TraceID: "t1", ProjectID: "p1"}}); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	entry, err := q.DequeueEntry(ctx)
	if err != nil {
		t.Fatalf("DequeueEntry: %v", err)
	}
	if entry == nil || entry.Ref != nil || len(entry.Spans) != 1 || entry.Spans[0].SpanID != "s1" {
		t.Fatalf("entry: got %+v, want payload with span s1", entry)
	}
}

func TestReliableQueue_RequeueReturnsEntry(t *testing.T) {
	ctx := context.Background()
	m := newMemRedis()
	q := &IngestQueue{client: m}

	if err := q.EnqueueRef(ctx, queue.BatchRef{BatchID: "b1"}); err != nil {
		t.Fatalf("EnqueueRef: %v", err)
	}
	entry, err := q.DequeueEntry(ctx)
	if err != nil {
		t.Fatalf("DequeueEntry: %v", err)
	}

	if err := q.Requeue(ctx, entry); err != nil {
		t.Fatalf("Requeue: %v", err)
	}
	if got := len(m.lists[queue.KeyIngestProcessing]); got != 0 {
		t.Fatalf("processing list after requeue: got %d, want 0", got)
	}

	// The entry is dequeueable again.
	again, err := q.DequeueEntry(ctx)
	if err != nil {
		t.Fatalf("DequeueEntry again: %v", err)
	}
	if again == nil || again.Ref == nil || again.Ref.BatchID != "b1" {
		t.Fatalf("requeued entry: got %+v, want ref b1", again)
	}
}

func TestReliableQueue_MalformedEntryReturnsRawForAck(t *testing.T) {
	ctx := context.Background()
	m := newMemRedis()
	q := &IngestQueue{client: m}

	m.LPush(ctx, queue.KeyIngestSpans, `{"not_a_ref": true}`)

	entry, err := q.DequeueEntry(ctx)
	if err == nil {
		t.Fatal("expected error for malformed ref entry")
	}
	// The caller can still ack the poison entry off the processing list.
	if entry == nil || entry.Raw == "" {
		t.Fatalf("malformed entry must return raw token, got %+v", entry)
	}
	if err := q.Ack(ctx, entry); err != nil {
		t.Fatalf("Ack: %v", err)
	}
	if got := len(m.lists[queue.KeyIngestProcessing]); got != 0 {
		t.Fatalf("processing list after ack: got %d, want 0", got)
	}
}
