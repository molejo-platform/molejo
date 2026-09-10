-- +goose Up
DROP TABLE operations;
DROP TABLE deployments;

TRUNCATE TABLE build_logs, releases, builds RESTART IDENTITY CASCADE;

ALTER TABLE app_github_sources
    DROP CONSTRAINT app_github_sources_primary_branch_valid,
    DROP COLUMN primary_branch;

CREATE TABLE app_environments (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    public_id TEXT NOT NULL UNIQUE,
    workspace_id BIGINT NOT NULL REFERENCES workspaces(id),
    project_id BIGINT NOT NULL,
    app_id BIGINT NOT NULL,
    environment_id BIGINT NOT NULL,
    source_branch TEXT NOT NULL,
    runtime_name TEXT NOT NULL,
    configuration_json JSONB NOT NULL,
    configuration_version BIGINT NOT NULL DEFAULT 1,
    version BIGINT NOT NULL DEFAULT 1,
    desired_deployment_id BIGINT,
    current_deployment_id BIGINT,
    current_release_id BIGINT,
    last_state TEXT NOT NULL DEFAULT 'Pending',
    last_message TEXT NOT NULL DEFAULT '',
    deletion_requested_at TIMESTAMPTZ,
    archived_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT app_environments_public_id_valid CHECK (public_id ~ '^aev-[a-z2-7]{20}$'),
    CONSTRAINT app_environments_project_workspace_fk FOREIGN KEY (project_id, workspace_id) REFERENCES projects(id, workspace_id),
    CONSTRAINT app_environments_app_project_fk FOREIGN KEY (app_id, project_id) REFERENCES apps(id, project_id),
    CONSTRAINT app_environments_environment_project_fk FOREIGN KEY (environment_id, project_id) REFERENCES environments(id, project_id),
    CONSTRAINT app_environments_branch_valid CHECK (char_length(source_branch) BETWEEN 1 AND 255),
    CONSTRAINT app_environments_runtime_name_valid CHECK (runtime_name ~ '^ap-[a-z0-9](?:[-a-z0-9]*[a-z0-9])?$' AND char_length(runtime_name) <= 63),
    CONSTRAINT app_environments_configuration_version_valid CHECK (configuration_version > 0),
    CONSTRAINT app_environments_version_valid CHECK (version > 0),
    CONSTRAINT app_environments_state_valid CHECK (last_state IN ('Pending', 'Progressing', 'Ready', 'Degraded', 'Unknown')),
    UNIQUE (id, app_id),
    UNIQUE (id, workspace_id),
    UNIQUE (workspace_id, runtime_name)
);

CREATE UNIQUE INDEX app_environments_app_environment_active
    ON app_environments(app_id, environment_id)
    WHERE archived_at IS NULL;
CREATE UNIQUE INDEX app_environments_slug_active
    ON app_environments((configuration_json->>'slug'))
    WHERE archived_at IS NULL AND configuration_json->>'exposure' = 'Public';
CREATE INDEX app_environments_app_cursor
    ON app_environments(app_id, id DESC);

CREATE TABLE deployments (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    public_id TEXT NOT NULL UNIQUE,
    workspace_id BIGINT NOT NULL REFERENCES workspaces(id),
    app_environment_id BIGINT NOT NULL,
    app_id BIGINT NOT NULL,
    release_id BIGINT NOT NULL,
    requested_by_actor_id BIGINT NOT NULL REFERENCES actors(id),
    configuration_version BIGINT NOT NULL,
    configuration_json JSONB NOT NULL,
    status TEXT NOT NULL DEFAULT 'Pending',
    message TEXT NOT NULL DEFAULT '',
    observed_release TEXT NOT NULL DEFAULT '',
    started_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT deployments_public_id_valid CHECK (public_id ~ '^dpl-[a-z2-7]{20}$'),
    CONSTRAINT deployments_app_environment_workspace_fk FOREIGN KEY (app_environment_id, workspace_id) REFERENCES app_environments(id, workspace_id),
    CONSTRAINT deployments_app_environment_app_fk FOREIGN KEY (app_environment_id, app_id) REFERENCES app_environments(id, app_id),
    CONSTRAINT deployments_release_app_fk FOREIGN KEY (release_id, app_id) REFERENCES releases(id, app_id) ON DELETE RESTRICT,
    CONSTRAINT deployments_configuration_version_valid CHECK (configuration_version > 0),
    CONSTRAINT deployments_status_valid CHECK (status IN ('Pending', 'Progressing', 'Ready', 'Degraded')),
    UNIQUE (id, app_environment_id)
);
CREATE INDEX deployments_app_environment_cursor ON deployments(app_environment_id, id DESC);

ALTER TABLE app_environments
    ADD CONSTRAINT app_environments_desired_deployment_fk
        FOREIGN KEY (desired_deployment_id, id) REFERENCES deployments(id, app_environment_id),
    ADD CONSTRAINT app_environments_current_deployment_fk
        FOREIGN KEY (current_deployment_id, id) REFERENCES deployments(id, app_environment_id),
    ADD CONSTRAINT app_environments_current_release_fk
        FOREIGN KEY (current_release_id, app_id) REFERENCES releases(id, app_id);

ALTER TABLE builds
    ADD COLUMN app_environment_id BIGINT NOT NULL,
    ADD CONSTRAINT builds_app_environment_app_fk
        FOREIGN KEY (app_environment_id, app_id) REFERENCES app_environments(id, app_id);
CREATE INDEX builds_app_environment_cursor ON builds(app_environment_id, id DESC);

CREATE TABLE operations (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    public_id TEXT NOT NULL UNIQUE,
    workspace_id BIGINT NOT NULL REFERENCES workspaces(id),
    app_environment_id BIGINT REFERENCES app_environments(id),
    deployment_id BIGINT REFERENCES deployments(id),
    actor_id BIGINT NOT NULL REFERENCES actors(id),
    kind TEXT NOT NULL,
    status TEXT NOT NULL,
    idempotency_hash BYTEA NOT NULL,
    payload_hash BYTEA NOT NULL,
    desired_version BIGINT NOT NULL,
    attempts INTEGER NOT NULL DEFAULT 0,
    next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    lease_until TIMESTAMPTZ,
    worker_id TEXT,
    fencing_token BIGINT NOT NULL DEFAULT 0,
    error_code TEXT NOT NULL DEFAULT '',
    error_message TEXT NOT NULL DEFAULT '',
    started_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT operations_kind_valid CHECK (kind IN ('ApplyDeployment', 'DeleteAppEnvironment', 'EnsureWorkspace')),
    CONSTRAINT operations_status_valid CHECK (status IN ('Pending', 'Running', 'Succeeded', 'Failed')),
    CONSTRAINT operations_hashes_valid CHECK (octet_length(idempotency_hash) = 32 AND octet_length(payload_hash) = 32),
    CONSTRAINT operations_versions_valid CHECK (desired_version > 0 AND attempts >= 0 AND fencing_token >= 0),
    CONSTRAINT operations_target_valid CHECK (
        (kind = 'EnsureWorkspace' AND app_environment_id IS NULL AND deployment_id IS NULL)
        OR (kind = 'ApplyDeployment' AND app_environment_id IS NOT NULL AND deployment_id IS NOT NULL)
        OR (kind = 'DeleteAppEnvironment' AND app_environment_id IS NOT NULL AND deployment_id IS NULL)
    ),
    UNIQUE (workspace_id, actor_id, idempotency_hash)
);
CREATE INDEX operations_claim ON operations(status, next_attempt_at, id);
CREATE INDEX operations_app_environment ON operations(app_environment_id, id DESC);
CREATE UNIQUE INDEX operations_one_active_per_app_environment
    ON operations(app_environment_id)
    WHERE status IN ('Pending', 'Running');
CREATE UNIQUE INDEX operations_one_active_workspace_bootstrap
    ON operations(workspace_id)
    WHERE kind = 'EnsureWorkspace' AND status IN ('Pending', 'Running');
