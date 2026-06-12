// Package buffer implements the Ingest Buffer from ADR-0004: S3-first
// staging for raw span batches. The Ingest API writes each translated batch
// to S3 under a unique Batch ID and enqueues only the reference; writers
// fetch the batch from the buffer, commit it to the Lake, then ack. The
// buffered object is the durable copy — Redis loss is non-fatal because the
// batch survives here for replay.
package buffer

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/minio/minio-go/v7"

	"github.com/omneval/omneval/internal/domain"
	"github.com/omneval/omneval/internal/idgen"
	"github.com/omneval/omneval/internal/queue"
)

// Prefix is the S3 key prefix under which staged batches live.
const Prefix = "buffer/"

// ErrNotFound reports that no staged batch exists for the Batch ID. The
// reconciliation sweep's retention GC deletes committed objects, so a
// missing object for a committed batch is expected; a missing object for an
// uncommitted batch means the data is gone and retrying cannot help.
var ErrNotFound = errors.New("buffer: batch not found")

// ObjectStore is the slice of object storage the buffer needs.
type ObjectStore interface {
	PutSized(ctx context.Context, key string, r io.Reader, size int64) error
	Get(ctx context.Context, key string) (io.ReadCloser, error)
}

// Key returns the S3 object key for a Batch ID.
func Key(batchID string) string {
	return Prefix + batchID + ".json"
}

// BatchIDFromKey extracts the Batch ID from a buffer object key. The second
// return is false for keys outside the buffer prefix or with an unexpected
// shape.
func BatchIDFromKey(key string) (string, bool) {
	rest, ok := strings.CutPrefix(key, Prefix)
	if !ok {
		return "", false
	}
	id, ok := strings.CutSuffix(rest, ".json")
	if !ok || id == "" || strings.Contains(id, "/") {
		return "", false
	}
	return id, true
}

// Buffer stages and fetches span batches in the Ingest Buffer.
type Buffer struct {
	store ObjectStore
}

// New creates a Buffer over the given object store.
func New(store ObjectStore) *Buffer {
	return &Buffer{store: store}
}

// Stage writes the batch to the buffer under the Batch ID. The object is
// the JSON encoding of the []*domain.Span batch — the internal domain
// format, regardless of whether spans arrived via OTLP or the native REST
// API.
func (b *Buffer) Stage(ctx context.Context, batchID string, spans []*domain.Span) error {
	data, err := json.Marshal(spans)
	if err != nil {
		return fmt.Errorf("buffer: marshal batch %s: %w", batchID, err)
	}
	if err := b.store.PutSized(ctx, Key(batchID), bytes.NewReader(data), int64(len(data))); err != nil {
		return fmt.Errorf("buffer: stage batch %s: %w", batchID, err)
	}
	return nil
}

// Fetch reads the staged batch for the Batch ID. Returns ErrNotFound when
// the object does not exist.
func (b *Buffer) Fetch(ctx context.Context, batchID string) ([]*domain.Span, error) {
	rc, err := b.store.Get(ctx, Key(batchID))
	if err != nil {
		return nil, fmt.Errorf("buffer: fetch batch %s: %w", batchID, mapNotFound(err))
	}
	defer rc.Close()

	data, err := io.ReadAll(rc)
	if err != nil {
		// minio's GetObject is lazy: a missing key surfaces on first Read.
		return nil, fmt.Errorf("buffer: read batch %s: %w", batchID, mapNotFound(err))
	}

	var spans []*domain.Span
	if err := json.Unmarshal(data, &spans); err != nil {
		return nil, fmt.Errorf("buffer: unmarshal batch %s: %w", batchID, err)
	}
	return spans, nil
}

// mapNotFound converts an S3 "NoSuchKey" error into ErrNotFound so callers
// can distinguish a vanished object from a transient storage failure.
func mapNotFound(err error) error {
	if resp := minio.ToErrorResponse(err); resp.Code == "NoSuchKey" {
		return fmt.Errorf("%w (%s)", ErrNotFound, resp.Code)
	}
	return err
}

// RefEnqueuer pushes Batch ID references onto the ingest queue.
type RefEnqueuer interface {
	EnqueueRef(ctx context.Context, ref queue.BatchRef) error
}

// Metrics receives staging outcomes. Implemented by the Ingest API's
// metrics helper; nil-safe in StagedQueue.
type Metrics interface {
	RecordBatchStaged(spanCount int)
	RecordStageFailure()
}

// StagedQueue is the S3-first implementation of the Ingest API's span
// queue: Enqueue stages the batch in the Ingest Buffer and enqueues only
// the Batch ID reference. An error from either step propagates so the
// Ingest API returns 503 — mirroring the legacy Redis-down behavior.
type StagedQueue struct {
	buf     *Buffer
	refs    RefEnqueuer
	metrics Metrics
}

// NewStagedQueue creates a StagedQueue. metrics may be nil.
func NewStagedQueue(buf *Buffer, refs RefEnqueuer, metrics Metrics) *StagedQueue {
	return &StagedQueue{buf: buf, refs: refs, metrics: metrics}
}

// Enqueue stages the batch and enqueues its reference. If staging succeeds
// but the reference enqueue fails, the orphaned buffer object is recovered
// by the reconciliation sweep; the caller still sees the error (503).
func (s *StagedQueue) Enqueue(ctx context.Context, spans []*domain.Span) error {
	batchID := idgen.Generate()
	if err := s.buf.Stage(ctx, batchID, spans); err != nil {
		if s.metrics != nil {
			s.metrics.RecordStageFailure()
		}
		return err
	}
	if err := s.refs.EnqueueRef(ctx, queue.BatchRef{BatchID: batchID}); err != nil {
		return err
	}
	if s.metrics != nil {
		s.metrics.RecordBatchStaged(len(spans))
	}
	return nil
}
