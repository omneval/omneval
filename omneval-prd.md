# PRD: Omneval — Self-Hostable LLM/Agent Tracing with DuckDB

**Status:** needs-triage  
**Type:** greenfield  
**Phase:** Phase 1 (Core Tracing MVP)

---

## Problem Statement

Engineering teams building LLM-powered agents and applications need production-grade tracing and observability tooling to understand what their models are doing, how much they cost, and how well they perform. Today there are two categories of tools:

- **Relational-backed tools** (e.g. MLflow + Postgres) — easy to self-host but not designed for the high write throughput and analytical query patterns that trace data demands. Expensive to operate at scale and slow to query.
- **OLAP-backed tools** (e.g. Langfuse, Helicone + ClickHouse) — technically superior for tracing workloads but require ClickHouse, whose only production-supported self-hosted offering is ClickHouse Cloud. Many organizations cannot store trace data in an external cloud environment due to compliance, security review, or data residency requirements.

The result is that teams are forced to choose between a tool that fits their compliance posture and a tool that actually works well for tracing. Teams at organizations with strict data governance end up on MLflow — a tool designed for experiment tracking, not agent tracing — because it is the only option they can self-host without months of security review.

DuckDB is an embedded OLAP database that is open source, requires no separate server process, and is already approved for use in many organizations where ClickHouse Cloud is not. No tracing tool exists that uses DuckDB as its analytical backend.

---

## Solution

Omneval is an open source LLM/Agent tracing and evaluation platform that uses DuckDB as its OLAP backend instead of ClickHouse. It provides the same class of observability as Langfuse — trace ingestion, span visualization, cost tracking, LLM-as-a-Judge evaluation, prompt management, and dataset curation — but is designed to be self-hostable in environments where ClickHouse Cloud is prohibited.

Omneval accepts traces via the OpenTelemetry Protocol (OTLP), making it a drop-in destination for any LLM framework already instrumented with OTel — including LangChain, LlamaIndex, CrewAI, and Smolagents — with zero SDK changes required.

A minimal deployment requires only a single `docker compose up`. A production Kubernetes deployment adds Postgres, Redis, and S3-compatible object storage — all dependencies that are universally approved in enterprise environments.

---

## User Stories

### Trace Ingestion

1. As a backend engineer, I want to point my existing OpenTelemetry exporter at Omneval's ingest endpoint, so that I can start capturing traces without changing my instrumentation code.
2. As a backend engineer, I want to send traces using OTLP over HTTP in both protobuf and JSON format, so that I can use any OTel-compatible SDK or library.
3. As a backend engineer, I want Omneval to automatically extract LLM-specific attributes from OTel GenAI semantic conventions, so that model name, token counts, and prompt/completion content are structured fields rather than raw attributes.
4. As a backend engineer, I want to send traces using Omneval's native REST API, so that I can instrument code without an OTel SDK dependency.
5. As a backend engineer, I want trace ingestion to have minimal latency impact on my production agents, so that observability does not degrade the user experience.
6. As a platform engineer, I want the ingest API to buffer writes in Redis, so that a temporary Writer Service outage does not result in lost traces.
7. As a platform engineer, I want the ingest API to authenticate requests using project-scoped API keys, so that only authorized services can write traces to a project.
8. As a platform engineer, I want to create service-scoped API keys that carry a service name label, so that I can audit which specific service or agent produced which traces.
9. As a platform engineer, I want API keys to follow a predictable format (`oev_proj_` and `oev_svc_` prefixes), so that keys are easily identifiable in logs and secret scanners.
10. As a platform engineer, I want the Ingest API to return `503 Service Unavailable` when Redis is unreachable, so that OTel SDK retry logic handles recovery automatically without losing spans.
11. As a platform engineer, I want duplicate spans arriving from SDK retries to be deduplicated automatically, so that a transient 503 does not produce duplicate rows in the trace waterfall.
12. As a developer building browser-based agents, I want CORS enabled on the Ingest API by default, so that I can send spans directly from the browser without a server-side proxy.

### Trace Visualization

13. As an ML engineer, I want to view a paginated, filterable list of traces for my project, so that I can quickly find traces of interest.
14. As an ML engineer, I want to filter traces by time range, model, span type, cost, duration, and score, so that I can narrow down to relevant traces efficiently.
15. As an ML engineer, I want stable pagination that does not drift as new spans are ingested, so that I can page through a long trace list without missing or repeating entries.
16. As an ML engineer, I want to view a waterfall visualization of a trace's full span tree, so that I can understand the sequence and nesting of operations within an agent run.
17. As an ML engineer, I want to view the input and output of each span within a trace, so that I can inspect what was sent to and received from each model or tool.
18. As an ML engineer, I want to see token usage and cost broken down per span, so that I can identify which parts of my agent are most expensive.
19. As an ML engineer, I want to see which prompt version was used to generate each span, so that I can correlate trace quality with prompt changes.
20. As an ML engineer, I want to see all scores attached to a span inline in the trace detail view, so that I can understand how evaluations judged that specific invocation.
21. As an ML engineer, I want recent traces to appear in the UI within 30 seconds of ingestion, so that I can observe agent behavior in near real-time.

### Dashboard and Analytics

22. As an ML engineer, I want a dashboard showing cost over time broken down by model, so that I can track spending trends and identify regressions.
23. As an ML engineer, I want to see token usage trends over time, so that I can understand how my agents' consumption is growing.
24. As an ML engineer, I want to see latency percentiles (p50, p95, p99) per model and span type, so that I can identify performance bottlenecks.
25. As an ML engineer, I want to see error rates over time, so that I can detect degradations in model reliability.
26. As an ML engineer, I want to see score distributions over time per evaluation name, so that I can track quality trends across deployments.
27. As a platform engineer, I want Omneval to expose a Prometheus `/metrics` endpoint, so that I can scrape operational metrics into my existing Grafana stack.
28. As a platform engineer, I want Prometheus metrics scoped per project, so that I can build per-team cost and quality dashboards.
29. As a platform engineer, I want metrics covering ingest queue depth, Writer flush health, S3 sync timestamps, and DuckDB file size, so that I can alert on Omneval's own operational health.

### Prompt Registry

30. As an ML engineer, I want to store prompt templates in Omneval's prompt registry with versioning, so that I have a single source of truth for all prompts used by my agents and judges.
31. As an ML engineer, I want to assign labels (`production`, `staging`, `dev`) to specific prompt versions, so that I can promote prompts through environments without changing code.
32. As a backend engineer, I want my production agent to fetch its prompt at runtime via `GET /api/v1/prompts/{name}?label=production`, so that prompt changes can be deployed without redeploying the agent.
33. As an ML engineer, I want prompt label reassignments to be visible to production agents within 30 seconds, so that I can hot-swap prompts quickly during incidents.
34. As an ML engineer, I want to view the full version history of a prompt, so that I can understand how it has evolved over time.
35. As an ML engineer, I want to diff two prompt versions side by side, so that I can understand exactly what changed between versions.
36. As an engineer, I want prompt versions to be immutable once created, so that I can trust that a version number always refers to the exact same template.
37. As an engineer, I want prompt templates to support variable interpolation using `{{variable}}` syntax, so that I can reuse templates with different inputs.
38. As an engineer, I want prompt versions to store model configuration (model name, temperature, max tokens) alongside the template, so that the full generation config is captured per version.

### Evaluation — LLM-as-a-Judge

39. As an ML engineer, I want to define evaluation rules that specify a judge model, a judge prompt template, a filter, and a sample rate, so that I can configure automatic evaluations without writing code.
40. As an ML engineer, I want evaluation rules to trigger automatically on every new span that matches a filter condition, so that I get continuous quality signals without manual intervention.
41. As an ML engineer, I want evaluation rules to support sampling (e.g. score 10% of matching spans), so that I can control evaluation cost for high-volume projects.
42. As an ML engineer, I want new evaluation rules to start firing within one minute of creation, so that I do not need to restart any service to activate them.
43. As an ML engineer, I want eval scores to appear on traces within 30 seconds of the span being ingested, so that the feedback loop is tight enough to be useful.
44. As an ML engineer, I want each score to include the judge's reasoning alongside the numeric value, so that I can understand why a span received a particular score.
45. As an ML engineer, I want scores to record which judge model and prompt version produced them, so that I can audit and reproduce evaluation results.
46. As an ML engineer, I want to write scores manually via the REST API, so that I can integrate human feedback or rule-based scoring alongside LLM judges.
47. As an ML engineer, I want to see score trends over time per evaluation name in the dashboard, so that I can track quality improvements from prompt changes.
48. As a platform engineer, I want Eval Workers to retry score write-back with exponential backoff for up to 5 minutes, so that transient Writer Service restarts do not cause scores to be silently lost.

### Datasets

49. As an ML engineer, I want to create a dataset by curating spans from the trace explorer, so that I can build evaluation datasets from real production traffic.
50. As an ML engineer, I want to upload a manually curated dataset of input/expected output pairs, so that I can use golden datasets created outside of production traffic.
51. As an ML engineer, I want to browse dataset items and see their source span, input, expected output, and any scores, so that I can inspect and curate dataset quality.
52. As an ML engineer, I want to run a dataset against an evaluation rule, so that I can score a set of examples in batch.
53. As an ML engineer, I want to view dataset run history with aggregate scores, so that I can compare runs across prompt versions and judge configurations.
54. As an ML engineer, I want cold dataset items (older than the hot window) to remain queryable for dataset building, even if they are slower to retrieve, so that I can curate datasets from historical traffic.

### Deployment and Operations

55. As a platform engineer, I want to run Omneval with a single `docker compose up` command for a proof-of-concept deployment, so that I can evaluate it without provisioning cloud infrastructure.
56. As a platform engineer, I want the demo deployment to use SQLite and MinIO, so that there are zero external dependencies for evaluation.
57. As a platform engineer, I want a production Helm chart for Kubernetes deployment, so that I can deploy Omneval using standard GitOps workflows.
58. As a platform engineer, I want a Kustomize base configuration, so that I can manage Omneval deployments with Flux CD or ArgoCD.
59. As a platform engineer, I want the Query API pods to be fully stateless, so that they can be scheduled on any node and scaled horizontally without volume constraints.
60. As a platform engineer, I want DuckDB snapshots synced to S3 at a configurable interval (default 30 seconds), so that Query API replicas always serve data within one sync interval of the latest writes.
61. As a platform engineer, I want trace data older than the hot window automatically flushed to S3 as Parquet files partitioned by project and date, so that the hot DuckDB file stays small and historical data is retained cheaply.
62. As a platform engineer, I want spans and their scores to be archived together in the same partition sweep, so that there is never a state where cold spans exist without their cold scores.
63. As a platform engineer, I want to use any S3-compatible storage (AWS S3, GCS, Azure Blob, MinIO), so that I am not locked into a specific cloud provider.
64. As a platform engineer, I want the Writer Service to recover automatically after a crash by draining Redis, so that no traces are lost during Writer restarts.
65. As a platform engineer, I want Omneval's configuration to be driven by a YAML file with environment variable overrides, so that I can manage base config in version control and inject secrets via Kubernetes Secrets.
66. As a platform engineer, I want Kubernetes liveness (`/healthz`) and readiness (`/readyz`) probes on every service, so that unhealthy pods are automatically restarted or removed from load balancer rotation.
67. As a platform engineer, I want all services to handle `SIGTERM` gracefully, finishing in-flight requests and final flushes before exiting, so that rolling deployments do not drop data.
68. As a platform engineer, I want the hot window threshold to be configurable, so that I can tune DuckDB file size versus cold storage access frequency for my deployment's query patterns.

### Multi-tenancy and Access Control

69. As an organization admin, I want to bootstrap the first admin user via environment variables on initial startup, so that I can deploy Omneval in automated environments without manual setup steps.
70. As an organization admin, I want to invite team members by generating a one-time temporary password, so that I can onboard users without an email sending infrastructure.
71. As a user, I want to change my own password after using a temporary invite password, so that I can rotate credentials without contacting an admin.
72. As an organization admin, I want to create multiple projects within my organization, so that I can separate traces from different applications or teams.
73. As an organization admin, I want to manage team members and their access to projects, so that I can control who can view and configure each project.
74. As a project admin, I want to create and revoke API keys for my project, so that I can manage service access without organization-level privileges.
75. As a project admin, I want to see which service name label each API key is associated with, so that I can audit ingestion sources.

### SDK and Developer Experience

76. As a Go developer, I want a Go SDK with `StartSpan`/`EndSpan` helpers and context propagation, so that I can instrument Go agents with minimal boilerplate.
77. As a Python developer, I want a `@omneval.trace` decorator, so that I can instrument my agent functions with two lines of code.
78. As a Python developer, I want the Omneval Python SDK to configure an OTel exporter pointing at Omneval automatically, so that I can use it alongside existing OTel instrumentation.
79. As a developer, I want prompt fetch to be cached client-side in the SDK, so that fetching a production prompt does not add latency to every agent invocation.
80. As a developer, I want to write scores manually from the SDK, so that I can submit human feedback or rule-based scores programmatically.

### Cost Tracking

81. As an ML engineer, I want Omneval to automatically compute the USD cost of each LLM span from the model name and token counts, so that I do not have to instrument cost tracking myself.
82. As a platform engineer, I want Omneval to pull model pricing from LiteLLM's pricing database at startup, so that cost calculations are kept current without manual updates.
83. As a platform engineer, I want Omneval to fall back to a bundled pricing snapshot if the LiteLLM fetch fails, so that the Writer Service starts successfully in air-gapped environments.
84. As a platform engineer, I want to override pricing for specific models in the Omneval config, so that I can account for fine-tuned models or enterprise pricing agreements.

---

## Implementation Decisions

### Service Architecture

Omneval is composed of five independently deployable services:

- **Ingest API** — accepts OTLP (protobuf and JSON) and native REST span payloads, validates API keys, translates OTLP to the internal domain format, and enqueues span batches to a Redis ingest queue. CORS enabled for browser SDK use. Written in Go.
- **Writer Service** — single-replica StatefulSet that drains the Redis ingest queue, batches writes to a local DuckDB file on a PVC, syncs DuckDB snapshots to S3, flushes aged partitions to S3 as Hive-partitioned Parquet files, and enqueues eval jobs to a Redis eval queue. Also exposes `POST /internal/v1/scores` for Eval Worker score write-back. Written in Go.
- **Query API** — stateless read service that downloads the latest DuckDB snapshot from S3 on startup and polls S3 for updates. Queries combine the hot snapshot and cold Parquet via a DuckDB UNION — always, regardless of time range. Serves the REST API, analytics DSL, prompt registry, auth, and the embedded React SPA. Written in Go.
- **Eval Workers** — horizontally scalable workers that drain the Redis eval queue, call a configurable OpenAI-compatible judge LLM, and write scores back to the Writer Service via REST with exponential backoff. Written in Go.
- **UI** — React SPA built with Vite, shadcn/ui, and Recharts. Compiled to static assets and embedded directly into the Query API binary via Go's `embed.FS`. Served by the Query API — no separate Nginx process.

### Storage Architecture

- **Hot store:** DuckDB file on the Writer Service's StatefulSet PVC. Owned exclusively in read-write mode by the Writer. Schema: `PRIMARY KEY (trace_id, span_id)` on spans for idempotent upsert; ART index on `(project_id, start_time)` for the dominant filter pattern; `input`/`output` columns as native DuckDB JSON type. The Writer syncs a snapshot to S3 solely for Query API consumption — Query API pods are stateless and never mount the PVC.
- **Cold store:** Hive-partitioned Parquet files on S3 at `s3://bucket/archive/project_id={id}/date={date}/spans/` and `.../scores/`. Both partitioned by the span's `StartTime` date so scores always co-locate with their span. Written via DuckDB's `COPY ... TO 's3://...' (FORMAT PARQUET)` using the `httpfs` extension — no separate Parquet library.
- **Snapshot store:** Live DuckDB file synced to S3 by the Writer on a configurable `writer.sync_interval` (default `30s`). Query API pods download on startup to a configurable local path (`query.duckdb_path`, default `/tmp/omneval-snapshot.duckdb`) and poll S3 on the same interval for updates.
- **Metadata store:** Postgres (production) or SQLite (demo) for all transactional data: orgs, projects, users, API keys, prompt versions, eval rules, datasets. Abstracted behind a `MetadataStore` interface. Migrations managed with golang-migrate, with separate SQL files per dialect.

### Hot/Cold Query Strategy

The Query API always issues a UNION of the hot DuckDB snapshot and `read_parquet(...)` with `hive_partitioning=true` against cold S3 Parquet, regardless of the query's time range. DuckDB's Hive partition pruning restricts S3 scans to the relevant date partitions. No manifest table. The analytics DSL compiler and span list handler never branch on time range to choose between hot-only, cold-only, or UNION paths.

### OTLP Translation

OTLP translation is a two-step pipeline in the Ingest API: (1) decode wire format (protobuf or JSON) into an intermediate `[]ResourceSpans` struct; (2) translate `ResourceSpans` into `[]*domain.Span`. Both steps happen in the Ingest API before the Redis enqueue, so the queue payload is always in the internal domain format regardless of input encoding. The translator is a pure function with no external dependencies. Key attribute mappings: `gen_ai.request.model`→`Model`; `gen_ai.usage.input_tokens`→`InputTokens`; `gen_ai.usage.output_tokens`→`OutputTokens`; `gen_ai.prompt.N.{role,content}`→`Input`; `gen_ai.completion.N.{role,content}`→`Output`. `Kind` is derived from attribute presence. All unmapped attributes land in the overflow `Attributes` map.

### Ingest Durability

The native REST endpoint (`POST /api/v1/spans`) returns `202 Accepted` — the span is enqueued, not yet written. The OTLP endpoint (`POST /v1/traces`) returns a standard empty `ExportTraceServiceResponse`. If Redis is unreachable, both endpoints return `503 Service Unavailable` immediately — OTel SDK retry handles recovery. Duplicate span arrivals are handled by `INSERT OR REPLACE` on the `PRIMARY KEY (trace_id, span_id)` — idempotent, no duplicate rows.

### Queues

Ingest queue: Redis List at `omneval:ingest:spans`. Ingest API `RPUSH`es one JSON-encoded `[]*domain.Span` batch per request. Writer `BLPOP`s batches. Eval queue: Redis List at `omneval:eval:jobs`. Writer `RPUSH`es one `domain.EvalJob` per matching span after writing. Eval Workers `BLPOP` jobs. Both queues share a single Redis instance.

### Span List Pagination

Keyset pagination on `(start_time DESC, span_id ASC)`. Cursor is a base64-encoded JSON blob of the last row's `start_time` and `span_id`. Stable under concurrent ingestion — no offset drift. The ART index on `(project_id, start_time)` covers the cursor predicate.

### Score Write-back

Eval Workers write scores to the Writer Service via `POST /internal/v1/scores` — an internal-only endpoint not exposed externally. Workers retry with exponential backoff for up to 5 minutes on failure. Scores that exhaust retries are logged at `Error` and dropped — losing an occasional score is acceptable; blocking the eval pipeline is not.

### Authentication and Session

API key validation uses a `CachingValidator` with a 60-second TTL — a revoked key may be accepted for up to 60 seconds after revocation. Session cookies: `omneval_session`, `HttpOnly`, `Secure` (disable via `auth.secure_cookie: false` for local dev), `SameSite=Lax`, TTL via `auth.session_ttl` (default `168h`). Admin bootstrap: if no users exist at startup and `OMNEVAL_AUTH_ADMIN_EMAIL` / `OMNEVAL_AUTH_ADMIN_PASSWORD` are set, the Query API creates the first admin user automatically. User invites generate a one-time temporary password shown to the inviter — no email infrastructure required.

### Prompt Cache

Two independent in-process caches in the Query API. Version lookups use an LRU with no TTL (versions are immutable). Label lookups use a 30-second TTL matching the snapshot staleness window.

### CORS

CORS is enabled on the Ingest API only. Configurable via `ingest.cors_allowed_origins` (default `*`). Allowed methods: `POST, OPTIONS`. Allowed headers: `Content-Type, Authorization`. Preflight `OPTIONS` returns `204`.

### Graceful Shutdown

All services listen for `SIGTERM` and `SIGINT`. On signal: stop accepting connections via `http.Server.Shutdown(ctx)`; drain in-flight HTTP requests within a 30-second timeout. Additional teardown: Writer finishes the current DuckDB write batch and performs a final snapshot sync before exiting; Eval Workers use a 120-second drain to finish any in-progress LLM call and do not `BLPOP` a new job after signal.

### Health and Readiness Probes

All services expose `GET /healthz` (liveness — `200` if process is alive, no external checks) and `GET /readyz` (readiness — `200` only when ready, `503` otherwise). Readiness gates: Ingest API requires Redis `PING`; Writer requires DuckDB open and writable plus Redis `PING`; Query API requires snapshot file on disk and metadata store reachable; Eval Workers require Redis `PING`.

### Pricing

Writer Service fetches LiteLLM's `model_prices_and_context_window.json` at startup; falls back to a bundled binary-embedded snapshot on fetch failure. `CostUSD` is pre-computed at write time — query-time aggregations need no re-computation. Unknown models store `CostUSD = 0`. Config overrides expressed in USD per million tokens, converted to per-token at load time.

### Disaster Recovery

Cold Parquet on S3 is the DR story in Phase 1. A PVC loss is bounded to the hot window (default 2 days). No additional PVC backup in Phase 1. Future: Kubernetes `VolumeSnapshot` support for point-in-time hot store backups.

### Logging

All Go services use `log/slog` (stdlib) for structured logging: `Info` for normal operations, `Warn` for recoverable anomalies, `Error` for failures needing attention. Every log call includes relevant context as key-value pairs. No `log.Printf` or `fmt.Println` in production code.

### Configuration

All services are configured via Viper: `omneval.yaml` with environment variable overrides (e.g. `OMNEVAL_AUTH_ADMIN_EMAIL`). Docker Compose ships a default `omneval.yaml`. Kubernetes deployments use ConfigMaps for base config and Secrets for sensitive values.

### Phase 1 Implementation Order

Vertical TDD slices, each with a failing test before any implementation:

1. Metadata Store + Config
2. Ingest API → Redis (REST span ingest, enqueue)
3. Writer Service → DuckDB (dequeue, write, snapshot sync)
4. Query API → span list + trace waterfall
5. Auth (login, session cookie, admin bootstrap)
6. React UI shell + `/traces`
7. Analytics DSL + `/dashboard`
8. OTLP ingest (proto+JSON decode, two-step translation)
9. Eval pipeline (rule cache, eval queue, Eval Workers, score write-back)
10. Prompt Registry (version create, label assign, caching client)
11. SDK (Go + Python)
12. Archival sweep (hot→cold Parquet flush via DuckDB httpfs)

Slices 8–12 are independent of each other once slices 1–4 are done.

---

## Testing Decisions

### What Makes a Good Test

Tests verify external behavior through public interfaces, not implementation details. A good test asserts what a service returns or writes given a specific input — not how it internally transforms data. Hand-written fakes (named `Fake<InterfaceName>`) are used throughout; no mock-generation tools. Integration tests that require a real database use `testcontainers-go` to spin up instances in Docker.

### Modules to Test

**MetadataStore implementations (Postgres and SQLite)**
Both implementations run against the same behavioral test suite, parameterized over the interface. Covers CRUD, API key validation, prompt version immutability, and migration application. Integration tests via `testcontainers-go`.

**OTLP translation layer**
Pure function, no external dependencies. Exhaustive unit tests: GenAI attribute extraction, span kind derivation, JSON overflow for unknown attributes, malformed/incomplete payloads, both proto and JSON input encodings.

**Analytics DSL compiler**
Pure function. Unit tests verify all filter operators, aggregation functions (including percentile compilation), group-by truncation, order-by, limit, `project_id` injection, and that the output is always a hot+cold UNION.

**Ingest queue enqueue/dequeue**
Unit tests with a fake Redis client. Covers batch serialization, error propagation, and idempotent upsert behavior.

**Writer Service ingest pipeline**
Fake Redis client and real in-memory DuckDB instance. Tests verify batch sizing, error handling on malformed spans, schema correctness, idempotent upsert on duplicate `(trace_id, span_id)`, and cost pre-computation.

**S3 sync and Parquet flush**
Fake S3 interface. Tests verify sync frequency, partition naming, that spans and scores flush atomically, that aged data is correctly identified, and that DuckDB is pruned only after successful Parquet write.

**Eval Worker judge pipeline**
Fake LLM client. Tests verify judge prompt rendering, score structuring, sampling rule application, and retry/backoff on score write-back failure.

**API key validation and caching**
Unit tests with a `FakeMetadataStore`. Covers valid project and service keys, invalid key rejection, cache hit behavior, and the 60-second revocation window.

**Span list cursor**
Unit tests verify cursor encoding/decoding, stable ordering under concurrent inserts, and correct page boundaries.

---

## Out of Scope

- **Writer Service high availability / active-passive failover** — the Writer Service runs as a single replica. Redis provides durability during Writer restarts. HA is a known limitation for a future phase.
- **Cross-node DuckDB file sharing via networked filesystem** — the hot store is not shared via NFS or ReadWriteMany PVC. S3 snapshot sync is the mechanism for cross-node Query API access.
- **Raw SQL query endpoint** — no endpoint accepts raw DuckDB SQL from clients.
- **Grafana datasource plugin** — Prometheus `/metrics` is the Grafana integration path.
- **SSO / SAML / OIDC authentication** — Phase 1 uses email + password only.
- **Kafka / Redpanda as an ingest buffer** — Redis is the only supported queue backend.
- **MotherDuck or networked DuckDB** — DuckDB is used exclusively in embedded mode.
- **PVC backup via VolumeSnapshot** — deferred; cold Parquet on S3 is the DR story in Phase 1.
- **Phase 2+ UI features** — Eval rules UI, Prompt Registry UI, Playground, and Dataset curation UI are Phase 2+.
- **TypeScript SDK** — Go and Python SDKs are Phase 1. TypeScript deferred.
- **OR / NOT eval filter conditions** — EvalFilter is AND-only in Phase 1.
- **Token-range eval filters** — `MinInputTokens`, `MaxOutputTokens`, etc. deferred.

---

## Further Notes

- The project is named **Omneval**. Go module path: `github.com/omneval/omneval`. Docker Hub: `omneval/ingest`, `omneval/writer`, `omneval/query`, `omneval/eval`, `omneval/ui`.
- The repo is a Go workspace monorepo. Each service has its own `go.mod`. Shared domain types, interfaces, Redis client, S3 client, auth middleware, config loading, DuckDB schema, pricing table, and OTLP translator live in a root-level `internal/` package imported via the Go workspace.
- OTel compatibility is a hard requirement. Any LLM framework that emits OTel spans — LangChain, LlamaIndex, CrewAI, Smolagents, OpenLLMetry, OpenInference — must work against Omneval's ingest endpoint with zero SDK changes.
- DuckDB's single-writer constraint is the central architectural constraint. All design decisions around the Writer Service (StatefulSet, PVC, S3 snapshot sync, read-only Query API connections) flow from this constraint.
- The 30-second staleness window for hot data is acceptable for the primary use case. Users viewing live eval scores on recent traces will see data no older than 30 seconds.
- The React SPA is served from the Query API binary via `embed.FS` — no separate Nginx container is required. This simplifies the deployment footprint and means the Query API is the single ingress point for all UI traffic.
- All Go services use `go-chi/chi/v5` as the HTTP router — a thin wrapper around `net/http` with no framework magic, integrating cleanly with `httptest`.
