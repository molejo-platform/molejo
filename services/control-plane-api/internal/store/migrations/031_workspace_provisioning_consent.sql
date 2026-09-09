-- +goose Up
ALTER TABLE agent_installations
    ADD COLUMN workspace_provisioning_mode TEXT NOT NULL DEFAULT 'Disabled'
    CHECK (workspace_provisioning_mode IN ('Disabled', 'Namespaced'));

-- +goose Down
ALTER TABLE agent_installations DROP COLUMN IF EXISTS workspace_provisioning_mode;
