-- +goose Up
ALTER TABLE github_installations
    ADD COLUMN status TEXT NOT NULL DEFAULT 'Active',
    ADD CONSTRAINT github_installations_status_valid
        CHECK (status IN ('Active', 'Suspended', 'Deleted'));

CREATE TABLE app_environment_delivery_policies (
    app_environment_id BIGINT PRIMARY KEY,
    workspace_id BIGINT NOT NULL,
    push_enabled BOOLEAN NOT NULL DEFAULT FALSE,
    release_enabled BOOLEAN NOT NULL DEFAULT FALSE,
    version BIGINT NOT NULL DEFAULT 1,
    updated_by_actor_id BIGINT NOT NULL REFERENCES actors(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT delivery_policies_app_environment_workspace_fk
        FOREIGN KEY (app_environment_id, workspace_id)
        REFERENCES app_environments(id, workspace_id) ON DELETE CASCADE,
    CONSTRAINT delivery_policies_version_valid CHECK (version > 0)
);
CREATE INDEX delivery_policies_workspace
    ON app_environment_delivery_policies(workspace_id, app_environment_id);

CREATE TABLE github_deliveries (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    delivery_id TEXT NOT NULL UNIQUE,
    event_type TEXT NOT NULL,
    action TEXT NOT NULL DEFAULT '',
    installation_external_id BIGINT NOT NULL,
    repository_id BIGINT NOT NULL DEFAULT 0,
    repository_full_name TEXT NOT NULL DEFAULT '',
    source_branch TEXT NOT NULL DEFAULT '',
    source_ref TEXT NOT NULL DEFAULT '',
    commit_sha TEXT NOT NULL DEFAULT '',
    tag_name TEXT NOT NULL DEFAULT '',
    repository_ids BIGINT[] NOT NULL DEFAULT '{}',
    payload_hash BYTEA NOT NULL,
    status TEXT NOT NULL DEFAULT 'Pending',
    attempts INTEGER NOT NULL DEFAULT 0,
    worker_id TEXT,
    fencing_token BIGINT NOT NULL DEFAULT 0,
    lease_until TIMESTAMPTZ,
    error_code TEXT NOT NULL DEFAULT '',
    error_message TEXT NOT NULL DEFAULT '',
    received_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    processed_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT github_deliveries_id_valid
        CHECK (char_length(delivery_id) BETWEEN 1 AND 128),
    CONSTRAINT github_deliveries_event_valid
        CHECK (event_type IN ('ping', 'push', 'release', 'installation', 'installation_repositories')),
    CONSTRAINT github_deliveries_installation_valid CHECK (
        (event_type IN ('push', 'release') AND installation_external_id > 0)
        OR (event_type NOT IN ('push', 'release') AND installation_external_id >= 0)
    ),
    CONSTRAINT github_deliveries_repository_valid
        CHECK (repository_id >= 0 AND char_length(repository_full_name) <= 255),
    CONSTRAINT github_deliveries_commit_valid
        CHECK (commit_sha = '' OR commit_sha ~ '^[a-f0-9]{40}$'),
    CONSTRAINT github_deliveries_payload_hash_valid CHECK (octet_length(payload_hash) = 32),
    CONSTRAINT github_deliveries_status_valid
        CHECK (status IN ('Pending', 'Running', 'Succeeded', 'Failed', 'Ignored')),
    CONSTRAINT github_deliveries_attempts_valid CHECK (attempts BETWEEN 0 AND 10)
);
CREATE INDEX github_deliveries_queue
    ON github_deliveries(status, lease_until, id)
    WHERE status IN ('Pending', 'Running');

ALTER TABLE builds DROP CONSTRAINT builds_status_valid;
ALTER TABLE builds
    ADD COLUMN trigger_type TEXT NOT NULL DEFAULT 'Manual',
    ADD COLUMN github_delivery_id BIGINT REFERENCES github_deliveries(id),
    ADD COLUMN commit_title TEXT NOT NULL DEFAULT '',
    ADD COLUMN commit_author_name TEXT NOT NULL DEFAULT '',
    ADD COLUMN commit_author_login TEXT NOT NULL DEFAULT '',
    ADD COLUMN committed_at TIMESTAMPTZ,
    ADD CONSTRAINT builds_trigger_valid CHECK (trigger_type IN ('Manual', 'Push', 'Release')),
    ADD CONSTRAINT builds_status_valid
        CHECK (status IN ('Pending', 'Running', 'Succeeded', 'Failed', 'Superseded', 'TimedOut'));

WITH ranked AS (
    SELECT id, row_number() OVER (PARTITION BY app_environment_id ORDER BY id DESC) AS position
    FROM builds WHERE status='Pending'
)
UPDATE builds SET status='Superseded',completed_at=now(),updated_at=now()
WHERE id IN (SELECT id FROM ranked WHERE position > 1);

CREATE UNIQUE INDEX builds_one_pending_manual_per_app_environment
    ON builds(app_environment_id) WHERE status='Pending' AND trigger_type='Manual';
CREATE UNIQUE INDEX builds_one_pending_delivery_per_app_environment
    ON builds(app_environment_id) WHERE status='Pending' AND trigger_type IN ('Push', 'Release');
CREATE UNIQUE INDEX builds_one_running_per_app_environment
    ON builds(app_environment_id) WHERE status='Running';
CREATE UNIQUE INDEX builds_delivery_target
    ON builds(github_delivery_id, app_environment_id)
    WHERE github_delivery_id IS NOT NULL;

ALTER TABLE releases
    ADD COLUMN app_environment_id BIGINT,
    ADD COLUMN availability_status TEXT NOT NULL DEFAULT 'Available',
    ADD COLUMN expired_at TIMESTAMPTZ,
    ADD CONSTRAINT releases_availability_valid
        CHECK (availability_status IN ('Available', 'Expired'));
UPDATE releases r SET app_environment_id=b.app_environment_id
FROM builds b WHERE b.id=r.build_id;
ALTER TABLE releases
    ALTER COLUMN app_environment_id SET NOT NULL,
    ADD CONSTRAINT releases_app_environment_app_fk
        FOREIGN KEY (app_environment_id, app_id)
        REFERENCES app_environments(id, app_id);
CREATE INDEX releases_app_environment_cursor
    ON releases(app_environment_id, id DESC);

CREATE TABLE delivery_targets (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    public_id TEXT NOT NULL UNIQUE,
    github_delivery_id BIGINT NOT NULL REFERENCES github_deliveries(id) ON DELETE CASCADE,
    workspace_id BIGINT NOT NULL REFERENCES workspaces(id),
    project_id BIGINT NOT NULL,
    app_id BIGINT NOT NULL,
    app_environment_id BIGINT NOT NULL,
    requested_by_actor_id BIGINT NOT NULL REFERENCES actors(id),
    policy_version BIGINT NOT NULL,
    trigger_type TEXT NOT NULL,
    source_branch TEXT NOT NULL,
    commit_sha TEXT NOT NULL,
    build_id BIGINT REFERENCES builds(id),
    deployment_id BIGINT REFERENCES deployments(id),
    status TEXT NOT NULL DEFAULT 'Building',
    error_code TEXT NOT NULL DEFAULT '',
    error_message TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    completed_at TIMESTAMPTZ,
    CONSTRAINT delivery_targets_public_id_valid CHECK (public_id ~ '^dlt-[a-z2-7]{20}$'),
    CONSTRAINT delivery_targets_project_workspace_fk
        FOREIGN KEY (project_id, workspace_id) REFERENCES projects(id, workspace_id),
    CONSTRAINT delivery_targets_app_project_fk
        FOREIGN KEY (app_id, project_id) REFERENCES apps(id, project_id),
    CONSTRAINT delivery_targets_app_environment_app_fk
        FOREIGN KEY (app_environment_id, app_id) REFERENCES app_environments(id, app_id),
    CONSTRAINT delivery_targets_trigger_valid CHECK (trigger_type IN ('Push', 'Release')),
    CONSTRAINT delivery_targets_branch_valid CHECK (char_length(source_branch) BETWEEN 1 AND 255),
    CONSTRAINT delivery_targets_commit_valid CHECK (commit_sha ~ '^[a-f0-9]{40}$'),
    CONSTRAINT delivery_targets_status_valid
        CHECK (status IN ('Building', 'DeployPending', 'Deploying', 'Succeeded', 'Failed', 'Superseded')),
    UNIQUE (github_delivery_id, app_environment_id)
);
CREATE INDEX delivery_targets_progress
    ON delivery_targets(status, id)
    WHERE status IN ('Building', 'DeployPending', 'Deploying');

CREATE TABLE release_gc_candidates (
    release_id BIGINT PRIMARY KEY REFERENCES releases(id) ON DELETE CASCADE,
    image TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'Blocked',
    reason TEXT NOT NULL DEFAULT 'registry_delete_not_enabled',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT release_gc_candidates_status_valid
        CHECK (status IN ('Blocked', 'Pending', 'Running', 'Succeeded', 'Failed'))
);
