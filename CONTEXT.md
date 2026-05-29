# Omneval — Bounded Context

## Omneval Domain Terms

### Span
The central fact type. Represents a single unit of work within a trace — an LLM call, a tool invocation, an agent step, or an internal operation. Each Span has a `Kind` (`llm`, `tool`, `agent`, `chain`, `internal`). LLM spans carry typed fields: `Model`, `Input`, `Output`, `InputTokens`, `OutputTokens`, `CostUSD`. Non-LLM spans carry whatever OTel attributes arrived in the overflow `Attributes` map.

`Input` is a JSON array of message objects following OTel GenAI semantic conventions: `[{"role":"system","content":"..."},{"role":"user","content":"..."}]`. The system prompt is included as the first element by default; this can be suppressed via `OMNEVAL_INGEST_LOG_SYSTEM_PROMPT=false`. `Output` is a JSON array of completion message objects: `[{"role":"assistant","content":"..."}]`. This format is intentionally aligned with OTel GenAI conventions — Omneval extends them where necessary but does not deviate from them.

### Trace
A group of Spans that share a `TraceID`. Has a single root Span (the one with no `ParentID`). Visualized as a waterfall in the UI.

### Score
An evaluation result attached to a specific Span. Has a numeric `Value`, a `Reasoning` string, and metadata about which judge model and prompt version produced it. Scores may be written by Eval Workers (LLM-as-a-Judge) or by humans/rule-based systems via the REST API.

### Hot Store
The live DuckDB file owned exclusively by the Writer Service. The file lives on the Writer's StatefulSet PVC — it is the authoritative source of truth. A new Writer pod recovers by mounting the same PVC and reopening the file. On first deploy the file is created fresh and the schema in `internal/duckdb/schema.sql` is applied. Schema: `input` and `output` columns are native DuckDB `JSON` type; ART index on `(project_id, start_time)` covers the dominant filter pattern; `PRIMARY KEY (trace_id, span_id)` on spans enables idempotent upsert. The Writer syncs a snapshot to S3 every 30 seconds solely for Query API consumption — Query API pods are stateless and never mount the PVC. If Redis is unreachable, the Ingest API returns `503 Service Unavailable` immediately — OTel SDK retry handles recovery. Duplicate span arrivals (SDK retry after 503) are handled by `INSERT OR REPLACE` on the primary key — idempotent, no duplicate rows in the waterfall.

### Hot/Cold Query Merge
The Query API always issues a UNION of the DuckDB snapshot (hot) and `read_parquet(...)` with `hive_partitioning=true` (cold), regardless of the query's time range. DuckDB's Hive partition pruning restricts S3 scans to only the relevant date partitions, making the UNION cheap when the range is fully within the hot window. No manifest table is maintained — S3 LIST + partition pruning is the discovery mechanism. The Analytics DSL compiler never inspects the time range to choose between hot-only, cold-only, or UNION paths.

### Cold Store
Hive-partitioned Parquet files on S3. Spans land at `s3://bucket/archive/project_id={id}/date={date}/spans/` and their scores land at `s3://bucket/archive/project_id={id}/date={date}/scores/`. Both are partitioned by the **span's** `StartTime` date, so a score always lives next to the span it annotates regardless of when the score was created. Contains spans and scores older than the hot window. Queried by the Query API via DuckDB's httpfs extension.

### Hot Window
The age threshold separating hot (DuckDB) from cold (Parquet on S3) storage. Spans older than this threshold are archived from DuckDB to cold storage. Configured globally at startup via `writer.flush_age_days` in `omneval.yaml` (default: 2 days / 48 hours). Not configurable per project. The Writer Service uses this value to schedule archival sweeps; the Query API always issues a hot+cold UNION and relies on Hive partition pruning to avoid unnecessary S3 scans.

Archival sweeps process spans and scores together for each `(project_id, date)` partition in a single operation — both Parquet files are written before either is deleted from DuckDB. A partition is never in a state where cold spans exist without cold scores. Parquet files are written via DuckDB's `COPY (SELECT ...) TO 's3://...' (FORMAT PARQUET)` using the `httpfs` extension — no separate Parquet library. S3 credentials come from `storage` config.

### Snapshot
The live DuckDB file synced to S3 by the Writer Service at an interval configured by `writer.sync_interval` in `omneval.yaml` (default: `30s`). Query API pods download the snapshot to a local path (`query.duckdb_path`, default `/tmp/omneval-snapshot.duckdb`) on startup, then poll S3 on the `query.sync_interval` cadence (default `30s`) to detect a newer object and re-download. No Redis pub-sub or push notification — S3 polling is sufficient given the staleness budget. Staleness for hot data is at most one sync interval. **Kubernetes note:** the snapshot path must not be on a ReadWriteOnce PVC shared with another pod — use an emptyDir or the pod's local ephemeral storage.

### Phase 1 UI Routes
`/login` (email+password), `/traces` (paginated filterable list), `/traces/:traceId` (span waterfall + detail panel with inline scores), `/dashboard` (cost/token/latency/error-rate charts), `/settings/project` (API key create/revoke), `/settings/team` (user invite). A project switcher dropdown in the nav covers multi-project deployments. Eval rules UI, Prompt Registry UI, Playground, and Dataset UI are Phase 2+.

### Query API Endpoints
- `POST /api/v1/spans/query` — paginated span list (`SpanQueryRequest`, keyset cursor)
- `GET /api/v1/traces/:traceId` — full waterfall for a single trace
- `POST /api/v1/analytics/spans` — Analytics DSL query
- `POST /api/v1/prompts` — create prompt version
- `GET /api/v1/prompts/:name` — resolve by `?version=N` or `?label=<label>`
- `PUT /api/v1/prompts/:name/labels/:label` — reassign label to a version
- `POST /api/v1/scores` — manual score write
- `POST /api/v1/users/invite` — create user with one-time temp password (admin only)
- `PUT /api/v1/users/me/password` — change own password (validates current password first)
- `POST /login`, `POST /logout` — session management
- `GET /api/v1/projects` — list projects for org (drives project switcher)
- Static files: all unmatched routes serve the embedded React SPA

### Span List Cursor
Keyset pagination on `(start_time DESC, span_id ASC)`. The cursor is a base64-encoded JSON blob of `{"start_time": "<RFC3339nano>", "span_id": "<hex>"}` representing the last row of the previous page. The next query appends `WHERE (start_time, span_id) < ($last_time, $last_id)`. Stable under concurrent span ingestion — no offset drift. The existing ART index on `(project_id, start_time)` covers the cursor predicate. The cursor is opaque to clients.

### Native REST Span API
`POST /api/v1/spans` accepts `{"spans": [...]}`. The SDK generates `span_id` (8 hex bytes) and `trace_id` (16 hex bytes) — the Ingest API validates but does not generate them. `input` and `output` fields accept either a JSON array of `{role, content}` objects or a plain string; plain strings are normalized to `[{"role":"assistant","content":"..."}]` on ingestion so DuckDB always stores a JSON array. Returns `202 Accepted` with no body — the span is enqueued, not yet written. The OTLP endpoint (`POST /v1/traces`) returns a standard empty `ExportTraceServiceResponse` encoded to match the request's `Content-Type`.

### Score Write-back
Eval Workers write completed scores to the Writer Service via `POST /internal/v1/scores`. This is an internal-only endpoint, not exposed to external clients. Eval Workers retry with exponential backoff for up to 5 minutes on failure (covering Writer restarts). Scores that still fail after 5 minutes are logged and dropped — losing an occasional score is acceptable; blocking the eval pipeline is not. REST is used (not a return queue) because score acceptance is a synchronous confirmation needed before the Eval Worker can acknowledge the job as done.

### Prompt Cache
The Query API caches prompt lookups in two independent caches. Version lookups (`/prompts/{name}?version=N`) use an LRU cache with no TTL — versions are immutable. Label lookups (`/prompts/{name}?label=production`) use a 30-second TTL, matching the DuckDB snapshot staleness window. A label reassignment is visible to production agents within 30 seconds.

### Metrics
All services expose Prometheus metrics on a configurable address (default `:9090/metrics`). `metrics.disable_project_labels: true` suppresses `project_id` label cardinality globally. Phase 1 metric set:
- **Ingest API**: `omneval_ingest_spans_received_total{project_id}`, `omneval_ingest_enqueue_errors_total`, `omneval_ingest_request_duration_seconds`
- **Writer Service**: `omneval_writer_spans_written_total{project_id}`, `omneval_writer_duckdb_write_duration_seconds`, `omneval_writer_snapshot_sync_duration_seconds`, `omneval_writer_archive_spans_total{project_id}`
- **Query API**: `omneval_query_request_duration_seconds{endpoint}`, `omneval_query_snapshot_age_seconds`
- **Eval Workers**: `omneval_eval_jobs_processed_total{project_id,rule_id}`, `omneval_eval_job_duration_seconds`, `omneval_eval_judge_errors_total`

### Ingest Queue
Redis List at key `omneval:ingest:spans`. Ingest API translates OTLP→`domain.Span` and `RPUSH`es one JSON-encoded `[]*domain.Span` batch per request. Writer Service `BLPOP`s batches and writes to DuckDB. Translation happens in the Ingest API so the queue payload is always in the internal domain format regardless of whether spans arrived via OTLP or the native REST API.

### Eval Queue
Redis List at key `omneval:eval:jobs`. Writer Service `RPUSH`es one `domain.EvalJob` per entry after writing spans. Eval Workers `BLPOP` jobs and execute LLM-as-a-Judge evaluations.

Both queues share a single Redis instance configured via `redis` in `omneval.yaml`. No separate Redis topology for ingest vs. eval in Phase 1.

### Pricing Table
Loaded at Writer Service startup by fetching LiteLLM's `model_prices_and_context_window.json`. Falls back to a bundled snapshot embedded in the binary (`internal/pricing/model_prices_and_context_window.json`) if the live fetch fails. Prices are stored internally as USD per token. Config overrides (`omneval.yaml`) are expressed in USD per million tokens (human-readable) and converted to per-token at load time. Cost formula: `cost_usd = (input_tokens × input_per_token) + (output_tokens × output_per_token)`. `CostUSD` is computed in the Writer Service when writing a span to DuckDB — pre-computed at write time so query-time aggregations need no re-computation. Unknown models store `CostUSD = 0`.

### OTLP Translation
The `internal/otlp` package translates `ResourceSpans` into `domain.Span` values. The Ingest API accepts OTLP at `POST /v1/traces` as both `Content-Type: application/x-protobuf` (default for OTel SDKs) and `Content-Type: application/json`. Both encodings are decoded into the same `[]ResourceSpans` input before translation. Key mapping rules: `gen_ai.request.model`→`Model`; `gen_ai.usage.input_tokens` (or legacy `prompt_tokens`)→`InputTokens`; `gen_ai.usage.output_tokens` (or legacy `completion_tokens`)→`OutputTokens`; `gen_ai.prompt.N.{role,content}` arrays→`Input` JSON; `gen_ai.completion.N.{role,content}`→`Output` JSON; `omneval.prompt.name`/`omneval.prompt.version`→prompt linkage. `service.name` comes from the OTLP Resource; a service-scoped API key's `ServiceName` overrides it. `Kind` is derived: explicit `omneval.kind` attribute wins, then `gen_ai.*` presence→`llm`, then `tool.*` presence→`tool`, else `internal`. All unmapped attributes land in `Span.Attributes` overflow map.

### Analytics DSL
The structured query language accepted by `POST /api/v1/analytics/spans`. Compiled server-side into parameterized DuckDB SQL; `project_id` is always injected. Supports `from`/`to` (absolute UTC `time.Time` — the client resolves any relative shortcuts before sending), `filters` (field + allowlisted op + value), `aggregations` (allowlisted function + field + alias), `group_by` (structured objects with optional `truncate` enum: `hour|day|week|month`), `order_by`, and `limit`. `duration_ms` is a virtual field compiled to `EPOCH_MS(end_time) - EPOCH_MS(start_time)`. Percentile aggregations (`p50`, `p95`, `p99`) compile to `approx_quantile(field, 0.X)`. Raw SQL strings are never accepted from clients. The compiler always emits a hot+cold UNION regardless of the time range.

### Eval Rule
A configuration that specifies a judge model, a judge prompt, an `EvalFilter`, and a sample rate. Triggers automatically on ingested spans that match the filter. Stored in the metadata store. The filter is evaluated in-process by the Writer Service against the `domain.Span` struct — no DuckDB query on the ingest hot path. The Writer loads all active rules at startup and refreshes them every 60 seconds via a background ticker — new rules start firing within one minute of creation. Sampling: `rand.Float64() < rule.SampleRate` per matching span per rule; `1.0` = score every match, `0.0` = effectively disabled.

### Eval Worker LLM Endpoint
Eval Workers call a judge LLM via a configurable OpenAI-compatible endpoint: `eval.llm_base_url` and `eval.llm_api_key` in `omneval.yaml`. Compatible with OpenAI, Anthropic (via LiteLLM proxy), Ollama, or any OpenAI-compatible server. The specific judge model is specified per `EvalRule`, not in global config.

### EvalFilter
A conjunction (AND) of optional conditions on a Span. Fields: `Kind`, `Model`, `ServiceName`, `PromptName`, `StatusCode`, `MinCostUSD`, `MaxCostUSD`, `MinDurationMS`, `MaxDurationMS`. All fields are pointer types; nil means the condition is inactive. AND-only in Phase 1 — OR/NOT deferred. Token-range filters (`MinInputTokens`, etc.) also deferred.

### API Key
Two kinds: `oev_proj_<43 base58 chars>` (project-scoped write access) and `oev_svc_<43 base58 chars>` (service-scoped, attaches a `service_name` label to every span). Generated with 32 bytes of `crypto/rand` encoded as base58. Only the SHA-256 hex hash is stored in the metadata store — the raw key is shown to the user exactly once at creation. The Ingest API validates via a `CachingValidator` with a 60-second TTL; a revoked key may be accepted for up to 60 seconds after revocation.

### Prompt Registry
The versioned store of prompt templates. Each `PromptVersion` is immutable once created. Labels (`production`, `staging`, `dev`) are mutable pointers to a specific version. All Prompt Registry CRUD lives on the Query API — it owns both reads and writes to the metadata store for prompts. The Writer Service is dedicated to trace processing and never handles prompt management requests. Endpoints: `POST /api/v1/prompts` (create version), `GET /api/v1/prompts/:name` (resolve by version or label), `PUT /api/v1/prompts/:name/labels/:label` (reassign label).

### Metadata Store
The transactional, relational database (Postgres in production, SQLite in demo) holding all non-trace data: orgs, projects, users, API keys, prompt versions, eval rules, datasets. Schema in `internal/metadata/{postgres,sqlite}/migrations/0001_init.up.sql`. Users authenticate with email + bcrypt password hash. Sessions are server-side: a `sessions` table holds `session_id → user_id + expires_at`. The UI receives a session cookie (`omneval_session`, `HttpOnly`, `Secure` — disable via `auth.secure_cookie: false` for local dev, `SameSite=Lax`, TTL configured via `auth.session_ttl`, default `168h` / 7 days). The Query API validates the session cookie on every UI-facing request. OAuth/OIDC is out of scope until a future phase. The React UI is served as embedded static files from the Query API binary via Go's `embed.FS`.

### User Bootstrap and Invite
On startup, if no users exist and `auth.admin_email` / `auth.admin_password` are set (typically via `OMNEVAL_AUTH_ADMIN_EMAIL` / `OMNEVAL_AUTH_ADMIN_PASSWORD` environment variables), the Query API creates the initial admin user automatically. For subsequent team members: an admin invites by email via `POST /api/v1/users/invite`, which creates the user record with a randomly-generated temporary password returned once in the response. The inviter shares the temporary password out-of-band. No email infrastructure required in Phase 1.

### SDK
Two SDKs live in `sdk/`: `sdk/go` (Go module `github.com/omneval/omneval/sdk/go`) and `sdk/python`. Both provide: (1) an OTLP HTTP exporter configured via `Configure(endpoint, apiKey)` that sends spans to the Omneval Ingest API; (2) span lifecycle helpers (`StartSpan`/`EndSpan` in Go, `@trace` decorator in Python) with context propagation (Go: `context.Context`; Python: `contextvars`); (3) a `Client` / `OmnevalClient` for prompt fetch with client-side caching and manual score writes. The SDK is a client library only — no HTTP server.

### HTTP Router
All Go HTTP services use `github.com/go-chi/chi/v5` — a thin wrapper around `net/http` that uses standard `http.Handler` throughout and integrates cleanly with `httptest`. No framework magic.

### Logging
All Go services use `log/slog` (stdlib) for structured logging. Log levels: `Info` for normal operations, `Warn` for recoverable anomalies, `Error` for failures that need attention. Every log call includes relevant context as key-value pairs (e.g., `slog.Error("failed to flush", "project_id", pid, "err", err)`). `log.Printf` and `fmt.Println` are never used in production code paths.

### Phase 1 Vertical Slice Order
Implementation proceeds as vertical TDD slices — each slice has a failing test first, then code to make it pass, then refactor. Dependency-ordered:

1. **Metadata Store + Config** — foundation, no deps
2. **Ingest API → Redis** — REST span ingest, enqueue to Redis
3. **Writer Service → DuckDB** — dequeue, write to DuckDB, snapshot to S3
4. **Query API → span list + trace** — read snapshot, serve `/api/v1/spans/query` and `/api/v1/traces/:id`
5. **Auth** — login, session cookie, admin bootstrap
6. **React UI shell + /traces** — SPA served from embed.FS, paginated trace list
7. **Analytics DSL + /dashboard** — compiler, hot+cold UNION, cost/latency charts
8. **OTLP ingest** — proto+JSON decode, two-step translation, same Redis enqueue as slice 2
9. **Eval pipeline** — rule cache, eval queue, Eval Workers, score write-back
10. **Prompt Registry** — version create, label assign, caching client
11. **SDK (Go + Python)** — tracer + client wired to Ingest API
12. **Archival sweep** — hot→cold Parquet flush, S3 COPY via httpfs

Slices 8–12 are independent of each other once slices 1–4 are done.

### Disaster Recovery
Cold Parquet on S3 is the DR story in Phase 1. The hot window is intentionally short (`writer.flush_age_days`, default 2 days), so a PVC loss is bounded to at most 2 days of recent spans. No additional PVC backup mechanism in Phase 1. Future: Kubernetes `VolumeSnapshot` support will be added to enable point-in-time PVC backups of the hot store.

### CORS
CORS is enabled on the Ingest API only — the Query API (same-origin with the UI) and Writer Service (internal-only) do not need it. Configurable via `ingest.cors_allowed_origins` in `omneval.yaml`; defaults to `*` (the API key is the auth mechanism). Allowed methods: `POST, OPTIONS`. Allowed headers: `Content-Type, Authorization`. Preflight `OPTIONS` requests return `204`.

### Graceful Shutdown
All services listen for `SIGTERM` (Kubernetes pod termination) and `SIGINT` (dev Ctrl-C). On signal: (1) stop accepting new connections via `http.Server.Shutdown(ctx)`; (2) drain in-flight HTTP requests with a 30-second timeout; (3) service-specific teardown. **Ingest API**: no extra teardown — Redis enqueue completes within the drain. **Writer Service**: finish the current DuckDB write batch; perform a final snapshot sync to S3; do not start a new archival sweep after signal. **Query API**: no extra teardown beyond HTTP drain. **Eval Workers**: finish the current eval job (LLM call may take tens of seconds) with a 120-second drain; do not `BLPOP` a new job after signal.

### Health and Readiness Probes
Each service exposes two routes. `GET /healthz` — liveness; returns `200 OK` if the process is alive, no external checks. `GET /readyz` — readiness; returns `200 OK` only when the service is ready for traffic, `503` otherwise. Kubernetes removes a pod from load balancer rotation while `/readyz` returns 503. Readiness gates per service:
- **Ingest API**: Redis `PING` succeeds
- **Writer Service**: DuckDB file open and writable; Redis `PING` succeeds
- **Query API**: snapshot file exists on disk (initial download complete); metadata store reachable
- **Eval Workers**: Redis `PING` succeeds

---

## Sandcastle / Orchestration Terms

### Sandcastle Label
The GitHub issue label (`Sandcastle`) that marks an issue as eligible for autonomous agent pickup via the Dev Loop. Applied manually by the developer when the issue is considered ready for agent work. Removed implicitly when the issue is closed after a successful merge.
_Avoid_: agent label, ready label

### Dev Loop
The multi-phase autonomous workflow that maintains and improves this codebase. Replaces the former TypeScript-based Sandcastle Orchestration. Runs on the homelab Kubernetes cluster via Temporal. Phases: Plan → Phase Gate (Discord approval) → Execute (parallel Agent Execution Jobs, one per issue) → Review → Phase Gate → Merge → Summarization. See `zbloss/home-server` `CONTEXT.md` for full definition of Dev Loop terms.
_Avoid_: Sandcastle Orchestration, agent pipeline, CI loop

### Local Model
The Qwen3-27b model served via `llama.cpp` on the DGX Spark at `http://192.168.68.104/v1` (external DNS: `https://dgx.blosshomelab.com`). Used as the LLM backend for all Dev Loop agents. Agent Execution Jobs inside Kubernetes reach it via the cluster-internal hostname or the fixed IP.
_Avoid_: local LLM, llama server, DGX model
