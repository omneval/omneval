package pipeline

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/omneval/omneval/internal/buffer"
	"github.com/omneval/omneval/internal/domain"
	"github.com/omneval/omneval/internal/idgen"
	"github.com/omneval/omneval/internal/lakeclient"
	"github.com/omneval/omneval/internal/metadata"
	"github.com/omneval/omneval/internal/pricing"
	"github.com/omneval/omneval/internal/queue"
	"github.com/omneval/omneval/services/writer/internal/metrics"
)

// BatchProcessor owns the full ingest workflow: dequeuing, ledger checking,
// buffer fetching, batching, and committing. The Pipeline delegates to it
// via composition so the loop semantics are encapsulated in one module.
type BatchProcessor struct {
	reliable    queue.ReliableIngestQueue
	fetcher     BatchFetcher
	ledger      metadata.BatchLedgerStore
	lake        lakeclient.Client
	pricing     *pricing.Table
	evalRuleStore metadata.EvalRuleStore
	evalQ       queue.EvalQueue
	metrics     *metrics.WriterMetrics
	// writeErr, if set, causes commitLake to return this error (test only).
	writeErr error
}

// NewBatchProcessor creates a BatchProcessor with the dependencies required
// for the S3-first ingest loop (ADR-0004).
func NewBatchProcessor(
	reliable queue.ReliableIngestQueue,
	fetcher BatchFetcher,
	ledger metadata.BatchLedgerStore,
	lake lakeclient.Client,
	pricing *pricing.Table,
	evalRuleStore metadata.EvalRuleStore,
	evalQ queue.EvalQueue,
	m *metrics.WriterMetrics,
) *BatchProcessor {
	return &BatchProcessor{
		reliable:      reliable,
		fetcher:       fetcher,
		ledger:        ledger,
		lake:          lake,
		pricing:       pricing,
		evalRuleStore: evalRuleStore,
		evalQ:         evalQ,
		metrics:       m,
	}
}

// Run is the full ingest loop: it continuously dequeues entries, accumulates
// them into batch windows bounded by batchFlushInterval or batchMaxBytes,
// and commits each window to the Lake. It returns when ctx is canceled.
func (bp *BatchProcessor) Run(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		bp.collectAndCommit(ctx)
	}
}

// ProcessEntry handles one dequeued ingest entry end-to-end as a single-entry
// batch. It returns nil when the entry was fully handled (acked or requeued),
// or nil after commit. Non-nil errors surface transient failures that the
// caller may wish to retry.
func (bp *BatchProcessor) ProcessEntry(ctx context.Context, entry *queue.IngestEntry) error {
	resolved, spans, _ := bp.resolveEntry(ctx, entry)
	if resolved == nil {
		return nil
	}
	bp.commitBatch(ctx, []*queue.IngestEntry{resolved}, spans)
	return nil
}

// collectAndCommit accumulates resolved entries into a window bounded by
// batchFlushInterval and batchMaxBytes, then commits them to the Lake as a
// single batch. It returns once the window closes, including immediately
// if the queue is idle (nothing to commit).
func (bp *BatchProcessor) collectAndCommit(ctx context.Context) {
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

		entry, err := bp.reliable.DequeueEntry(ctx)
		if err != nil {
			slog.ErrorContext(ctx, "dequeue failed, continuing", "err", err)
			if bp.metrics != nil {
				bp.metrics.RecordDequeueError()
			}
			// A malformed entry can never succeed; drop it from the
			// processing list instead of letting it linger forever.
			if entry != nil && entry.Raw != "" {
				bp.ack(ctx, entry)
			}
			continue
		}
		if entry == nil {
			// Queue idle: flush whatever was accumulated so far.
			break
		}

		resolved, spans, size := bp.resolveEntry(ctx, entry)
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
	bp.commitBatch(ctx, pending, allSpans)
}

// resolveEntry resolves one dequeued ingest entry to its spans, handling the
// Batch Ledger dedupe check and Ingest Buffer fetch for reference entries.
// It returns (nil, nil, 0) when the entry was fully handled here (acked or
// requeued) and must not be included in the batch commit. The returned size
// is the JSON-encoded size of the resolved spans, used for batchMaxBytes.
func (bp *BatchProcessor) resolveEntry(ctx context.Context, entry *queue.IngestEntry) (*queue.IngestEntry, []*domain.Span, int) {
	spans := entry.Spans
	if entry.Ref != nil {
		committed, err := bp.ledger.IsBatchCommitted(ctx, entry.Ref.BatchID)
		if err != nil {
			slog.ErrorContext(ctx, "batch ledger lookup failed, requeueing",
				"batch_id", entry.Ref.BatchID, "err", err)
			bp.requeue(ctx, entry)
			return nil, nil, 0
		}
		if committed {
			// Redelivery of an already-committed batch: ack without
			// touching the Lake (zero new rows).
			if bp.metrics != nil {
				bp.metrics.RecordLedgerSkip()
			}
			bp.ack(ctx, entry)
			return nil, nil, 0
		}

		spans, err = bp.fetcher.Fetch(ctx, entry.Ref.BatchID)
		if err != nil {
			if bp.metrics != nil {
				bp.metrics.RecordBufferFetchError()
			}
			if errors.Is(err, buffer.ErrNotFound) {
				// Uncommitted batch with no buffer object: the data is
				// gone and retrying cannot recover it. Ack so the entry
				// does not poison the queue.
				slog.ErrorContext(ctx, "staged batch missing from ingest buffer, dropping",
					"batch_id", entry.Ref.BatchID, "err", err)
				bp.ack(ctx, entry)
				return nil, nil, 0
			}
			slog.ErrorContext(ctx, "ingest buffer fetch failed, requeueing",
				"batch_id", entry.Ref.BatchID, "err", err)
			bp.requeue(ctx, entry)
			return nil, nil, 0
		}
	}
	return entry, spans, estimateSize(spans)
}

// commitBatch commits the combined spans from one or more resolved entries
// to the Lake as a single snapshot, then records each entry's outcome
// (Batch Ledger insert + ack, or requeue on failure). The Lake commit is
// authoritative for every entry: a failure requeues the whole batch so it
// stays replayable (ADR-0004, Lake is the sole tier).
func (bp *BatchProcessor) commitBatch(ctx context.Context, pending []*queue.IngestEntry, spans []*domain.Span) {
	bp.computeCosts(spans)

	if err := bp.commitLake(ctx, spans); err != nil {
		slog.ErrorContext(ctx, "lake commit failed, requeueing batch",
			"span_count", len(spans), "entry_count", len(pending), "err", err)
		if bp.metrics != nil {
			bp.metrics.RecordWriteError()
		}
		for _, entry := range pending {
			bp.requeue(ctx, entry)
		}
		return
	}

	for _, entry := range pending {
		if entry.Ref != nil {
			if err := bp.ledger.MarkBatchCommitted(ctx, entry.Ref.BatchID, time.Now()); err != nil {
				// Crash window: the Lake commit stood but the ledger insert
				// failed. Requeue — redelivery re-commits and trace-detail
				// reads dedupe the residual duplicates (ADR-0004).
				slog.ErrorContext(ctx, "batch ledger insert failed, requeueing",
					"batch_id", entry.Ref.BatchID, "err", err)
				bp.requeue(ctx, entry)
				continue
			}
		}
		bp.ack(ctx, entry)
	}

	rules, err := bp.listEvalRules(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "list eval rules failed, skipping eval", "err", err)
		return
	}
	for _, span := range spans {
		bp.evalSpans(ctx, span, rules)
	}
}

func (bp *BatchProcessor) ack(ctx context.Context, entry *queue.IngestEntry) {
	if err := bp.reliable.Ack(ctx, entry); err != nil {
		slog.ErrorContext(ctx, "ack failed; entry stays on processing list", "err", err)
	}
}

func (bp *BatchProcessor) requeue(ctx context.Context, entry *queue.IngestEntry) {
	if err := bp.reliable.Requeue(ctx, entry); err != nil {
		slog.ErrorContext(ctx, "requeue failed; entry stays on processing list", "err", err)
	}
}

// computeCosts fills in span.CostUSD and defaults missing start/end times,
// in place, before the batch is committed to the Lake.
func (bp *BatchProcessor) computeCosts(spans []*domain.Span) {
	now := time.Now()
	for _, span := range spans {
		var cost float64
		if bp.pricing != nil {
			cost = bp.pricing.Cost(span.Model, span.InputTokens, span.OutputTokens)
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
// success and the failure counter on error.
func (bp *BatchProcessor) commitLake(ctx context.Context, spans []*domain.Span) error {
	if bp.writeErr != nil {
		return bp.writeErr
	}
	if len(spans) == 0 {
		return nil
	}
	if bp.lake == nil {
		return nil
	}

	start := time.Now()
	if err := bp.lake.InsertSpans(ctx, spans); err != nil {
		if bp.metrics != nil {
			bp.metrics.RecordLakeWriteError("spans")
		}
		return err
	}
	if bp.metrics != nil {
		bp.metrics.RecordLakeWriteDuration(time.Since(start).Seconds())

		projectCounts := make(map[string]int)
		for _, span := range spans {
			projectCounts[span.ProjectID]++
		}
		for projectID, count := range projectCounts {
			bp.metrics.RecordSpansWritten(projectID, count)
		}
	}
	return nil
}

// evalSpans checks each eval rule against the span and enqueues matching jobs.
func (bp *BatchProcessor) evalSpans(ctx context.Context, span *domain.Span, rules []domain.EvalRule) {
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
			if err := bp.evalQ.Enqueue(ctx, job); err != nil {
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
func (bp *BatchProcessor) listEvalRules(ctx context.Context) ([]domain.EvalRule, error) {
	if bp.evalRuleStore == nil {
		return nil, nil
	}
	raw, err := bp.evalRuleStore.ListEvalRules(ctx, "") // all projects
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