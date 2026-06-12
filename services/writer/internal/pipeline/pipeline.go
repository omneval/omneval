package pipeline

import (
	"context"
	"crypto/rand"
	"database/sql"
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

// insertSpansSQL is the INSERT OR REPLACE statement for the DuckDB spans table.
// Columns: span_id, trace_id, parent_id, conversation_id, project_id, service_name,
// name, kind, start_time, end_time, model, input, output, input_tokens, output_tokens,
// cost_usd, prompt_name, prompt_version, status_code, status_message, attributes.
const insertSpansSQL = `
	INSERT OR REPLACE INTO spans (
		span_id, trace_id, parent_id, conversation_id, project_id, service_name,
		name, kind, start_time, end_time,
		model, input, output, input_tokens, output_tokens, cost_usd,
		prompt_name, prompt_version,
		status_code, status_message, attributes
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
`

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

// Pipeline drains the Redis ingest queue and batches writes into DuckDB.
type Pipeline struct {
	ingest  queue.IngestQueue
	db      *sql.DB
	pricing *pricing.Table
	store   metadata.Store
	evalQ   queue.EvalQueue
	metrics *metrics.WriterMetrics
	// lake, when non-nil, receives a dual-write of every batch after the
	// legacy DuckDB write succeeds (writer.lake.enabled).
	lake SpanLakeWriter
	// reliable + fetcher + ledger, when set via WithBuffer, switch Run to
	// the S3-first loop: dequeue references, fetch from the Ingest Buffer,
	// skip ledgered batches, ack only after commit + ledger insert.
	reliable queue.ReliableIngestQueue
	fetcher  BatchFetcher
	ledger   BatchLedger
	// writeErr, if set, causes writeSpans to return this error (test only).
	writeErr error
}

// New creates a new Pipeline.
func New(
	ingest queue.IngestQueue,
	db *sql.DB,
	pricing *pricing.Table,
	store metadata.Store,
	evalQ queue.EvalQueue,
	m *metrics.WriterMetrics,
) *Pipeline {
	return &Pipeline{
		ingest:  ingest,
		db:      db,
		pricing: pricing,
		store:   store,
		evalQ:   evalQ,
		metrics: m,
	}
}

// WithLake enables dual-writing every batch to the Lake. Returns the
// pipeline for chaining at wiring time.
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
// writes them to DuckDB, computes cost, matches eval rules, and enqueues eval jobs.
// On non-fatal errors (dequeue, write, eval rule listing), the pipeline logs
// the error and continues processing subsequent batches instead of crashing.
func (p *Pipeline) Run(ctx context.Context) error {
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

		if err := p.writeSpans(ctx, spans); err != nil {
			slog.ErrorContext(ctx, "write spans failed, skipping batch",
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
// via the Batch Ledger.
func (p *Pipeline) runBuffered(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
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
			continue
		}
		p.processEntry(ctx, entry)
	}
}

// processEntry handles one dequeued ingest entry end to end. The entry is
// acked only after every durable step succeeded; any failure requeues it
// for another attempt. The staged buffer object is never touched here, so
// a crash at any point leaves the batch replayable.
func (p *Pipeline) processEntry(ctx context.Context, entry *queue.IngestEntry) {
	spans := entry.Spans
	if entry.Ref != nil {
		committed, err := p.ledger.IsBatchCommitted(ctx, entry.Ref.BatchID)
		if err != nil {
			slog.ErrorContext(ctx, "batch ledger lookup failed, requeueing",
				"batch_id", entry.Ref.BatchID, "err", err)
			p.requeue(ctx, entry)
			return
		}
		if committed {
			// Redelivery of an already-committed batch: ack without
			// touching the Lake (zero new rows).
			if p.metrics != nil {
				p.metrics.RecordLedgerSkip()
			}
			p.ack(ctx, entry)
			return
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
				return
			}
			slog.ErrorContext(ctx, "ingest buffer fetch failed, requeueing",
				"batch_id", entry.Ref.BatchID, "err", err)
			p.requeue(ctx, entry)
			return
		}
	}

	if err := p.writeLegacy(ctx, spans); err != nil {
		slog.ErrorContext(ctx, "write spans failed, requeueing",
			"span_count", len(spans), "err", err)
		if p.metrics != nil {
			p.metrics.RecordWriteError()
		}
		p.requeue(ctx, entry)
		return
	}

	if entry.Ref != nil {
		// For buffered batches the Lake commit is authoritative — unlike
		// dual-write, a failure here must retry, not be swallowed.
		if p.lake != nil {
			if err := p.commitLake(ctx, spans); err != nil {
				slog.ErrorContext(ctx, "lake commit failed, requeueing",
					"batch_id", entry.Ref.BatchID, "err", err)
				p.requeue(ctx, entry)
				return
			}
		} else {
			slog.WarnContext(ctx, "buffered batch without lake enabled — committed to legacy store only; enable writer.lake.enabled",
				"batch_id", entry.Ref.BatchID)
		}
		if err := p.ledger.MarkBatchCommitted(ctx, entry.Ref.BatchID, time.Now()); err != nil {
			// Crash window: the Lake commit stood but the ledger insert
			// failed. Requeue — redelivery re-commits and trace-detail
			// reads dedupe the residual duplicates (ADR-0004).
			slog.ErrorContext(ctx, "batch ledger insert failed, requeueing",
				"batch_id", entry.Ref.BatchID, "err", err)
			p.requeue(ctx, entry)
			return
		}
	} else {
		// Legacy payload entry: keep dual-write semantics (best effort).
		p.dualWriteLake(ctx, spans)
	}

	p.ack(ctx, entry)

	rules, err := p.listEvalRules(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "list eval rules failed, skipping eval", "err", err)
		return
	}
	for _, span := range spans {
		p.evalSpans(ctx, span, rules)
	}
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

// writeSpans writes a batch of spans to DuckDB using INSERT OR REPLACE,
// then dual-writes the Lake best-effort (legacy flow).
func (p *Pipeline) writeSpans(ctx context.Context, spans []*domain.Span) error {
	if err := p.writeLegacy(ctx, spans); err != nil {
		return err
	}
	p.dualWriteLake(ctx, spans)
	return nil
}

// writeLegacy computes cost and writes a batch to the legacy DuckDB store.
func (p *Pipeline) writeLegacy(ctx context.Context, spans []*domain.Span) error {
	if p.writeErr != nil {
		return p.writeErr
	}
	if len(spans) == 0 {
		return nil
	}

	start := time.Now()

	tx, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("pipeline: begin tx: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, insertSpansSQL)
	if err != nil {
		return fmt.Errorf("pipeline: prepare: %w", err)
	}
	defer stmt.Close()

	now := time.Now()
	for _, span := range spans {
		var cost float64
		if p.pricing != nil {
			cost = p.pricing.Cost(span.Model, span.InputTokens, span.OutputTokens)
		}
		span.CostUSD = cost

		startTime := span.StartTime
		if startTime.IsZero() {
			startTime = now
		}
		endTime := span.EndTime
		if endTime.IsZero() {
			endTime = now
		}

		// Coerce empty strings to nil so DuckDB stores NULL instead of
		// rejecting them as malformed JSON in JSON-typed columns.
		var inputVal, outputVal interface{}
		if span.Input != "" {
			inputVal = span.Input
		}
		if span.Output != "" {
			outputVal = span.Output
		}

		_, err := stmt.ExecContext(ctx,
			span.SpanID,
			span.TraceID,
			span.ParentID,
			span.ConversationID,
			span.ProjectID,
			span.ServiceName,
			span.Name,
			string(span.Kind),
			startTime,
			endTime,
			span.Model,
			inputVal,
			outputVal,
			span.InputTokens,
			span.OutputTokens,
			cost,
			span.PromptName,
			span.PromptVersion,
			span.StatusCode,
			span.StatusMessage,
			attributesJSON(span.Attributes),
		)
		if err != nil {
			return fmt.Errorf("pipeline: exec span %s: %w", span.SpanID, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("pipeline: commit: %w", err)
	}

	// Record metrics after successful write.
	if p.metrics != nil {
		elapsed := time.Since(start).Seconds()
		p.metrics.RecordDuckDBWriteDuration(elapsed)

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

// commitLake commits the batch to the Lake, recording commit latency on
// success and the failure counter on error. The caller decides whether a
// failure is fatal for the batch (buffered flow) or tolerated (dual-write).
func (p *Pipeline) commitLake(ctx context.Context, spans []*domain.Span) error {
	start := time.Now()
	if err := p.lake.InsertSpans(ctx, spans); err != nil {
		if p.metrics != nil {
			p.metrics.RecordLakeWriteError("spans")
		}
		return err
	}
	if p.metrics != nil {
		p.metrics.RecordLakeWriteDuration(time.Since(start).Seconds())
	}
	return nil
}

// dualWriteLake commits the batch to the Lake after a successful legacy
// write. A lake-write failure must never fail the batch while
// dual-writing: it is logged and counted, and the legacy write stands.
func (p *Pipeline) dualWriteLake(ctx context.Context, spans []*domain.Span) {
	if p.lake == nil {
		return
	}
	if err := p.commitLake(ctx, spans); err != nil {
		slog.ErrorContext(ctx, "lake write failed, legacy write kept",
			"span_count", len(spans),
			"err", err)
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
	if p.store == nil {
		return nil, nil
	}
	raw, err := p.store.ListEvalRules(ctx, "") // all projects
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

func attributesJSON(attrs map[string]any) string {
	if len(attrs) == 0 {
		return "null"
	}
	data, err := json.Marshal(attrs)
	if err != nil {
		return "null"
	}
	return string(data)
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
