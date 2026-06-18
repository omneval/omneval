# DuckLake replaces the hot/snapshot/cold storage tiers

Status: accepted (supersedes ADR-0001; §Catalog superseded by ADR-0006 — see there for current Catalog-backend guidance)

The three-tier storage design (hot DuckDB on a PVC, full-file S3 snapshot every 30s, Hive-partitioned Parquet archive) hit two walls on the path to Langfuse-scale ingestion: the Writer is a single replica because DuckDB allows one RW process, and the snapshot sync re-ships the entire database file every 30 seconds — already slow enough in production to need a startupProbe workaround. We decided to replace all three tiers with a single DuckLake table set (spans, scores) stored as Parquet on S3, with the DuckLake catalog in Postgres (the same instance as the metadata store). DuckLake gives ACID commits over Parquet through catalog transactions, which makes the Writer a horizontally scalable stateless Deployment and lets Query API pods read committed data directly — no snapshot download, no hot/cold UNION, no archival sweep.

## Decisions bundled here

- **Catalog**: Postgres, sharing the existing metadata-store instance. The demo profile may use a DuckDB-file catalog (single-writer is acceptable there).
- **No hot tier**: writers commit straight to DuckLake. User-visible freshness improves from ≤30s (snapshot staleness) to ~5s (commit cadence).
- **S3-first ingestion**: the Ingest API writes raw span batches to S3 and enqueues a reference; writers fetch, commit, then ack. Raw batches are retained for a bounded window (lifecycle policy), making ingestion replayable and Redis loss non-fatal.
- **Idempotency**: DuckLake does not enforce primary keys, so the `INSERT OR REPLACE (trace_id, span_id)` upsert dies. Each S3 batch carries a unique batch ID recorded in a `committed_batches` Postgres table; writers skip already-committed batches on redelivery. Residual duplicates (crash between commit and ledger insert, SDK retries across batches) are deduped at read time on trace-detail queries and tolerated as rare bounded skew in aggregates.
- **Commit cadence and maintenance**: writers batch up to ~5s or ~16MB per commit. The leader-elected writer (existing `internal/leader` Redis SETNX election) runs `ducklake_merge_adjacent_files`, `ducklake_expire_snapshots`, and orphan-file cleanup on a schedule.
- **Partitioning**: the DuckLake spans and scores tables are partitioned by `project_id` and the span's `start_time` date, preserving ADR-0002's score-next-to-span layout and the DSL compiler's pruning assumptions.
- **Eval queue**: stays a plain Redis list. Eval jobs are derived data and re-derivable; loss is tolerable, unlike spans.

## Considered Options

- **Keep three tiers, add incremental snapshots**: less code churn, but keeps the single-writer ceiling and two storage representations of the same data.
- **Hybrid (hot DuckDB write buffer + DuckLake)**: sub-second freshness nothing in the UI needs, at the cost of keeping the PVC, the StatefulSet, and most of the complexity being deleted.
- **Redis Streams only (no S3-first)**: at-least-once delivery with consumer groups, smaller change, but the buffer is bounded by Redis memory and not replayable; rejected in favor of the Langfuse-style S3-backed event log.
- **MERGE INTO / query-time dedupe everywhere**: correct but a permanent per-batch or per-query cost paying for an event that almost never happens; the batch ledger handles the common case for the price of one Postgres insert per batch.

## Consequences

- ADR-0001 (Writer StatefulSet + PVC) is superseded; the Writer loses its PVC.
- Deleted subsystems: snapshot sync/polling, swappable read-only DB, hot+cold UNION emission in the DSL compiler, archival sweep, snapshot-restore runbook.
- The Postgres catalog becomes availability-critical for writes *and* queries; it was already critical for auth and metadata.
- Migration is a one-off backfill: existing hot DuckDB rows plus existing cold Parquet are inserted into the DuckLake tables.
