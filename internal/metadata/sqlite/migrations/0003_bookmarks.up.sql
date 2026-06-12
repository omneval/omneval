CREATE TABLE bookmarks (
    trace_id   TEXT NOT NULL,
    project_id TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    PRIMARY KEY (trace_id, project_id)
);

CREATE INDEX idx_bookmarks_project ON bookmarks (project_id);
