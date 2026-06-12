-- Batch Ledger (ADR-0004): every Batch ID already committed to the Lake.
-- Writers consult it to skip redelivered batches — the replacement for the
-- old primary-key upsert, since DuckLake does not enforce primary keys.
CREATE TABLE IF NOT EXISTS committed_batches (
    batch_id     TEXT PRIMARY KEY,
    committed_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
