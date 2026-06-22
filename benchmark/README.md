# Benchmark Harness

A custom Go benchmark harness for measuring **ingest throughput** (spans/sec accepted and committed) and **end-to-end ingest-to-queryable latency** against a live omneval deployment.

## What it does

- Generates a representative multi-span agent-trace workload (flat fan-out via `parent_id`, ~1.5 KB `Input` + ~1.5 KB `Output` per span)
- Drives ingest load against a live omneval deployment (issue #205 environment)
- Reports **p50 / p95 / p99** across multiple runs after a warm-up period (not just averages)
- Explicitly records and reports the **Commit Cadence** (batch-flush interval) alongside all results
- Auto-generates a results markdown doc at `docs/benchmarks/ingest-throughput-and-latency.md`

## Quick start

```bash
export OMNEVAL_INGEST_ENDPOINT="http://<ingress-host>:8000/api/v1/spans"
export OMNEVAL_QUERY_ENDPOINT="http://<ingress-host>:8000/api/v1/spans/query"
export OMNEVAL_API_KEY="<your-api-key>"

go run ./benchmark/cmd/benchmark \
  --commit-cadence=10s \
  --num-traces=20 \
  --spans-per-trace=5 \
  --batch-size=25 \
  --run-count=5 \
  --warmup-runs=2
```

Full flag reference in [docs/benchmarks/ingest-throughput-and-latency.md](../docs/benchmarks/ingest-throughput-and-latency.md).

## Environment

The benchmark harness targets the single-replica, real-S3 environment from [issue #205](../benchmark/env/README.md).  See `benchmark/env/README.md` for deployment scripts and topology.

## Structure

| File | Purpose |
|---|---|
| `cmd/benchmark/main.go` | Entry point — flag parsing, orchestration, doc generation |
| `workload.go` | Multi-span agent-trace generator (fan-out via `parent_id`, ~1.5 KB I/O per span) |
| `ingest.go` | Ingest driver — batches spans and posts to the Ingest API |
| `writer.go` | Writer metric scraper — reads committed spans/sec from `/metrics` |
| `latency.go` | Latency client — polls Query API until each span first appears, records delta |
| `results.go` | Percentile computation (p50/p95/p99) and formatted report output |