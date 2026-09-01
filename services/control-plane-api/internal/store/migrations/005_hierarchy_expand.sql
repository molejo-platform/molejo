-- +goose Up
ALTER TABLE workspaces
    ADD COLUMN version BIGINT NOT NULL DEFAULT 1;

ALTER TABLE workspaces
    ADD CONSTRAINT workspaces_version_valid CHECK (version > 0);

CREATE TABLE projects (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    public_id TEXT NOT NULL UNIQUE,
    workspace_id BIGINT NOT NULL REFERENCES workspaces(id),
    name TEXT NOT NULL,
    name_key TEXT NOT NULL,
    version BIGINT NOT NULL DEFAULT 1,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    archived_at TIMESTAMPTZ,
    CONSTRAINT projects_public_id_valid CHECK (public_id ~ '^prj-[a-z2-7]{20}$'),
    CONSTRAINT projects_name_valid CHECK (char_length(name) BETWEEN 1 AND 80),
    CONSTRAINT projects_version_valid CHECK (version > 0),
    UNIQUE (id, workspace_id)
);
CREATE UNIQUE INDEX projects_workspace_name_active
    ON projects(workspace_id, name_key)
    WHERE archived_at IS NULL;
CREATE INDEX projects_workspace_cursor
    ON projects(workspace_id, id DESC);

CREATE TABLE environments (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    public_id TEXT NOT NULL UNIQUE,
    project_id BIGINT NOT NULL REFERENCES projects(id),
    name TEXT NOT NULL,
    name_key TEXT NOT NULL,
    version BIGINT NOT NULL DEFAULT 1,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    archived_at TIMESTAMPTZ,
    CONSTRAINT environments_public_id_valid CHECK (public_id ~ '^env-[a-z2-7]{20}$'),
    CONSTRAINT environments_name_valid CHECK (char_length(name) BETWEEN 1 AND 80),
    CONSTRAINT environments_version_valid CHECK (version > 0),
    UNIQUE (id, project_id)
);
CREATE UNIQUE INDEX environments_project_name_active
    ON environments(project_id, name_key)
    WHERE archived_at IS NULL;
CREATE INDEX environments_project_cursor
    ON environments(project_id, id DESC);

CREATE TABLE apps (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    public_id TEXT NOT NULL UNIQUE,
    project_id BIGINT NOT NULL REFERENCES projects(id),
    name TEXT NOT NULL,
    name_key TEXT NOT NULL,
    version BIGINT NOT NULL DEFAULT 1,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    archived_at TIMESTAMPTZ,
    CONSTRAINT apps_public_id_valid CHECK (public_id ~ '^app-[a-z2-7]{20}$'),
    CONSTRAINT apps_name_valid CHECK (char_length(name) BETWEEN 1 AND 80),
    CONSTRAINT apps_version_valid CHECK (version > 0),
    UNIQUE (id, project_id)
);
CREATE UNIQUE INDEX apps_project_name_active
    ON apps(project_id, name_key)
    WHERE archived_at IS NULL;
CREATE INDEX apps_project_cursor
    ON apps(project_id, id DESC);

ALTER TABLE deployments
    ADD COLUMN project_id BIGINT,
    ADD COLUMN app_id BIGINT,
    ADD COLUMN environment_id BIGINT;

CREATE UNIQUE INDEX operations_workspace_idempotency
    ON operations(actor_id, idempotency_hash)
    WHERE kind = 'EnsureWorkspace'
      AND deployment_id IS NULL
      AND intent_json <> '{}'::jsonb;
