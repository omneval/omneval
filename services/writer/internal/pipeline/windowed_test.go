package pipeline

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/omneval/omneval/internal/domain"
	"github.com/omneval/omneval/internal/queue"
)

// recordingLake is an in-memory lakeclient.Client that records each
// InsertSpans call, for asserting on batching behavior.
type recordingLake struct {
	mu        sync.Mutex
	calls     [][]*domain.Span
	callTotal int
	err       error
}

func (l *recordingLake) InsertSpans(_ context.Context, spans []*domain.Span) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.callTotal++
	if l.err != nil {
		return l.err
	}
	l.calls = append(l.calls, spans)
	return nil
}

func (l *recordingLake) callCount() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.callTotal
}

func (l *recordingLake) totalSpans() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	n := 0
	for _, c := range l.calls {
		n += len(c)
	}
	return n
}

// sequenceReliableQueue serves a fixed sequence of entries from
// DequeueEntry, then returns nil, nil (idle) forever after.
type sequenceReliableQueue struct {
	mu       sync.Mutex
	entries  []*queue.IngestEntry
	acked    []*queue.IngestEntry
	requeued []*queue.IngestEntry
}

func (q *sequenceReliableQueue) EnqueueRef(context.Context, queue.BatchRef) error { return nil }

func (q *sequenceReliableQueue) DequeueEntry(context.Context) (*queue.IngestEntry, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.entries) == 0 {
		return nil, nil
	}
	e := q.entries[0]
	q.entries = q.entries[1:]
	return e, nil
}

func (q *sequenceReliableQueue) Ack(_ context.Context, e *queue.IngestEntry) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.acked = append(q.acked, e)
	return nil
}

func (q *sequenceReliableQueue) Requeue(_ context.Context, e *queue.IngestEntry) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.requeued = append(q.requeued, e)
	return nil
}

func windowedTestPipeline(rq *sequenceReliableQueue, lk lakeclient.Client) *Pipeline {
	return New(nil, testPricing, nil, newFakeLedger(), nil, nil).
		WithLake(lk).
		WithBuffer(rq, &fakeFetcher{batches: make(map[string][]*domain.Span)}, newFakeLedger())
}

// TestCollectAndCommit_BatchesMultipleEntries proves that several
// payload entries dequeued within the same window are committed to the
// Lake in a single InsertSpans call.
func TestCollectAndCommit_BatchesMultipleEntries(t *testing.T) {
	ctx := context.Background()
	lk := &recordingLake{}
	rq := &sequenceReliableQueue{entries: []*queue.IngestEntry{
		{Spans: []*domain.Span{bufferedTestSpan("s1")}, Raw: "raw-1"},
		{Spans: []*domain.Span{bufferedTestSpan("s2")}, Raw: "raw-2"},
		{Spans: []*domain.Span{bufferedTestSpan("s3")}, Raw: "raw-3"},
	}}
	p := windowedTestPipeline(rq, lk)

	p.collectAndCommit(ctx)

	if got := lk.callCount(); got != 1 {
		t.Errorf("InsertSpans calls: got %d, want 1", got)
	}
	if got := lk.totalSpans(); got != 3 {
		t.Errorf("total spans committed: got %d, want 3", got)
	}
	if len(rq.acked) != 3 {
		t.Errorf("acked entries: got %d, want 3", len(rq.acked))
	}
	if len(rq.requeued) != 0 {
		t.Errorf("requeued entries: got %d, want 0", len(rq.requeued))
	}
}

// TestCollectAndCommit_LakeFailureRequeuesWholeBatch proves that a Lake
// commit failure requeues every entry accumulated in the window, not just
// one.
func TestCollectAndCommit_LakeFailureRequeuesWholeBatch(t *testing.T) {
	ctx := context.Background()
	lk := &recordingLake{err: errors.New("lake unavailable")}
	rq := &sequenceReliableQueue{entries: []*queue.IngestEntry{
		{Spans: []*domain.Span{bufferedTestSpan("s1")}, Raw: "raw-1"},
		{Spans: []*domain.Span{bufferedTestSpan("s2")}, Raw: "raw-2"},
	}}
	p := windowedTestPipeline(rq, lk)

	p.collectAndCommit(ctx)

	if got := lk.callCount(); got != 1 {
		t.Errorf("InsertSpans calls: got %d, want 1", got)
	}
	if len(rq.requeued) != 2 {
		t.Errorf("requeued entries: got %d, want 2", len(rq.requeued))
	}
	if len(rq.acked) != 0 {
		t.Errorf("acked entries: got %d, want 0", len(rq.acked))
	}
}

// TestCollectAndCommit_IdleQueueReturnsImmediately proves that an idle
// queue (DequeueEntry returns nil, nil) does not block commitBatch on an
// empty batch.
func TestCollectAndCommit_IdleQueueReturnsImmediately(t *testing.T) {
	ctx := context.Background()
	lk := &recordingLake{}
	rq := &sequenceReliableQueue{}
	p := windowedTestPipeline(rq, lk)

	done := make(chan struct{})
	go func() {
		p.collectAndCommit(ctx)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("collectAndCommit blocked on an idle queue")
	}

	if got := lk.callCount(); got != 0 {
		t.Errorf("InsertSpans calls: got %d, want 0", got)
	}
}

// TestCollectAndCommit_MaxBytesStopsWindowEarly proves that an entry whose
// size already exceeds batchMaxBytes closes the window before a
// subsequent entry is pulled into the same commit.
func TestCollectAndCommit_MaxBytesStopsWindowEarly(t *testing.T) {
	ctx := context.Background()
	lk := &recordingLake{}

	big := bufferedTestSpan("big")
	big.Input = strings.Repeat("x", batchMaxBytes+1)

	rq := &sequenceReliableQueue{entries: []*queue.IngestEntry{
		{Spans: []*domain.Span{big}, Raw: "raw-big"},
		{Spans: []*domain.Span{bufferedTestSpan("s2")}, Raw: "raw-2"},
	}}
	p := windowedTestPipeline(rq, lk)

	p.collectAndCommit(ctx)

	if got := lk.callCount(); got != 1 {
		t.Fatalf("InsertSpans calls: got %d, want 1", got)
	}
	if got := len(lk.calls[0]); got != 1 {
		t.Errorf("first commit span count: got %d, want 1", got)
	}
	if len(rq.acked) != 1 {
		t.Errorf("acked entries: got %d, want 1", len(rq.acked))
	}

	// The second entry remains queued for the next window.
	rq.mu.Lock()
	remaining := len(rq.entries)
	rq.mu.Unlock()
	if remaining != 1 {
		t.Errorf("remaining entries: got %d, want 1", remaining)
	}
}
