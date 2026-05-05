# Lantern — Roadmap

Phase 1 implementation progress. Work is organized as vertical TDD slices, each building on the previous ones.

## Completed

### Slice 1: Metadata Store + Config ✅

- [x] Viper config loading with `lantern.yaml` + `LANTERN_*` env var overrides
- [x] Domain types: Span, Trace, Score, API Key, Prompt, EvalRule, Dataset
- [x] SQLite metadata store with full CRUD + integration tests
- [x] Postgres metadata store with full CRUD + integration tests (testcontainers)
- [x] Migrations for both dialects
- [x] API key generation (base58, SHA-256 hash storage) and validation with 60s TTL cache
- [x] DuckDB schema and helper

### Slice 2: Ingest API → Redis ✅

- [x] REST span ingest endpoint (`POST /api/v1/spans`)
- [x] Redis queue enqueue (`RPUSH` JSON batches to `lantern:ingest:spans`)
- [x] CORS middleware for browser SDK support
- [x] API key authentication (project + service scoped keys)
- [x] `503 Service Unavailable` when Redis is unreachable
- [x] Structured logging with `log/slog`
- [x] Prometheus metrics (`lantern_ingest_spans_received_total`, enqueue errors)

### Slice 3: Writer Service → DuckDB ✅

- [x] Redis dequeue (`BLPOP` batches from ingest queue)
- [x] DuckDB batch writes with idempotent upserts (`INSERT OR REPLACE` on PK)
- [x] Cost pre-computation at write time (LiteLLM pricing table + bundled fallback)
- [x] S3 snapshot sync (configurable interval, default 30s)
- [x] S3 sync with ETag/LastModified comparison to avoid unnecessary downloads
- [x] Prometheus metrics (`lantern_writer_spans_written_total`, durations)
- [x] ObjectStore abstraction (S3, mock, failing)

### Slice 4: Query API → Span List + Trace ✅

- [x] S3 snapshot download on startup + S3 polling for updates
- [x] DuckDB opened read-only for query safety during snapshot swaps
- [x] Span list endpoint (`POST /api/v1/spans/query`) with keyset cursor pagination
- [x] Field filters validated against allowlisted column names and operators
- [x] Operator mapping (eq, neq, gt, gte, lt, lte, in) to SQL symbols
- [x] Trace waterfall endpoint (`GET /api/v1/traces/:traceId`) with nested span tree
- [x] Inline scores attached to parent spans in trace detail
- [x] Cursor base64-encode/decode with unit tests
- [x] DuckDB integration tests
- [x] Hot+cold UNION query pattern established (cold side is no-op stub awaiting archival)
- [x] Prometheus metrics and `/metrics` endpoint
- [x] Graceful score table fallback when missing

### Slice 5+: Work Remaining

### Slice 5: Auth ⬜

- [ ] Login endpoint with email + password
- [ ] Session cookie management (HttpOnly, Secure, SameSite)
- [ ] Admin bootstrap via environment variables
- [ ] User invite with one-time temporary password
- [ ] Password change endpoint
- [ ] Session cookie middleware on Query API routes

### Slice 6: React UI + `/traces` ⬜

- [ ] React SPA scaffold (Vite + TypeScript)
- [ ] `/login` page
- [ ] `/traces` paginated filterable list
- [ ] `/traces/:traceId` span waterfall view
- [ ] Nav with project switcher
- [ ] Embedded into Query API binary via `embed.FS`

### Slice 7: Analytics DSL + `/dashboard` ⬜

- [ ] Analytics DSL compiler (filter, aggregation, group-by, order-by, limit)
- [ ] Hot+cold UNION always emitted (time-range independent)
- [ ] `/api/v1/analytics/spans` endpoint
- [ ] `/dashboard` cost/token/latency/error-rate charts
- [ ] Percentile aggregations (`p50`, `p95`, `p99`) via `approx_quantile`

### Slice 8: OTLP Ingest ⬜

- [ ] OTLP proto decode (`application/x-protobuf`)
- [ ] OTLP JSON decode (`application/json`)
- [ ] Two-step translation: wire format → `ResourceSpans` → `domain.Span`
- [ ] GenAI semantic convention mapping (`gen_ai.request.model`, token counts, prompts)
- [ ] Span kind derivation (explicit `lantern.kind`, `gen_ai.*`, `tool.*`, `internal`)
- [ ] Overflow attributes for unmapped fields
- [ ] Service name resolution (Resource `service.name` + API key override)

### Slice 9: Eval Pipeline ⬜

- [ ] Eval rule cache in Writer Service (reload every 60s)
- [ ] Eval queue enqueue (`RPUSH` `domain.EvalJob` per matching span)
- [ ] Eval Worker `BLPOP` from eval queue
- [ ] Judge LLM call (OpenAI-compatible endpoint)
- [ ] Score write-back to Writer Service via `POST /internal/v1/scores`
- [ ] Exponential backoff retry (up to 5 minutes)
- [ ] Sampling support (`SampleRate` per rule)

### Slice 10: Prompt Registry ⬜

- [ ] Prompt version create (`POST /api/v1/prompts`)
- [ ] Prompt resolve by version or label (`GET /api/v1/prompts/:name`)
- [ ] Label reassignment (`PUT /api/v1/prompts/:name/labels/:label`)
- [ ] Version immutability
- [ ] `{{variable}}` template interpolation
- [ ] Prompt cache in Query API (LRU for versions, 30s TTL for labels)

### Slice 11: SDK ⬜

- [ ] Go SDK — tracer (`StartSpan`/`EndSpan`, context propagation)
- [ ] Go SDK — client (prompt fetch, score write)
- [ ] Python SDK — tracer decorator (`@trace`)
- [ ] Python SDK — OTel exporter auto-configuration
- [ ] Python SDK — client (prompt fetch, score write)
- [ ] Client-side prompt caching in both SDKs

### Slice 12: Archival Sweep ⬜

- [ ] Hot→cold Parquet flush via DuckDB `COPY ... TO` with `httpfs`
- [ ] Hive-partitioned S3 layout (`project_id={id}/date={date}/`)
- [ ] Atomic flush: both spans and scores written before DuckDB prune
- [ ] Configurable flush age threshold (`writer.flush_age_days`, default 2 days)
- [ ] Scheduled archival sweep in Writer Service

### Future / Phase 2

- [ ] Eval rules UI
- [ ] Prompt Registry UI
- [ ] Playground
- [ ] Dataset curation UI
- [ ] TypeScript SDK
- [ ] Kubernetes Helm chart (production)
- [ ] Kustomize base for GitOps (Flux CD / ArgoCD)
- [ ] Cross-node Writer Service HA (active-passive)
- [ ] PVC backup via VolumeSnapshot
- [ ] OR / NOT eval filter conditions
