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

Omneval consists of four independently deployable Go services communicating via Redis queues:

| Service | Role | Deployment |
|---------|------|------------|
| **Ingest API** | Accepts OTLP (proto+JSON) and native REST spans, validates API keys, translates to domain format, stages batches in the Ingest Buffer (S3) and enqueues references to Redis | Stateless, horizontally scalable |
| **Writer Service** | Drains the ingest queue, fetches batches from the Ingest Buffer, computes cost, commits spans/scores to the Lake via the Quack Server, matches eval rules and enqueues eval jobs | Stateless, horizontally scalable |
| **Query API** | Attaches read-only to the Lake via the Quack Server, serves REST API + embedded React SPA | Stateless, horizontally scalable |
| **Eval Worker** | Drains eval queue, calls judge LLM, writes scores back to Writer | Horizontally scalable |

The React SPA (Vite + Tailwind CSS) is built as static assets and embedded into the Query API binary via Go's `embed.FS` — no separate Nginx or build step at runtime.

### Storage

The **Lake** is the single authoritative span/score store (ADR-0004): DuckLake tables (`spans`, `scores`) stored as Parquet on S3 with ACID transactions through a Postgres **Catalog** shared with the metadata store. The **Quack Server** (ADR-0005, `services/quack/`) is the sole holder of a direct DuckLake Catalog/data-path connection and runs the Table Maintenance scheduler (compaction, snapshot expiry, retention GC); Writer, Query API, and Eval attach to it as thin Quack clients via `quack.client.*`. Writers stage incoming batches in the S3 **Ingest Buffer**, commit them to the Lake on the Commit Cadence (~5s/16MB), and record commits in the Postgres **Batch Ledger** (`committed_batches`) for idempotent recovery. There is no separate hot DuckDB tier, snapshot file, or cold-Parquet UNION — all reads and writes go through the Lake.

### Key Design Points

- **Cost pre-computed at write time** — `cost_usd` is calculated by the Writer before committing to the Lake, using the LiteLLM pricing table. No query-time recomputation.
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

### Ingest API

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/v1/traces` | POST | OTLP span ingest (protobuf or JSON) |
| `/api/v1/spans` | POST | Native REST span ingest. Request body: `{"spans": [{"trace_id": "<32-char hex>", "span_id": "<16-char hex>", "name": "...", ...}]}`. `trace_id` must be a 32-character lowercase hex string (0-9, a-f); `span_id` must be a 16-character lowercase hex string (0-9, a-f). |

### Query API

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/api/v1/spans/query` | POST | Paginated span list with keyset cursor |
| `/api/v1/traces/:traceId` | GET | Full span waterfall for a trace |
| `/api/v1/traces/:traceId/bookmark` | POST | Toggle bookmark for a trace |
| `/api/v1/scores` | POST | Manual score write |
| `/api/v1/analytics/spans` | POST | Analytics query (filter, aggregation, group-by, percentiles) |
| `/api/v1/prompts` | GET | List prompts |
| `/api/v1/prompts` | POST | Create prompt version |
| `/api/v1/prompts/:name` | GET | Resolve prompt by version or label |
| `/api/v1/prompts/:name/versions` | GET | List all versions of a prompt |
| `/api/v1/prompts/:name/labels/:label` | PUT | Assign/reassign a label to a prompt version |
| `/api/v1/eval-rules` | GET | List evaluation rules |
| `/api/v1/eval-rules` | POST | Create evaluation rule |
| `/api/v1/eval-rules/preview` | POST | Preview rule matching |
| `/api/v1/eval-rules/:id` | DELETE | Delete evaluation rule |
| `/api/v1/datasets` | GET | List datasets |
| `/api/v1/datasets` | POST | Create dataset |
| `/api/v1/datasets/:id` | GET | Get dataset details |
| `/api/v1/datasets/:id` | DELETE | Delete dataset |
| `/api/v1/datasets/:id/items` | GET | List dataset items |
| `/api/v1/datasets/:id/items` | POST | Add items to dataset |
| `/api/v1/datasets/:id/items/batch` | POST | Batch add items to dataset |
| `/api/v1/datasets/:id/runs` | POST | Create dataset evaluation run |
| `/api/v1/datasets/:id/runs` | GET | List dataset runs |
| `/api/v1/datasets/:id/runs/:runId` | GET | Get dataset run details |
| `/api/v1/datasets/:id/runs/:runId/status` | GET | Get dataset run status |
| `/api/v1/playground/run` | POST | Run prompt in playground |

### Auth & Projects

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/login` | POST | User login |
| `/logout` | POST | User logout |
| `/api/v1/me` | GET | Current user info |
| `/api/v1/projects` | GET | List projects |
| `/api/v1/projects` | POST | Create project |
| `/api/v1/projects/:id/api-keys` | POST | Generate API key |
| `/api/v1/projects/:id/api-keys` | GET | List API keys |
| `/api/v1/projects/:id/api-keys/:keyId` | DELETE | Revoke API key |
| `/api/v1/users/invite` | POST | Invite new user |
| `/api/v1/users/reset-password` | POST | Reset password |
| `/api/v1/users/me/password` | PUT | Change password |

### Admin

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/api/v1/admin/api-keys` | GET | List all API keys |
| `/api/v1/admin/api-keys/` | DELETE | Delete any API key |
| `/api/v1/admin/traces/` | GET | Count traces |
| `/api/v1/admin/traces/` | DELETE | Delete traces |
| `/api/v1/admin/projects/` | DELETE | Delete project |

### Observability (All Services)

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/healthz` | GET | Health check |
| `/readyz` | GET | Readiness probe |
| `/metrics` | GET | Prometheus metrics |

## SDKs

| Language | Package | Status |
|----------|---------|--------|
| Go | `github.com/omneval/omneval/sdk/go` | ✅ Implemented |
| Python | `omneval-sdk` | ✅ Implemented |
| TypeScript | `@omneval/sdk` | ✅ Implemented (browser + Node.js) |

## Documentation

| Document | Description |
|----------|-------------|
| [PRD](omneval-prd.md) | Product requirements and user stories |
| [Architecture Decisions](docs/adr/) | Key design decisions and rationale |
| [Context](CONTEXT.md) | Bounded context and domain terminology |
| [CLAUDE](CLAUDE.md) | Development commands and architecture reference |
| [Ingestion Guide](docs/ingestion.md) | Trace ingestion: auth headers, project model, OTLP setup |
| [ROADMAP](ROADMAP.md) | Implementation progress and remaining work |

## Status

Omneval is under active development. The following features are implemented and tested:

- **Tracing pipeline** — OTLP + REST span ingest via the Ingest Buffer, Writer commits to the Lake through the Quack Server
- **Evaluation pipeline** — configurable judge LLM rules, score write-back, sample-rate support
- **Eval rules** — create, list, preview, and delete eval rules with filter conditions
- **Prompt registry** — version management, label resolution, `{{variable}}` template interpolation, prompt caching
- **Datasets** — create datasets, add items (batch or single), run evaluations, track run status
- **Playground** — run prompts with LLM via the playground API endpoint
- **Analytics** — DSL-based span queries with aggregation, group-by, and percentiles (p50/p95/p99)
- **Auth** — login, session cookies, admin bootstrap, user invites, password change
- **Project management** — create projects, generate/list/revoke API keys, per-project isolation
- **Admin** — API key management, trace counting/deletion, project deletion
- **Bookmarks** — bookmark traces for quick access
- **UI** — React SPA with traces, dashboard, span waterfall, datasets, eval rules, prompts, admin, and settings pages
- **SDKs** — Go (`omneval/tracer` + `omneval/client`), Python (`omneval-sdk` with `@trace` decorator), TypeScript (`@omneval/sdk` with browser + Node.js OTel support)
- **Observability** — health/readiness probes, Prometheus metrics on all services, graceful shutdown

See the [ROADMAP](ROADMAP.md) for detailed progress on each component.

## License

Apache 2.0
