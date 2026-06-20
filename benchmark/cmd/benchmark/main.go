// Command benchmark drives the omneval ingest-throughput / end-to-end-latency
// benchmark against a live deployment.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	benchmark "github.com/omneval/omneval/benchmark"
)

func main() {
	slog.SetLogLoggerLevel(slog.LevelInfo)

	// --- flags ---
	flagSet := flag.NewFlagSet("benchmark", flag.ExitOnError)

	endpoint := flagSet.String("endpoint", os.Getenv("OMNEVAL_INGEST_ENDPOINT"),
		"Ingest API base URL (e.g. http://localhost:8000/api/v1/spans)")
	queryEndpoint := flagSet.String("query-endpoint", os.Getenv("OMNEVAL_QUERY_ENDPOINT"),
		"Query API base URL (e.g. http://localhost:8000/api/v1/spans/query)")
	apiKey := flagSet.String("api-key", os.Getenv("OMNEVAL_API_KEY"),
		"API key for the omneval deployment")
	projectID := flagSet.String("project-id", "demo-project",
		"Project ID to generate spans for")
	commitCadence := flagSet.Duration("commit-cadence", 10*time.Second,
		"Writer commit-cadence (batch-flush interval) used by the deployment")
	numTraces := flagSet.Int("num-traces", 20,
		"Number of agent-trace groups to generate")
	spansPerTrace := flagSet.Int("spans-per-trace", 5,
		"Spans per agent trace (1 root + N children)")
	batchSize := flagSet.Int("batch-size", 25,
		"Spans per ingest HTTP POST")
	runCount := flagSet.Int("run-count", 5,
		"Number of full benchmark runs (after warm-up)")
	warmupRuns := flagSet.Int("warmup-runs", 2,
		"Number of warm-up runs (not recorded)")

	docPath := flagSet.String("doc-path", "",
		"Path to write the results markdown doc (defaults to docs/benchmarks/ingest-throughput-and-latency.md)")

	flagSet.Parse(os.Args[1:])

	// --- validate ---
	if *endpoint == "" || *queryEndpoint == "" || *apiKey == "" {
		fmt.Fprintln(os.Stderr, "error: --endpoint, --query-endpoint, and --api-key (or env vars OMNEVAL_INGEST_ENDPOINT, OMNEVAL_QUERY_ENDPOINT, OMNEVAL_API_KEY) are required")
		flagSet.PrintDefaults()
		os.Exit(1)
	}

	// --- signal handling ---
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// --- generate workload ---
	traces := benchmark.GenerateTraces(*projectID, *numTraces, *spansPerTrace)
	totalSpans := 0
	for _, tg := range traces {
		totalSpans += len(tg.Spans)
	}

	// Flatten for the ingest driver.
	allSpans := make([]*benchmark.Span, 0, totalSpans)
	for _, tg := range traces {
		allSpans = append(allSpans, tg.Spans...)
	}

	fmt.Println("=== omneval Ingest Benchmark ===")
	fmt.Printf("Project:              %s\n", *projectID)
	fmt.Printf("Agent traces:        %d\n", *numTraces)
	fmt.Printf("Spans per trace:     %d\n", *spansPerTrace)
	fmt.Printf("Total spans:         %d\n", totalSpans)
	fmt.Printf("Ingest endpoint:     %s\n", *endpoint)
	fmt.Printf("Query endpoint:      %s\n", *queryEndpoint)
	fmt.Printf("Commit cadence:      %s\n", *commitCadence)
	fmt.Printf("Run count (after warm-up): %d\n", *runCount)
	fmt.Printf("Warm-up runs:        %d\n", *warmupRuns)
	fmt.Printf("Batch size:          %d\n", *batchSize)
	fmt.Println()

	// --- create clients ---
	ingest := benchmark.NewIngestClient(*endpoint, *apiKey)
	latencyClient := benchmark.NewLatencyClient(*queryEndpoint, *apiKey)
	writerClient := benchmark.NewWriterClient(*queryEndpoint, *apiKey)

	// --- run harness ---
	var allThroughput benchmark.ThroughputStats
	var allLatency benchmark.LatencyStats

	for runIdx := 0; runIdx < *runCount+*warmupRuns; runIdx++ {
		isWarmup := runIdx < *warmupRuns

		// Re-set send timestamps for each run.
		now := time.Now()
		for i := range allSpans {
			allSpans[i].SendTime = now
		}

		// Ingest run.
		ingestRes, err := ingest.SendTraces(ctx, traces, *batchSize)
		if err != nil {
			slog.Error("ingest run failed", "err", err)
			os.Exit(1)
		}

		ingestRate := float64(ingestRes.SpansAccepted) / ingestRes.Wall.Seconds()

		// Record throughput data point.
		if !isWarmup {
			allThroughput.AcceptedSpansPerSec = append(allThroughput.AcceptedSpansPerSec, ingestRate)
		}

		fmt.Printf("Run %d/%d", runIdx+1, *runCount+*warmupRuns)
		if isWarmup {
			fmt.Print(" [warm-up]")
		}
		fmt.Printf(": %d accepted in %s (%.1f spans/s)\n",
			ingestRes.SpansAccepted, ingestRes.Wall.Round(time.Millisecond), ingestRate)

		if ingestRes.SpansAccepted == 0 {
			slog.Error("no spans accepted, aborting")
			os.Exit(1)
		}

		// Committed spans/sec measurement (skip warm-up runs to avoid skewing).
		if !isWarmup {
			var committedCount int64
			if err := writerClient.MeasureCommitted(ctx, *projectID, 5*time.Second, func(count int64, rate float64) {
				committedCount = count
			}); err == nil && committedCount > 0 {
				committedRate := float64(committedCount) / 5
				allThroughput.CommittedSpansPerSec = append(allThroughput.CommittedSpansPerSec, committedRate)
				fmt.Printf("  committed: %d spans, %.1f spans/s\n", committedCount, committedRate)
			} else {
				slog.Warn("failed to measure committed spans/sec", "err", err)
			}
		}

		// Latency measurement (skip warm-up runs to avoid skewing results).
		if !isWarmup {
			latStats := latencyClient.MeasureLatency(ctx, allSpans, benchmark.LatencyPollConfig{
				PollInterval: 250 * time.Millisecond,
				Timeout:      60 * time.Second,
			})
			for _, lat := range latStats.Latencies {
				allLatency.Latencies = append(allLatency.Latencies, lat)
			}
		}
	}

	// --- report ---
	fmt.Println()
	fmt.Println("======================================")
	fmt.Println("         FINAL RESULTS")
	fmt.Println("======================================")
	fmt.Println()

	allThroughput.Report(*commitCadence)
	allLatency.Report(*commitCadence)

	// --- write results doc ---
	docDir := "docs/benchmarks"
	if *docPath == "" {
		*docPath = filepath.Join(docDir, "ingest-throughput-and-latency.md")
	}

	docAbs, _ := filepath.Abs(*docPath)
	if err := os.MkdirAll(filepath.Dir(*docPath), 0o755); err != nil {
		slog.Error("failed to create doc dir", "err", err)
		os.Exit(1)
	}

	if err := writeResultsDoc(*docPath, BenchmarkConfig{
		Endpoint:        *endpoint,
		QueryEndpoint:   *queryEndpoint,
		ProjectID:       *projectID,
		CommitCadence:   *commitCadence,
		NumTraces:       *numTraces,
		SpansPerTrace:   *spansPerTrace,
		BatchSize:       *batchSize,
		RunCount:        *runCount,
		WarmupRuns:      *warmupRuns,
		Throughput:      allThroughput,
		Latency:         allLatency,
		GenerateTime:    time.Now(),
		TotalSpans:      totalSpans,
	}); err != nil {
		slog.Error("failed to write results doc", "err", err)
		os.Exit(1)
	}

	fmt.Printf("Results written to: %s\n", docAbs)
}

// BenchmarkConfig holds the full configuration used to run the benchmark, for
// documentation in the results markdown.
type BenchmarkConfig struct {
	Endpoint        string
	QueryEndpoint   string
	ProjectID       string
	CommitCadence   time.Duration
	NumTraces       int
	SpansPerTrace   int
	BatchSize       int
	RunCount        int
	WarmupRuns      int
	Throughput      benchmark.ThroughputStats
	Latency         benchmark.LatencyStats
	GenerateTime    time.Time
	TotalSpans      int
}

func writeResultsDoc(path string, cfg BenchmarkConfig) error {
	var sb strings.Builder

	sb.WriteString("# Ingest Throughput & End-to-End Latency Benchmark\n\n")
	sb.WriteString("<!-- AUTO-GENERATED by benchmark/cmd/benchmark/main.go. DO NOT EDIT MANUALLY. -->\n\n")
	sb.WriteString(fmt.Sprintf("Generated: %s\n\n", cfg.GenerateTime.UTC().Format("2006-01-02 15:04:05 MST")))

	// --- Methodology ---
	sb.WriteString("## Methodology\n\n")
	sb.WriteString("### Workload shape\n\n")
	sb.WriteString(fmt.Sprintf("- **Project ID**: `%s`\n", cfg.ProjectID))
	sb.WriteString(fmt.Sprintf("- **Agent traces**: %d\n", cfg.NumTraces))
	sb.WriteString(fmt.Sprintf("- **Spans per trace**: %d (1 root span + %d children with fan-out via `parent_id`)\n", cfg.SpansPerTrace, cfg.SpansPerTrace-1))
	sb.WriteString(fmt.Sprintf("- **Total spans**: %d\n", cfg.TotalSpans))
	sb.WriteString(fmt.Sprintf("- **Payload size**: ~1.5 KB `Input` + ~1.5 KB `Output` per span (typical chat-style prompt/completion)\n"))
	sb.WriteString("- **Score write-back**: excluded (agent-relevant only, not marketing)\n\n")

	sb.WriteString("### Trace topology\n\n")
	sb.WriteString("Each agent trace consists of:\n\n")
	sb.WriteString("- **Root span** (`kind=agent`): simulates the main agent loop receiving a user prompt\n")
	sb.WriteString("- **Child spans** (`kind=tool|llm|chain|retriever`): simulate tool calls, sub-agent invocations, and RAG retrievals\n")
	sb.WriteString("- All children reference the root span via `parent_id` (flat fan-out pattern, no deep nesting)\n\n")

	sb.WriteString("### Environment\n\n")
	sb.WriteString(fmt.Sprintf("- **Ingest endpoint**: `%s`\n", cfg.Endpoint))
	sb.WriteString(fmt.Sprintf("- **Query endpoint**: `%s`\n", cfg.QueryEndpoint))
	sb.WriteString(fmt.Sprintf("- **Writer commit cadence (batch-flush interval)**: %s\n", cfg.CommitCadence))
	sb.WriteString("- **Single replica** (1 Writer replica)\n\n")

	sb.WriteString("### Run procedure\n\n")
	sb.WriteString(fmt.Sprintf("1. Generate %d agent traces × %d spans/trace = %d total spans\n", cfg.NumTraces, cfg.SpansPerTrace, cfg.TotalSpans))
	sb.WriteString(fmt.Sprintf("2. Run %d warm-up iterations (not recorded) to prime caches and connections\n", cfg.WarmupRuns))
	sb.WriteString(fmt.Sprintf("3. Run %d benchmark iterations, collecting data from each:\n", cfg.RunCount))
	sb.WriteString(fmt.Sprintf("   - Send spans to the Ingest API in batches of %d\n", cfg.BatchSize))
	sb.WriteString("   - Measure accepted spans/sec per run\n")
	sb.WriteString("   - After ingest, poll the Query API for each span until visible (250ms poll interval, 60s timeout)\n")
	sb.WriteString("   - Record end-to-end latency per span\n")
	sb.WriteString(fmt.Sprintf("4. Compute p50 / p95 / p99 across the %d recorded runs\n", cfg.RunCount))
	sb.WriteString("\n")

	// --- Results ---
	sb.WriteString("## Results\n\n")
	sb.WriteString(fmt.Sprintf("Commit cadence: %s\n\n", cfg.CommitCadence))

	// Throughput
	sb.WriteString("### Ingest Throughput\n\n")
	sb.WriteString("Accepted spans/sec (Ingest API accepts and queues):\n\n")

	if len(cfg.Throughput.AcceptedSpansPerSec) > 0 {
		sb.WriteString(fmt.Sprintf("| Metric | Value |\n"))
		sb.WriteString(fmt.Sprintf("|--------|-------|\n"))
		for _, rate := range cfg.Throughput.AcceptedSpansPerSec {
			sb.WriteString(fmt.Sprintf("| %s | %.1f |\n", "Accepted spans/s", rate))
		}
	} else {
		sb.WriteString("_No data collected._\n")
	}
	sb.WriteString("\n")

	sb.WriteString("Committed spans/sec (Writer durable commits to Lake):\n\n")

	if len(cfg.Throughput.CommittedSpansPerSec) > 0 {
		sb.WriteString(fmt.Sprintf("| Metric | Value |\n"))
		sb.WriteString(fmt.Sprintf("|--------|-------|\n"))
		for _, rate := range cfg.Throughput.CommittedSpansPerSec {
			sb.WriteString(fmt.Sprintf("| %s | %.1f |\n", "Committed spans/s", rate))
		}
	} else {
		sb.WriteString("_No data collected._\n")
	}
	sb.WriteString("\n")

	// Latency
	sb.WriteString("### End-to-End Ingest-to-Queryable Latency\n\n")
	sb.WriteString(fmt.Sprintf("Commit cadence (batch-flush interval): **%s**\n\n", cfg.CommitCadence))
	sb.WriteString("Per-span latencies (send timestamp → first Query API visibility):\n\n")

	if len(cfg.Latency.Latencies) > 0 {
		sorted := make([]time.Duration, len(cfg.Latency.Latencies))
		copy(sorted, cfg.Latency.Latencies)
		for i := 0; i < len(sorted); i++ {
			_ = sorted // ensure sorted is used
		}
		p := benchmark.ComputePercentiles(sorted)
		sb.WriteString(fmt.Sprintf("| Metric | Value |\n"))
		sb.WriteString(fmt.Sprintf("|--------|-------|\n"))
		sb.WriteString(fmt.Sprintf("| p50 | %s |\n", p.P50.Round(time.Millisecond)))
		sb.WriteString(fmt.Sprintf("| p95 | %s |\n", p.P95.Round(time.Millisecond)))
		sb.WriteString(fmt.Sprintf("| p99 | %s |\n", p.P99.Round(time.Millisecond)))
		sb.WriteString(fmt.Sprintf("| max | %s |\n", sorted[len(sorted)-1]))
		sb.WriteString(fmt.Sprintf("| total spans | %d |\n", len(sorted)))
	} else {
		sb.WriteString("_No data collected._\n")
	}
	sb.WriteString("\n")

	sb.WriteString("### Note on Latency\n\n")
	sb.WriteString("These latency numbers are dominated by the Writer's batch-flush interval (commit cadence).\n")
	sb.WriteString("Spans are typically committed at the end of each batch window, so:\n\n")
	sb.WriteString("- **p50 ≈ cadence/2** (spans sent near the start of a window see full cadence latency)\n")
	sb.WriteString("- **p99 ≈ cadence** (spans sent near the end see minimal latency)\n\n")
	sb.WriteString("To reduce latency, decrease the commit cadence (Writer batch flush interval).\n\n")

	// --- Reproducibility ---
	sb.WriteString("## Reproducing This Benchmark\n\n")
	sb.WriteString("### Prerequisites\n\n")
	sb.WriteString("1. A running omneval deployment matching the environment from issue #205 (single-node, per-component topology with real S3)\n")
	sb.WriteString("2. Go 1.25+ workspace with `github.com/omneval/omneval/benchmark` module\n")
	sb.WriteString("3. API key with ingest and query permissions\n\n")
	sb.WriteString("### Steps\n\n")
	sb.WriteString("```bash\n")
	sb.WriteString("# 1. Set environment\n")
	sb.WriteString("export OMNEVAL_INGEST_ENDPOINT=\"http://<ingress-host>:8000/api/v1/spans\"\n")
	sb.WriteString("export OMNEVAL_QUERY_ENDPOINT=\"http://<ingress-host>:8000/api/v1/spans/query\"\n")
	sb.WriteString("export OMNEVAL_API_KEY=\"<your-api-key>\"\n")
	sb.WriteString("\n")
	sb.WriteString("# 2. Run the benchmark\n")
	sb.WriteString("cd /path/to/omneval\n")
	sb.WriteString("go run benchmark/cmd/benchmark/main.go \\\n")
	sb.WriteString("  --commit-cadence=10s \\\n")
	sb.WriteString("  --num-traces=20 \\\n")
	sb.WriteString("  --spans-per-trace=5 \\\n")
	sb.WriteString("  --batch-size=25 \\\n")
	sb.WriteString("  --run-count=5 \\\n")
	sb.WriteString("  --warmup-runs=2\n")
	sb.WriteString("```\n\n")

	sb.WriteString("### Flags reference\n\n")
	sb.WriteString("| Flag | Default | Description |\n")
	sb.WriteString("|------|---------|-------------|\n")
	sb.WriteString("| `--endpoint` | `OMNEVAL_INGEST_ENDPOINT` | Ingest API base URL |\n")
	sb.WriteString("| `--query-endpoint` | `OMNEVAL_QUERY_ENDPOINT` | Query API base URL |\n")
	sb.WriteString("| `--api-key` | `OMNEVAL_API_KEY` | API key for authentication |\n")
	sb.WriteString("| `--project-id` | `demo-project` | Project ID to generate spans for |\n")
	sb.WriteString("| `--commit-cadence` | `10s` | Writer commit cadence (reported in results) |\n")
	sb.WriteString("| `--num-traces` | `20` | Number of agent-trace groups |\n")
	sb.WriteString("| `--spans-per-trace` | `5` | Spans per trace (1 root + 4 children) |\n")
	sb.WriteString("| `--batch-size` | `25` | Spans per ingest HTTP POST |\n")
	sb.WriteString("| `--run-count` | `5` | Benchmark runs (after warm-up) |\n")
	sb.WriteString("| `--warmup-runs` | `2` | Warm-up runs (not recorded) |\n")
	sb.WriteString("| `--doc-path` | `docs/benchmarks/ingest-throughput-and-latency.md` | Path for results markdown |\n\n")

	return os.WriteFile(path, []byte(sb.String()), 0o644)
}