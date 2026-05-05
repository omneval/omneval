# Lantern

**Self-hostable LLM/Agent tracing and evaluation — powered by DuckDB.**

Lantern provides production-grade tracing and observability for LLM-powered agents and applications, with the same class of features as Langfuse — but using DuckDB instead of ClickHouse. This makes it viable for organizations with strict data residency, compliance, or security review requirements that prevent using cloud-hosted OLAP backends.

## Why Lantern?

Teams building LLM agents need to understand what their models are doing, how much they cost, and how well they perform. Today the choices are:

- **Relational-backed tools** (MLflow + Postgres) — easy to self-host but not designed for high-write-throughput trace data or analytical queries.
- **OLAP-backed tools** (Langfuse, Helicone + ClickHouse) — technically superior for tracing workloads, but ClickHouse's only production self-hosted offering is ClickHouse Cloud, which many organizations cannot use due to compliance or data residency requirements.

Lantern bridges this gap. It accepts traces via OpenTelemetry (OTLP), making it a drop-in destination for any LLM framework already instrumented with OTel — including LangChain, LlamaIndex, CrewAI, and Smolagents — with zero SDK changes.

A minimal deployment requires only a single `docker compose up`. A production Kubernetes deployment adds Postgres, Redis, and S3-compatible object storage.

## Architecture

Lantern consists of five independently deployable Go services communicating via Redis queues:

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

### Demo (SQLite + MinIO)

```bash
docker compose up
```

Access the UI at `http://localhost:3000`. An admin user is bootstrapped via environment variables.

### Production (Postgres + Redis + S3)

```bash
docker compose -f docker-compose.prod.yml up
```

Or deploy via the Helm chart in `deploy/helm/lantern/`.

## API Overview

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/v1/traces` | POST | OTLP span ingest (protobuf or JSON) |
| `/api/v1/spans` | POST | Native REST span ingest |
| `/api/v1/spans/query` | POST | Paginated span list with keyset cursor |
| `/api/v1/traces/:traceId` | GET | Full span waterfall for a trace |
| `/api/v1/scores` | POST | Manual score write |
| `/api/v1/prompts` | POST | Create prompt version |
| `/api/v1/prompts/:name` | GET | Resolve prompt by version or label |

## SDKs

| Language | Package | Status |
|----------|---------|--------|
| Go | `github.com/zbloss/lantern/sdk/go` | ✅ Implemented |
| Python | `lantern-sdk` | ✅ Implemented |

## Documentation

| Document | Description |
|----------|-------------|
| [PRD](lantern-prd.md) | Product requirements and user stories |
| [Architecture Decisions](docs/adr/) | Key design decisions and rationale |
| [Context](CONTEXT.md) | Bounded context and domain terminology |
| [CLAUDE](CLAUDE.md) | Development commands and architecture reference |
| [ROADMAP](ROADMAP.md) | Implementation progress and remaining work |

## Status

Lantern is under active development. The core tracing pipeline (ingest → write → query) is implemented and tested. See the [ROADMAP](ROADMAP.md) for detailed progress on each component.

## License

Apache 2.0
