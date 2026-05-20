package pipeline

import (
	"context"
	"crypto/rand"
	"database/sql"
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

// insertSpansSQL is the INSERT OR REPLACE statement for the DuckDB spans table.
// Columns: span_id, trace_id, parent_id, project_id, service_name, name, kind,
// start_time, end_time, model, input, output, input_tokens, output_tokens,
// cost_usd, prompt_name, prompt_version, status_code, status_message, attributes.
const insertSpansSQL = `
	INSERT OR REPLACE INTO spans (
		span_id, trace_id, parent_id, project_id, service_name,
		name, kind, start_time, end_time,
		model, input, output, input_tokens, output_tokens, cost_usd,
		prompt_name, prompt_version,
		status_code, status_message, attributes
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
`

// Pipeline drains the Redis ingest queue and batches writes into DuckDB.
type Pipeline struct {
	ingest  queue.IngestQueue
	db      *sql.DB
	pricing *pricing.Table
	store   metadata.Store
	evalQ   queue.EvalQueue
	metrics *metrics.WriterMetrics
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

// Run blocks until ctx is canceled. It continuously dequeues spans from Redis,
// writes them to DuckDB, computes cost, matches eval rules, and enqueues eval jobs.
// On non-fatal errors (dequeue, write, eval rule listing), the pipeline logs
// the error and continues processing subsequent batches instead of crashing.
func (p *Pipeline) Run(ctx context.Context) error {
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

// writeSpans writes a batch of spans to DuckDB using INSERT OR REPLACE.
func (p *Pipeline) writeSpans(ctx context.Context, spans []*domain.Span) error {
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
		cost := p.pricing.Cost(span.Model, span.InputTokens, span.OutputTokens)
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
