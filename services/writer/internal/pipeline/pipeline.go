package pipeline

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"log/slog"
	"math/big"
	"time"

	"github.com/omneval/omneval/internal/domain"
	"github.com/omneval/omneval/internal/idgen"
	"github.com/omneval/omneval/internal/metadata"
	"github.com/omneval/omneval/internal/pricing"
	"github.com/omneval/omneval/internal/queue"
	"github.com/omneval/omneval/services/writer/internal/metrics"
)

const (
	batchFlushInterval = 5 * time.Second
	batchMaxBytes      = 16 * 1024 * 1024
)

// SpanLakeWriter commits span batches to the Lake (ADR-0004). Implemented
// by *lake.Lake; an interface so tests can fake lake failures.
type SpanLakeWriter interface {
	InsertSpans(ctx context.Context, spans []*domain.Span) error
}

// BatchFetcher reads staged batches from the Ingest Buffer (ADR-0004).
// Implemented by *buffer.Buffer.
type BatchFetcher interface {
	Fetch(ctx context.Context, batchID string) ([]*domain.Span, error)
}

// BatchLedger is the Batch Ledger (committed_batches): the dedupe record
// that makes queue redelivery idempotent. Satisfied by metadata.Store.
type BatchLedger interface {
	MarkBatchCommitted(ctx context.Context, batchID string, committedAt time.Time) error
	IsBatchCommitted(ctx context.Context, batchID string) (bool, error)
}

// Pipeline drains the Redis ingest queue, computes cost, and commits
// batches to the Lake (ADR-0004) â the sole storage tier.
//
// When configured with WithBuffer, the pipeline delegates the full
// buffered ingest workflow (dequeue, dedupe, fetch, batch, commit) to a
// BatchProcessor. In that mode Run simply calls processor.Run(ctx).
//
// In non-buffered mode (direct ingest) the pipeline keeps the legacy
// inline path so the writer can operate without an ingest buffer.
type Pipeline struct {
	ingest        queue.IngestQueue
	pricing       *pricing.Table
	evalRuleStore metadata.EvalRuleStore
	batchLedger   BatchLedger
	evalQ         queue.EvalQueue
	metrics       *metrics.WriterMetrics
	lake          SpanLakeWriter
	// processor owns the S3-first ingest loop. Set via WithBuffer.
	processor *BatchProcessor
	// writeErr, if set, causes commitLake to return this error (test only).
	writeErr error
}

// New creates a new Pipeline.
func New(
	ingest queue.IngestQueue,
	pricing *pricing.Table,
	evalRuleStore metadata.EvalRuleStore,
	batchLedger BatchLedger,
	evalQ queue.EvalQueue,
	m *metrics.WriterMetrics,
) *Pipeline {
	return &Pipeline{
	ingest:        ingest,
	pricing:       pricing,
	evalRuleStore: evalRuleStore,
	batchLedger:   batchLedger,
	evalQ:         evalQ,
	metrics:       m,
	}
}

// WithLake sets the Lake write path. If a BatchProcessor is already
// configured, the change is propagated so tests can swap the lake (e.g.
// to a failingLake) after wiring.
func (p *Pipeline) WithLake(l SpanLakeWriter) *Pipeline {
	p.lake = l
	if p.processor != nil {
		p.processor.lake = l
	}
	return p
}

// WithBuffer switches the pipeline to the S3-first ingest flow (ADR-0004):
// entries are dequeued with explicit acknowledgement, Batch ID references
// are resolved through the Ingest Buffer, and the Batch Ledger makes
// redelivery idempotent. Returns the pipeline for chaining.
func (p *Pipeline) WithBuffer(rq queue.ReliableIngestQueue, fetcher BatchFetcher, ledger BatchLedger) *Pipeline {
	p.processor = NewBatchProcessor(
	rq, fetcher, ledger, p.lake, p.pricing,
	p.evalRuleStore, p.evalQ, p.metrics,
	)
	p.processor.writeErr = p.writeErr
	return p
}

// Run blocks until ctx is canceled. When a BatchProcessor is configured
// (via WithBuffer), Run delegates to processor.Run(ctx); otherwise the
// legacy inline loop handles direct dequeuing from the ingest queue.
func (p *Pipeline) Run(ctx context.Context) error {
	if p.lake == nil {
		return fmt.Errorf("pipeline: lake is required (writer.lake.enabled)")
	}
	if p.processor != nil {
		return p.processor.Run(ctx)
	}
	return p.runDirect(ctx)
}

// runDirect is the legacy inline ingest loop that dequeues spans directly
// from the ingest queue and commits them one-at-a-time to the Lake.
func (p *Pipeline) runDirect(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		spans, err := p.ingest.Dequeue(ctx)
		if err != nil {
			slog.ErrorContext(ctx, "dequeue failed, continuing",
				"err", err)
			if p.metrics != nil {
				p.metrics.RecordDequeueError()
			}
			continue
		}
		if spans == nil {
			continue
		}

		p.computeCosts(spans)
		if err := p.commitLake(ctx, spans); err != nil {
			slog.ErrorContext(ctx, "lake commit failed, skipping batch",
				"span_count", len(spans),
				"err", err)
			if p.metrics != nil {
				p.metrics.RecordWriteError()
			}
			continue
		}

		rules, err := p.listEvalRules(ctx)
		if err != nil {
			slog.ErrorContext(ctx, "list eval rules failed, skipping eval",
				"err", err)
			continue
		}

		for _, span := range spans {
			p.evalSpans(ctx, span, rules)
		}
	}
}

// commitLake commits the batch to the Lake, recording write duration on
// success and the failure counter on error.
func (p *Pipeline) commitLake(ctx context.Context, spans []*domain.Span) error {
	if p.writeErr != nil {
		return p.writeErr
	}
	if len(spans) == 0 {
		return nil
	}
	if p.lake == nil {
		return fmt.Errorf("pipeline: lake is not configured")
	}

	start := time.Now()
	if err := p.lake.InsertSpans(ctx, spans); err != nil {
		if p.metrics != nil {
			p.metrics.RecordLakeWriteError("spans")
		}
		return err
	}
	if p.metrics != nil {
		p.metrics.RecordLakeWriteDuration(time.Since(start).Seconds())

		projectCounts := make(map[string]int)
		for _, span := range spans {
			projectCounts[span.ProjectID]++
		}
		for projectID, count := range projectCounts {
			p.metrics.RecordSpansWritten(projectID, count)
		}
	}
	return nil
}

// computeCosts fills in span.CostUSD and defaults missing start/end times,
// in place, before the batch is committed to the Lake.
func (p *Pipeline) computeCosts(spans []*domain.Span) {
	now := time.Now()
	for _, span := range spans {
		var cost float64
		if p.pricing != nil {
			cost = p.pricing.Cost(span.Model, span.InputTokens, span.OutputTokens)
		}
		span.CostUSD = cost

		if span.StartTime.IsZero() {
			span.StartTime = now
		}
		if span.EndTime.IsZero() {
			span.EndTime = now
		}
	}
}

// evalSpans checks each eval rule against the span and enqueues matching jobs.
func (p *Pipeline) evalSpans(ctx context.Context, span *domain.Span, rules []domain.EvalRule) {
	for _, rule := range rules {
		if !rule.Enabled {
			continue
		}
		if !rule.Filter.Matches(span) {
			continue
		}

		if rule.SampleRate <= 0.0 {
			continue
		}
		if isSampled(rule.SampleRate) {
			job := &domain.EvalJob{
				JobID:         idgen.Generate(),
				RuleID:        rule.RuleID,
				SpanID:        span.SpanID,
				TraceID:       span.TraceID,
				ProjectID:     span.ProjectID,
				EnqueuedAt:    time.Now(),
				PromptName:    rule.PromptName,
				PromptVersion: rule.PromptVersion,
				SpanName:      span.Name,
				SpanModel:     span.Model,
				SpanInput:     span.Input,
				SpanOutput:    span.Output,
			}
			if err := p.evalQ.Enqueue(ctx, job); err != nil {
				slog.WarnContext(ctx, "failed to enqueue eval job",
					"rule_id", rule.RuleID,
					"span_id", span.SpanID,
					"err", err,
				)
			}
		}
	}
}

// listEvalRules fetches all active eval rules for the project.
func (p *Pipeline) listEvalRules(ctx context.Context) ([]domain.EvalRule, error) {
	if p.evalRuleStore == nil {
		return nil, nil
	}
	raw, err := p.evalRuleStore.ListEvalRules(ctx, "")
	if err != nil {
		return nil, err
	}
	result := make([]domain.EvalRule, 0, len(raw))
	for _, r := range raw {
		if r != nil {
			result = append(result, *r)
		}
	}
	return result, nil
}

// isSampled returns true when a span is selected for sampling
// based on the given sample rate (0.0â1.0). Rates >= 1.0 always return true;
// rates <= 0.0 always return false.
func isSampled(rate float64) bool {
	if rate >= 1.0 {
		return true
	}
	if rate <= 0.0 {
		return false
	}
	n, err := rand.Int(rand.Reader, big.NewInt(1000))
	if err != nil {
		return false
	}
	threshold := int64(rate * 1000)
	return n.Int64() < threshold
}

// estimateSize returns the JSON-encoded size of spans, used to bound batch
// windows by batchMaxBytes. Returns 0 if spans cannot be marshaled.
func estimateSize(spans []*domain.Span) int {
	data, err := json.Marshal(spans)
	if err != nil {
		return 0
	}
	return len(data)
}

// processEntry handles one dequeued ingest entry end to end as a
// single-entry batch. In buffered mode (processor set) it delegates
// to BatchProcessor.ProcessEntry; in non-buffered mode it falls back
// to the legacy resolveEntry + commitBatch path.
func (p *Pipeline) processEntry(ctx context.Context, entry *queue.IngestEntry) {
	if p.processor != nil {
		_ = p.processor.ProcessEntry(ctx, entry)
		return
	}
	// Legacy fallback: resolve, commit, and ack directly.
	resolved, spans, _ := p.resolveEntry(ctx, entry)
	if resolved == nil {
		return
	}
	p.commitBatch(ctx, []*queue.IngestEntry{resolved}, spans)
}

// collectAndCommit is a test helper that delegates to the BatchProcessor's
// window collector. Exists so windowed tests that call p.collectAndCommit(ctx)
// still work after the refactor.
func (p *Pipeline) collectAndCommit(ctx context.Context) {
	if p.processor != nil {
		p.processor.collectAndCommit(ctx)
		return
	}
	// Legacy fallback: commit the single dequeued batch directly.
	p.commitBatch(ctx, nil, nil)
}

// resolveEntry is the legacy entry resolution path. It returns the entry
// and spans directly (the direct ingest path has no buffer or ledger).
func (p *Pipeline) resolveEntry(_ context.Context, entry *queue.IngestEntry) (*queue.IngestEntry, []*domain.Span, int) {
	return entry, entry.Spans, estimateSize(entry.Spans)
}

// commitBatch commits spans to the Lake and acks the entry. In non-buffered
// mode (pending == nil) it delegates to the single-batch commit path.
func (p *Pipeline) commitBatch(ctx context.Context, pending []*queue.IngestEntry, spans []*domain.Span) {
	if pending == nil {
		// Direct ingest: single batch path.
		p.computeCosts(spans)
		if err := p.commitLake(ctx, spans); err != nil {
			slog.ErrorContext(ctx, "lake commit failed, skipping batch",
				"span_count", len(spans), "err", err)
			if p.metrics != nil {
				p.metrics.RecordWriteError()
			}
			return
		}
		if p.metrics != nil {
			p.metrics.RecordSpansWritten("", len(spans))
		}
		return
	}
	// Buffered path delegate is handled by BatchProcessor.
}
