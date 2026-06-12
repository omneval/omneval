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

1. **Ingest API** (`services/ingest/`) — Accepts OTLP (proto+JSON at `POST /v1/traces`) and native REST spans (`POST /api/v1/spans`). Validates API keys (60s-TTL cache), translates OTLP to `domain.Span`, enqueues batches to Redis.

2. **Writer Service** (`services/writer/`) — Dequeues span batches, computes `cost_usd` (LiteLLM pricing + bundled fallback), upserts to DuckDB, syncs a DuckDB snapshot to S3, archives old spans to Hive-partitioned Parquet, matches eval rules (refreshed every 60s) and enqueues eval jobs. Redis SETNX leader election lives in `internal/leader`. Receives score write-backs at `POST /internal/v1/scores`.

3. **Query API** (`services/query/`) — Stateless. Downloads the DuckDB snapshot from S3, polls for updates, queries hot+cold UNION (snapshot + S3 Parquet). Serves the embedded React SPA (`embed.FS`), session auth, and all metadata CRUD: projects, API keys, prompts (versioned + labels), eval rules, datasets + dataset runs, conversations, bookmarks, playground runs, admin endpoints, Analytics DSL (`POST /api/v1/analytics/spans`).

4. **Eval Workers** (`services/eval/`) — Dequeue eval jobs, call an OpenAI-compatible judge LLM, write scores back to the Writer with exponential-backoff retry.

5. **Shared** (`internal/`) — domain types, config (Viper, `omneval.yaml` / `OMNEVAL_*`), auth, metadata stores (Postgres prod / SQLite demo), DuckDB schema, OTLP translation, pricing, normalizer, queue, S3, leader election, probes.

**IMPORTANT — target architecture shift:** ADR-0004 (`docs/adr/0004-ducklake-storage-core.md`) replaces the hot-DuckDB/snapshot/cold-Parquet tiers with a single DuckLake table set (Postgres catalog, S3-first ingestion, batch-ledger dedupe, multi-writer). When touching storage, snapshot, or archival code, read that ADR first — the snapshot/UNION/archival subsystems are scheduled for deletion. Until the migration lands, the code still implements the three-tier design.

### Key Design Constraints

- **Writer is single-replica** (until ADR-0004 lands): DuckDB allows one RW process. The Writer owns the PVC; Query API reads only the S3 snapshot.
- **Idempotent upserts**: spans PK `(trace_id, span_id)` — duplicate Redis deliveries are safe. (Replaced by the Batch Ledger under ADR-0004.)
- **Cost pre-computed at write time** — never recomputed at query time. Model names are normalized (provider prefix stripped) before pricing; unknown models store cost 0 and surface as "unpriced".
- **Traces list = one row per Trace** (root span + rollups), never a flat span list.
- **API key format**: `oev_proj_<43 base58>` / `oev_svc_<43 base58>`; only SHA-256 hashes stored.
- **Raw SQL never accepted from clients** — the Analytics DSL compiles to parameterized SQL with allowlisted fields/ops; `project_id` always injected.

### Go Workspace

`go.work` ties together six modules: `./internal`, `./sdk/go`, and the four services. Each has its own `go.mod`.

## Development Notes

The UI (`ui/src/`) is React + Vite + Tailwind v4 with a custom dark theme (see `ui/BRANDING.md`); pages: Dashboard, Traces, TraceDetail, Conversations, Prompts, EvalRules, Datasets, Settings, Admin, Login.

Domain language lives in `CONTEXT.md` — follow it exactly (Span, Trace, Conversation, Score, Lake, Catalog, Ingest Buffer, Batch Ledger, End User vs User). Architecture decisions are in `docs/adr/`. The PRD is `omneval-prd.md`; progress tracking in `ROADMAP.md`. Coding standards in `.devloop/CODING_STANDARDS.md`.
