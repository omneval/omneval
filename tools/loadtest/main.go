// Command loadtest drives sustained span ingest against the Omneval Ingest
// API to measure end-to-end throughput, and reports the Writer's Lake
// commit metrics so the effect of the Writer's batched-commit window
// (batchFlushInterval / batchMaxBytes, see services/writer/internal/pipeline)
// can be observed directly.
//
// Usage:
//
//	go run . -url http://localhost:8000 -api-key oev_proj_... \
//	    -duration 30s -concurrency 20 -batch-size 50 -payload-bytes 500 \
//	    -writer-metrics-url http://localhost:9091/metrics
package main

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

func main() {
	url := flag.String("url", "http://localhost:8000", "Ingest API base URL")
	apiKey := flag.String("api-key", "", "X-API-Key for the target project (required)")
	duration := flag.Duration("duration", 30*time.Second, "How long to run the load test")
	concurrency := flag.Int("concurrency", 20, "Number of concurrent sender workers")
	batchSize := flag.Int("batch-size", 50, "Spans per POST /api/v1/spans request")
	payloadBytes := flag.Int("payload-bytes", 500, "Approximate size in bytes of each span's input+output text")
	rate := flag.Float64("rate", 0, "Target spans/sec across all workers (0 = unlimited, send as fast as possible)")
	writerMetricsURL := flag.String("writer-metrics-url", "http://localhost:9091/metrics", "Writer Service Prometheus metrics endpoint, used to report Lake commit stats (empty to skip)")
	flag.Parse()

	if *apiKey == "" {
		fmt.Fprintln(os.Stderr, "error: -api-key is required")
		os.Exit(2)
	}

	endpoint := strings.TrimRight(*url, "/") + "/api/v1/spans"

	before := fetchWriterMetrics(*writerMetricsURL)

	log.Printf("loadtest: starting against %s for %s (concurrency=%d, batch-size=%d, payload-bytes=%d, rate=%v spans/sec)",
		endpoint, *duration, *concurrency, *batchSize, *payloadBytes, rateLabel(*rate))

	result := run(loadConfig{
		endpoint:     endpoint,
		apiKey:       *apiKey,
		duration:     *duration,
		concurrency:  *concurrency,
		batchSize:    *batchSize,
		payloadBytes: *payloadBytes,
		rateSpansSec: *rate,
	})

	after := fetchWriterMetrics(*writerMetricsURL)

	result.print()
	printWriterDelta(before, after, result.wall)
}

func rateLabel(rate float64) string {
	if rate <= 0 {
		return "unlimited"
	}
	return strconv.FormatFloat(rate, 'f', -1, 64)
}

// --- load generation ---

type loadConfig struct {
	endpoint     string
	apiKey       string
	duration     time.Duration
	concurrency  int
	batchSize    int
	payloadBytes int
	rateSpansSec float64
}

type result struct {
	requests int64
	accepted int64
	failed   int64
	spans    int64
	wall     time.Duration

	mu        sync.Mutex
	latencies []time.Duration
}

func run(cfg loadConfig) *result {
	res := &result{}
	httpClient := &http.Client{Timeout: 30 * time.Second}

	// Optional rate limiting: a single shared ticker gates how often any
	// worker may send the next batch. interval is the time between batches
	// across all workers combined to hit cfg.rateSpansSec.
	var tokens chan struct{}
	if cfg.rateSpansSec > 0 {
		interval := time.Duration(float64(cfg.batchSize) / cfg.rateSpansSec * float64(time.Second))
		if interval <= 0 {
			interval = time.Nanosecond
		}
		tokens = make(chan struct{})
		go func() {
			ticker := time.NewTicker(interval)
			defer ticker.Stop()
			for range ticker.C {
				select {
				case tokens <- struct{}{}:
				default:
				}
			}
		}()
	}

	start := time.Now()
	deadline := start.Add(cfg.duration)

	var wg sync.WaitGroup
	for w := 0; w < cfg.concurrency; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			payload := newPayloadGenerator(workerID, cfg.batchSize, cfg.payloadBytes)
			for time.Now().Before(deadline) {
				if tokens != nil {
					<-tokens
				}
				body := payload.next()
				sendBatch(httpClient, cfg.endpoint, cfg.apiKey, body, cfg.batchSize, res)
			}
		}(w)
	}
	wg.Wait()
	res.wall = time.Since(start)
	return res
}

func sendBatch(client *http.Client, endpoint, apiKey string, body []byte, batchSize int, res *result) {
	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		atomic.AddInt64(&res.failed, 1)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", apiKey)

	start := time.Now()
	resp, err := client.Do(req)
	elapsed := time.Since(start)

	atomic.AddInt64(&res.requests, 1)
	res.mu.Lock()
	res.latencies = append(res.latencies, elapsed)
	res.mu.Unlock()

	if err != nil {
		atomic.AddInt64(&res.failed, 1)
		return
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()

	if resp.StatusCode == http.StatusAccepted {
		atomic.AddInt64(&res.accepted, 1)
		atomic.AddInt64(&res.spans, int64(batchSize))
	} else {
		atomic.AddInt64(&res.failed, 1)
	}
}

func (r *result) print() {
	r.mu.Lock()
	latencies := append([]time.Duration(nil), r.latencies...)
	r.mu.Unlock()
	sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })

	fmt.Println()
	fmt.Println("=== Ingest API results ===")
	fmt.Printf("wall time:        %s\n", r.wall.Round(time.Millisecond))
	fmt.Printf("requests sent:    %d (%d accepted, %d failed)\n", r.requests, r.accepted, r.failed)
	fmt.Printf("spans accepted:   %d\n", r.spans)
	fmt.Printf("spans/sec:        %.1f\n", float64(r.spans)/r.wall.Seconds())
	fmt.Printf("requests/sec:     %.1f\n", float64(r.requests)/r.wall.Seconds())
	if len(latencies) > 0 {
		fmt.Printf("latency p50/p90/p99/max: %s / %s / %s / %s\n",
			percentile(latencies, 0.50).Round(time.Millisecond),
			percentile(latencies, 0.90).Round(time.Millisecond),
			percentile(latencies, 0.99).Round(time.Millisecond),
			latencies[len(latencies)-1].Round(time.Millisecond))
	}
	fmt.Println()
	fmt.Println("Note: \"accepted\" means the Ingest API staged the batch and queued it for")
	fmt.Println("the Writer (HTTP 202) — not that it is committed to the Lake yet. See the")
	fmt.Println("Writer Lake commit stats below for end-to-end write throughput.")
}

func percentile(sorted []time.Duration, p float64) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	idx := int(p * float64(len(sorted)))
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}

// --- payload generation ---

// payloadGenerator builds POST bodies for one worker. Span/trace IDs are
// randomized so each request looks like a distinct set of LLM calls.
type payloadGenerator struct {
	workerID  int
	batchSize int
	text      string
	seq       int64
}

func newPayloadGenerator(workerID, batchSize, payloadBytes int) *payloadGenerator {
	return &payloadGenerator{
		workerID:  workerID,
		batchSize: batchSize,
		text:      strings.Repeat("a", payloadBytes),
	}
}

type nativeSpan struct {
	SpanID       string `json:"span_id"`
	TraceID      string `json:"trace_id"`
	Name         string `json:"name"`
	Kind         string `json:"kind"`
	Model        string `json:"model"`
	Input        string `json:"input"`
	Output       string `json:"output"`
	InputTokens  int64  `json:"input_tokens"`
	OutputTokens int64  `json:"output_tokens"`
}

type ingestRequest struct {
	Spans []nativeSpan `json:"spans"`
}

func (g *payloadGenerator) next() []byte {
	traceID := randomHex(16)
	spans := make([]nativeSpan, g.batchSize)
	for i := 0; i < g.batchSize; i++ {
		spans[i] = nativeSpan{
			SpanID:       randomHex(8),
			TraceID:      traceID,
			Name:         "chat-completion",
			Kind:         "llm",
			Model:        "gpt-4o-mini",
			Input:        g.text,
			Output:       g.text,
			InputTokens:  120,
			OutputTokens: 80,
		}
		g.seq++
	}
	data, err := json.Marshal(ingestRequest{Spans: spans})
	if err != nil {
		// Span fields are all static/random strings; marshaling cannot fail.
		panic(err)
	}
	return data
}

// randomHex returns a lowercase hex string of 2*n characters, matching the
// Ingest API's span_id (16 chars / 8 bytes) and trace_id (32 chars / 16
// bytes) validation.
func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

// --- writer metrics ---

// writerSnapshot holds the Writer Service's Lake-commit Prometheus metrics
// at a point in time.
type writerSnapshot struct {
	ok            bool
	spansWritten  float64
	commitCount   float64
	commitSumSecs float64
}

func fetchWriterMetrics(url string) writerSnapshot {
	if url == "" {
		return writerSnapshot{}
	}
	resp, err := http.Get(url)
	if err != nil {
		log.Printf("loadtest: writer metrics unavailable (%v); skipping Lake commit report", err)
		return writerSnapshot{}
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		log.Printf("loadtest: writer metrics returned %s; skipping Lake commit report", resp.Status)
		return writerSnapshot{}
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("loadtest: failed to read writer metrics (%v); skipping Lake commit report", err)
		return writerSnapshot{}
	}

	snap := writerSnapshot{ok: true}
	for _, line := range strings.Split(string(body), "\n") {
		if strings.HasPrefix(line, "#") || line == "" {
			continue
		}
		switch {
		case strings.HasPrefix(line, "omneval_writer_spans_written_total"):
			snap.spansWritten += lastFloatField(line)
		case strings.HasPrefix(line, "omneval_writer_lake_write_duration_seconds_count"):
			snap.commitCount += lastFloatField(line)
		case strings.HasPrefix(line, "omneval_writer_lake_write_duration_seconds_sum"):
			snap.commitSumSecs += lastFloatField(line)
		}
	}
	return snap
}

// lastFloatField parses the value field of a Prometheus exposition line
// ("metric_name{labels} value").
func lastFloatField(line string) float64 {
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return 0
	}
	v, err := strconv.ParseFloat(fields[len(fields)-1], 64)
	if err != nil {
		return 0
	}
	return v
}

func printWriterDelta(before, after writerSnapshot, wall time.Duration) {
	fmt.Println("=== Writer Lake commit stats ===")
	if !before.ok || !after.ok {
		fmt.Println("(unavailable — pass -writer-metrics-url pointing at the Writer's :9091/metrics)")
		return
	}

	spansWritten := after.spansWritten - before.spansWritten
	commits := after.commitCount - before.commitCount
	commitSecs := after.commitSumSecs - before.commitSumSecs

	fmt.Printf("spans written:    %.0f\n", spansWritten)
	fmt.Printf("spans/sec:        %.1f\n", spansWritten/wall.Seconds())
	fmt.Printf("lake commits:     %.0f\n", commits)
	if commits > 0 {
		fmt.Printf("avg spans/commit: %.1f\n", spansWritten/commits)
		fmt.Printf("avg commit time:  %s\n", time.Duration(commitSecs/commits*float64(time.Second)).Round(time.Millisecond))
		fmt.Printf("commits/sec:      %.2f\n", commits/wall.Seconds())
	}
}
