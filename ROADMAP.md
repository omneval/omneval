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
- [x] DuckDB opened read-only for query safety during snapshot swaps
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

## Phase 2

### Eval rules UI
- [ ] Eval rules CRUD in UI
- [ ] Rule matching preview

### Prompt Registry UI
- [ ] Prompt version management UI
- [ ] Label assignment UI
- [ ] Prompt diff/comparison view

### Playground
- [ ] Prompt-and-test interface
- [ ] Span output inspection with edit-and-replay

### Dataset curation UI
- [ ] Dataset creation and versioning
- [ ] Span/trace selection for evaluation datasets

### TypeScript SDK
- [ ] Browser-compatible tracer
- [ ] Node.js tracer with OTel exporter

### Kubernetes production deployment
- [ ] Helm chart (production)
- [ ] Kustomize base for GitOps (Flux CD / ArgoCD)

### Cross-node Writer Service HA (active-passive)
- [ ] Leader election for Writer Service
- [ ] PVC fencing to prevent dual-writer scenarios
- [ ] Snapshot reconciliation on failover

### PVC backup via VolumeSnapshot
- [ ] CronJob for periodic DuckDB snapshot backups
- [ ] Restore procedure documentation

### Eval filter enhancements
- [ ] OR / NOT eval filter conditions
- [ ] Regex-based attribute filters
- [ ] Nested attribute access (`attributes.key.subkey`)
