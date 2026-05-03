package queue

import (
	"context"

	"github.com/zbloss/lantern/internal/domain"
)

// IngestQueue is the durability buffer between the Ingest API and the Writer.
type IngestQueue interface {
	Enqueue(ctx context.Context, spans []*domain.Span) error
	Dequeue(ctx context.Context, maxCount int64) ([]*domain.Span, error)
}

// EvalQueue carries eval jobs from the Writer to the Eval Workers.
type EvalQueue interface {
	Enqueue(ctx context.Context, jobs []*domain.EvalJob) error
	Dequeue(ctx context.Context, maxCount int64) ([]*domain.EvalJob, error)
}
