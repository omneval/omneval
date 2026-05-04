package pipeline

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math/rand"
	"time"

	"github.com/zbloss/lantern/internal/domain"
	"github.com/zbloss/lantern/internal/metadata"
	"github.com/zbloss/lantern/internal/pricing"
	"github.com/zbloss/lantern/internal/queue"
)

// Pipeline drains the Redis ingest queue and batches writes into DuckDB.
type Pipeline struct {
	ingest  queue.IngestQueue
	db      *sql.DB
	pricing *pricing.Table
	store   metadata.Store
	evalQ   queue.EvalQueue
}

// New creates a new Pipeline.
func New(
	ingest queue.IngestQueue,
	db *sql.DB,
	pricing *pricing.Table,
	store metadata.Store,
	evalQ queue.EvalQueue,
) *Pipeline {
	return &Pipeline{
		ingest:  ingest,
		db:      db,
		pricing: pricing,
		store:   store,
		evalQ:   evalQ,
	}
}

// Run blocks until ctx is canceled. It continuously dequeues spans from Redis,
// writes them to DuckDB, computes cost, matches eval rules, and enqueues eval jobs.
func (p *Pipeline) Run(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		spans, err := p.ingest.Dequeue(ctx)
		if err != nil {
			return fmt.Errorf("pipeline: dequeue: %w", err)
		}
		if spans == nil {
			continue // timeout, no spans
		}

		if err := p.writeSpans(ctx, spans); err != nil {
			return fmt.Errorf("pipeline: write spans: %w", err)
		}

		// Evaluate eval rules against written spans.
		rules, err := p.listEvalRules(ctx)
		if err != nil {
			return fmt.Errorf("pipeline: list eval rules: %w", err)
		}

		for _, span := range spans {
			p.evalSpans(ctx, span, rules)
		}

		// Refresh eval rules every 60 seconds.
		// TODO: Add a ticker-based cache with 60s refresh.
	}
}

// writeSpans writes a batch of spans to DuckDB using INSERT OR REPLACE.
func (p *Pipeline) writeSpans(ctx context.Context, spans []*domain.Span) error {
	if len(spans) == 0 {
		return nil
	}

	tx, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("pipeline: begin tx: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, `
		INSERT OR REPLACE INTO spans (
			span_id, trace_id, parent_id, project_id, service_name,
			name, kind, start_time, end_time,
			model, input, output, input_tokens, output_tokens, cost_usd,
			prompt_name, prompt_version,
			status_code, status_message, attributes
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return fmt.Errorf("pipeline: prepare: %w", err)
	}
	defer stmt.Close()

	now := time.Now()
	for _, span := range spans {
		// Pre-compute cost.
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
			span.Input,
			span.Output,
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
	return nil
}

// evalSpans checks each eval rule against the span and enqueues matching jobs.
func (p *Pipeline) evalSpans(ctx context.Context, span *domain.Span, rules []domain.EvalRule) {
	for _, rule := range rules {
		if !rule.Enabled {
			continue
		}
		if !matchesFilter(span, rule.Filter) {
			continue
		}
		// Sample based on sample rate.
		if rule.SampleRate <= 0.0 {
			continue
		}
		if rule.SampleRate >= 1.0 || rand.Float64() < rule.SampleRate {
			job := &domain.EvalJob{
				JobID:      generateJobID(),
				RuleID:     rule.RuleID,
				SpanID:     span.SpanID,
				TraceID:    span.TraceID,
				ProjectID:  span.ProjectID,
				EnqueuedAt: time.Now(),
			}
			if err := p.evalQ.Enqueue(ctx, job); err != nil {
				// Log but don't fail the pipeline on eval enqueue errors.
				// These will be retried by the eval worker's retry loop.
			}
		}
	}
}

// matchesFilter checks if a span matches an EvalFilter.
func matchesFilter(span *domain.Span, f domain.EvalFilter) bool {
	if f.Kind != nil && span.Kind != *f.Kind {
		return false
	}
	if f.Model != nil && span.Model != *f.Model {
		return false
	}
	if f.ServiceName != nil && span.ServiceName != *f.ServiceName {
		return false
	}
	if f.PromptName != nil && span.PromptName != *f.PromptName {
		return false
	}
	if f.StatusCode != nil && span.StatusCode != *f.StatusCode {
		return false
	}
	if f.MinCostUSD != nil && span.CostUSD < *f.MinCostUSD {
		return false
	}
	if f.MaxCostUSD != nil && span.CostUSD > *f.MaxCostUSD {
		return false
	}
	durationMS := int64(0)
	if !span.StartTime.IsZero() && !span.EndTime.IsZero() {
		durationMS = span.EndTime.Sub(span.StartTime).Milliseconds()
	}
	if f.MinDurationMS != nil && durationMS < *f.MinDurationMS {
		return false
	}
	if f.MaxDurationMS != nil && durationMS > *f.MaxDurationMS {
		return false
	}
	return true
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
	if attrs == nil || len(attrs) == 0 {
		return "null"
	}
	data, err := json.Marshal(attrs)
	if err != nil {
		return "null"
	}
	return string(data)
}

func generateJobID() string {
	// Generate a simple ID using time + random.
	b := make([]byte, 8)
	rand.Read(b)
	return fmt.Sprintf("%x", b)
}
