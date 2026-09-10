-- +goose Up
CREATE TABLE storage_profiles (
    id TEXT PRIMARY KEY,
    display_name TEXT NOT NULL,
    minimum_size_gib BIGINT NOT NULL,
    maximum_size_gib BIGINT NOT NULL,
    total_capacity_gib BIGINT NOT NULL,
    workspace_quota_gib BIGINT NOT NULL,
    expandable BOOLEAN NOT NULL,
    snapshots BOOLEAN NOT NULL,
    automatic_backup BOOLEAN NOT NULL,
    durability TEXT NOT NULL,
    runtime_binding TEXT NOT NULL,
    enabled BOOLEAN NOT NULL DEFAULT FALSE,
    version BIGINT NOT NULL DEFAULT 1,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT storage_profiles_id_valid CHECK (id ~ '^[a-z0-9](?:[-a-z0-9]*[a-z0-9])?$' AND char_length(id) <= 63),
    CONSTRAINT storage_profiles_sizes_valid CHECK (minimum_size_gib > 0 AND maximum_size_gib >= minimum_size_gib AND total_capacity_gib >= maximum_size_gib AND workspace_quota_gib >= minimum_size_gib),
    CONSTRAINT storage_profiles_durability_valid CHECK (durability IN ('NodeLocal', 'Replicated', 'ProviderManaged')),
    CONSTRAINT storage_profiles_binding_valid CHECK ((enabled AND runtime_binding <> '') OR (NOT enabled)),
    CONSTRAINT storage_profiles_version_valid CHECK (version > 0)
);

ALTER TABLE app_environments
    ADD COLUMN workload_kind TEXT NOT NULL DEFAULT 'Stateless',
    ADD CONSTRAINT app_environments_workload_kind_valid CHECK (workload_kind IN ('Stateless', 'Stateful'));

CREATE TABLE app_volumes (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    public_id TEXT NOT NULL UNIQUE,
    workspace_id BIGINT NOT NULL REFERENCES workspaces(id),
    app_environment_id BIGINT NOT NULL,
    storage_profile_id TEXT NOT NULL REFERENCES storage_profiles(id),
    requested_size_gib BIGINT NOT NULL,
    observed_size_gib BIGINT NOT NULL DEFAULT 0,
    mount_path TEXT NOT NULL,
    retention_policy TEXT NOT NULL DEFAULT 'Preserve',
    desired_state TEXT NOT NULL DEFAULT 'Ready',
    observed_state TEXT NOT NULL DEFAULT 'Pending',
    message TEXT NOT NULL DEFAULT '',
    version BIGINT NOT NULL DEFAULT 1,
    deletion_requested_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT app_volumes_public_id_valid CHECK (public_id ~ '^vol-[a-z2-7]{20}$'),
    CONSTRAINT app_volumes_app_environment_workspace_fk FOREIGN KEY (app_environment_id, workspace_id) REFERENCES app_environments(id, workspace_id),
    CONSTRAINT app_volumes_size_valid CHECK (requested_size_gib > 0 AND observed_size_gib >= 0 AND observed_size_gib <= requested_size_gib),
    CONSTRAINT app_volumes_mount_path_valid CHECK (mount_path ~ '^/[^[:cntrl:]]{0,254}$' AND mount_path <> '/'),
    CONSTRAINT app_volumes_retention_valid CHECK (retention_policy = 'Preserve'),
    CONSTRAINT app_volumes_desired_state_valid CHECK (desired_state IN ('Ready', 'Deleted')),
    CONSTRAINT app_volumes_observed_state_valid CHECK (observed_state IN ('Pending', 'Provisioning', 'Ready', 'Expanding', 'Retained', 'Degraded')),
    CONSTRAINT app_volumes_version_valid CHECK (version > 0),
    UNIQUE (id, app_environment_id),
    UNIQUE (id, workspace_id)
);
CREATE UNIQUE INDEX app_volumes_one_active_per_environment
    ON app_volumes(app_environment_id)
    WHERE deletion_requested_at IS NULL;
CREATE INDEX app_volumes_workspace_profile
    ON app_volumes(workspace_id, storage_profile_id);

ALTER TABLE deployments
    ADD COLUMN workload_kind TEXT NOT NULL DEFAULT 'Stateless',
    ADD COLUMN app_volume_id BIGINT,
    ADD CONSTRAINT deployments_workload_kind_valid CHECK (workload_kind IN ('Stateless', 'Stateful')),
    ADD CONSTRAINT deployments_volume_environment_fk FOREIGN KEY (app_volume_id, app_environment_id) REFERENCES app_volumes(id, app_environment_id),
    ADD CONSTRAINT deployments_workload_volume_valid CHECK (
        (workload_kind = 'Stateless' AND app_volume_id IS NULL)
        OR (workload_kind = 'Stateful' AND app_volume_id IS NOT NULL)
    );

ALTER TABLE operations
    ADD COLUMN app_volume_id BIGINT REFERENCES app_volumes(id),
    DROP CONSTRAINT operations_kind_valid,
    DROP CONSTRAINT operations_target_valid,
    ADD CONSTRAINT operations_kind_valid CHECK (kind IN ('ApplyDeployment', 'DeleteAppEnvironment', 'EnsureWorkspace', 'EnsureVolume', 'ExpandVolume', 'DeleteVolume')),
    ADD CONSTRAINT operations_target_valid CHECK (
        (kind = 'EnsureWorkspace' AND app_environment_id IS NULL AND deployment_id IS NULL AND app_volume_id IS NULL)
        OR (kind = 'ApplyDeployment' AND app_environment_id IS NOT NULL AND deployment_id IS NOT NULL AND app_volume_id IS NULL)
        OR (kind = 'DeleteAppEnvironment' AND app_environment_id IS NOT NULL AND deployment_id IS NULL AND app_volume_id IS NULL)
        OR (kind IN ('EnsureVolume', 'ExpandVolume', 'DeleteVolume') AND app_environment_id IS NOT NULL AND deployment_id IS NULL AND app_volume_id IS NOT NULL)
    );

-- Existing rows are deterministically Stateless. This migration is forward-only;
-- operational rollback is database restoration because workload classification is
-- immutable once stateful resources exist.
