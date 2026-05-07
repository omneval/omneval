package worker

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/zbloss/lantern/internal/config"
	"github.com/zbloss/lantern/internal/domain"
	"github.com/zbloss/lantern/internal/queue"
	"github.com/zbloss/lantern/services/eval/internal/judge"
)

// fakeEvalQueue returns jobs up to a count, then nil forever.
type fakeEvalQueue struct {
	remaining int
	nextJob   *domain.EvalJob
	nextErr   error
}

func (f *fakeEvalQueue) Enqueue(_ context.Context, _ *domain.EvalJob) error {
	return nil
}

func (f *fakeEvalQueue) Dequeue(_ context.Context) (*domain.EvalJob, error) {
	if f.nextErr != nil {
		return nil, f.nextErr
	}
	if f.remaining <= 0 {
		return nil, nil
	}
	f.remaining--
	return f.nextJob, nil
}

// fakeJudge always succeeds.
type fakeJudge struct {
	calls int
}

func (f *fakeJudge) Evaluate(_ context.Context, _ *domain.EvalJob) (*judge.Score, error) {
	f.calls++
	return &judge.Score{Score: 0.9, Reasoning: "great"}, nil
}

func TestRun_StopsOnContextCancel(t *testing.T) {
	j := &domain.EvalJob{
		JobID:     "job-1",
		RuleID:    "rule-1",
		SpanID:    "span-1",
		TraceID:   "trace-1",
		ProjectID: "proj-1",
	}

	judgeLLM := &fakeJudge{}

	w := &Worker{
		evalQ:   &fakeEvalQueue{remaining: 1, nextJob: j},
		judge:   judgeLLM,
		scores:  &http.Client{},
		baseURL: "http://localhost:0",
		retries: 0,
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)

	go func() {
		done <- w.Run(ctx)
	}()

	// Let the single job complete.
	time.Sleep(50 * time.Millisecond)

	// Cancel — worker should stop dequeuing.
	cancel()

	select {
	case err := <-done:
		if err != context.Canceled {
			t.Logf("Run returned: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("worker did not stop within 5s after cancel")
	}

	if judgeLLM.calls != 1 {
		t.Errorf("judge calls: got %d, want 1", judgeLLM.calls)
	}
}

func TestRun_NoNewJobAfterCancel(t *testing.T) {
	// Queue that always returns nil (no jobs).
	j := &domain.EvalJob{
		JobID:     "job-1",
		RuleID:    "rule-1",
		SpanID:    "span-1",
		TraceID:   "trace-1",
		ProjectID: "proj-1",
	}

	w := &Worker{
		evalQ:   &fakeEvalQueue{remaining: 0, nextJob: j},
		judge:   &fakeJudge{},
		scores:  &http.Client{},
		baseURL: "http://localhost:0",
		retries: 0,
	}

	ctx, cancel := context.WithCancel(context.Background())

	// Give worker a very short window, then cancel.
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()

	done := make(chan error, 1)
	go func() {
		done <- w.Run(ctx)
	}()

	select {
	case err := <-done:
		if err != context.Canceled {
			t.Logf("Run returned: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("worker did not stop within 5s after cancel")
	}
}

func TestRun_ErrorDequeueContinues(t *testing.T) {
	w := &Worker{
		evalQ: &fakeEvalQueue{
			remaining: 0,
			nextJob:   nil,
			nextErr:   queue.ErrQueueUnreachable,
		},
		judge:   &fakeJudge{},
		scores:  &http.Client{},
		baseURL: "http://localhost:0",
		retries: 0,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- w.Run(ctx)
	}()

	select {
	case err := <-done:
		if err != context.DeadlineExceeded {
			t.Logf("Run returned: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("worker did not stop within 5s")
	}
}

func TestNew_DefaultRetries(t *testing.T) {
	cfg := &config.Config{
		Eval: config.EvalConfig{
			RetryCount: 0,
		},
	}
	w := New(nil, nil, cfg)
	if w.retries != 3 {
		t.Errorf("retries: got %d, want %d", w.retries, 3)
	}
}

func TestNew_CustomRetries(t *testing.T) {
	cfg := &config.Config{
		Eval: config.EvalConfig{
			RetryCount: 5,
		},
	}
	w := New(nil, nil, cfg)
	if w.retries != 5 {
		t.Errorf("retries: got %d, want %d", w.retries, 5)
	}
}

func TestNew_NegativeRetriesDefaultsToThree(t *testing.T) {
	cfg := &config.Config{
		Eval: config.EvalConfig{
			RetryCount: -1,
		},
	}
	w := New(nil, nil, cfg)
	if w.retries != 3 {
		t.Errorf("retries: got %d, want %d (default for negative)", w.retries, 3)
	}
}
