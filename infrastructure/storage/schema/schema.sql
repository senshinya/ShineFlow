-- ShineFlow 持久层 schema
-- 9 张表 + 索引 + 约束。详见 docs/superpowers/specs/2026-04-26-shineflow-workflow-infra-design.md §5。

-- ============================================================
-- 1) workflow_definitions
-- ============================================================
CREATE TABLE workflow_definitions (
    id                    UUID PRIMARY KEY,
    name                  TEXT NOT NULL,
    description           TEXT NOT NULL DEFAULT '',
    draft_version_id      UUID,
    published_version_id  UUID,
    created_by            TEXT NOT NULL,
    created_at            TIMESTAMPTZ NOT NULL,
    updated_at            TIMESTAMPTZ NOT NULL,
    deleted_at            TIMESTAMPTZ
);
CREATE INDEX idx_workflow_definitions_created_by ON workflow_definitions (created_by);
CREATE INDEX idx_workflow_definitions_name       ON workflow_definitions (name);

-- ============================================================
-- 2) workflow_versions
-- ============================================================
CREATE TABLE workflow_versions (
    id              UUID PRIMARY KEY,
    definition_id   UUID NOT NULL REFERENCES workflow_definitions(id),
    version         INTEGER NOT NULL,
    state           TEXT NOT NULL CHECK (state IN ('draft', 'release')),
    dsl             JSONB NOT NULL,
    revision        INTEGER NOT NULL,
    published_at    TIMESTAMPTZ,
    published_by    TEXT,
    created_at      TIMESTAMPTZ NOT NULL,
    updated_at      TIMESTAMPTZ NOT NULL,
    UNIQUE (definition_id, version)
);
-- 同一 definition 至多一个 draft（DiscardDraft 走硬删，无需 deleted_at 过滤）
CREATE UNIQUE INDEX uq_workflow_versions_one_draft
    ON workflow_versions (definition_id) WHERE state = 'draft';
CREATE INDEX idx_workflow_versions_definition ON workflow_versions (definition_id, version DESC);

-- ============================================================
-- 3) workflow_runs
-- ============================================================
CREATE TABLE workflow_runs (
    id               UUID PRIMARY KEY,
    definition_id    UUID NOT NULL REFERENCES workflow_definitions(id),
    version_id       UUID NOT NULL REFERENCES workflow_versions(id),
    trigger_kind     TEXT NOT NULL CHECK (trigger_kind IN ('manual','webhook','api','cron')),
    trigger_ref      TEXT NOT NULL DEFAULT '',
    trigger_payload  JSONB NOT NULL DEFAULT '{}'::jsonb,
    status           TEXT NOT NULL CHECK (status IN ('pending','running','success','failed','cancelled')),
    started_at       TIMESTAMPTZ,
    ended_at         TIMESTAMPTZ,
    vars             JSONB NOT NULL DEFAULT '{}'::jsonb,
    end_node_id      TEXT,
    output           JSONB,
    error            JSONB,
    created_by       TEXT NOT NULL,
    created_at       TIMESTAMPTZ NOT NULL
);
CREATE INDEX idx_workflow_runs_definition_recent ON workflow_runs (definition_id, created_at DESC);
CREATE INDEX idx_workflow_runs_active            ON workflow_runs (status)
    WHERE status IN ('pending','running');

-- ============================================================
-- 4) workflow_node_runs
-- ============================================================
CREATE TABLE workflow_node_runs (
    id                UUID PRIMARY KEY,
    run_id            UUID NOT NULL REFERENCES workflow_runs(id),
    node_id           TEXT NOT NULL,
    node_type_key     TEXT NOT NULL,
    attempt           INTEGER NOT NULL,
    status            TEXT NOT NULL CHECK (status IN ('pending','running','success','failed','skipped','cancelled')),
    started_at        TIMESTAMPTZ,
    ended_at          TIMESTAMPTZ,
    resolved_config   JSONB NOT NULL DEFAULT '{}'::jsonb,
    resolved_inputs   JSONB NOT NULL DEFAULT '{}'::jsonb,
    output            JSONB,
    fired_port        TEXT NOT NULL DEFAULT '',
    fallback_applied  BOOLEAN NOT NULL DEFAULT FALSE,
    error             JSONB,
    external_refs     JSONB NOT NULL DEFAULT '[]'::jsonb,
    UNIQUE (run_id, node_id, attempt)
);
CREATE INDEX idx_workflow_node_runs_latest ON workflow_node_runs (run_id, node_id, attempt DESC);

-- ============================================================
-- 5) cron_jobs
-- ============================================================
CREATE TABLE cron_jobs (
    id              UUID PRIMARY KEY,
    definition_id   UUID NOT NULL REFERENCES workflow_definitions(id),
    name            TEXT NOT NULL,
    description     TEXT NOT NULL DEFAULT '',
    expression      TEXT NOT NULL,
    timezone        TEXT NOT NULL,
    payload         JSONB NOT NULL DEFAULT '{}'::jsonb,
    enabled         BOOLEAN NOT NULL DEFAULT TRUE,
    next_fire_at    TIMESTAMPTZ,
    last_fire_at    TIMESTAMPTZ,
    last_run_id     UUID,
    created_by      TEXT NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL,
    updated_at      TIMESTAMPTZ NOT NULL,
    deleted_at      TIMESTAMPTZ
);
CREATE INDEX idx_cron_jobs_due ON cron_jobs (next_fire_at) WHERE enabled = TRUE;

-- ============================================================
-- 6) http_plugins
-- ============================================================
CREATE TABLE http_plugins (
    id                UUID PRIMARY KEY,
    name              TEXT NOT NULL,
    description       TEXT NOT NULL DEFAULT '',
    method            TEXT NOT NULL,
    url               TEXT NOT NULL,
    headers           JSONB NOT NULL DEFAULT '{}'::jsonb,
    query_params      JSONB NOT NULL DEFAULT '{}'::jsonb,
    body_template     TEXT NOT NULL DEFAULT '',
    auth_kind         TEXT NOT NULL CHECK (auth_kind IN ('none','api_key','bearer','basic')),
    credential_id     UUID,
    input_schema      JSONB NOT NULL DEFAULT '[]'::jsonb,
    output_schema     JSONB NOT NULL DEFAULT '[]'::jsonb,
    response_mapping  JSONB NOT NULL DEFAULT '{}'::jsonb,
    enabled           BOOLEAN NOT NULL DEFAULT TRUE,
    created_by        TEXT NOT NULL,
    created_at        TIMESTAMPTZ NOT NULL,
    updated_at        TIMESTAMPTZ NOT NULL,
    deleted_at        TIMESTAMPTZ
);
CREATE UNIQUE INDEX uq_http_plugins_name ON http_plugins (name) WHERE deleted_at IS NULL;

-- ============================================================
-- 7) mcp_servers
-- ============================================================
CREATE TABLE mcp_servers (
    id               UUID PRIMARY KEY,
    name             TEXT NOT NULL,
    transport        TEXT NOT NULL CHECK (transport IN ('stdio','http','sse')),
    config           JSONB NOT NULL,
    credential_id    UUID,
    enabled          BOOLEAN NOT NULL DEFAULT TRUE,
    last_synced_at   TIMESTAMPTZ,
    last_sync_error  TEXT,
    created_by       TEXT NOT NULL,
    created_at       TIMESTAMPTZ NOT NULL,
    updated_at       TIMESTAMPTZ NOT NULL,
    deleted_at       TIMESTAMPTZ
);
CREATE UNIQUE INDEX uq_mcp_servers_name ON mcp_servers (name) WHERE deleted_at IS NULL;

-- ============================================================
-- 8) mcp_tools
-- ============================================================
CREATE TABLE mcp_tools (
    id                UUID PRIMARY KEY,
    server_id         UUID NOT NULL REFERENCES mcp_servers(id),
    name              TEXT NOT NULL,
    description       TEXT NOT NULL DEFAULT '',
    input_schema_raw  JSONB NOT NULL,
    enabled           BOOLEAN NOT NULL DEFAULT TRUE,
    synced_at         TIMESTAMPTZ NOT NULL,
    UNIQUE (server_id, name)
);

-- ============================================================
-- 9) credentials
-- ============================================================
CREATE TABLE credentials (
    id                 UUID PRIMARY KEY,
    name               TEXT NOT NULL,
    kind               TEXT NOT NULL CHECK (kind IN ('api_key','bearer','basic','custom')),
    encrypted_payload  BYTEA NOT NULL,
    created_by         TEXT NOT NULL,
    created_at         TIMESTAMPTZ NOT NULL,
    updated_at         TIMESTAMPTZ NOT NULL,
    deleted_at         TIMESTAMPTZ
);
CREATE UNIQUE INDEX uq_credentials_name ON credentials (name) WHERE deleted_at IS NULL;
