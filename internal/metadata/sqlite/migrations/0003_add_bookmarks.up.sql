-- Bookmarks (starred traces) move from the hot DuckDB store to the
-- Metadata Store: they are mutable user state, and the hot store is
-- scheduled for deletion under the DuckLake migration (ADR-0004).
CREATE TABLE IF NOT EXISTS bookmarks (
    project_id TEXT NOT NULL,
    trace_id   TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    PRIMARY KEY (project_id, trace_id)
);
