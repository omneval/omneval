CREATE TABLE IF NOT EXISTS spans (
    span_id           VARCHAR      NOT NULL,
    trace_id          VARCHAR      NOT NULL,
    parent_id         VARCHAR,
    conversation_id   VARCHAR,
    project_id        VARCHAR      NOT NULL,
    service_name      VARCHAR,

    name              VARCHAR,
    kind              VARCHAR,
    start_time        TIMESTAMPTZ  NOT NULL,
    end_time          TIMESTAMPTZ,

    model             VARCHAR,
    input             VARCHAR,
    output            VARCHAR,
    input_tokens      BIGINT,
    output_tokens     BIGINT,
    cost_usd          DOUBLE,

    prompt_name       VARCHAR,
    prompt_version    BIGINT,

    status_code       VARCHAR,
    status_message    VARCHAR,

    attributes        VARCHAR,

    PRIMARY KEY (trace_id, span_id)
);

CREATE INDEX IF NOT EXISTS idx_spans_project_time
    ON spans (project_id, start_time);

-- idx_spans_conversation is created by migration 0001 (not here): on a
-- database created before conversation_id existed, the schema pass runs
-- BEFORE migrations, so an index referencing the column would fail Open()
-- until 0001's ADD COLUMN has run. Migration 0001 both adds the column and
-- creates the index, idempotently, for old and fresh databases alike.

CREATE TABLE IF NOT EXISTS bookmarks (
    trace_id       VARCHAR      NOT NULL,
    project_id     VARCHAR      NOT NULL,
    created_at     TIMESTAMPTZ  NOT NULL,
    PRIMARY KEY (trace_id, project_id)
);

CREATE TABLE IF NOT EXISTS scores (
    score_id       VARCHAR      NOT NULL PRIMARY KEY,
    span_id        VARCHAR      NOT NULL,
    trace_id       VARCHAR      NOT NULL,
    project_id     VARCHAR      NOT NULL,
    eval_name      VARCHAR,
    value          DOUBLE,
    reasoning      VARCHAR,
    judge_model    VARCHAR,
    prompt_name    VARCHAR,
    prompt_version BIGINT,
    created_at     TIMESTAMPTZ  NOT NULL
);
