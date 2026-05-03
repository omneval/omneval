# PRD: Lantern — Self-Hostable LLM/Agent Tracing with DuckDB

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

Lantern is an open source LLM/Agent tracing and evaluation platform that uses DuckDB as its OLAP backend instead of ClickHouse. It provides the same class of observability as Langfuse — trace ingestion, span visualization, cost tracking, LLM-as-a-Judge evaluation, prompt management, and dataset curation — but is designed to be self-hostable in environments where ClickHouse Cloud is prohibited.

Lantern accepts traces via the OpenTelemetry Protocol (OTLP), making it a drop-in destination for any LLM framework already instrumented with OTel — including LangChain, LlamaIndex, CrewAI, and Smolagents — with zero SDK changes required.

A minimal deployment requires only a single `docker compose up`. A production Kubernetes deployment adds Postgres, Redis, and S3-compatible object storage — all dependencies that are universally approved in enterprise environments.

---

## User Stories

### Trace Ingestion

1. As a backend engineer, I want to point my existing OpenTelemetry exporter at Lantern's ingest endpoint, so that I can start capturing traces without changing my instrumentation code.
2. As a backend engineer, I want to send traces using OTLP over HTTP in both protobuf and JSON format, so that I can use any OTel-compatible SDK or library.
3. As a backend engineer, I want Lantern to automatically extract LLM-specific attributes from OTel GenAI semantic conventions, so that model name, token counts, and prompt/completion content are structured fields rather than raw attributes.
4. As a backend engineer, I want to send traces using Lantern's native REST API, so that I can instrument code without an OTel SDK dependency.
5. As a backend engineer, I want trace ingestion to have minimal latency impact on my production agents, so that observability does not degrade the user experience.
6. As a platform engineer, I want the ingest API to buffer writes in Redis, so that a temporary Writer Service outage does not result in lost traces.
7. As a platform engineer, I want the ingest API to authenticate requests using project-scoped API keys, so that only authorized services can write traces to a project.
8. As a platform engineer, I want to create service-scoped API keys that carry a service name label, so that I can audit which specific service or agent produced which traces.
9. As a platform engineer, I want API keys to follow a predictable format (`ltn_proj_` and `ltn_svc_` prefixes), so that keys are easily identifiable in logs and secret scanners.

### Trace Visualization

10. As an ML engineer, I want to view a paginated, filterable list of traces for my project, so that I can quickly find traces of interest.
11. As an ML engineer, I want to filter traces by time range, model, span type, cost, duration, and score, so that I can narrow down to relevant traces efficiently.
12. As an ML engineer, I want to view a waterfall visualization of a trace's full span tree, so that I can understand the sequence and nesting of operations within an agent run.
13. As an ML engineer, I want to view the input and output of each span within a trace, so that I can inspect what was sent to and received from each model or tool.
14. As an ML engineer, I want to see token usage and cost broken down per span, so that I can identify which parts of my agent are most expensive.
15. As an ML engineer, I want to see which prompt version was used to generate each span, so that I can correlate trace quality with prompt changes.
16. As an ML engineer, I want to see all scores attached to a span inline in the trace detail view, so that I can understand how evaluations judged that specific invocation.
17. As an ML engineer, I want recent traces to appear in the UI within 30 seconds of ingestion, so that I can observe agent behavior in near real-time.

### Dashboard and Analytics

18. As an ML engineer, I want a dashboard showing cost over time broken down by model, so that I can track spending trends and identify regressions.
19. As an ML engineer, I want to see token usage trends over time, so that I can understand how my agents' consumption is growing.
20. As an ML engineer, I want to see latency percentiles (p50, p95, p99) per model and span type, so that I can identify performance bottlenecks.
21. As an ML engineer, I want to see error rates over time, so that I can detect degradations in model reliability.
22. As an ML engineer, I want to see score distributions over time per evaluation name, so that I can track quality trends across deployments.
23. As a platform engineer, I want Lantern to expose a Prometheus `/metrics` endpoint, so that I can scrape operational metrics into my existing Grafana stack.
24. As a platform engineer, I want Prometheus metrics scoped per project, so that I can build per-team cost and quality dashboards.
25. As a platform engineer, I want metrics covering ingest queue depth, Writer flush health, S3 sync timestamps, and DuckDB file size, so that I can alert on Lantern's own operational health.

### Prompt Registry

26. As an ML engineer, I want to store prompt templates in Lantern's prompt registry with versioning, so that I have a single source of truth for all prompts used by my agents and judges.
27. As an ML engineer, I want to assign labels (`production`, `staging`, `dev`) to specific prompt versions, so that I can promote prompts through environments without changing code.
28. As a backend engineer, I want my production agent to fetch its prompt at runtime via `GET /api/v1/prompts/{name}?label=production`, so that prompt changes can be deployed without redeploying the agent.
29. As an ML engineer, I want to view the full version history of a prompt, so that I can understand how it has evolved over time.
30. As an ML engineer, I want to diff two prompt versions side by side, so that I can understand exactly what changed between versions.
31. As an ML engineer, I want to run a prompt version in a playground against a live model, so that I can evaluate its output before promoting it to production.
32. As an engineer, I want prompt versions to be immutable once created, so that I can trust that a version number always refers to the exact same template.
33. As an engineer, I want prompt templates to support variable interpolation using `{{variable}}` syntax, so that I can reuse templates with different inputs.
34. As an engineer, I want prompt versions to store model configuration (model name, temperature, max tokens) alongside the template, so that the full generation config is captured per version.

### Evaluation — LLM-as-a-Judge

35. As an ML engineer, I want to define evaluation rules in the UI that specify a judge model, a judge prompt template, and a scoring rubric, so that non-engineers can configure evaluations without writing code.
36. As an engineer, I want to define evaluation rules in code or config files, so that eval configurations are version-controlled alongside my application.
37. As an ML engineer, I want evaluation rules to trigger automatically on every new trace that matches a filter condition, so that I get continuous quality signals without manual intervention.
38. As an ML engineer, I want evaluation rules to support sampling (e.g. score 10% of traces), so that I can control evaluation cost for high-volume projects.
39. As an ML engineer, I want eval scores to appear on traces within 30 seconds of the trace being ingested, so that the feedback loop is tight enough to be useful.
40. As an ML engineer, I want each score to include the judge's reasoning alongside the numeric value, so that I can understand why a trace received a particular score.
41. As an ML engineer, I want scores to record which judge model and prompt version produced them, so that I can audit and reproduce evaluation results.
42. As an ML engineer, I want to write scores manually via the REST API, so that I can integrate human feedback or rule-based scoring alongside LLM judges.
43. As an ML engineer, I want to see score trends over time per evaluation name in the dashboard, so that I can track quality improvements from prompt changes.

### Datasets

44. As an ML engineer, I want to create a dataset by curating spans from the trace explorer, so that I can build evaluation datasets from real production traffic.
45. As an ML engineer, I want to upload a manually curated dataset of input/expected output pairs, so that I can use golden datasets created outside of production traffic.
46. As an ML engineer, I want to browse dataset items and see their source span, input, expected output, and any scores, so that I can inspect and curate dataset quality.
47. As an ML engineer, I want to run a dataset against an evaluation rule, so that I can score a set of examples in batch.
48. As an ML engineer, I want to run a dataset through a prompt version in the playground, so that I can compare prompt versions against a held-out evaluation set.
49. As an ML engineer, I want to view dataset run history with aggregate scores, so that I can compare runs across prompt versions and judge configurations.
50. As an ML engineer, I want cold dataset items (older than 48 hours) to remain queryable for dataset building, even if they are slower to retrieve, so that I can curate datasets from historical traffic.

### Deployment and Operations

51. As a platform engineer, I want to run Lantern with a single `docker compose up` command for a proof-of-concept deployment, so that I can evaluate it without provisioning cloud infrastructure.
52. As a platform engineer, I want the demo deployment to use SQLite and MinIO, so that there are zero external dependencies for evaluation.
53. As a platform engineer, I want a production Helm chart for Kubernetes deployment, so that I can deploy Lantern using standard GitOps workflows.
54. As a platform engineer, I want a Kustomize base configuration, so that I can manage Lantern deployments with Flux CD or ArgoCD.
55. As a platform engineer, I want the Query API pods to be fully stateless, so that they can be scheduled on any node and scaled horizontally without volume constraints.
56. As a platform engineer, I want DuckDB snapshots synced to S3 every 30 seconds, so that Query API replicas always serve data no older than 30 seconds.
57. As a platform engineer, I want trace data older than 48 hours automatically flushed to S3 as Parquet files partitioned by project and date, so that the hot DuckDB file stays small and historical data is retained cheaply.
58. As a platform engineer, I want to use any S3-compatible storage (AWS S3, GCS, Azure Blob, MinIO), so that I am not locked into a specific cloud provider.
59. As a platform engineer, I want the Writer Service to recover automatically after a crash by draining Redis, so that no traces are lost during Writer restarts.
60. As a platform engineer, I want Lantern's configuration to be driven by a YAML file with environment variable overrides, so that I can manage base config in version control and inject secrets via Kubernetes Secrets.

### Multi-tenancy and Access Control

61. As an organization admin, I want to create multiple projects within my organization, so that I can separate traces from different applications or teams.
62. As an organization admin, I want to manage team members and their access to projects, so that I can control who can view and configure each project.
63. As a project admin, I want to create and revoke API keys for my project, so that I can manage service access without organization-level privileges.
64. As a project admin, I want to see which service name label each API key is associated with, so that I can audit ingestion sources.

### SDK and Developer Experience

65. As a Python developer, I want a `@lantern.trace` decorator, so that I can instrument my agent functions with two lines of code.
66. As a Python developer, I want the Lantern Python SDK to configure an OTel exporter pointing at Lantern automatically, so that I can use it alongside existing OTel instrumentation.
67. As a TypeScript developer, I want a TypeScript SDK with equivalent tracing primitives, so that I can instrument Node.js agents.
68. As a developer, I want prompt fetch to be cached client-side in the SDK, so that fetching a production prompt does not add latency to every agent invocation.

### Cost Tracking

69. As an ML engineer, I want Lantern to automatically compute the USD cost of each LLM span from the model name and token counts, so that I do not have to instrument cost tracking myself.
70. As a platform engineer, I want Lantern to pull model pricing from LiteLLM's pricing database at startup, so that cost calculations are kept current without manual updates.
71. As a platform engineer, I want to override pricing for specific models in the Lantern config, so that I can account for fine-tuned models or enterprise pricing agreements.

---

## Implementation Decisions

### Service Architecture

Lantern is composed of five independently deployable services:

- **Ingest API** — accepts OTLP (protobuf and JSON) and native REST trace payloads, validates API keys against the metadata store, and enqueues span batches to a Redis ingest queue. Written in Go.
- **Writer Service** — single-replica service that drains the Redis ingest queue, batches writes to a local DuckDB file, syncs DuckDB snapshots to S3 every 30 seconds, flushes aged partitions (>48 hours) to S3 as Hive-partitioned Parquet files every 30 minutes, and enqueues eval jobs to a Redis eval queue. Written in Go.
- **Query API** — stateless read service that pulls the latest DuckDB snapshot from S3 on startup and polls for updates every 30 seconds, queries the local snapshot for hot data, and queries S3 Parquet via DuckDB's httpfs extension for cold data. Exposes REST, analytics DSL, and Prometheus metrics endpoints. Written in Go.
- **Eval Workers** — horizontally scalable worker pool that drains the Redis eval queue, executes LLM-as-a-Judge evaluations, and writes scores back to the Writer Service via its REST API. Written in Go.
- **UI** — static single-page application built with Vite, React, shadcn/ui, and Recharts. Served via Nginx. Communicates exclusively with the Query API.

### Storage Architecture

- **Hot store:** DuckDB file owned exclusively by the Writer Service in read-write mode. Query API opens the same snapshot (pulled from S3) in read-only mode. Single-writer guarantee maintained by Writer Service's single-replica constraint.
- **Cold store:** Hive-partitioned Parquet files on S3 at `s3://bucket/archive/project_id={id}/date={date}/`. Queried via DuckDB's httpfs extension with partition pruning.
- **Snapshot store:** Live DuckDB file synced to `s3://bucket/snapshots/traces.duckdb` every 30 seconds. Query API pods pull this on startup and poll for updates.
- **Metadata store:** Postgres (production) or SQLite (demo) for all transactional, relational data. Abstracted behind a `MetadataStore` interface with implementations for each dialect. Migrations managed with golang-migrate using separate SQL files per dialect.

### Core Domain Model

The central fact table is `spans` — a wide, denormalized columnar table. High-cardinality analytical fields (model, token counts, cost, duration) are extracted as typed columns. Arbitrary OTel attributes are stored in a JSON overflow column. Key tables:

- `spans` — primary fact table in DuckDB (hot) and Parquet (cold)
- `scores` — eval scores keyed to span and trace IDs, stored in DuckDB
- `prompt_versions` — immutable versioned prompt templates with label assignments, stored in metadata store
- `datasets`, `dataset_items`, `dataset_runs`, `dataset_run_items` — dataset and eval run metadata, stored in metadata store
- `eval_rules` — evaluation trigger configurations, stored in metadata store
- `organizations`, `projects`, `users`, `api_keys` — access control, stored in metadata store

### OTel Compatibility

The Ingest API acts as an OTel Collector replacement. It accepts `POST /v1/traces` with `ExportTraceServiceRequest` payloads in both protobuf and JSON encoding. A translation layer maps OTel GenAI semantic convention attributes (`gen_ai.request.model`, `gen_ai.usage.input_tokens`, etc.) to Lantern's internal span model. Remaining attributes are stored in the JSON overflow column. The native REST API (`POST /api/v1/spans`) accepts Lantern's internal span format directly for SDK use.

### Write Buffer and Durability

Redis serves as the durability buffer between the Ingest API and the Writer Service. If the Writer Service crashes, traces accumulate in Redis and are drained when the Writer recovers. Redis is also used as the eval job queue with separate queue keys for ingest and eval workloads. A single Redis instance serves both queues.

### Authentication

API keys are validated on every ingest request. Project-scoped keys (`ltn_proj_{random}`) grant write access to all resources within a project. Service-scoped keys (`ltn_svc_{random}`) are also project-scoped but carry a `service_name` label that is attached to every span they produce, enabling per-service audit trails. Key validation is performed against the metadata store with an in-memory cache to minimize latency on the ingest hot path.

### Configuration

All services are configured via Viper, which reads a `lantern.yaml` file with environment variable overrides. Docker Compose ships with a default `lantern.yaml`. Kubernetes deployments use ConfigMaps for base config and Secrets for sensitive values injected as environment variables.

### Pricing

At startup, the Writer Service fetches LiteLLM's model pricing JSON from its published URL and caches it in memory. Cost per span is computed from model name and token counts using this table. Users can override specific model prices in `lantern.yaml` to account for fine-tuned models or enterprise pricing.

### Prompt Fetch Caching

The `GET /api/v1/prompts/{name}` endpoint returns prompt versions by label or version number. Prompt versions are immutable once created, making them safe to cache aggressively. The Query API caches prompt responses in memory with a short TTL. The Python and TypeScript SDKs implement client-side caching so that prompt fetches do not add latency to production agent invocations.

### Deployment Profiles

- **Demo:** `docker compose up` with SQLite metadata store, local DuckDB file, and MinIO for S3-compatible object storage. Zero cloud dependencies.
- **Production:** Separate Kubernetes services for each component. Postgres via CloudNativePG operator. Redis via standard K8s deployment. DuckDB snapshot and Parquet archive on S3-compatible storage. Helm chart and Kustomize base provided.

### Metadata Store Abstraction

All services that access the metadata store do so through a `MetadataStore` interface. Concrete implementations exist for Postgres and SQLite. The active implementation is selected at startup based on the configured `database.driver` value. This allows Docker Compose and Kubernetes deployments to share identical service code with different database backends.

### Prometheus Metrics

The Query API exposes a `/metrics` endpoint using `prometheus/client_golang`. Metrics are labeled by `project_id` where applicable, with a config option to disable per-project labels for deployments with high project cardinality. Key metric families: ingest counters, token and cost counters, score histograms, eval latency histograms, Writer flush and S3 sync gauges, DuckDB file size gauge.

### Analytics Query DSL

The Query API exposes `POST /api/v1/analytics/spans` which accepts a structured query DSL (filters, aggregations, group-by, order-by, limit). This is compiled server-side into parameterized DuckDB SQL with `project_id` always enforced from the authenticated session. Raw SQL is never accepted from clients.

---

## Testing Decisions

### What Makes a Good Test

Tests should verify external behavior through public interfaces, not implementation details. A good test for Lantern asserts what a service returns or what it writes given a specific input — not how it internally transforms data. Tests should be runnable without external dependencies wherever possible by using interfaces and fakes rather than real Redis, DuckDB, or S3 connections.

### Modules to Test

**MetadataStore interface implementations (Postgres and SQLite)**
Both implementations must be tested against the same behavioral test suite by parameterizing tests over the interface. Tests verify that CRUD operations, API key validation, prompt version immutability, and migration application behave identically across both dialects. These are integration tests run against real database instances spun up in Docker.

**OTLP translation layer**
The translator that maps `ExportTraceServiceRequest` protobuf payloads to Lantern's internal span model is a pure function with no external dependencies. It should have exhaustive unit tests covering GenAI semantic convention attribute extraction, span type derivation, JSON overflow behavior for unknown attributes, and handling of malformed or incomplete payloads.

**Analytics DSL compiler**
The component that compiles the analytics query DSL into parameterized DuckDB SQL is a pure function. Unit tests should verify that all filter operators, aggregation functions, group-by clauses, and ordering produce correct SQL, and that `project_id` is always injected regardless of client input.

**Writer Service ingest pipeline**
The pipeline from Redis queue drain through DuckDB write should be tested with a fake Redis client and an in-memory DuckDB instance. Tests verify batch sizing, error handling on malformed spans, and that the DuckDB schema is correctly populated.

**S3 sync and Parquet flush**
The snapshot sync and hot-to-cold flush logic should be tested against a fake S3 interface. Tests verify sync frequency, partition naming conventions, that aged data is correctly identified and flushed, and that the hot DuckDB file is pruned after successful flush.

**Eval Worker judge pipeline**
The LLM judge execution pipeline should be tested with a fake LLM client. Tests verify that judge prompt templates are correctly rendered with span data, that scores are correctly structured and written back, and that sampling rules are respected.

**API key validation and caching**
The auth middleware should be tested as a unit with a fake MetadataStore. Tests verify that valid project and service keys are accepted, invalid keys are rejected, and that the in-memory cache correctly serves repeated validations without hitting the store.

**Prometheus metrics**
Tests verify that ingesting a known set of spans produces the expected metric counter and histogram values on the `/metrics` endpoint.

---

## Out of Scope

- **Writer Service high availability / active-passive failover** — the Writer Service runs as a single replica. Redis provides durability during Writer restarts. HA is a known limitation to be addressed in a future phase.
- **Cross-node DuckDB file sharing via networked filesystem** — the Writer Service's local DuckDB file is not shared via NFS or ReadWriteMany PVC. S3 snapshot sync is the mechanism for cross-node Query API access.
- **Raw SQL query endpoint** — no endpoint accepts raw DuckDB SQL from clients. The analytics DSL covers external query needs.
- **Grafana datasource plugin** — Prometheus `/metrics` is the Grafana integration path. A native Grafana JSON datasource is not in scope.
- **SSO / SAML / OIDC authentication** — user authentication in Phase 1 uses username/password. SSO is a future concern.
- **Kafka / Redpanda as an ingest buffer** — Redis is the only supported queue backend.
- **MotherDuck or networked DuckDB** — DuckDB is used exclusively in embedded mode.
- **Phase 2+ features** — Prompt Registry UI, Playground, Dataset curation UI, Eval Worker service, and hot/cold Parquet flush are out of scope for the Phase 1 MVP.

---

## Further Notes

- The project is named **Lantern**. Go module path: `github.com/{org}/lantern`. Docker Hub: `lantern/ingest`, `lantern/writer`, `lantern/query`, `lantern/eval`, `lantern/ui`.
- The repo is a Go workspace monorepo. Each service has its own `go.mod`. Shared domain types, the MetadataStore interface, Redis client, S3 client, auth middleware, and config loading live in a root-level `internal/` package imported via the Go workspace.
- The Python SDK is a first-class citizen of the monorepo from day one, located at `sdk/python/`. It provides a `@lantern.trace` decorator and an OTel exporter configuration helper.
- OTel compatibility is a hard requirement. Any LLM framework that emits OTel spans — LangChain, LlamaIndex, CrewAI, Smolagents, OpenLLMetry, OpenInference — must work against Lantern's ingest endpoint with zero SDK changes.
- DuckDB's single-writer constraint is the central architectural constraint. All design decisions around the Writer Service (single replica, S3 snapshot sync, read-only Query API connections) flow from this constraint.
- The 30-second staleness window for hot data is acceptable for the primary use case. Users viewing live eval scores on recent traces will see data no older than 30 seconds.
- LiteLLM's pricing JSON is fetched at Writer Service startup and cached in memory. If the fetch fails, Lantern falls back to a bundled pricing snapshot included in the binary.
