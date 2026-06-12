package buffer

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/omneval/omneval/internal/domain"
	"github.com/omneval/omneval/internal/queue"
)

// fakeObjectStore is an in-memory ObjectStore.
type fakeObjectStore struct {
	objects map[string][]byte
	putErr  error
	getErr  error
}

func newFakeObjectStore() *fakeObjectStore {
	return &fakeObjectStore{objects: make(map[string][]byte)}
}

func (f *fakeObjectStore) PutSized(_ context.Context, key string, r io.Reader, _ int64) error {
	if f.putErr != nil {
		return f.putErr
	}
	data, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	f.objects[key] = data
	return nil
}

func (f *fakeObjectStore) Get(_ context.Context, key string) (io.ReadCloser, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	data, ok := f.objects[key]
	if !ok {
		return nil, fmt.Errorf("get %s: %w", key, ErrNotFound)
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

func TestKeyRoundTrip(t *testing.T) {
	key := Key("abc123")
	if key != "buffer/abc123.json" {
		t.Fatalf("Key: got %q", key)
	}
	id, ok := BatchIDFromKey(key)
	if !ok || id != "abc123" {
		t.Fatalf("BatchIDFromKey: got %q, %v", id, ok)
	}
	if _, ok := BatchIDFromKey("archive/foo.json"); ok {
		t.Error("BatchIDFromKey accepted a non-buffer key")
	}
	if _, ok := BatchIDFromKey("buffer/nested/x.json"); ok {
		t.Error("BatchIDFromKey accepted a nested key")
	}
}

func TestStageFetchRoundTrip(t *testing.T) {
	ctx := context.Background()
	store := newFakeObjectStore()
	b := New(store)

	spans := []*domain.Span{
		{SpanID: "s1", TraceID: "t1", ProjectID: "p1", Name: "op", StartTime: time.Now().UTC()},
		{SpanID: "s2", TraceID: "t1", ProjectID: "p1", Name: "op2"},
	}
	if err := b.Stage(ctx, "batch-1", spans); err != nil {
		t.Fatalf("Stage: %v", err)
	}

	got, err := b.Fetch(ctx, "batch-1")
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if len(got) != 2 || got[0].SpanID != "s1" || got[1].SpanID != "s2" {
		t.Fatalf("Fetch returned wrong batch: %+v", got)
	}
}

func TestFetchNotFound(t *testing.T) {
	b := New(newFakeObjectStore())
	_, err := b.Fetch(context.Background(), "missing")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

// fakeRefEnqueuer captures enqueued references.
type fakeRefEnqueuer struct {
	refs []queue.BatchRef
	err  error
}

func (f *fakeRefEnqueuer) EnqueueRef(_ context.Context, ref queue.BatchRef) error {
	if f.err != nil {
		return f.err
	}
	f.refs = append(f.refs, ref)
	return nil
}

func TestStagedQueue_StagesAndEnqueuesReference(t *testing.T) {
	ctx := context.Background()
	store := newFakeObjectStore()
	refs := &fakeRefEnqueuer{}
	q := NewStagedQueue(New(store), refs, nil)

	spans := []*domain.Span{{SpanID: "s1", TraceID: "t1", ProjectID: "p1"}}
	if err := q.Enqueue(ctx, spans); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	if len(refs.refs) != 1 {
		t.Fatalf("expected 1 reference enqueued, got %d", len(refs.refs))
	}
	// The queue entry carries only the reference; the payload lives in the
	// buffer under the same Batch ID.
	if _, ok := store.objects[Key(refs.refs[0].BatchID)]; !ok {
		t.Fatalf("staged object missing for batch %s", refs.refs[0].BatchID)
	}
}

func TestStagedQueue_StageFailurePropagates(t *testing.T) {
	store := newFakeObjectStore()
	store.putErr = errors.New("s3 down")
	refs := &fakeRefEnqueuer{}
	q := NewStagedQueue(New(store), refs, nil)

	err := q.Enqueue(context.Background(), []*domain.Span{{SpanID: "s1"}})
	if err == nil {
		t.Fatal("expected error when buffer is unreachable")
	}
	if len(refs.refs) != 0 {
		t.Fatal("no reference must be enqueued when staging fails")
	}
}

func TestStagedQueue_EnqueueRefFailurePropagates(t *testing.T) {
	store := newFakeObjectStore()
	refs := &fakeRefEnqueuer{err: errors.New("redis down")}
	q := NewStagedQueue(New(store), refs, nil)

	err := q.Enqueue(context.Background(), []*domain.Span{{SpanID: "s1"}})
	if err == nil {
		t.Fatal("expected error when reference enqueue fails")
	}
	// The orphaned staged object remains for the reconciliation sweep.
	if len(store.objects) != 1 {
		t.Fatalf("staged object should survive enqueue failure, got %d objects", len(store.objects))
	}
}
