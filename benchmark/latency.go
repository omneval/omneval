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
	"time"
)

// LatencyClient polls the Query API until a span appears and records the
// end-to-end latency for each span.
type LatencyClient struct {
	queryEndpoint string // e.g. http://localhost:8000/api/v1/spans/query
	apiKey        string
	client        *http.Client
}

// NewLatencyClient creates a LatencyClient for the Query API.
func NewLatencyClient(queryEndpoint, apiKey string) *LatencyClient {
	return &LatencyClient{
		queryEndpoint: queryEndpoint,
		apiKey:        apiKey,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// LatencyPollConfig controls the polling behaviour for end-to-end latency
// measurement.
type LatencyPollConfig struct {
	PollInterval time.Duration // time between polls
	Timeout      time.Duration // max time to wait for a span to become visible
}

// defaultLatencyPollConfig returns a sensible default config.
func defaultLatencyPollConfig() LatencyPollConfig {
	return LatencyPollConfig{
		PollInterval: 250 * time.Millisecond,
		Timeout:      60 * time.Second,
	}
}

// MeasureLatency polls the Query API for each span, starting at each span's
// SendTime and polling every cfg.PollInterval until the span is found or
// cfg.Timeout elapses.  Returns a LatencyStats with per-span measurements.
func (c *LatencyClient) MeasureLatency(ctx context.Context, spans []*Span, cfg LatencyPollConfig) *LatencyStats {
	if cfg.PollInterval == 0 {
		cfg = defaultLatencyPollConfig()
	}
	stats := &LatencyStats{}

	type work struct {
		span *Span
	}

	workQueue := make([]work, 0, len(spans))
	for _, s := range spans {
		workQueue = append(workQueue, work{span: s})
	}

	// Poll all spans concurrently.  Each goroutine polls until it finds its
	// span or the context deadline expires.
	mu := sync.Mutex{}

	var wg sync.WaitGroup
	for _, w := range workQueue {
		wg.Add(1)
		go func(w work) {
			defer wg.Done()

			sendTime := w.span.SendTime
			if sendTime.IsZero() {
				// Fallback: use the span's StartTime as send proxy.
				sendTime = w.span.StartTime
			}

			pollNo := 0
			var visibleAt time.Time
			for {
				pollNo++
				visibleAt = time.Now()

				foundIt, err := c.querySpan(ctx, w.span.SpanID, w.span.ProjectID)
				if err != nil {
					slog.Warn("query poll error", "span", w.span.SpanID, "poll", pollNo, "err", err)
					visibleAt = time.Time{}
				}
				if foundIt {
					break
				}

				if visibleAt.IsZero() || time.Since(sendTime) > cfg.Timeout {
					// Timed out — record a max-value so it shows up in p99.
					mu.Lock()
					stats.Latencies = append(stats.Latencies, cfg.Timeout)
					mu.Unlock()
					return
				}

				time.Sleep(cfg.PollInterval)
			}

			latency := visibleAt.Sub(sendTime)
			slog.Info("span visible", "span_id", w.span.SpanID, "latency", latency, "polls", pollNo)

			mu.Lock()
			defer mu.Unlock()
			stats.Latencies = append(stats.Latencies, latency)
		}(w)
	}
	wg.Wait()

	// Sort latencies so Report can compute percentiles.
	sortDurations(stats.Latencies)
	return stats
}

// querySpan returns true if the span is visible in the Query API.
func (c *LatencyClient) querySpan(ctx context.Context, spanID, projectID string) (bool, error) {
	reqBody := querySpanRequest{
		From: "lake.spans",
		Filters: []queryFilter{
			{Field: "span_id", Op: "eq", Value: spanID},
		},
		Limit: 1,
	}
	data, err := json.Marshal(reqBody)
	if err != nil {
		return false, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.queryEndpoint, bytes.NewReader(data))
	if err != nil {
		return false, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", c.apiKey)

	resp, err := c.client.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusBadRequest {
		io.Copy(io.Discard, resp.Body)
		return false, nil // 400 typically means no results
	}
	if resp.StatusCode != http.StatusOK {
		io.Copy(io.Discard, resp.Body)
		return false, fmt.Errorf("query returned %d", resp.StatusCode)
	}

	var respBody querySpanResponse
	if err := json.NewDecoder(resp.Body).Decode(&respBody); err != nil {
		return false, err
	}

	return len(respBody.Spans) > 0, nil
}

// Query API request/response types.

type querySpanRequest struct {
	From    string         `json:"from"`
	Filters []queryFilter  `json:"filters"`
	Limit   int            `json:"limit"`
}

type queryFilter struct {
	Field string `json:"field"`
	Op    string `json:"op"`
	Value any    `json:"value"`
}

type querySpanResponse struct {
	Spans []querySpan `json:"spans"`
}

type querySpan struct {
	SpanID string `json:"span_id"`
}