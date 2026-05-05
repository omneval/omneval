package worker

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/zbloss/lantern/internal/domain"
	"github.com/zbloss/lantern/internal/metadata"
	"github.com/zbloss/lantern/internal/queue"
	"github.com/zbloss/lantern/services/eval/internal/judge"
)

// maxRetries is the maximum number of retry attempts for score write-back.
// With exponential backoff starting at 1s, 5 retries cover ~31 seconds.
// A separate long-lived retry loop extends coverage to ~5 minutes.
const maxRetries = 5

// backoffBase is the starting delay for exponential backoff.
const backoffBase = time.Second

// ScoreWriter handles writing scores back to the Writer service.
type ScoreWriter struct {
	BaseURL string
	Client  *http.Client
}

// WriteScore sends a score to the Writer service's internal endpoint.
func (w *ScoreWriter) WriteScore(ctx context.Context, score *domain.Score) error {
	if w.BaseURL == "" {
		return fmt.Errorf("score writer: no writer URL configured")
	}
	client := w.Client
	if client == nil {
		client = &http.Client{}
	}

	reqBody := ScoreRequest{
		ScoreID:       score.ScoreID,
		SpanID:        score.SpanID,
		TraceID:       score.TraceID,
		ProjectID:     score.ProjectID,
		EvalName:      score.EvalName,
		Value:         score.Value,
		Reasoning:     score.Reasoning,
		JudgeModel:    score.JudgeModel,
		PromptName:    score.PromptName,
		PromptVersion: score.PromptVersion,
	}

	data, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("score writer: marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, w.BaseURL+"/internal/v1/scores", bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("score writer: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("score writer: request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("score writer: status %d: %s", resp.StatusCode, string(body))
	}
	return nil
}

// ScoreRequest is the JSON body for POST /internal/v1/scores.
type ScoreRequest struct {
	ScoreID       string  `json:"score_id"`
	SpanID        string  `json:"span_id"`
	TraceID       string  `json:"trace_id"`
	ProjectID     string  `json:"project_id"`
	EvalName      string  `json:"eval_name"`
	Value         float64 `json:"value"`
	Reasoning     string  `json:"reasoning"`
	JudgeModel    string  `json:"judge_model"`
	PromptName    string  `json:"prompt_name"`
	PromptVersion int64   `json:"prompt_version"`
}

// Worker drains the Redis eval queue and dispatches jobs to the judge pipeline.
type Worker struct {
	evalQ    queue.EvalQueue
	scoreQ   ScoreWriter
	judge    *judge.Judge
	db       *sql.DB
	store    metadata.Store
	conc     int
}

// New creates a new Worker.
func New(
	evalQ queue.EvalQueue,
	scoreWriter ScoreWriter,
	judge *judge.Judge,
	db *sql.DB,
	store metadata.Store,
	concurrency int,
) *Worker {
	return &Worker{
		evalQ:  evalQ,
		scoreQ: scoreWriter,
		judge:  judge,
		db:     db,
		store:  store,
		conc:   concurrency,
	}
}

// Run starts the worker pool, draining eval jobs and processing them.
func (w *Worker) Run(ctx context.Context) error {
	// Launch concurrent workers.
	var err error
	for i := 0; i < w.conc; i++ {
		go func(id int) {
			if wErr := w.worker(ctx, id); wErr != nil && err == nil {
				err = wErr
			}
		}(i)
	}

	// Wait for context cancellation.
	<-ctx.Done()
	slog.InfoContext(ctx, "eval: workers shutting down")
	return err
}

// worker is a single goroutine that drains the eval queue and processes jobs.
func (w *Worker) worker(ctx context.Context, id int) error {
	slog.InfoContext(ctx, "eval: worker started", "id", id)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		job, err := w.evalQ.Dequeue(ctx)
		if err != nil {
			if err == redis.Nil {
				continue // timeout, no job
			}
			slog.ErrorContext(ctx, "eval: dequeue error", "worker", id, "err", err)
			time.Sleep(time.Second)
			continue
		}

		if job == nil {
			continue // timeout, no job
		}

		if err := w.processJob(ctx, job); err != nil {
			slog.ErrorContext(ctx, "eval: process job failed",
				"worker", id, "job_id", job.JobID, "err", err)
		}
	}
}

// processJob fetches the span, runs the judge, and writes the score back.
func (w *Worker) processJob(ctx context.Context, job *domain.EvalJob) error {
	slog.DebugContext(ctx, "eval: processing job",
		"job_id", job.JobID, "rule_id", job.RuleID, "span_id", job.SpanID)

	// Fetch the span from DuckDB.
	span, err := w.fetchSpan(ctx, job)
	if err != nil {
		return fmt.Errorf("worker: fetch span: %w", err)
	}

	// Run the judge.
	score, err := w.judge.Evaluate(ctx, job, span)
	if err != nil {
		return fmt.Errorf("worker: judge eval: %w", err)
	}

	// Write the score back with retries.
	return w.writeScoreWithRetry(ctx, score)
}

// fetchSpan retrieves a span from the DuckDB hot store.
func (w *Worker) fetchSpan(ctx context.Context, job *domain.EvalJob) (*domain.Span, error) {
	rows, err := w.db.QueryContext(ctx,
		`SELECT span_id, trace_id, parent_id, project_id, service_name,
		        name, kind, start_time, end_time,
		        model, input, output, input_tokens, output_tokens, cost_usd,
		        prompt_name, prompt_version, status_code, status_message, attributes
		 FROM spans WHERE trace_id = ? AND span_id = ?`,
		job.TraceID, job.SpanID,
	)
	if err != nil {
		return nil, fmt.Errorf("worker: query span: %w", err)
	}
	defer rows.Close()

	if !rows.Next() {
		return nil, fmt.Errorf("worker: span not found: %s/%s", job.TraceID, job.SpanID)
	}

	span := &domain.Span{}
	var attrs []byte
	if err := rows.Scan(
		&span.SpanID, &span.TraceID, &span.ParentID, &span.ProjectID, &span.ServiceName,
		&span.Name, &span.Kind, &span.StartTime, &span.EndTime,
		&span.Model, &span.Input, &span.Output, &span.InputTokens, &span.OutputTokens, &span.CostUSD,
		&span.PromptName, &span.PromptVersion,
		&span.StatusCode, &span.StatusMessage, &attrs,
	); err != nil {
		return nil, fmt.Errorf("worker: scan span: %w", err)
	}

	if len(attrs) > 0 {
		_ = json.Unmarshal(attrs, &span.Attributes)
	}

	return span, nil
}

// writeScoreWithRetry writes the score back to the Writer service with exponential backoff.
// Retries up to maxRetries times within ~31 seconds. Scores that exhaust retries are
// logged at Error and dropped. A separate retry loop extends coverage to 5 minutes.
func (w *Worker) writeScoreWithRetry(ctx context.Context, score *domain.Score) error {
	// Phase 1: Fast retries with exponential backoff.
	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			delay := backoffBase * time.Duration(1<<uint(attempt-1))
			slog.InfoContext(ctx, "eval: retrying score write",
				"attempt", attempt, "delay", delay, "score_id", score.ScoreID)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(delay):
			}
		}

		writeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		err := w.scoreQ.WriteScore(writeCtx, score)
		cancel()

		if err == nil {
			slog.DebugContext(ctx, "eval: score written",
				"score_id", score.ScoreID, "span_id", score.SpanID, "value", score.Value)
			return nil
		}

		lastErr = err
		slog.WarnContext(ctx, "eval: score write failed (will retry)",
			"attempt", attempt, "score_id", score.ScoreID, "err", err)
	}

	// Phase 2: Long-lived retry loop for up to 5 minutes.
	return w.longRetryLoop(ctx, score, lastErr)
}

// longRetryLoop continues retrying score write-back for up to 5 minutes
// using increasing delays. After 5 minutes, logs at Error and drops.
func (w *Worker) longRetryLoop(ctx context.Context, score *domain.Score, initialErr error) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	maxDelay := 60 * time.Second
	delay := time.Second

	for {
		select {
		case <-ctx.Done():
			slog.ErrorContext(ctx, "eval: score write timed out after 5 minutes, dropping score",
				"score_id", score.ScoreID, "span_id", score.SpanID,
				"last_error", initialErr.Error())
			return fmt.Errorf("score write timed out: %w", initialErr)
		default:
		}

		writeCtx, writeCancel := context.WithTimeout(ctx, 10*time.Second)
		err := w.scoreQ.WriteScore(writeCtx, score)
		writeCancel()

		if err == nil {
			slog.Info("eval: score written after long retry",
				"score_id", score.ScoreID, "span_id", score.SpanID)
			return nil
		}

		slog.Warn("eval: score write still failing, continuing retry",
			"score_id", score.ScoreID, "delay", delay, "err", err)

		select {
		case <-time.After(delay):
			// Increase delay exponentially, capped at maxDelay.
			delay = delay * 2
			if delay > maxDelay {
				delay = maxDelay
			}
		case <-ctx.Done():
			slog.Error("eval: score write timed out, dropping score",
				"score_id", score.ScoreID, "span_id", score.SpanID,
				"last_error", err.Error())
			return fmt.Errorf("score write timed out: %w", err)
		}
	}
}

// generateID creates a unique ID string.
func generateID() string {
	b := make([]byte, 8)
	rand.Read(b) //nolint:errcheck // crypto/rand.Read only fails for truly pathological reasons
	return fmt.Sprintf("%x", b)
}
