# Architecture

Omneval is a self-hostable LLM/Agent tracing and evaluation platform built on
DuckDB/DuckLake (Parquet on S3) instead of ClickHouse. Storage is accessed
exclusively through the **Quack Server**, which is the sole holder of a
direct DuckLake Catalog/data-path connection (ADR-0004, ADR-0005).

## Service diagram

```mermaid
flowchart LR
    subgraph Clients
        SDKApp[App / SDK<br/>OTLP or REST spans]
        Browser[Browser<br/>React SPA]
        JudgeLLM[Judge LLM<br/>OpenAI-compatible]
    end

    subgraph Omneval
        Ingest[Ingest API<br/>POST /v1/traces (OTLP)<br/>POST /api/v1/spans<br/>API key auth]
        Writer[Writer Service<br/>cost calc, Lake commits,<br/>Batch Ledger, eval matching,<br/>POST /internal/v1/scores]
        Query[Query API<br/>React SPA + session auth<br/>metadata CRUD, Analytics DSL]
        Eval[Eval Workers<br/>run eval jobs,<br/>call judge LLM]
        Quack[Quack Server<br/>sole DuckLake Catalog/data-path<br/>connection + Table Maintenance]
    end

    subgraph Storage
        Buffer[(Ingest Buffer<br/>S3)]
        Redis[(Redis<br/>batch refs + eval job queue)]
        Postgres[(Postgres<br/>metadata + Batch Ledger)]
        Lake[(DuckLake<br/>Catalog + Parquet on S3)]
    end

    SDKApp -->|spans| Ingest
    Ingest -->|stage batch| Buffer
    Ingest -->|enqueue batch ref| Redis

    Redis -->|dequeue batch ref| Writer
    Writer -->|fetch batch| Buffer
    Writer -->|ATTACH quack://<br/>InsertSpans/InsertScores| Quack
    Writer -->|record commit| Postgres
    Writer -->|enqueue eval job| Redis

    Redis -->|dequeue eval job| Eval
    Eval -->|judge call| JudgeLLM
    Eval -->|write score| Writer

    Browser --> Query
    Query -->|ATTACH quack:// read-only| Quack
    Query --> Postgres

    Quack -->|Catalog + data path| Lake
```

## Services

1. **Ingest API** (`services/ingest/`) — Accepts OTLP (proto+JSON at
   `POST /v1/traces`) and native REST spans (`POST /api/v1/spans`). Validates
   API keys (60s-TTL cache), translates OTLP to `domain.Span`, stages batches
   in the Ingest Buffer (S3), and enqueues Batch ID references to Redis.

2. **Writer Service** (`services/writer/`) — Dequeues batch references,
   fetches batches from the Ingest Buffer, computes `cost_usd` (LiteLLM
   pricing + bundled fallback), commits spans/scores to the Lake via the
   Quack Server, records commits in the Batch Ledger (`committed_batches`),
   matches eval rules (refreshed every 60s) and enqueues eval jobs. Runs an
   Ingest Buffer reconciliation sweep (recovery + retention GC) on a plain
   ticker. Receives score write-backs at `POST /internal/v1/scores`.

3. **Query API** (`services/query/`) — Stateless. Attaches read-only to the
   Lake via the Quack Server. Serves the embedded React SPA (`embed.FS`),
   session auth, and all metadata CRUD: projects, API keys, prompts
   (versioned + labels), eval rules, datasets + dataset runs, conversations,
   bookmarks, playground runs, admin endpoints, Analytics DSL
   (`POST /api/v1/analytics/spans`).

4. **Eval Workers** (`services/eval/`) — Dequeue eval jobs, call an
   OpenAI-compatible judge LLM, write scores back to the Writer with
   exponential-backoff retry.

5. **Quack Server** (`services/quack/`) — The sole holder of a direct
   DuckLake Catalog/data-path connection; runs the Table Maintenance
   scheduler (compaction, snapshot expiry, retention GC). Writer, Query API,
   and Eval attach as thin Quack clients (`ATTACH 'quack://...'`) via
   `internal/lake`'s client-attach code path.

6. **Shared** (`internal/`) — domain types, config (Viper, `omneval.yaml` /
   `OMNEVAL_*`), auth, metadata stores (Postgres prod / SQLite demo),
   `internal/lake` (DuckLake/Quack client attach), OTLP translation, pricing,
   normalizer, queue, S3, probes.

See `docs/adr/0004-ducklake-storage-core.md` and
`docs/adr/0005-quack-server-as-lake-gateway.md` for the rationale behind the
Lake/Quack storage architecture — the sole storage tier as of #90 (no hot
DuckDB store, snapshot file, cold-Parquet UNION, or leader election).
