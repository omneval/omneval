# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What This Is

Lantern is a self-hostable LLM/Agent tracing and evaluation platform. Its key differentiator: it uses DuckDB (embedded, no separate server) instead of ClickHouse, making it viable for organizations with strict data residency requirements.

## Commands

### Go (workspace root)
```bash
go build ./services/ingest/cmd/ingest/
go build ./services/query/cmd/query/
go build ./services/writer/cmd/writer/
go build ./services/eval/cmd/eval/
go test ./...                          # all services + internal
go test ./services/ingest/...          # single service
go vet ./...
```

### UI
```bash
cd ui && npm install
npm run dev      # dev server
npm run build    # tsc + vite build
```

### Python SDK
```bash
pip install -e "sdk/python[dev]"
pytest sdk/python/
```

### Sandcastle (autonomous multi-agent orchestration)
```bash
npm run sandcastle   # requires llama-server at localhost:8080
```

## Architecture

### Five Independent Services

The system is split into five Go services that communicate via Redis queues:

1. **Ingest API** (`services/ingest/`) — Accepts OTLP (proto+JSON) and native REST spans. Validates API keys with a 60s-TTL cache, translates OTLP to `domain.Span`, and enqueues JSON batches to Redis key `lantern:ingest:spans`.

2. **Writer Service** (`services/writer/`) — Single-replica StatefulSet (owns the DuckDB PVC). `BLPOP`s from ingest queue, upserts to DuckDB (PK: `trace_id, span_id`), syncs a DuckDB snapshot to S3 every 30s, and enqueues eval jobs for spans matching active rules.

3. **Query API** (`services/query/`) — Stateless, horizontally scalable. Downloads the DuckDB snapshot from S3 on startup, polls every 30s for updates. Runs hot+cold UNION queries (DuckDB snapshot + Hive-partitioned Parquet on S3). Also serves the embedded React SPA and manages the metadata store (prompts, eval rules, API keys, sessions).

4. **Eval Workers** (`services/eval/`) — `BLPOP` eval jobs, call a configurable OpenAI-compatible judge LLM, write scores back via `POST /internal/v1/scores` to the Writer Service. Horizontally scalable; retry with exponential backoff up to 5 minutes.

5. **Shared packages** (`internal/`) — domain types, config (Viper + `lantern.yaml`/`LANTERN_*` env vars), auth (bcrypt + session cookies), metadata SQL (Postgres prod / SQLite demo), DuckDB schema, OTLP translation, pricing, Redis queue abstraction, S3 abstraction.

### Data Flow

```
SDK → Ingest API → Redis (ingest queue) → Writer → DuckDB (PVC)
                                                  → S3 snapshot (every 30s)
                                                  → S3 Parquet archive (every 30m, spans > 2 days old)
                                                  → Redis (eval queue) → Eval Workers → Writer (scores)

Query API ← S3 snapshot (polled every 30s)
Query API ← S3 Parquet (queried via DuckDB read_parquet + hive_partitioning)
```

### Storage Tiers

| Tier | Location | Owner | Latency |
|------|----------|-------|---------|
| Hot store | DuckDB file on Writer PVC | Writer (exclusive RW) | < 1s |
| Snapshot | S3 DuckDB file | Writer writes, Query reads | ≤ 30s stale |
| Cold archive | S3 Hive-partitioned Parquet | Writer writes, Query reads via `read_parquet` | ≤ 30m stale |
| Metadata | Postgres (prod) / SQLite (dev) | Query API | transactional |

### Key Design Constraints

- **Writer is single-replica**: DuckDB cannot be opened RW by two processes simultaneously. The Writer owns the PVC; Query API only reads the S3 snapshot — it never mounts the PVC.
- **Idempotent upserts**: DuckDB spans table PK is `(trace_id, span_id)`, so duplicate deliveries from Redis are safe.
- **Cost pre-computed at write time**: `cost_usd` is calculated when spans land in DuckDB using the LiteLLM pricing table (bundled fallback in `internal/pricing/`). No query-time recomputation.
- **Eval rule cache in Writer**: Rules loaded from metadata store on startup, refreshed every 60s. New rules fire within ~1 minute.
- **API key format**: `ltn_proj_<43 base58>` (project) or `ltn_svc_<43 base58>` (service). Only the SHA-256 hash is stored.

### OTLP Translation

`internal/otlp/` translates OTel GenAI semantic conventions:
- `gen_ai.request.model` → `Span.Model`
- `gen_ai.usage.{input,output}_tokens` → token counts
- `gen_ai.prompt.N.{role,content}` → `Span.Input` (JSON array)
- `gen_ai.completion.N.{role,content}` → `Span.Output`
- All unmapped attributes → `Span.Attributes` overflow JSON map

### Go Workspace

`go.work` ties together six modules: `./internal`, `./sdk/go`, and the four services. Each service has its own `go.mod`. Run `go` commands from the workspace root to address all modules at once.

## Development Notes

Most service logic is currently stubbed with `panic("not implemented")`. The domain models, config structure, DuckDB schema, and metadata migrations are the completed foundational pieces. Follow the vertical slice order in `CONTEXT.md` when implementing: metadata → ingest→Redis → writer→DuckDB → query → auth → UI → analytics → OTLP → eval → prompts → SDKs → archival.

Full bounded-context documentation is in `CONTEXT.md`. Architecture decisions are in `docs/adr/`. The PRD is `lantern-prd.md`.
