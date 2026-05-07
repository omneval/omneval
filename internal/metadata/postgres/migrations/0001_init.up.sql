CREATE TABLE organizations (
    org_id     TEXT        PRIMARY KEY,
    name       TEXT        NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE projects (
    project_id TEXT        PRIMARY KEY,
    org_id     TEXT        NOT NULL REFERENCES organizations(org_id),
    name       TEXT        NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE users (
    user_id       TEXT        PRIMARY KEY,
    org_id        TEXT        NOT NULL REFERENCES organizations(org_id),
    email         TEXT        NOT NULL UNIQUE,
    password_hash TEXT        NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE sessions (
    session_id TEXT        PRIMARY KEY,
    user_id    TEXT        NOT NULL REFERENCES users(user_id),
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE api_keys (
    key_id       TEXT        PRIMARY KEY,
    project_id   TEXT        NOT NULL REFERENCES projects(project_id),
    kind         TEXT        NOT NULL CHECK (kind IN ('project', 'service')),
    service_name TEXT,
    hashed_key   TEXT        NOT NULL UNIQUE,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    revoked_at   TIMESTAMPTZ
);

CREATE TABLE prompt_versions (
    version_id   TEXT        PRIMARY KEY,
    project_id   TEXT        NOT NULL REFERENCES projects(project_id),
    name         TEXT        NOT NULL,
    version      BIGINT      NOT NULL,
    template     TEXT        NOT NULL,
    model        TEXT,
    temperature  DOUBLE PRECISION,
    max_tokens   INTEGER,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (project_id, name, version)
);

CREATE TABLE prompt_labels (
    project_id TEXT        NOT NULL,
    name       TEXT        NOT NULL,
    label      TEXT        NOT NULL,
    version    BIGINT      NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (project_id, name, label)
);

CREATE TABLE eval_rules (
    rule_id        TEXT             PRIMARY KEY,
    project_id     TEXT             NOT NULL REFERENCES projects(project_id),
    name           TEXT             NOT NULL,
    judge_model    TEXT             NOT NULL,
    prompt_name    TEXT             NOT NULL,
    prompt_version BIGINT           NOT NULL,
    filter         JSONB            NOT NULL DEFAULT '{}',
    sample_rate    DOUBLE PRECISION NOT NULL DEFAULT 1.0,
    enabled        BOOLEAN          NOT NULL DEFAULT true,
    created_at     TIMESTAMPTZ      NOT NULL DEFAULT now()
);

CREATE TABLE datasets (
    dataset_id TEXT        PRIMARY KEY,
    project_id TEXT        NOT NULL REFERENCES projects(project_id),
    name       TEXT        NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE dataset_items (
    item_id         TEXT        PRIMARY KEY,
    dataset_id      TEXT        NOT NULL REFERENCES datasets(dataset_id),
    source_span_id  TEXT,
    input           TEXT        NOT NULL,
    expected_output TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE dataset_runs (
    run_id         TEXT        PRIMARY KEY,
    dataset_id     TEXT        NOT NULL REFERENCES datasets(dataset_id),
    eval_rule_id   TEXT        NOT NULL REFERENCES eval_rules(rule_id),
    prompt_version BIGINT      NOT NULL,
    status         TEXT        NOT NULL DEFAULT 'pending',
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE dataset_run_items (
    run_item_id TEXT PRIMARY KEY,
    run_id      TEXT NOT NULL REFERENCES dataset_runs(run_id),
    item_id     TEXT NOT NULL REFERENCES dataset_items(item_id),
    score       DOUBLE PRECISION NOT NULL DEFAULT 0.0,
    reasoning   TEXT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX ON sessions (user_id);
CREATE INDEX ON api_keys (project_id);
CREATE INDEX ON prompt_versions (project_id, name);
CREATE INDEX ON eval_rules (project_id);
CREATE INDEX ON datasets (project_id);
CREATE INDEX ON dataset_items (dataset_id);
