# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What This Is

Omneval is a self-hostable LLM/Agent tracing and evaluation platform. Its key differentiator: it uses DuckDB/DuckLake (embedded engine, Parquet on S3, no ClickHouse cluster) instead of ClickHouse, making it viable for organizations with strict data residency requirements while still targeting high ingest scale.

## Commands

### Go (run from the workspace root — `./...` does NOT work across the workspace)
```bash
go build ./services/ingest/cmd/ingest/
go build ./services/query/cmd/query/
go build ./services/writer/cmd/writer/
go build ./services/eval/cmd/eval/
go test ./internal/...                 # shared packages
go test ./services/ingest/... ./services/writer/... ./services/query/... ./services/eval/...
go vet ./internal/... ./services/ingest/... ./services/writer/... ./services/query/... ./services/eval/...
```
Some integration tests use testcontainers and skip/fail without a running Docker daemon.

### UI
```bash
cd ui && npm install
npm run dev      # dev server
npm run build    # tsc + vite build
npm test         # vitest
```

### SDKs
```bash
pip install -e "sdk/python[dev]" && pytest sdk/python/   # Python
cd sdk/ts && npm install && npm test                      # TypeScript
go test ./sdk/go/...                                      # Go (from workspace root)
```

## Architecture

Four Go services communicating via Redis, plus shared packages:

1. **Ingest API** (`services/ingest/`) — Accepts OTLP (proto+JSON at `POST /v1/traces`) and native REST spans (`POST /api/v1/spans`). Validates API keys (60s-TTL cache), translates OTLP to `domain.Span`, stages batches in the Ingest Buffer (S3) and enqueues Batch ID references to Redis.

2. **Writer Service** (`services/writer/`) — Dequeues batch references, fetches batches from the Ingest Buffer, computes `cost_usd` (LiteLLM pricing + bundled fallback), commits spans/scores to the Lake via the Quack Server, records commits in the Batch Ledger (`committed_batches`), matches eval rules (refreshed every 60s) and enqueues eval jobs. Runs an Ingest Buffer reconciliation sweep (recovery + retention GC) on a plain ticker. Receives score write-backs at `POST /internal/v1/scores`.

3. **Query API** (`services/query/`) — Stateless. Attaches read-only to the Lake via the Quack Server. Serves the embedded React SPA (`embed.FS`), session auth, and all metadata CRUD: projects, API keys, prompts (versioned + labels), eval rules, datasets + dataset runs, conversations, bookmarks, playground runs, admin endpoints, Analytics DSL (`POST /api/v1/analytics/spans`).

4. **Eval Workers** (`services/eval/`) — Dequeue eval jobs, call an OpenAI-compatible judge LLM, write scores back to the Writer with exponential-backoff retry.

5. **Quack Server** (`services/quack/`) — The sole holder of a direct DuckLake Catalog/data-path connection; runs the Table Maintenance scheduler (compaction, snapshot expiry, retention GC). Writer, Query API, and Eval attach as thin Quack clients via `quack.client.*`.

6. **Shared** (`internal/`) — domain types, config (Viper, `omneval.yaml` / `OMNEVAL_*`), auth, metadata stores (Postgres prod / SQLite demo), `internal/lake` (DuckLake/Quack client attach), OTLP translation, pricing, normalizer, queue, S3, probes.

ADR-0004 (`docs/adr/0004-ducklake-storage-core.md`) and ADR-0005 (`docs/adr/0005-quack-server-as-lake-gateway.md`) describe the Lake/Quack storage architecture, which is now the sole storage tier (cutover complete as of #90) — there is no hot DuckDB store, snapshot file, cold-Parquet UNION, or leader election (`internal/leader` was retired; DuckLake supports multi-writer).

### Key Design Constraints

- **All services are stateless and horizontally scalable**: DuckLake supports multi-writer, so there is no single-writer constraint and no PVC for Writer/Query. Only the Quack Server is a single-replica StatefulSet (sole direct Catalog/data-path connection).
- **Idempotent commits**: the Batch Ledger (`committed_batches` in Postgres) records committed Batch IDs so redelivered batches are skipped; residual duplicates are deduped at read time on trace-detail queries.
- **Cost pre-computed at write time** — never recomputed at query time. Model names are normalized (provider prefix stripped) before pricing; unknown models store cost 0 and surface as "unpriced".
- **Traces list = one row per Trace** (root span + rollups), never a flat span list.
- **API key format**: `oev_proj_<43 base58>` / `oev_svc_<43 base58>`; only SHA-256 hashes stored.
- **Raw SQL never accepted from clients** — the Analytics DSL compiles to parameterized SQL with allowlisted fields/ops; `project_id` always injected.

### Go Workspace

`go.work` ties together six modules: `./internal`, `./sdk/go`, and the four services. Each has its own `go.mod`.

## Development Notes

The UI (`ui/src/`) is React + Vite + Tailwind v4 with a custom dark theme (see `ui/BRANDING.md`); pages: Dashboard, Traces, TraceDetail, Conversations, Prompts, EvalRules, Datasets, Settings, Admin, Login.

Domain language lives in `CONTEXT.md` — follow it exactly (Span, Trace, Conversation, Score, Lake, Catalog, Ingest Buffer, Batch Ledger, End User vs User). Architecture decisions are in `docs/adr/`. The PRD is `omneval-prd.md`; progress tracking in `ROADMAP.md`. Coding standards in `.devloop/CODING_STANDARDS.md`.

## Agent skills

### Issue tracker

GitHub Issues on `omneval/omneval`, via the `gh` CLI. See `docs/agents/issue-tracker.md`.

### Triage labels

Five canonical roles map to this repo's existing labels — notably `ready-for-agent` maps to the pre-existing `agent-ready` label rather than a new one. See `docs/agents/triage-labels.md`.

### Domain docs

Single-context: one `CONTEXT.md` + `docs/adr/` at the repo root. See `docs/agents/domain.md`.
