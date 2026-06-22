package benchmark

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// WriterClient reads the Prometheus metrics endpoint exposed by the Writer
// service to measure how many spans have been durably committed to the Lake.
type WriterClient struct {
	metricsURL string // e.g. http://localhost:9091/metrics
	apiKey     string
	client     *http.Client
}

// NewWriterClient creates a WriterClient pointing at the given Prometheus
// metrics URL.
func NewWriterClient(metricsURL, apiKey string) *WriterClient {
	return &WriterClient{
		metricsURL: metricsURL,
		apiKey:     apiKey,
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// MeasureCommitted queries the Prometheus /metrics endpoint at regular intervals
// for duration, extracting the omneval_writer_spans_written_total counter for
// the given projectID. For each sample it calls the callback with the raw
// counter value and the per-second rate (delta since the previous sample).
//
// This is the canonical way to measure Writer → Lake committed spans/sec
// without coupling to the Writer's internal state.
func (c *WriterClient) MeasureCommitted(ctx context.Context, projectID string, interval time.Duration,
	onSample func(count int64, ratePerSec float64),
) error {
	if interval == 0 {
		interval = 5 * time.Second
	}

	// Prometheus text format: lines like
	//   omneval_writer_spans_written_total{project_id="foo",} 12345
	// We parse the value after the last space on matching lines.
	samplePattern := "omneval_writer_spans_written_total"

	var mu sync.Mutex
	var prevCount int64

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		count, err := c.fetchCounter(ctx, samplePattern, projectID)
		if err != nil {
			slog.Error("failed to fetch writer metrics", "err", err)
			// Continue to next sample rather than failing the whole run.
			time.Sleep(interval)
			continue
		}

		mu.Lock()
		rate := 0.0
		if prevCount > 0 {
			rate = float64(count-prevCount) / interval.Seconds()
		}
		mu.Unlock()

		onSample(count, rate)
		prevCount = count

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(interval):
		}
	}
}

// fetchCounter queries /metrics once and returns the value of the counter
// line matching samplePattern for the given projectID.  Returns -1 on error.
func (c *WriterClient) fetchCounter(ctx context.Context, samplePattern, projectID string) (int64, error) {
	url := c.metricsURL
	if url == "" {
		return -1, fmt.Errorf("metrics URL is empty")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return -1, err
	}
	if c.apiKey != "" {
		req.Header.Set("X-API-Key", c.apiKey)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return -1, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return -1, fmt.Errorf("metrics returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return -1, err
	}

	// Find the matching line.
	lines := splitLines(body)
	for _, line := range lines {
		if !containsAny(line, samplePattern) {
			continue
		}
		// Check that the line contains our project_id label.
		if !contains(line, fmt.Sprintf(`project_id="%s"`, projectID)) &&
			!contains(line, fmt.Sprintf(`project_id='%s'`, projectID)) {
			continue
		}
		// The value is the last space-separated token.
		lastSpace := lastIndexOf(line, " ")
		if lastSpace == -1 {
			// No label block — value is the whole line.
			lastSpace = 0
		}
		valStr := line[lastSpace+1:]
		val, err := strconv.ParseInt(valStr, 10, 64)
		if err != nil {
			continue
		}
		return val, nil
	}

	return -1, fmt.Errorf("counter %q for project %q not found", samplePattern, projectID)
}

// --- lightweight string helpers (avoid encoding/gofuzz etc.) ---

func contains(s, substr string) bool {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func containsAny(s string, substrs ...string) bool {
	for _, sub := range substrs {
		if contains(s, sub) {
			return true
		}
	}
	return false
}

func lastIndexOf(s, substr string) int {
	for i := len(s) - len(substr); i >= 0; i-- {
		if i+len(substr) <= len(s) && s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}

func lastIndexOfByte(s string, b byte) int {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == b {
			return i
		}
	}
	return -1
}

func splitLines(data []byte) []string {
	lines := make([]string, 0, 16)
	start := 0
	for i := 0; i < len(data); i++ {
		if data[i] == '\n' {
			lines = append(lines, string(data[start:i]))
			start = i + 1
		}
	}
	if start < len(data) {
		lines = append(lines, string(data[start:]))
	}
	return lines
}