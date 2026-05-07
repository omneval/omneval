package worker

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/zbloss/lantern/internal/config"
	"github.com/zbloss/lantern/internal/domain"
	"github.com/zbloss/lantern/internal/queue"
	"github.com/zbloss/lantern/services/eval/internal/judge"
)

// Worker drains the Redis eval queue and dispatches jobs to the judge pipeline.
type Worker struct {
	evalQ   queue.EvalQueue
	judge   judge.JudgeExecutor
	scores  *http.Client
	baseURL string
	retries int
}

// New creates a new Worker.
func New(
	evalQ queue.EvalQueue,
	judgeLLM judge.JudgeExecutor,
	cfg *config.Config,
) *Worker {
	retries := cfg.Eval.RetryCount
	if retries <= 0 {
		retries = 3
	}
	return &Worker{
		evalQ:   evalQ,
		judge:   judgeLLM,
		scores:  &http.Client{Timeout: 30 * time.Second},
		baseURL: cfg.Writer.Addr,
		retries: retries,
	}
}

// Run blocks until ctx is canceled. It continuously dequeues eval jobs from
// Redis, evaluates them via the judge LLM, and writes scores back to the
// Writer Service. On shutdown it does not dequeue new jobs — only finishes
// any in-flight evaluation within the drain window.
func (w *Worker) Run(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		job, err := w.evalQ.Dequeue(ctx)
		if err != nil {
			slog.WarnContext(ctx, "worker: dequeue failed", "err", err)
			continue
		}
		if job == nil {
			continue // timeout, no job
		}

		slog.InfoContext(ctx, "worker: processing eval job",
			"job_id", job.JobID,
			"rule_id", job.RuleID,
		)

		// Evaluate with retry.
		score, err := w.evaluateWithRetry(ctx, job)
		if err != nil {
			slog.ErrorContext(ctx, "worker: eval failed after retries",
				"job_id", job.JobID,
				"err", err,
			)
			continue
		}

		// Write score back to Writer Service.
		if err := w.writeScore(ctx, job, score); err != nil {
			slog.ErrorContext(ctx, "worker: score write failed",
				"job_id", job.JobID,
				"err", err,
			)
		}
	}
}

// evaluateWithRetry evaluates the job with exponential backoff up to w.retries.
func (w *Worker) evaluateWithRetry(ctx context.Context, job *domain.EvalJob) (*judge.Score, error) {
	var lastErr error
	for attempt := 0; attempt <= w.retries; attempt++ {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		score, err := w.judge.Evaluate(ctx, job)
		if err != nil {
			lastErr = err
			if attempt < w.retries {
				backoff := time.Duration(1<<uint(attempt)) * time.Second
				slog.WarnContext(ctx, "worker: eval attempt failed, retrying",
					"job_id", job.JobID,
					"attempt", attempt+1,
					"backoff", backoff,
					"err", err,
				)
				select {
				case <-time.After(backoff):
					continue
				case <-ctx.Done():
					return nil, ctx.Err()
				}
			}
			break
		}
		return score, nil
	}
	return nil, fmt.Errorf("worker: eval failed after %d retries: %w", w.retries, lastErr)
}

// writeScore sends the evaluation score back to the Writer Service.
func (w *Worker) writeScore(ctx context.Context, job *domain.EvalJob, score *judge.Score) error {
	scorePayload := map[string]any{
		"job_id":     job.JobID,
		"rule_id":    job.RuleID,
		"span_id":    job.SpanID,
		"trace_id":   job.TraceID,
		"project_id": job.ProjectID,
		"score":      score.Score,
		"reasoning":  score.Reasoning,
	}

	data, err := json.Marshal(scorePayload)
	if err != nil {
		return fmt.Errorf("worker: marshal score: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST",
		fmt.Sprintf("%s/internal/v1/scores", w.baseURL),
		bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("worker: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := w.scores.Do(req)
	if err != nil {
		return fmt.Errorf("worker: POST score: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("worker: unexpected score status %d", resp.StatusCode)
	}

	slog.InfoContext(ctx, "worker: score written",
		"job_id", job.JobID,
		"score", score.Score,
	)

	return nil
}
