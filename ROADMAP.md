# Omneval — Roadmap

Phase 1 implementation progress. All Phase 1 slices are complete. Work is organized as vertical TDD slices, each building on the previous ones.

## Completed

### Slice 1: Metadata Store + Config ✅ (Issues #1, #2, #19)

- [x] Viper config loading with `omneval.yaml` + `OMNEVAL_*` env var overrides
- [x] Domain types: Span, Trace, Score, API Key, Prompt, EvalRule, Dataset
- [x] SQLite metadata store with full CRUD + integration tests
- [x] Postgres metadata store with full CRUD + integration tests (testcontainers)
- [x] Migrations for both dialects
- [x] API key generation (base58, SHA-256 hash storage) and validation with 60s TTL cache
- [x] DuckDB schema and helper

### Slice 2: Ingest API → Redis ✅ (Issues #3, #4, #22)

- [x] REST span ingest endpoint (`POST /api/v1/spans`)
- [x] OTLP span ingest (`POST /v1/traces`, protobuf + JSON)
- [x] Redis queue enqueue (`RPUSH` JSON batches to `omneval:ingest:spans`)
- [x] CORS middleware for browser SDK support
- [x] API key authentication (project + service scoped keys)
- [x] `503 Service Unavailable` when Redis is unreachable
- [x] Structured logging with `log/slog`
- [x] Prometheus metrics (`omneval_ingest_spans_received_total`, enqueue errors)

### Slice 3: Writer Service → DuckDB ✅ (Issues #5, #6, #21, #23)

- [x] Redis dequeue (`BLPOP` batches from ingest queue)
- [x] DuckDB batch writes with idempotent upserts (`INSERT OR REPLACE` on PK)
- [x] Cost pre-computation at write time (LiteLLM pricing table + bundled fallback)
- [x] S3 snapshot sync (configurable interval, default 30s)
- [x] S3 sync with ETag/LastModified comparison to avoid unnecessary downloads
- [x] Prometheus metrics (`omneval_writer_spans_written_total`, durations)
- [x] ObjectStore abstraction (S3, mock, failing)
- [x] Graceful shutdown on all services
- [x] Health/readiness probes (`/healthz`, `/readyz`)

### Slice 4: Query API → Span List + Trace ✅ (Issues #7, #8, #20)

- [x] S3 snapshot download on startup + S3 polling for updates
- [x] ~~DuckDB opened read-only for query safety during snapshot swaps~~ (code drifted on the legacy snapshot path: the snapshot is opened read-write, which made admin deletes cosmetic — fixed on the Lake path by #91, which gives the Query API a dedicated read-write Lake attachment for durable admin deletes alongside its read-only attachment for span reads; the legacy snapshot path remains broken until cutover, #90)
- [x] Span list endpoint (`POST /api/v1/spans/query`) with keyset cursor pagination
- [x] Field filters validated against allowlisted column names and operators
- [x] Operator mapping (eq, neq, gt, gte, lt, lte, in) to SQL symbols
- [x] Trace waterfall endpoint (`GET /api/v1/traces/:traceId`) with nested span tree
- [x] Inline scores attached to parent spans in trace detail
- [x] Cursor base64-encode/decode with unit tests
- [x] DuckDB integration tests
- [x] Hot+cold UNION query pattern (cold side is S3 Parquet with `read_parquet` + hive partitioning)
- [x] Prometheus metrics and `/metrics` endpoint
- [x] Graceful score table fallback when missing

### Slice 5: Auth ✅ (Issue #9)

- [x] Login endpoint with email + password
- [x] Session cookie management (HttpOnly, Secure, SameSite)
- [x] Admin bootstrap via environment variables
- [x] User invite with one-time temporary password
- [x] Password change endpoint
- [x] Session cookie middleware on Query API routes

### Slice 6: React UI ✅ (Issues #10, #28, #29)

- [x] React SPA scaffold (Vite + TypeScript)
- [x] `/login` page
- [x] `/traces` paginated filterable list
- [x] `/traces/:traceId` span waterfall view
- [x] Dashboard with cost/token/latency charts
- [x] Nav with project switcher
- [x] Embedded into Query API binary via `embed.FS`
- [x] Tailwind CSS v4 with custom Omneval dark theme (#000000 bg, orange accents)
- [x] Collapsible Sidebar with 5 navigation sections
- [x] Header with project/time-range/environment selectors
- [x] Dashboard widgets: traces-by-name bar chart, model costs table, scores empty state, traces-by-time line chart, model usage tabs, user consumption bar chart
- [x] Traces page: filter sidebar, multi-select trace name filter, hover glow states, bookmark star toggle, pagination
- [x] Branding guide: TypeScript constants, CSS custom properties, comprehensive documentation

### Slice 7: Analytics DSL + /dashboard ✅ (Issue #11)

- [x] Analytics DSL compiler (filter, aggregation, group-by, order-by, limit)
- [x] Hot+cold UNION always emitted (time-range independent)
- [x] `/api/v1/analytics/spans` endpoint
- [x] `/dashboard` cost/token/latency/error-rate charts
- [x] Percentile aggregations (`p50`, `p95`, `p99`) via `approx_quantile`

### Slice 8: OTLP Ingest ✅ (Issues #12, #13)

- [x] OTLP proto decode (`application/x-protobuf`)
- [x] OTLP JSON decode (`application/json`)
- [x] Two-step translation: wire format → `ResourceSpans` → `domain.Span`
- [x] GenAI semantic convention mapping (`gen_ai.request.model`, token counts, prompts)
- [x] Span kind derivation (explicit `omneval.kind`, `gen_ai.*`, `tool.*`, `internal`)
- [x] Overflow attributes for unmapped fields
- [x] Service name resolution (Resource `service.name` + API key override)
- [x] End-to-end integration with Writer Service

### Slice 9: Eval Pipeline ✅ (Issue #14)

- [x] Eval rule cache in Writer Service (reload every 60s)
- [x] Eval queue enqueue (`RPUSH` `domain.EvalJob` per matching span)
- [x] Eval Worker `BLPOP` from eval queue
- [x] Judge LLM call (OpenAI-compatible endpoint)
- [x] Score write-back to Writer Service via `POST /internal/v1/scores`
- [x] Exponential backoff retry (up to 5 minutes)
- [x] Sampling support (`SampleRate` per rule)

### Slice 10: Prompt Registry ✅ (Issue #15)

- [x] Prompt version create (`POST /api/v1/prompts`)
- [x] Prompt resolve by version or label (`GET /api/v1/prompts/:name`)
- [x] Label reassignment (`PUT /api/v1/prompts/:name/labels/:label`)
- [x] Version immutability
- [x] `{{variable}}` template interpolation
- [x] Prompt cache in Query API (LRU for versions, 30s TTL for labels)

### Slice 11: SDK ✅ (Issues #16, #17)

- [x] Go SDK — tracer (`StartSpan`/`EndSpan`, context propagation)
- [x] Go SDK — client (prompt fetch, score write)
- [x] Python SDK — tracer decorator (`@trace`)
- [x] Python SDK — OTel exporter auto-configuration
- [x] Python SDK — client (prompt fetch, score write)
- [x] Client-side prompt caching in both SDKs

### Slice 12: Archival Sweep ✅ (Issue #18)

- [x] Hot→cold Parquet flush via DuckDB `COPY ... TO` with `httpfs`
- [x] Hive-partitioned S3 layout (`project_id={id}/date={date}/`)
- [x] Atomic flush: both spans and scores written before DuckDB prune
- [x] Configurable flush age threshold (`writer.flush_age_days`, default 2 days)
- [x] Scheduled archival sweep in Writer Service

### Infrastructure ✅ (Issues #24–#27)

- [x] Updated README with architecture, API overview, SDKs, status
- [x] Docker Compose for local development (Postgres + Redis + MinIO)
- [x] Docker Compose startup fixes (health checks, dependency ordering)

## Phase 2 (complete except Playground UI)

- [x] Eval rules CRUD UI + rule matching preview
- [x] Prompt Registry UI (version management, label assignment, diff view)
- [x] Dataset curation UI (creation, items, runs with status)
- [x] TypeScript SDK (tracer, OTel exporter, client)
- [x] Helm chart + Kustomize base
- [x] Eval filter enhancements (OR / NOT, regex attribute filters, nested `attributes.key.subkey` access)
- [x] Conversations (grouping, list + detail UI)
- [x] Playground API (`POST /api/v1/playground/run`)
- [ ] Playground UI (prompt-and-test interface, edit-and-replay) — carried to Phase 3
- ~~Cross-node Writer HA / PVC fencing / VolumeSnapshot backup~~ — superseded by ADR-0004 (DuckLake removes the PVC and the single-writer constraint)

## Phase 3 — DuckLake migration (ADR-0004)

Storage rearchitecture to chase Langfuse-scale ingestion while keeping the no-ClickHouse pitch:

Ticketed as #83–#92 (dependency graph: #83/#84 unblocked → #85/#86/#87/#91 → #88/#89 → #92 → #90 cutover):

- [ ] DuckLake spans/scores tables + Writer dual-write behind `writer.lake.enabled` (#83)
- [ ] Move bookmarks from the hot DuckDB store to the metadata store (#84)
- [ ] Query API reads from the Lake behind `query.lake.enabled`; single-table DSL, read-time dedupe on trace detail (#85)
- [ ] S3-first ingestion: Ingest Buffer + Batch Ledger (`committed_batches`) idempotency (#86)
- [ ] One-off backfill: existing hot DuckDB + cold Parquet → Lake (#87)
- [x] Ingest Buffer reconciliation sweep + retention GC (#88 — `services/writer/internal/reconcile.Worker` runs on a plain ticker (DuckLake supports multi-writer, so no leader-election gate) and lists `buffer/` objects older than `grace_period_minutes`; re-enqueues Batch IDs absent from the Batch Ledger (recovery), and deletes committed objects past `retention_hours` (GC), never deleting uncommitted ones; configurable under `writer.reconciliation.*`)
- ~~Writer fleet: stateless Deployment, ~5s/16MB commit cadence, leader-run Table Maintenance (#89)~~ — superseded by ADR-0005 (#105): stateless Writer/Query/Eval and Table Maintenance are now inherent to the Quack Server design, not a separate milestone
- [x] Durable trace/project deletion against the Lake (#91 — `lake.Lake.DeleteProject` deletes spans+scores and runs `ducklake_expire_snapshots`/`ducklake_delete_orphaned_files`/`ducklake_cleanup_old_files`; the Query API gets a dedicated read-write `AdminLake` attachment so admin deletes are durable and visible immediately, fixing the legacy bug where admin deletes hit the local snapshot copy and resurrected within ~30s)
- [x] Quack Server as sole Lake gateway + Table Maintenance scheduler (#105, ADR-0005 — `services/quack` holds the only direct DuckLake Catalog/data-path connection; Writer/Query API/backfill attach via `quack.client.*` as thin Quack clients; deploy: `deploy/helm` gains a single-replica `quack-server` StatefulSet + Service wired into the shared ConfigMap/Secret, Writer/Query/Eval read `quack.client.url`/token from it; `deploy/docker-compose` gains an `omneval-quack` service with `OMNEVAL_QUACK_CLIENT_*` wiring for Writer/Query/Eval. `internal/leader`'s Redis SETNX election remains for now — full retirement is #90's job once the legacy hot-DuckDB/snapshot tiers are deleted)
- [x] Lake-native retention enforcement (#92 — `lakeserver.RetentionConfig` controls time-based DELETE of aged spans/scores through the DuckLake Catalog during Table Maintenance; `ducklake_rewrite_data_files` reclaims physical Parquet files in the same pass; replaces legacy S3-prefix file deletion which corrupts the Catalog)
- [x] Cutover: flip defaults, run prod backfill, delete snapshot sync/polling, swappable DB, hot+cold UNION, archival sweep, PVC/StatefulSet (#90 — Lake/Quack is now the sole storage tier; removed Writer Syncer/Flusher/archival sweep and the legacy DuckDB write path/dual-write, retired `internal/leader` and all leader-election wiring (DuckLake supports multi-writer), removed the one-off `writer backfill` tool now that the prod backfill (#87) and PVC deletion are complete, and cleaned up the now-unused `duckdb_path`/`sync_interval`/`flush_interval`/`flush_age_days`/`leader_election` config fields and Helm values)

## Phase 3 — Product (deferred — not yet ticketed)

Scoped in the 2026-06-11 grilling session but **deliberately not converted to issues yet**: the migration batch (#83–#92) goes first, and several of these are better specced once the Lake query path exists — full-text search and the public read API in particular should be designed against DuckLake, not the snapshot/UNION path that #90 deletes. Run `/to-issues` against this section when the migration is underway.

Suggested pickup order: **Cost integrity first** (writer-localized, independent of the migration, and visibly broken on the live instance today), then trace list correctness, End User identity, the competitive gaps, and OSS launch alongside.

### Cost integrity
- [ ] Model normalization at write time (strip provider prefixes; raw name preserved in attributes)
- [ ] Per-project Custom Pricing in metadata store + Settings UI (precedence over LiteLLM table)
- [ ] "Unpriced model" indicator in dashboard (never silent $0.00)
- [ ] Exclude non-LLM spans from model charts (kills the "unknown" bucket)

### Trace list correctness
- [ ] Traces page: one row per Trace (root span + rollups: total cost/tokens, span count, end-to-end latency, worst status)
- [ ] Flat span listing as separate view/filter
- [ ] Fix Input/Output columns rendering "–" for agent spans

### End User identity
- [ ] `EndUserID` span field (OTel `user.id` mapping + SDK helpers)
- [ ] DSL filter/group-by support + per-end-user consumption view
- [ ] Relabel "User Consumption" widget → "By Service" until then

### Competitive gaps
- [ ] Public read API (API-key-authenticated query/export: spans, traces, scores, analytics)
- [ ] Human annotation: manual score entry in UI + annotation queues
- [ ] Full-text search over span input/output
- [ ] Playground UI
- [ ] Threshold alerts (cost spike, error rate, score drop) via webhook
- [ ] CSV/Parquet export

### OSS launch
- [ ] Quickstart docs (docker compose → traced app in 5 minutes)
- [ ] "Why DuckDB/DuckLake instead of ClickHouse" positioning page
- [ ] CONTRIBUTING.md + SECURITY.md
