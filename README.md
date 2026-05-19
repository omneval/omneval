# Omneval

**Self-hostable LLM/Agent tracing and evaluation — powered by DuckDB.**

Omneval provides production-grade tracing and observability for LLM-powered agents and applications, with the same class of features as Langfuse — but using DuckDB instead of ClickHouse. This makes it viable for organizations with strict data residency, compliance, or security review requirements that prevent using cloud-hosted OLAP backends.

## Why Omneval?

Teams building LLM agents need to understand what their models are doing, how much they cost, and how well they perform. Today the choices are:

- **Relational-backed tools** (MLflow + Postgres) — easy to self-host but not designed for high-write-throughput trace data or analytical queries.
- **OLAP-backed tools** (Langfuse, Helicone + ClickHouse) — technically superior for tracing workloads, but ClickHouse's only production self-hosted offering is ClickHouse Cloud, which many organizations cannot use due to compliance or data residency requirements.

Omneval bridges this gap. It accepts traces via OpenTelemetry (OTLP), making it a drop-in destination for any LLM framework already instrumented with OTel — including LangChain, LlamaIndex, CrewAI, and Smolagents — with zero SDK changes.

A minimal development deployment requires a single `docker compose` command in `deploy/docker-compose/`. Production Kubernetes deployments add Postgres, Redis, and S3-compatible object storage.

## Architecture

Omneval consists of five independently deployable Go services communicating via Redis queues:

| Service | Role | Deployment |
|---------|------|------------|
| **Ingest API** | Accepts OTLP (proto+JSON) and native REST spans, validates API keys, translates to domain format, enqueues to Redis | Stateless, horizontally scalable |
| **Writer Service** | Drains Redis queue, writes to DuckDB, syncs snapshots to S3, archives aged data to Parquet | Single-replica StatefulSet (DuckDB single-writer constraint) |
| **Query API** | Downloads DuckDB snapshot from S3, serves REST API + embedded React SPA | Stateless, horizontally scalable |
| **Eval Workers** | Drains eval queue, calls judge LLM, writes scores back to Writer | Horizontally scalable |
| **UI** | React SPA (Vite + shadcn/ui + Recharts), embedded into Query API binary | Served by Query API — no separate Nginx |

### Storage Tiers

| Tier | Location | Description |
|------|----------|-------------|
| **Hot** | DuckDB file on Writer PVC | Exclusive read-write by Writer; idempotent upserts on `(trace_id, span_id)` |
| **Snapshot** | S3 DuckDB file | Written by Writer every 30s; read by Query API pods |
| **Cold** | S3 Hive-partitioned Parquet | Aged spans flushed every 30m; queried via `read_parquet` with partition pruning |

### Key Design Points

- **Cost pre-computed at write time** — `cost_usd` is calculated when spans land in DuckDB using the LiteLLM pricing table. No query-time recomputation.
- **OTLP compatible** — any LLM framework emitting OTel spans works with zero instrumentation changes.
- **Self-hostable** — demo mode uses SQLite + MinIO; production uses Postgres + any S3-compatible store.
- **Single binary deployment** — the React UI is compiled to static assets and embedded into the Query API binary via Go's `embed.FS`.

## Getting Started

### Local Development (Postgres + Redis + MinIO)

```bash
cd deploy/docker-compose && docker compose up
```

Access the UI at `http://localhost:8002`. The Ingest API accepts spans at `http://localhost:8000`.

Configure infrastructure credentials via `.env` (see `deploy/docker-compose/.env.example`).

### Service Commands

```bash
# Run a single service locally (useful for debugging)
docker compose run --rm ingest
docker compose run --rm writer
docker compose run --rm query
docker compose run --rm eval
```

## API Overview

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/v1/traces` | POST | OTLP span ingest (protobuf or JSON) |
| `/api/v1/spans` | POST | Native REST span ingest. Request body: `{"spans": [{"trace_id": "<32-char hex>", "span_id": "<16-char hex>", "name": "...", ...}]}`. `trace_id` must be a 32-character lowercase hex string (0-9, a-f); `span_id` must be a 16-character lowercase hex string (0-9, a-f). |
| `/api/v1/spans/query` | POST | Paginated span list with keyset cursor |
| `/api/v1/traces/:traceId` | GET | Full span waterfall for a trace |
| `/api/v1/scores` | POST | Manual score write |
| `/api/v1/prompts` | POST | Create prompt version |
| `/api/v1/prompts/:name` | GET | Resolve prompt by version or label |
| `/api/v1/prompts/:name/versions` | GET | List all versions of a prompt |
| `/api/v1/prompts/:name/labels/:label` | PUT | Assign/reassign a label to a prompt version |
| `/api/v1/analytics/spans` | POST | Analytics query (filter, aggregation, group-by, percentiles) |
| `/healthz` | GET | Health check (all services) |
| `/readyz` | GET | Readiness probe (all services) |
| `/metrics` | GET | Prometheus metrics (all services) |

## SDKs

| Language | Package | Status |
|----------|---------|--------|
| Go | `github.com/omneval/omneval/sdk/go` | ✅ Implemented |
| Python | `omneval-sdk` | ✅ Implemented |

## Documentation

| Document | Description |
|----------|-------------|
| [PRD](omneval-prd.md) | Product requirements and user stories |
| [Architecture Decisions](docs/adr/) | Key design decisions and rationale |
| [Context](CONTEXT.md) | Bounded context and domain terminology |
| [CLAUDE](CLAUDE.md) | Development commands and architecture reference |
| [ROADMAP](ROADMAP.md) | Implementation progress and remaining work |

## Status

Omneval is under active development. The following features are implemented and tested:

- **Tracing pipeline** — OTLP + REST span ingest, DuckDB write, snapshot/Parquet archival, hot+cold queries
- **Evaluation pipeline** — configurable judge LLM rules, score write-back, sample-rate support
- **Prompt registry** — version management, label resolution, `{{variable}}` template interpolation, prompt caching
- **Analytics** — DSL-based span queries with aggregation, group-by, and percentiles (p50/p95/p99)
- **Auth** — login, session cookies, admin bootstrap, user invites, password change
- **UI** — React SPA with traces view, span waterfall, project switcher
- **SDKs** — Go (`omneval/tracer` + `omneval/client`) and Python (`omneval-sdk` with `@trace` decorator)
- **Observability** — health/readiness probes, Prometheus metrics on all services, graceful shutdown

See the [ROADMAP](ROADMAP.md) for detailed progress on each component.

## License

Apache 2.0
