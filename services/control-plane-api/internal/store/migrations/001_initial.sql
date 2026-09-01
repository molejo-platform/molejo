-- +goose Up
CREATE TABLE IF NOT EXISTS actors (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    actor_key TEXT NOT NULL UNIQUE,
    role TEXT NOT NULL,
    password_hash TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS workspaces (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    public_id TEXT NOT NULL UNIQUE,
    name TEXT NOT NULL,
    namespace_name TEXT NOT NULL UNIQUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS workspace_actors (
    workspace_id BIGINT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    actor_id BIGINT NOT NULL REFERENCES actors(id) ON DELETE CASCADE,
    PRIMARY KEY (workspace_id, actor_id)
);

CREATE TABLE IF NOT EXISTS deployments (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    public_id TEXT NOT NULL UNIQUE,
    workspace_id BIGINT NOT NULL REFERENCES workspaces(id),
    name TEXT NOT NULL,
    slug TEXT,
    intent_json JSONB NOT NULL,
    desired_version BIGINT NOT NULL DEFAULT 1,
    observed_version BIGINT NOT NULL DEFAULT 0,
    observed_release TEXT NOT NULL DEFAULT '',
    last_state TEXT NOT NULL DEFAULT 'Pending',
    last_message TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at TIMESTAMPTZ
);
CREATE UNIQUE INDEX IF NOT EXISTS deployments_workspace_name_active ON deployments(workspace_id, name) WHERE deleted_at IS NULL;
CREATE UNIQUE INDEX IF NOT EXISTS deployments_slug_active ON deployments(slug) WHERE slug IS NOT NULL AND deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS deployments_workspace_cursor ON deployments(workspace_id, id DESC);

CREATE TABLE IF NOT EXISTS operations (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    public_id TEXT NOT NULL UNIQUE,
    workspace_id BIGINT NOT NULL REFERENCES workspaces(id),
    deployment_id BIGINT NOT NULL REFERENCES deployments(id),
    actor_id BIGINT NOT NULL REFERENCES actors(id),
    kind TEXT NOT NULL,
    status TEXT NOT NULL,
    idempotency_hash BYTEA NOT NULL,
    payload_hash BYTEA NOT NULL,
    intent_json JSONB NOT NULL,
    desired_version BIGINT NOT NULL,
    sequence BIGINT NOT NULL,
    attempts INTEGER NOT NULL DEFAULT 0,
    next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    lease_until TIMESTAMPTZ,
    worker_id TEXT,
    fencing_token BIGINT NOT NULL DEFAULT 0,
    error_code TEXT NOT NULL DEFAULT '',
    error_message TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (workspace_id, kind, deployment_id, idempotency_hash)
);
CREATE INDEX IF NOT EXISTS operations_claim ON operations(status, next_attempt_at, id);
CREATE INDEX IF NOT EXISTS operations_deployment ON operations(deployment_id, id DESC);
CREATE UNIQUE INDEX IF NOT EXISTS operations_create_idempotency ON operations(workspace_id, idempotency_hash) WHERE kind = 'CreateDeployment';

CREATE TABLE IF NOT EXISTS sessions (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    token_hash BYTEA NOT NULL UNIQUE,
    actor_id BIGINT NOT NULL REFERENCES actors(id),
    csrf_hash BYTEA NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    revoked_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS sessions_expiry ON sessions(expires_at);
