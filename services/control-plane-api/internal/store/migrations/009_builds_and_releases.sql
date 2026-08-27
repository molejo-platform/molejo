-- +goose Up
CREATE TABLE builds (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    public_id TEXT NOT NULL UNIQUE,
    workspace_id BIGINT NOT NULL REFERENCES workspaces(id),
    project_id BIGINT NOT NULL,
    app_id BIGINT NOT NULL,
    requested_by_actor_id BIGINT NOT NULL REFERENCES actors(id),
    github_installation_id BIGINT REFERENCES github_installations(id) ON DELETE SET NULL,
    github_installation_external_id BIGINT NOT NULL,
    repository_id BIGINT NOT NULL,
    repository_full_name TEXT NOT NULL,
    commit_sha TEXT NOT NULL,
    platform TEXT NOT NULL DEFAULT 'linux/amd64',
    status TEXT NOT NULL DEFAULT 'Pending',
    attempts INTEGER NOT NULL DEFAULT 0,
    worker_id TEXT,
    fencing_token BIGINT NOT NULL DEFAULT 0,
    lease_until TIMESTAMPTZ,
    idempotency_hash BYTEA NOT NULL,
    payload_hash BYTEA NOT NULL,
    error_code TEXT,
    error_message TEXT,
    started_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT builds_public_id_valid CHECK (public_id ~ '^bld-[a-z2-7]{20}$'),
    CONSTRAINT builds_project_workspace_fk FOREIGN KEY (project_id, workspace_id) REFERENCES projects(id, workspace_id),
    CONSTRAINT builds_app_project_fk FOREIGN KEY (app_id, project_id) REFERENCES apps(id, project_id),
    CONSTRAINT builds_installation_valid CHECK (github_installation_external_id > 0),
    CONSTRAINT builds_repository_valid CHECK (repository_id > 0 AND char_length(repository_full_name) > 2),
    CONSTRAINT builds_commit_valid CHECK (commit_sha ~ '^[a-f0-9]{40}$'),
    CONSTRAINT builds_platform_valid CHECK (platform = 'linux/amd64'),
    CONSTRAINT builds_status_valid CHECK (status IN ('Pending', 'Running', 'Succeeded', 'Failed')),
    CONSTRAINT builds_attempts_valid CHECK (attempts BETWEEN 0 AND 3),
    UNIQUE (workspace_id, requested_by_actor_id, idempotency_hash),
    UNIQUE (id, app_id)
);

CREATE INDEX builds_queue ON builds(status, lease_until, id) WHERE status IN ('Pending', 'Running');
CREATE INDEX builds_app_cursor ON builds(app_id, id DESC);

CREATE TABLE build_logs (
    build_id BIGINT NOT NULL REFERENCES builds(id) ON DELETE CASCADE,
    sequence BIGINT GENERATED ALWAYS AS IDENTITY,
    message TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (build_id, sequence),
    CONSTRAINT build_logs_message_valid CHECK (char_length(message) BETWEEN 1 AND 4000)
);

CREATE TABLE releases (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    public_id TEXT NOT NULL UNIQUE,
    workspace_id BIGINT NOT NULL REFERENCES workspaces(id),
    project_id BIGINT NOT NULL,
    app_id BIGINT NOT NULL,
    build_id BIGINT NOT NULL UNIQUE,
    commit_sha TEXT NOT NULL,
    image TEXT NOT NULL,
    platform TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT releases_public_id_valid CHECK (public_id ~ '^rel-[a-z2-7]{20}$'),
    CONSTRAINT releases_project_workspace_fk FOREIGN KEY (project_id, workspace_id) REFERENCES projects(id, workspace_id),
    CONSTRAINT releases_app_project_fk FOREIGN KEY (app_id, project_id) REFERENCES apps(id, project_id),
    CONSTRAINT releases_build_app_fk FOREIGN KEY (build_id, app_id) REFERENCES builds(id, app_id),
    CONSTRAINT releases_commit_valid CHECK (commit_sha ~ '^[a-f0-9]{40}$'),
    CONSTRAINT releases_image_valid CHECK (image ~ '^[a-z0-9][a-z0-9._:/-]*@sha256:[a-f0-9]{64}$'),
    CONSTRAINT releases_platform_valid CHECK (platform = 'linux/amd64'),
    UNIQUE (id, app_id)
);

CREATE INDEX releases_app_cursor ON releases(app_id, id DESC);

ALTER TABLE deployments ADD COLUMN release_id BIGINT;
ALTER TABLE deployments ADD CONSTRAINT deployments_release_app_fk
    FOREIGN KEY (release_id, app_id) REFERENCES releases(id, app_id) ON DELETE RESTRICT;
