package pipeline

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"time"

	"github.com/omneval/omneval/internal/buffer"
	"github.com/omneval/omneval/internal/domain"
	"github.com/omneval/omneval/internal/idgen"
	"github.com/omneval/omneval/internal/metadata"
	"github.com/omneval/omneval/internal/pricing"
	"github.com/omneval/omneval/internal/queue"
	"github.com/omneval/omneval/services/writer/internal/metrics"
)

const (
	// batchFlushInterval and batchMaxBytes bound how long the buffered loop
	// (runBuffered) accumulates dequeued entries before committing them to
	// the Lake as a single DuckLake snapshot — the commit cadence described
	// in ADR-0004 / CONTEXT.md.
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
// batches to the Lake (ADR-0004) — the sole storage tier.
type Pipeline struct {
	ingest       queue.IngestQueue
	pricing      *pricing.Table
	evalRuleStore metadata.EvalRuleStore
	batchLedger   BatchLedger
	evalQ        queue.EvalQueue
	metrics      *metrics.WriterMetrics
	// lake is the Lake write path. Required when writer.lake.enabled
	// (the default); a nil lake is a misconfiguration and Run returns an
	// error at startup.
	lake SpanLakeWriter
	// reliable + fetcher + ledger, when set via WithBuffer, switch Run to
	// the S3-first loop: dequeue references, fetch from the Ingest Buffer,
	// skip ledgered batches, ack only after commit + ledger insert.
	reliable queue.ReliableIngestQueue
	fetcher  BatchFetcher
	ledger   BatchLedger
	// writeErr, if set, causes commitLake to return this error (test only).
	writeErr error
}

// New creates a new Pipeline. lake is the Lake write path (ADR-0004); it is
// required for Run to operate (a nil lake causes Run to return an error).
func New(
	ingest queue.IngestQueue,
	pricing *pricing.Table,
	evalRuleStore metadata.EvalRuleStore,
	batchLedger BatchLedger,
	evalQ queue.EvalQueue,
	m *metrics.WriterMetrics,
) *Pipeline {
	return &Pipeline{
		ingest:       ingest,
		pricing:      pricing,
		evalRuleStore: evalRuleStore,
		batchLedger:  batchLedger,
		evalQ:        evalQ,
		metrics:      m,
	}
}

// WithLake sets the Lake write path. Returns the pipeline for chaining at
// wiring time.
func (p *Pipeline) WithLake(l SpanLakeWriter) *Pipeline {
	p.lake = l
	return p
}

// WithBuffer switches the pipeline to the S3-first ingest flow (ADR-0004):
// entries are dequeued with explicit acknowledgement, Batch ID references
// are resolved through the Ingest Buffer, and the Batch Ledger makes
// redelivery idempotent. Returns the pipeline for chaining at wiring time.
func (p *Pipeline) WithBuffer(rq queue.ReliableIngestQueue, fetcher BatchFetcher, ledger BatchLedger) *Pipeline {
	p.reliable = rq
	p.fetcher = fetcher
	p.ledger = ledger
	return p
}

// Run blocks until ctx is canceled. It continuously dequeues spans from Redis,
// computes cost, commits the batch to the Lake, matches eval rules, and
// enqueues eval jobs. On non-fatal errors (dequeue, write, eval rule
// listing), the pipeline logs the error and continues processing subsequent
// batches instead of crashing. Requires p.lake to be non-nil; returns an
// error immediately if it is not.
func (p *Pipeline) Run(ctx context.Context) error {
	if p.lake == nil {
		return fmt.Errorf("pipeline: lake is required (writer.lake.enabled)")
	}
	if p.reliable != nil {
		return p.runBuffered(ctx)
	}
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

// runBuffered is the S3-first ingest loop (ADR-0004). Entries are dequeued
// onto a processing list and acked only after the batch is durably
// committed; references are resolved through the Ingest Buffer and deduped
// via the Batch Ledger. Entries are accumulated into a window (up to
// batchFlushInterval or batchMaxBytes) and committed to the Lake together as
// a single DuckLake snapshot.
func (p *Pipeline) runBuffered(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		p.collectAndCommit(ctx)
	}
}

// collectAndCommit accumulates resolved entries into a window bounded by
// batchFlushInterval and batchMaxBytes, then commits them to the Lake as a
// single batch. It returns once the window closes, including immediately
// if the queue is idle (nothing to commit).
func (p *Pipeline) collectAndCommit(ctx context.Context) {
	var pending []*queue.IngestEntry
	var allSpans []*domain.Span
	var totalBytes int
	var deadline time.Time

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		if len(pending) > 0 && (totalBytes >= batchMaxBytes || time.Now().After(deadline)) {
			break
		}

		entry, err := p.reliable.DequeueEntry(ctx)
		if err != nil {
			slog.ErrorContext(ctx, "dequeue failed, continuing", "err", err)
			if p.metrics != nil {
				p.metrics.RecordDequeueError()
			}
			// A malformed entry can never succeed; drop it from the
			// processing list instead of letting it linger forever.
			if entry != nil && entry.Raw != "" {
				_ = p.reliable.Ack(ctx, entry)
			}
			continue
		}
		if entry == nil {
			// Queue idle: flush whatever was accumulated so far.
			break
		}

		resolved, spans, size := p.resolveEntry(ctx, entry)
		if resolved == nil {
			continue
		}
		if len(pending) == 0 {
			deadline = time.Now().Add(batchFlushInterval)
		}
		pending = append(pending, resolved)
		allSpans = append(allSpans, spans...)
		totalBytes += size
	}

	if len(pending) == 0 {
		return
	}
	p.commitBatch(ctx, pending, allSpans)
}

// resolveEntry resolves one dequeued ingest entry to its spans, handling the
// Batch Ledger dedupe check and Ingest Buffer fetch for reference entries.
// It returns (nil, nil, 0) when the entry was fully handled here (acked or
// requeued) and must not be included in the batch commit. The returned size
// is the JSON-encoded size of the resolved spans, used for batchMaxBytes.
func (p *Pipeline) resolveEntry(ctx context.Context, entry *queue.IngestEntry) (*queue.IngestEntry, []*domain.Span, int) {
	spans := entry.Spans
	if entry.Ref != nil {
		committed, err := p.ledger.IsBatchCommitted(ctx, entry.Ref.BatchID)
		if err != nil {
			slog.ErrorContext(ctx, "batch ledger lookup failed, requeueing",
				"batch_id", entry.Ref.BatchID, "err", err)
			p.requeue(ctx, entry)
			return nil, nil, 0
		}
		if committed {
			// Redelivery of an already-committed batch: ack without
			// touching the Lake (zero new rows).
			if p.metrics != nil {
				p.metrics.RecordLedgerSkip()
			}
			p.ack(ctx, entry)
			return nil, nil, 0
		}

		spans, err = p.fetcher.Fetch(ctx, entry.Ref.BatchID)
		if err != nil {
			if p.metrics != nil {
				p.metrics.RecordBufferFetchError()
			}
			if errors.Is(err, buffer.ErrNotFound) {
				// Uncommitted batch with no buffer object: the data is
				// gone and retrying cannot recover it. Ack so the entry
				// does not poison the queue.
				slog.ErrorContext(ctx, "staged batch missing from ingest buffer, dropping",
					"batch_id", entry.Ref.BatchID, "err", err)
				p.ack(ctx, entry)
				return nil, nil, 0
			}
			slog.ErrorContext(ctx, "ingest buffer fetch failed, requeueing",
				"batch_id", entry.Ref.BatchID, "err", err)
			p.requeue(ctx, entry)
			return nil, nil, 0
		}
	}
	return entry, spans, estimateSize(spans)
}

// estimateSize returns the JSON-encoded size of spans, used to bound batch
// windows by batchMaxBytes. Returns 0 if spans cannot be marshaled (the
// later Lake commit will surface the real error).
func estimateSize(spans []*domain.Span) int {
	data, err := json.Marshal(spans)
	if err != nil {
		return 0
	}
	return len(data)
}

// commitBatch commits the combined spans from one or more resolved entries
// to the Lake as a single snapshot, then records each entry's outcome
// (Batch Ledger insert + ack, or requeue on failure). The Lake commit is
// authoritative for every entry: a failure requeues the whole batch so it
// stays replayable (ADR-0004, Lake is the sole tier).
func (p *Pipeline) commitBatch(ctx context.Context, pending []*queue.IngestEntry, spans []*domain.Span) {
	p.computeCosts(spans)

	if err := p.commitLake(ctx, spans); err != nil {
		slog.ErrorContext(ctx, "lake commit failed, requeueing batch",
			"span_count", len(spans), "entry_count", len(pending), "err", err)
		if p.metrics != nil {
			p.metrics.RecordWriteError()
		}
		for _, entry := range pending {
			p.requeue(ctx, entry)
		}
		return
	}

	for _, entry := range pending {
		if entry.Ref != nil {
			if err := p.ledger.MarkBatchCommitted(ctx, entry.Ref.BatchID, time.Now()); err != nil {
				// Crash window: the Lake commit stood but the ledger insert
				// failed. Requeue — redelivery re-commits and trace-detail
				// reads dedupe the residual duplicates (ADR-0004).
				slog.ErrorContext(ctx, "batch ledger insert failed, requeueing",
					"batch_id", entry.Ref.BatchID, "err", err)
				p.requeue(ctx, entry)
				continue
			}
		}
		p.ack(ctx, entry)
	}

	rules, err := p.listEvalRules(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "list eval rules failed, skipping eval", "err", err)
		return
	}
	for _, span := range spans {
		p.evalSpans(ctx, span, rules)
	}
}

// processEntry handles one dequeued ingest entry end to end as a
// single-entry batch. Used directly by tests; runBuffered uses
// collectAndCommit to accumulate multiple entries per commit.
func (p *Pipeline) processEntry(ctx context.Context, entry *queue.IngestEntry) {
	resolved, spans, _ := p.resolveEntry(ctx, entry)
	if resolved == nil {
		return
	}
	p.commitBatch(ctx, []*queue.IngestEntry{resolved}, spans)
}

func (p *Pipeline) ack(ctx context.Context, entry *queue.IngestEntry) {
	if err := p.reliable.Ack(ctx, entry); err != nil {
		slog.ErrorContext(ctx, "ack failed; entry stays on processing list", "err", err)
	}
}

func (p *Pipeline) requeue(ctx context.Context, entry *queue.IngestEntry) {
	if err := p.reliable.Requeue(ctx, entry); err != nil {
		slog.ErrorContext(ctx, "requeue failed; entry stays on processing list", "err", err)
	}
}

// computeCosts fills in span.CostUSD and defaults missing start/end times,
// in place, before the batch is committed to the Lake. Cost is pre-computed
// at write time and never recomputed at query time.
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

// commitLake commits the batch to the Lake, recording write duration on
// success and the failure counter on error. The Lake is the sole storage
// tier (ADR-0004): a failure here is fatal for the batch and the caller
// requeues/skips accordingly.
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
	raw, err := p.evalRuleStore.ListEvalRules(ctx, "") // all projects
	if err != nil {
		return nil, err
	}
	// Convert []*domain.EvalRule to []domain.EvalRule.
	result := make([]domain.EvalRule, 0, len(raw))
	for _, r := range raw {
		if r != nil {
			result = append(result, *r)
		}
	}
	return result, nil
}

// isSampled returns true when a span is selected for sampling
// based on the given sample rate (0.0–1.0). Rates >= 1.0 always return true;
// rates <= 0.0 always return false.
func isSampled(rate float64) bool {
	if rate >= 1.0 {
		return true
	}
	if rate <= 0.0 {
		return false
	}
	// Use crypto/rand for unbiased sampling.
	n, err := rand.Int(rand.Reader, big.NewInt(1000))
	if err != nil {
		return false
	}
	threshold := int64(rate * 1000)
	return n.Int64() < threshold
}
