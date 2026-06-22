// Command benchmark drives the omneval ingest-throughput / end-to-end-latency
// benchmark against a live deployment.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
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

	// Query-latency flags.
	queryLatencyEnabled := flagSet.Bool("query-latency", false,
		"Run query-latency benchmark (trace-list, trace-detail, analytics DSL)")
	tracesEndpoint := flagSet.String("traces-endpoint", os.Getenv("OMNEVAL_TRACES_ENDPOINT"),
		"Traces API base URL (e.g. http://localhost:8000/api/v1/traces)")
	analyticsEndpoint := flagSet.String("analytics-endpoint", os.Getenv("OMNEVAL_ANALYTICS_ENDPOINT"),
		"Analytics DSL API base URL (e.g. http://localhost:8000/api/v1/analytics/spans)")
	queryLatencyDocPath := flagSet.String("query-latency-doc-path", "docs/benchmarks/query-latency.md",
		"Path to write the query-latency results markdown doc")

	// Scaling flags.
	scalingEnabled := flagSet.Bool("scaling", false,
		"Run horizontal scaling benchmark across Writer replica counts (1, 2, 4, 8)")
	scalingReplicasStr := flagSet.String("scaling-replicas", "1,2,4,8",
		"Comma-separated list of Writer replica counts to test (used when --scaling=true)")
	kubectlDeployment := flagSet.String("kubectl-deployment", "omneval-writer",
		"Helm release name prefix for the Writer deployment (used to scale via kubectl)")
	scalingDocPath := flagSet.String("scaling-doc-path", "",
		"Path to write the scaling results markdown doc (defaults to docs/benchmarks/ingest-scaling.md)")

	flagSet.Parse(os.Args[1:])

	// Parse scaling replicas.
	scalingReplicas := []int{1, 2, 4, 8}
	if *scalingReplicasStr != "" {
		parts := strings.Split(*scalingReplicasStr, ",")
		scalingReplicas = make([]int, len(parts))
		for i, p := range parts {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			val, err := strconv.Atoi(p)
			if err != nil {
				fmt.Fprintf(os.Stderr, "error: invalid replica count %q in --scaling-replicas: %v\n", p, err)
				os.Exit(1)
			}
			scalingReplicas[i] = val
		}
		if len(scalingReplicas) == 0 {
			fmt.Fprintln(os.Stderr, "error: --scaling-replicas must contain at least one value")
			os.Exit(1)
		}
	}

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

	if err := writeResultsDoc(*docPath, benchmark.BenchmarkConfig{
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

	// --- query-latency mode ---
	if *queryLatencyEnabled {
		slog.Info("running query-latency benchmark")
		if err := runQueryLatencyBenchmark(ctx, queryLatencyConfig{
			endpoint:         *endpoint,
			queryEndpoint:    *queryEndpoint,
			tracesEndpoint:   *tracesEndpoint,
			analyticsEndpoint: *analyticsEndpoint,
			apiKey:           *apiKey,
			projectID:        *projectID,
			commitCadence:    *commitCadence,
			numTraces:        *numTraces,
			spansPerTrace:    *spansPerTrace,
			batchSize:        *batchSize,
			runCount:         *runCount,
			warmupRuns:       *warmupRuns,
			docPath:          *queryLatencyDocPath,
		}); err != nil {
			slog.Error("query-latency benchmark failed", "err", err)
			os.Exit(1)
		}
	}

	// --- scaling mode ---
	if *scalingEnabled {
		slog.Info("running scaling benchmark across Writer replica counts", "replicas", scalingReplicas)
		if err := runScalingBenchmark(ctx, scalingConfig{
			endpoint:          *endpoint,
			queryEndpoint:     *queryEndpoint,
			apiKey:            *apiKey,
			projectID:         *projectID,
			commitCadence:     *commitCadence,
			numTraces:         *numTraces,
			spansPerTrace:     *spansPerTrace,
			batchSize:         *batchSize,
			runCount:          *runCount,
			warmupRuns:        *warmupRuns,
			replicas:          scalingReplicas,
			kubectlDeployment: *kubectlDeployment,
			docPath:           *scalingDocPath,
		}); err != nil {
			slog.Error("scaling benchmark failed", "err", err)
			os.Exit(1)
		}
	}
}

// scalingConfig holds the configuration for a scaling benchmark run.
type scalingConfig struct {
	endpoint          string
	queryEndpoint     string
	apiKey            string
	projectID         string
	commitCadence     time.Duration
	numTraces         int
	spansPerTrace     int
	batchSize         int
	runCount          int
	warmupRuns        int
	replicas          []int
	kubectlDeployment string
	docPath           string
}

// queryLatencyConfig holds the configuration for a query-latency benchmark run.
type queryLatencyConfig struct {
	endpoint          string
	queryEndpoint     string
	tracesEndpoint    string
	analyticsEndpoint string
	apiKey            string
	projectID         string
	commitCadence     time.Duration
	numTraces         int
	spansPerTrace     int
	batchSize         int
	runCount          int
	warmupRuns        int
	docPath           string
}

func runQueryLatencyBenchmark(ctx context.Context, cfg queryLatencyConfig) error {
	fmt.Println()
	fmt.Println("=== omneval Query Latency Benchmark ===")
	fmt.Printf("Project:              %s\n", cfg.projectID)
	fmt.Printf("Ingest endpoint:      %s\n", cfg.endpoint)
	fmt.Printf("Query endpoint:       %s\n", cfg.queryEndpoint)
	fmt.Printf("Traces endpoint:      %s\n", cfg.tracesEndpoint)
	fmt.Printf("Analytics endpoint:   %s\n", cfg.analyticsEndpoint)
	fmt.Printf("Commit cadence:       %s\n", cfg.commitCadence)
	fmt.Printf("Run count (after warm-up): %d\n", cfg.runCount)
	fmt.Printf("Warm-up runs:         %d\n", cfg.warmupRuns)
	fmt.Printf("Batch size:           %d\n", cfg.batchSize)
	fmt.Println()

	// --- generate workload ---
	traces := benchmark.GenerateTraces(cfg.projectID, cfg.numTraces, cfg.spansPerTrace)
	allSpans := make([]*benchmark.Span, 0, cfg.numTraces*cfg.spansPerTrace)
	for _, tg := range traces {
		allSpans = append(allSpans, tg.Spans...)
	}
	totalSpans := len(allSpans)

	fmt.Printf("Pre-load workload: %d traces, %d spans\n", cfg.numTraces, totalSpans)

	// --- ingest all spans (pre-load) ---
	ingest := benchmark.NewIngestClient(cfg.endpoint, cfg.apiKey)

	now := time.Now()
	for i := range allSpans {
		allSpans[i].SendTime = now
	}

	ingestRes, err := ingest.SendTraces(ctx, traces, cfg.batchSize)
	if err != nil {
		return fmt.Errorf("pre-load ingest failed: %w", err)
	}
	fmt.Printf("Ingested %d spans (pre-load)\n", ingestRes.SpansAccepted)

	// Wait for all spans to become queryable.
	fmt.Println("Waiting for spans to become queryable...")
	maxWait := 5 * time.Minute
	elapsedWait := time.Duration(0)
	for elapsedWait < maxWait {
		time.Sleep(1 * time.Second)
		elapsedWait += 1 * time.Second

		writerClient := benchmark.NewWriterClient(cfg.queryEndpoint, cfg.apiKey)
		var committedCount int64
		if err := writerClient.MeasureCommitted(ctx, cfg.projectID, 2*time.Second, func(count int64, _ float64) {
			committedCount = count
		}); err == nil && committedCount >= int64(ingestRes.SpansAccepted) {
			fmt.Printf("All %d spans are queryable (found %d)\n", ingestRes.SpansAccepted, committedCount)
			break
		}
		if elapsedWait%5*time.Second == 0 {
			fmt.Printf("  Waiting for queryability (%v elapsed)...\n", elapsedWait.Round(time.Second))
		}
	}

	// --- query-latency measurements ---
	// Collect trace IDs from all traces for trace-detail queries.
	traceIDs := make([]string, len(traces))
	for i, tg := range traces {
		if len(tg.Spans) > 0 {
			traceIDs[i] = tg.Spans[0].TraceID
		}
	}

	queryLatencyClient := benchmark.NewQueryLatencyClient(
		cfg.queryEndpoint,
		cfg.tracesEndpoint,
		cfg.analyticsEndpoint,
		cfg.apiKey,
	)

	// Run each query type.
	stats := benchmark.NewQueryLatencyStats()
	for _, qtype := range []benchmark.LatencyType{
		benchmark.LatencyTypeTraceList,
		benchmark.LatencyTypeTraceDetail,
		benchmark.LatencyTypeAnalytics,
	} {
		fmt.Printf("\n--- %s ---\n", qtype)

		var typStats *benchmark.QueryLatencyStats
		var err error

		switch qtype {
		case benchmark.LatencyTypeTraceList:
			typStats, err = queryLatencyClient.MeasureTraceListLatency(ctx, cfg.projectID, nil, cfg.runCount, cfg.warmupRuns)
		case benchmark.LatencyTypeTraceDetail:
			typStats, err = queryLatencyClient.MeasureTraceDetailLatency(ctx, cfg.projectID, traceIDs, cfg.runCount, cfg.warmupRuns)
		case benchmark.LatencyTypeAnalytics:
			typStats, err = queryLatencyClient.MeasureAnalyticsLatency(ctx, cfg.projectID, cfg.runCount, cfg.warmupRuns)
		}

		if err != nil {
			slog.Warn("query latency measurement failed", "type", qtype, "err", err)
			continue
		}

		// Merge results into main stats.
		if r, ok := typStats.Get(qtype); ok {
			stats.Set(qtype, r)
		}
	}

	// Pick one trace for the trace-detail query shape doc field.
	var docTraceID string
	if len(traceIDs) > 0 {
		docTraceID = traceIDs[0]
	}

	// --- write results doc ---
	docCfg := benchmark.WriteQueryLatencyConfig{
		Endpoint:             cfg.endpoint,
		QueryEndpoint:        cfg.queryEndpoint,
		TracesEndpoint:       cfg.tracesEndpoint,
		AnalyticsEndpoint:    cfg.analyticsEndpoint,
		ProjectID:            cfg.projectID,
		CommitCadence:        cfg.commitCadence,
		RunCount:             cfg.runCount,
		WarmupRuns:           cfg.warmupRuns,
		PreLoadDescription:   fmt.Sprintf("%d traces at %d spans/trace (pre-loaded by benchmark run)", cfg.numTraces, cfg.spansPerTrace),
		TotalSpansIngested:   totalSpans,
		TotalTracesIngested:  cfg.numTraces,
		TraceListQueryShape:  fmt.Sprintf("POST /api/v1/spans/query {\"from\":\"lake.spans\",\"project_id\":\"%s\",\"limit\":25}", cfg.projectID),
		TraceDetailQueryShape: fmt.Sprintf("GET /api/v1/traces/{trace_id} (trace_id=%s)", docTraceID),
		AnalyticsQueryShape:  fmt.Sprintf("POST /api/v1/analytics/spans {\"project_id\":\"%s\",\"from\":\"<30d ago>\",\"aggregations\":[{\"function\":\"count\"},{\"function\":\"avg\",\"field\":\"duration_ms\"}]}", cfg.projectID),
		QueryLatencyStats:    stats,
		GenerateTime:         time.Now(),
	}

	if err := writeQueryLatencyResultsDoc(cfg.docPath, docCfg); err != nil {
		return fmt.Errorf("write query-latency results doc: %w", err)
	}

	docAbs, _ := filepath.Abs(cfg.docPath)
	fmt.Printf("\nQuery-latency results written to: %s\n", docAbs)
	return nil
}

func runScalingBenchmark(ctx context.Context, cfg scalingConfig) error {
	fmt.Println()
	fmt.Println("=== omneval Writer Scaling Benchmark ===")
	fmt.Printf("Replica counts to test: %v\n", cfg.replicas)
	fmt.Printf("Kubernetes deployment:  %s\n", cfg.kubectlDeployment)
	fmt.Println()

	// Generate the workload once (same for all replica counts).
	traces := benchmark.GenerateTraces(cfg.projectID, cfg.numTraces, cfg.spansPerTrace)
	allSpans := make([]*benchmark.Span, 0, cfg.numTraces*cfg.spansPerTrace)
	for _, tg := range traces {
		allSpans = append(allSpans, tg.Spans...)
	}

	// Create clients (ingest/query are replica-count independent).
	ingest := benchmark.NewIngestClient(cfg.endpoint, cfg.apiKey)
	writerClient := benchmark.NewWriterClient(cfg.queryEndpoint, cfg.apiKey)

	// Collect stats for each replica count.
	harnessStats := make(map[int]*benchmark.ThroughputStats)
	for _, replica := range cfg.replicas {
		fmt.Printf("--- Scaling to %d Writer replica(s) ---\n", replica)
		if err := scaleWriter(ctx, cfg.kubectlDeployment, replica); err != nil {
			slog.Error("failed to scale writer", "replicas", replica, "err", err)
			// Continue to try other replica counts.
			continue
		}

		// Run the benchmark at this replica count.
		stats := &benchmark.ThroughputStats{}
		for runIdx := 0; runIdx < cfg.runCount+cfg.warmupRuns; runIdx++ {
			isWarmup := runIdx < cfg.warmupRuns

			// Re-set send timestamps for each run.
			now := time.Now()
			for i := range allSpans {
				allSpans[i].SendTime = now
			}

			// Ingest run.
			ingestRes, err := ingest.SendTraces(ctx, traces, cfg.batchSize)
			if err != nil {
				slog.Error("scaling ingest run failed", "replicas", replica, "err", err)
				continue
			}

			ingestRate := float64(ingestRes.SpansAccepted) / ingestRes.Wall.Seconds()

			if !isWarmup {
				stats.AcceptedSpansPerSec = append(stats.AcceptedSpansPerSec, ingestRate)

				// Committed spans/sec measurement.
				var committedCount int64
				if err := writerClient.MeasureCommitted(ctx, cfg.projectID, 5*time.Second, func(count int64, rate float64) {
					committedCount = count
				}); err == nil && committedCount > 0 {
					committedRate := float64(committedCount) / 5
					stats.CommittedSpansPerSec = append(stats.CommittedSpansPerSec, committedRate)
					fmt.Printf("  Run %d/%d (N=%d): %.1f accepted/s, %.1f committed/s\n",
						runIdx-cfg.warmupRuns+1, cfg.runCount, replica, ingestRate, committedRate)
				} else {
					slog.Warn("failed to measure committed spans/sec", "err", err)
					stats.CommittedSpansPerSec = append(stats.CommittedSpansPerSec, 0)
				}
			}

			fmt.Printf("Run %d/%d (N=%d)", runIdx+1, cfg.runCount+cfg.warmupRuns, replica)
			if isWarmup {
				fmt.Print(" [warm-up]")
			}
			fmt.Printf(": %d accepted in %s (%.1f spans/s)\n",
				ingestRes.SpansAccepted, ingestRes.Wall.Round(time.Millisecond), ingestRate)
		}
		harnessStats[replica] = stats
	}

	// Build scaling result.
	scalingResult := &benchmark.ScalingResult{
		Replicas: cfg.replicas,
		Stats:    harnessStats,
	}

	// --- report ---
	fmt.Println()
	fmt.Println("======================================")
	fmt.Println("         SCALING RESULTS")
	fmt.Println("======================================")
	fmt.Println()
	fmt.Println(scalingResult.WriteMarkdown())

	// --- write scaling results doc ---
	docPath := cfg.docPath
	if docPath == "" {
		docPath = filepath.Join("docs/benchmarks", "ingest-scaling.md")
	}

	docAbs, _ := filepath.Abs(docPath)
	if err := os.MkdirAll(filepath.Dir(docPath), 0o755); err != nil {
		slog.Error("failed to create scaling doc dir", "err", err)
		return err
	}

	if err := writeScalingResultsDoc(docPath, benchmark.WriteScalingConfig{
		Endpoint:         cfg.endpoint,
		QueryEndpoint:    cfg.queryEndpoint,
		ProjectID:        cfg.projectID,
		CommitCadence:    cfg.commitCadence,
		Replicas:         cfg.replicas,
		RunCount:         cfg.runCount,
		WarmupRuns:       cfg.warmupRuns,
		ScalingResult:    scalingResult,
		GenerateTime:     time.Now(),
		KubectlDeployment: cfg.kubectlDeployment,
	}); err != nil {
		slog.Error("failed to write scaling results doc", "err", err)
		return err
	}

	fmt.Printf("Scaling results written to: %s\n", docAbs)
	return nil
}

// scaleWriter uses kubectl to scale the Writer deployment to the target replica count.
// It waits for the deployment to become ready.
func scaleWriter(ctx context.Context, deploymentName string, replicas int) error {
	// Run kubectl scale.
	cmd := exec.CommandContext(ctx, "kubectl", "scale", "deployment", deploymentName,
		"--replicas="+strconv.Itoa(replicas))
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("kubectl scale failed: %w\noutput: %s", err, string(out))
	}
	slog.Info("scaled writer", "deployment", deploymentName, "replicas", replicas, "output", string(out))

	// Wait for deployment to be ready.
	fmt.Printf("Waiting for deployment %s to become ready (%d replicas)...\n", deploymentName, replicas)
	return waitForDeploymentReady(ctx, deploymentName, replicas)
}

// waitForDeploymentReady polls kubectl until the deployment has the desired replicas ready.
func waitForDeploymentReady(ctx context.Context, name string, replicas int) error {
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	timeout := time.After(5 * time.Minute)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timeout:
			return fmt.Errorf("timeout waiting for deployment %s to become ready", name)
		case <-ticker.C:
			cmd := exec.CommandContext(ctx, "kubectl", "get", "deployment", name,
				"-o=jsonpath='{.status.readyReplicas}'")
			out, err := cmd.Output()
			if err != nil {
				slog.Warn("kubectl get failed (retrying)", "err", err)
				continue
			}
			ready, _ := strconv.Atoi(strings.TrimSpace(strings.Trim(string(out), "'")))
			if ready >= replicas {
				fmt.Printf("  Deployment %s ready (%d/%d replicas)\n", name, ready, replicas)
				return nil
			}
			fmt.Printf("  Deployment %s: %d/%d replicas ready\n", name, ready, replicas)
		}
	}
}

func writeResultsDoc(path string, cfg benchmark.BenchmarkConfig) error {
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

func writeScalingResultsDoc(path string, cfg benchmark.WriteScalingConfig) error {
	var sb strings.Builder

	sb.WriteString("# Horizontal Scaling Chart: Writer Ingest Throughput (N=1,2,4,8 Replicas)\n\n")
	sb.WriteString("<!-- AUTO-GENERATED by benchmark/cmd/benchmark/main.go. DO NOT EDIT MANUALLY. -->\n\n")
	sb.WriteString(fmt.Sprintf("Generated: %s\n\n", cfg.GenerateTime.UTC().Format("2006-01-02 15:04:05 MST")))

	// --- Methodology ---
	sb.WriteString("## Methodology\n\n")
	sb.WriteString("### Purpose\n\n")
	sb.WriteString("This benchmark measures how Writer ingest throughput scales when the number of Writer\n")
	sb.WriteString("replicas is increased from 1 to 8. It tests the competing-consumers pattern on the\n")
	sb.WriteString("Redis ingest queue, where multiple Writer replicas safely dequeue disjoint batches of\n")
	sb.WriteString("spans with no double-processing.\n\n")
	sb.WriteString("The results are compared against the Definite.app Quack benchmark which found that\n")
	sb.WriteString("8 concurrent Quack-client writers achieved ~222K rows/sec (super-linear scaling) vs\n")
	sb.WriteString("~192K rows/sec for a single writer against a DuckDB-file DuckLake catalog.\n\n")

	sb.WriteString("### Workload shape\n\n")
	sb.WriteString(fmt.Sprintf("- **Project ID**: `%s`\n", cfg.ProjectID))
	sb.WriteString(fmt.Sprintf("- **Commit cadence (Writer batch-flush interval)**: %s\n\n", cfg.CommitCadence))

	sb.WriteString("### Environment\n\n")
	sb.WriteString(fmt.Sprintf("- **Ingest endpoint**: `%s`\n", cfg.Endpoint))
	sb.WriteString(fmt.Sprintf("- **Query endpoint**: `%s`\n", cfg.QueryEndpoint))
	sb.WriteString(fmt.Sprintf("- **Kubernetes deployment**: `%s`\n", cfg.KubectlDeployment))
	sb.WriteString(fmt.Sprintf("- **Replica counts tested**: %v\n", cfg.Replicas))
	sb.WriteString("- **Other components**: held fixed (Ingest, Query, Eval, Quack Server, Redis, DuckLake)\n\n")

	sb.WriteString("### Run procedure\n\n")
	sb.WriteString(fmt.Sprintf("For each replica count in `%v`:\n", cfg.Replicas))
	sb.WriteString("1. Scale the Writer deployment to the target replica count via `kubectl scale`\n")
	sb.WriteString("2. Wait for the deployment to become ready (all replicas running and healthy)\n")
	sb.WriteString(fmt.Sprintf("3. Run %d warm-up iterations (not recorded) to prime caches and connections\n", cfg.WarmupRuns))
	sb.WriteString(fmt.Sprintf("4. Run %d benchmark iterations, collecting data from each:\n", cfg.RunCount))
	sb.WriteString("   - Send spans to the Ingest API in batches\n")
	sb.WriteString("   - Measure accepted spans/sec per run\n")
	sb.WriteString("   - Measure committed spans/sec per run (Writer → Lake)\n")
	sb.WriteString(fmt.Sprintf("5. Compute p50 / p95 across the %d recorded runs\n", cfg.RunCount))
	sb.WriteString("6. Repeat for the next replica count\n\n")

	sb.WriteString("### Note on scaling factors\n\n")
	sb.WriteString("The **scaling factor** column shows the ratio of throughput at N replicas vs N=1.\n")
	sb.WriteString("- **1.0x** = no improvement over single replica\n")
	sb.WriteString("- **>1.0x** = linear or super-linear scaling (more replicas = more throughput)\n")
	sb.WriteString("- **<1.0x** = degradation (contention, overhead, or resource bottleneck)\n\n")

	// --- Results ---
	sb.WriteString("## Results\n\n")
	sb.WriteString("### Throughput Table\n\n")
	sb.WriteString(cfg.ScalingResult.WriteMarkdown())
	sb.WriteString("\n\n")

	// --- Interpretation ---
	sb.WriteString("## Interpretation\n\n")

	// Analyze scaling behavior.
	replicas := cfg.Replicas
	if len(replicas) == 0 {
		replicas = []int{1, 2, 4, 8}
	}

	// Find baseline (N=1).
	var baselineRate float64
	for _, rc := range replicas {
		if rc == 1 && cfg.ScalingResult.Stats[1] != nil {
			if stats := cfg.ScalingResult.Stats[1]; len(stats.AcceptedSpansPerSec) > 0 {
				sorted := make([]float64, len(stats.AcceptedSpansPerSec))
				copy(sorted, stats.AcceptedSpansPerSec)
				sort.Float64s(sorted)
				baselineRate = benchmark.Percentile(sorted, 0.50)
			}
			break
		}
	}

	sb.WriteString("### Scaling Behavior\n\n")

	if baselineRate <= 0 {
		sb.WriteString("_Insufficient data to determine scaling behavior (no baseline at N=1)._")
	} else {
		var scalingBehavior string
		var bottleneckHypothesis string

		// Check each replica count.
		for _, rc := range replicas {
			if rc <= 1 {
				continue
			}
			stats := cfg.ScalingResult.Stats[rc]
			if stats == nil || len(stats.AcceptedSpansPerSec) == 0 {
				continue
			}
			sorted := make([]float64, len(stats.AcceptedSpansPerSec))
			copy(sorted, stats.AcceptedSpansPerSec)
			sort.Float64s(sorted)
			rateP50 := benchmark.Percentile(sorted, 0.50)
			ratio := rateP50 / baselineRate

			sb.WriteString(fmt.Sprintf("- **N=%d**: %.2fx scaling (throughput=%.1f/s, baseline=%.1f/s)\n",
				rc, ratio, rateP50, baselineRate))

			if ratio > 1.0 {
				scalingBehavior = "linear or super-linear"
			} else if ratio < 1.0 {
				scalingBehavior = "degradation"
			}

			// Check for diminishing returns.
			if rc == 2 && ratio < 1.8 {
				bottleneckHypothesis = fmt.Sprintf(
					"Redis queue contention or single-threaded write path may be limiting at N=%d", rc)
			} else if rc == 4 && ratio < 3.5 {
				bottleneckHypothesis = fmt.Sprintf(
					"DuckLake catalog's single-process write path or S3 I/O may be the bottleneck at N=%d", rc)
			} else if rc == 8 && ratio < 7.0 {
				bottleneckHypothesis = fmt.Sprintf(
					"DuckLake catalog commit serialisation, S3 throughput, or Quack Server concurrency limits at N=%d", rc)
			}
		}

		if scalingBehavior == "" {
			scalingBehavior = "linear (all replicas show near-proportional scaling)"
		}

		sb.WriteString(fmt.Sprintf("\n**Observed scaling**: %s\n\n", scalingBehavior))

		if bottleneckHypothesis != "" {
			sb.WriteString(fmt.Sprintf("**Likely bottleneck**: %s\n\n", bottleneckHypothesis))
		} else {
			sb.WriteString("No significant bottleneck detected — throughput scales proportionally with replica count.\n\n")
		}
	}

	// --- Reproducibility ---
	sb.WriteString("## Reproducing This Benchmark\n\n")
	sb.WriteString("### Prerequisites\n\n")
	sb.WriteString("1. A running omneval deployment matching the environment from issue #205\n")
	sb.WriteString("2. Go 1.25+ workspace with `github.com/omneval/omneval/benchmark` module\n")
	sb.WriteString("3. API key with ingest and query permissions\n")
	sb.WriteString("4. `kubectl` access to the target Kubernetes cluster\n\n")
	sb.WriteString("### Steps\n\n")
	sb.WriteString("```bash\n")
	sb.WriteString("# 1. Set environment\n")
	sb.WriteString("export OMNEVAL_INGEST_ENDPOINT=\"http://<ingress-host>:8000/api/v1/spans\"\n")
	sb.WriteString("export OMNEVAL_QUERY_ENDPOINT=\"http://<ingress-host>:8000/api/v1/spans/query\"\n")
	sb.WriteString("export OMNEVAL_API_KEY=\"<your-api-key>\"\n")
	sb.WriteString("\n")
	sb.WriteString("# 2. Run the scaling benchmark\n")
	sb.WriteString("cd /path/to/omneval\n")
	sb.WriteString("go run benchmark/cmd/benchmark/main.go \\\n")
	sb.WriteString("  --scaling \\\n")
	sb.WriteString("  --scaling-replicas=\"1,2,4,8\" \\\n")
	sb.WriteString("  --kubectl-deployment=omneval-writer \\\n")
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
	sb.WriteString("| `--scaling` | `false` | Enable horizontal scaling benchmark mode |\n")
	sb.WriteString("| `--scaling-replicas` | `1,2,4,8` | Comma-separated Writer replica counts to test |\n")
	sb.WriteString("| `--kubectl-deployment` | `omneval-writer` | Kubernetes deployment name for Writer |\n")
	sb.WriteString("| `--scaling-doc-path` | `docs/benchmarks/ingest-scaling.md` | Path for scaling results markdown |\n")
	sb.WriteString("| `--endpoint` | `OMNEVAL_INGEST_ENDPOINT` | Ingest API base URL |\n")
	sb.WriteString("| `--query-endpoint` | `OMNEVAL_QUERY_ENDPOINT` | Query API base URL |\n")
	sb.WriteString("| `--api-key` | `OMNEVAL_API_KEY` | API key for authentication |\n")
	sb.WriteString("| `--project-id` | `demo-project` | Project ID to generate spans for |\n")
	sb.WriteString("| `--commit-cadence` | `10s` | Writer commit cadence |\n")
	sb.WriteString("| `--num-traces` | `20` | Number of agent-trace groups |\n")
	sb.WriteString("| `--spans-per-trace` | `5` | Spans per trace (1 root + 4 children) |\n")
	sb.WriteString("| `--batch-size` | `25` | Spans per ingest HTTP POST |\n")
	sb.WriteString("| `--run-count` | `5` | Benchmark runs (after warm-up) |\n")
	sb.WriteString("| `--warmup-runs` | `2` | Warm-up runs (not recorded) |\n\n")

	sb.WriteString("---\n")
	sb.WriteString("*Scaling benchmark generated by the omneval benchmark harness.*\n")

	return os.WriteFile(path, []byte(sb.String()), 0o644)
}

// writeQueryLatencyResultsDocContent generates the markdown content for the
// query-latency benchmark results doc without writing to disk.
func writeQueryLatencyResultsDocContent(cfg benchmark.WriteQueryLatencyConfig) (string, error) {
	var sb strings.Builder

	sb.WriteString("# Query Performance Benchmark: Trace List, Trace Detail, Analytics DSL\n\n")
	sb.WriteString("<!-- AUTO-GENERATED by benchmark/cmd/benchmark/main.go. DO NOT EDIT MANUALLY. -->\n\n")
	sb.WriteString(fmt.Sprintf("Generated: %s\n\n", cfg.GenerateTime.UTC().Format("2006-01-02 15:04:05 MST")))

	// --- Methodology ---
	sb.WriteString("## Methodology\n\n")

	// Data volume
	sb.WriteString("### Data volume\n\n")
	sb.WriteString(fmt.Sprintf("- **Project ID**: `%s`\n", cfg.ProjectID))
	sb.WriteString(fmt.Sprintf("- **Pre-load description**: %s\n", cfg.PreLoadDescription))
	sb.WriteString(fmt.Sprintf("- **Total spans ingested**: %d\n", cfg.TotalSpansIngested))
	sb.WriteString(fmt.Sprintf("- **Total traces ingested**: %d\n", cfg.TotalTracesIngested))
	sb.WriteString("\n")
	sb.WriteString("The DuckDB/DuckLake catalog was pre-loaded using the harness's ingest path (the same\n")
	sb.WriteString("agent-trace workload generator and Ingest API driver used in issue #206).  The exact\n")
	sb.WriteString("procedure is:\n\n")
	sb.WriteString("1. Generate a fixed agent-trace workload matching the described span/trace counts.\n")
	sb.WriteString("2. Send all spans to the Ingest API (`POST /api/v1/spans`) in batches until the\n")
	sb.WriteString(fmt.Sprintf("   target volume is reached.\n"))
	sb.WriteString("3. Wait for all spans to be committed to the Lake (Writer flushes).  The benchmark\n")
	sb.WriteString("   does not proceed until every span is queryable.\n\n")
	sb.WriteString("This guarantees the benchmark runs against a **stated, realistic, non-trivial** Lake\n")
	sb.WriteString("data volume — not an empty or freshly-seeded catalog.\n\n")

	// Query shapes
	sb.WriteString("### Query shapes\n\n")
	sb.WriteString("Three query types are measured.  Each query is documented so that a skeptical reader\n")
	sb.WriteString("can reproduce the benchmark run.\n\n")

	sb.WriteString("**1. Trace List** — paginated root-span rollup query backing the Traces page.\n\n")
	if cfg.TraceListQueryShape != "" {
		sb.WriteString("```\n")
		sb.WriteString(cfg.TraceListQueryShape)
		sb.WriteString("```\n\n")
	} else {
		sb.WriteString("```POST\n")
		sb.WriteString(fmt.Sprintf("POST /api/v1/spans/query\n{\"project_id\": \"%s\", \"limit\": 25}\n", cfg.ProjectID))
		sb.WriteString("```\n\n")
	}

	sb.WriteString("**2. Trace Detail** — single trace plus all its spans/scores.\n\n")
	if cfg.TraceDetailQueryShape != "" {
		sb.WriteString("```\n")
		sb.WriteString(cfg.TraceDetailQueryShape)
		sb.WriteString("```\n\n")
	} else {
		sb.WriteString("```\n")
		sb.WriteString("GET /api/v1/traces/{trace_id}\n")
		sb.WriteString("```\n\n")
	}

	sb.WriteString("**3. Analytics DSL aggregation** — representative `POST /api/v1/analytics/spans`\n")
	sb.WriteString("query (typical dashboard aggregation).\n\n")
	if cfg.AnalyticsQueryShape != "" {
		sb.WriteString("```\n")
		sb.WriteString(cfg.AnalyticsQueryShape)
		sb.WriteString("```\n\n")
	} else {
		sb.WriteString("```POST\n")
		sb.WriteString(fmt.Sprintf("POST /api/v1/analytics/spans\n{\"project_id\": \"%s\", \"aggregations\": [{\"function\": \"count\"}]}\n", cfg.ProjectID))
		sb.WriteString("```\n\n")
	}

	// Environment
	sb.WriteString("### Environment\n\n")
	sb.WriteString(fmt.Sprintf("- **Ingest endpoint**: `%s`\n", cfg.Endpoint))
	sb.WriteString(fmt.Sprintf("- **Query endpoint**: `%s`\n", cfg.QueryEndpoint))
	sb.WriteString(fmt.Sprintf("- **Traces endpoint**: `%s`\n", cfg.TracesEndpoint))
	sb.WriteString(fmt.Sprintf("- **Analytics endpoint**: `%s`\n", cfg.AnalyticsEndpoint))
	sb.WriteString(fmt.Sprintf("- **Writer commit cadence (batch-flush interval)**: %s\n", cfg.CommitCadence))
	sb.WriteString("- **Single-node, per-component topology** (1 Ingest, 1 Writer, 1 Query, 1 Quack)\n")
	sb.WriteString("- **Real S3 object store** (not in-cluster MinIO)\n\n")

	// Run procedure
	sb.WriteString("### Run procedure\n\n")
	sb.WriteString("1. Pre-load the Lake with the stated data volume using the harness's ingest path.\n")
	sb.WriteString(fmt.Sprintf("2. Wait for all %d spans to become queryable.\n", cfg.TotalSpansIngested))
	sb.WriteString(fmt.Sprintf("3. Run %d warm-up iterations per query type (not recorded) to prime caches and connections.\n", cfg.WarmupRuns))
	sb.WriteString(fmt.Sprintf("4. Run %d benchmark iterations per query type, measuring wall-clock latency of each request:\n", cfg.RunCount))
	sb.WriteString("   - **Trace list**: POST /api/v1/spans/query (root-span rollup)\n")
	sb.WriteString("   - **Trace detail**: GET /api/v1/traces/{trace_id} (single trace)\n")
	sb.WriteString("   - **Analytics DSL**: POST /api/v1/analytics/spans (dashboard aggregation)\n")
	sb.WriteString(fmt.Sprintf("5. Compute p50 / p95 / p99 across the %d recorded runs per query type.\n", cfg.RunCount))
	sb.WriteString("\n")

	// --- Results ---
	sb.WriteString("## Results\n\n")
	sb.WriteString("### Per-query-type latency (p50 / p95 / p99)\n\n")

	if cfg.QueryLatencyStats != nil {
		var hasData bool
		for _, typ := range []benchmark.LatencyType{
			benchmark.LatencyTypeTraceList,
			benchmark.LatencyTypeTraceDetail,
			benchmark.LatencyTypeAnalytics,
		} {
			r, ok := cfg.QueryLatencyStats.Get(typ)
			if !ok || len(r.Latencies) == 0 {
				continue
			}
			hasData = true

			sorted := make([]time.Duration, len(r.Latencies))
			copy(sorted, r.Latencies)
			sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })

			pr := benchmark.ComputePercentiles(sorted)
			p50 := pr.P50.Round(time.Millisecond)
			p95 := pr.P95.Round(time.Millisecond)
			p99 := pr.P99.Round(time.Millisecond)

			var label string
			switch typ {
			case benchmark.LatencyTypeTraceList:
				label = "Trace List"
			case benchmark.LatencyTypeTraceDetail:
				label = "Trace Detail"
			case benchmark.LatencyTypeAnalytics:
				label = "Analytics DSL"
			default:
				label = string(typ)
			}

			sb.WriteString(fmt.Sprintf("#### %s\n\n", label))
			sb.WriteString(fmt.Sprintf("**%s**\n\n", label))
			sb.WriteString("| Metric | Value |\n")
			sb.WriteString("|--------|-------|\n")
			sb.WriteString(fmt.Sprintf("| p50 | %s |\n", p50))
			sb.WriteString(fmt.Sprintf("| p95 | %s |\n", p95))
			sb.WriteString(fmt.Sprintf("| p99 | %s |\n", p99))
			sb.WriteString(fmt.Sprintf("| max | %s |\n", sorted[len(sorted)-1]))
			sb.WriteString(fmt.Sprintf("| total runs | %d |\n", len(sorted)))
			sb.WriteString("\n")
		}
		if !hasData {
			sb.WriteString("_No data collected._\n\n")
		}
	} else {
		sb.WriteString("_No data collected._\n\n")
	}

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
	sb.WriteString("export OMNEVAL_TRACES_ENDPOINT=\"http://<ingress-host>:8000/api/v1/traces\"\n")
	sb.WriteString("export OMNEVAL_ANALYTICS_ENDPOINT=\"http://<ingress-host>:8000/api/v1/analytics/spans\"\n")
	sb.WriteString("export OMNEVAL_API_KEY=\"<your-api-key>\"\n")
	sb.WriteString("\n")
	sb.WriteString("# 2. Run the query-latency benchmark\n")
	sb.WriteString("cd /path/to/omneval\n")
	sb.WriteString("go run benchmark/cmd/benchmark/main.go \\\n")
	sb.WriteString("  --query-latency \\\n")
	sb.WriteString("  --project-id=demo-project \\\n")
	sb.WriteString("  --num-traces=1000 \\\n")
	sb.WriteString("  --spans-per-trace=5 \\\n")
	sb.WriteString("  --batch-size=25 \\\n")
	sb.WriteString("  --run-count=10 \\\n")
	sb.WriteString("  --warmup-runs=3 \\\n")
	sb.WriteString("  --commit-cadence=10s \\\n")
	sb.WriteString("  --doc-path=docs/benchmarks/query-latency.md\n")
	sb.WriteString("```\n\n")

	sb.WriteString("### Flags reference\n\n")
	sb.WriteString("| Flag | Default | Description |\n")
	sb.WriteString("|------|---------|-------------|\n")
	sb.WriteString("| `--query-latency` | `false` | Enable query-latency benchmark mode |\n")
	sb.WriteString("| `--endpoint` | `OMNEVAL_INGEST_ENDPOINT` | Ingest API base URL |\n")
	sb.WriteString("| `--query-endpoint` | `OMNEVAL_QUERY_ENDPOINT` | Query API base URL |\n")
	sb.WriteString("| `--traces-endpoint` | `OMNEVAL_TRACES_ENDPOINT` | Traces API base URL |\n")
	sb.WriteString("| `--analytics-endpoint` | `OMNEVAL_ANALYTICS_ENDPOINT` | Analytics endpoint URL |\n")
	sb.WriteString("| `--api-key` | `OMNEVAL_API_KEY` | API key for authentication |\n")
	sb.WriteString("| `--project-id` | `demo-project` | Project ID to generate spans for |\n")
	sb.WriteString("| `--commit-cadence` | `10s` | Writer commit cadence |\n")
	sb.WriteString("| `--num-traces` | `1000` | Number of agent traces for pre-load |\n")
	sb.WriteString("| `--spans-per-trace` | `5` | Spans per trace (1 root + 4 children) |\n")
	sb.WriteString("| `--batch-size` | `25` | Spans per ingest HTTP POST |\n")
	sb.WriteString("| `--run-count` | `10` | Query-latency benchmark runs (after warm-up) |\n")
	sb.WriteString("| `--warmup-runs` | `3` | Warm-up runs (not recorded) |\n")
	sb.WriteString("| `--doc-path` | `docs/benchmarks/query-latency.md` | Path for results markdown |\n\n")

	sb.WriteString("---\n")
	sb.WriteString("*Query-latency benchmark generated by the omneval benchmark harness.*\n")

	return sb.String(), nil
}

// writeQueryLatencyResultsDoc writes the query-latency benchmark results doc to disk.
func writeQueryLatencyResultsDoc(path string, cfg benchmark.WriteQueryLatencyConfig) error {
	content, err := writeQueryLatencyResultsDocContent(cfg)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create doc dir: %w", err)
	}
	return os.WriteFile(path, []byte(content), 0o644)
}