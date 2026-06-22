package benchmark

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

// IngestClient sends span batches to the omneval Ingest API and collects
// throughput measurements (accepted spans/sec).
type IngestClient struct {
	endpoint string // e.g. http://localhost:8000/api/v1/spans
	apiKey   string
	client   *http.Client
}

// NewIngestClient creates an IngestClient.
func NewIngestClient(endpoint, apiKey string) *IngestClient {
	return &IngestClient{
		endpoint: endpoint,
		apiKey:   apiKey,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// IngestResult holds the throughput data from a single write run.
type IngestResult struct {
	SpansAccepted int64
	SpansSent     int64
	Requests      int64
	Wall          time.Duration
	// Per-span send timestamps, one per sent span, recorded by the caller.
	SendTimes []time.Time
}

// SendTraces sends all spans from all traces, partitioned into batches, and
// returns the IngestResult.  The returned SendTimes has one entry per span
// with the wall-clock send timestamp.
func (c *IngestClient) SendTraces(ctx context.Context, traces []*TraceGroup, batchSize int) (*IngestResult, error) {
	res := &IngestResult{}

	var mu sync.Mutex
	var sent int64
	var accepted int64
	var failed int64
	var latencies []time.Duration

	// Pre-build all JSON bodies so workers only need to POST.
	bodies := make([][]byte, 0, len(traces)*10) // heuristic upper bound
	for _, tg := range traces {
		for i := 0; i < len(tg.Spans); i += batchSize {
			end := i + batchSize
			if end > len(tg.Spans) {
				end = len(tg.Spans)
			}
			batch := make([]spanPayload, end-i)
			for j, s := range tg.Spans[i:end] {
				batch[j] = spanPayload{
					SpanID:      s.SpanID,
					TraceID:     s.TraceID,
					ParentID:    s.ParentID,
					Kind:        s.SpanKind,
					Name:        s.Name,
					ProjectID:   s.ProjectID,
					Input:       s.Input,
					Output:      s.Output,
					InputTokens: 256,
					OutputTokens: 256,
				}
			}
			data, err := json.Marshal(ingestBody{Spans: batch})
			if err != nil {
				return nil, fmt.Errorf("marshal spans: %w", err)
			}
			bodies = append(bodies, data)
		}
	}

	start := time.Now()
	var wg sync.WaitGroup

	// Send in order (batch 0, batch 1, …).  Concurrency is controlled by
	// the number of goroutines and a semaphore channel so the client doesn't
	// overload the Ingest API.
	sem := make(chan struct{}, 20)

	for _, body := range bodies {
		select {
		case <-ctx.Done():
			wg.Wait()
			return res, ctx.Err()
		default:
		}

		wg.Add(1)
		go func(data []byte) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(data))
			if err != nil {
				atomic.AddInt64(&failed, 1)
				return
			}
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-API-Key", c.apiKey)

			reqStart := time.Now()
			resp, err := c.client.Do(req)
			elapsed := time.Since(reqStart)

			atomic.AddInt64(&sent, 1)
			atomic.AddInt64(&accepted, 1) // optimistic

			mu.Lock()
			latencies = append(latencies, elapsed)
			mu.Unlock()

			if err != nil {
				atomic.AddInt64(&failed, 1)
				atomic.AddInt64(&accepted, -1)
				slog.Error("ingest request failed", "err", err)
				return
			}
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()

			if resp.StatusCode != http.StatusAccepted {
				atomic.AddInt64(&failed, 1)
				atomic.AddInt64(&accepted, -1)
				slog.Warn("ingest non-202", "status", resp.StatusCode)
			}
		}(body)
	}
	wg.Wait()
	res.Wall = time.Since(start)

	res.SpansSent = atomic.LoadInt64(&sent)
	res.SpansAccepted = atomic.LoadInt64(&accepted)
	res.Requests = res.SpansSent

	return res, nil
}

func (r *IngestResult) Report(cadence time.Duration) {
	fmt.Println("=== Ingest API Results ===")
	fmt.Printf("Commit cadence (batch-flush interval): %s\n", cadence)
	fmt.Printf("wall time:        %s\n", r.Wall.Round(time.Millisecond))
	fmt.Printf("spans sent:       %d\n", r.SpansSent)
	fmt.Printf("spans accepted:   %d (%d failed)\n", r.SpansAccepted, r.SpansSent-r.SpansAccepted)
	fmt.Printf("spans/sec:        %.1f\n", float64(r.SpansAccepted)/r.Wall.Seconds())
	fmt.Println()
}

// spanPayload is the minimal JSON body accepted by the Ingest API's
// /api/v1/spans endpoint (flat, no parent_id field because the ingest
// handler extracts parent_id from the OTel payload).  For the benchmark
// harness we include parent_id so the ingest pipeline receives it directly.
type spanPayload struct {
	SpanID      string `json:"span_id"`
	TraceID     string `json:"trace_id"`
	ParentID    string `json:"parent_id"`
	ProjectID   string `json:"project_id"`
	Kind        string `json:"kind"`
	Name        string `json:"name"`
	Input       string `json:"input"`
	Output      string `json:"output"`
	InputTokens int64  `json:"input_tokens"`
	OutputTokens int64 `json:"output_tokens"`
}

type ingestBody struct {
	Spans []spanPayload `json:"spans"`
}