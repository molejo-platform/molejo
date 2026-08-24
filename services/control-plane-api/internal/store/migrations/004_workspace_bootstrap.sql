-- +goose Up
ALTER TABLE workspaces
    ADD COLUMN bootstrap_state TEXT NOT NULL DEFAULT 'Pending',
    ADD COLUMN updated_at TIMESTAMPTZ NOT NULL DEFAULT now();

ALTER TABLE workspaces
    ADD CONSTRAINT workspaces_bootstrap_state_valid
    CHECK (bootstrap_state IN ('Pending', 'Running', 'Ready', 'Failed'));

ALTER TABLE operations ALTER COLUMN deployment_id DROP NOT NULL;
ALTER TABLE operations
    ADD COLUMN started_at TIMESTAMPTZ,
    ADD COLUMN completed_at TIMESTAMPTZ;

ALTER TABLE operations DROP CONSTRAINT operations_kind_valid;
ALTER TABLE operations
    ADD CONSTRAINT operations_kind_valid
    CHECK (kind IN ('CreateDeployment', 'UpdateDeployment', 'DeleteDeployment', 'EnsureWorkspace'));

CREATE UNIQUE INDEX operations_one_active_workspace_bootstrap
    ON operations(workspace_id)
    WHERE kind = 'EnsureWorkspace' AND status IN ('Pending', 'Running');
