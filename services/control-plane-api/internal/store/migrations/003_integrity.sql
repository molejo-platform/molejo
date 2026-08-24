-- +goose Up
ALTER TABLE deployments ADD CONSTRAINT deployments_versions_valid CHECK (desired_version > 0 AND observed_version >= 0 AND observed_version <= desired_version);
ALTER TABLE deployments ADD CONSTRAINT deployments_state_valid CHECK (last_state IN ('Pending', 'Progressing', 'Ready', 'Degraded', 'Unknown'));
ALTER TABLE operations ADD CONSTRAINT operations_kind_valid CHECK (kind IN ('CreateDeployment', 'UpdateDeployment', 'DeleteDeployment'));
ALTER TABLE operations ADD CONSTRAINT operations_status_valid CHECK (status IN ('Pending', 'Running', 'Succeeded', 'Failed', 'Superseded'));
ALTER TABLE operations ADD CONSTRAINT operations_hashes_valid CHECK (octet_length(idempotency_hash) = 32 AND octet_length(payload_hash) = 32);
ALTER TABLE operations ADD CONSTRAINT operations_versions_valid CHECK (desired_version > 0 AND attempts >= 0 AND fencing_token >= 0);
CREATE UNIQUE INDEX IF NOT EXISTS operations_one_active_per_deployment ON operations(deployment_id) WHERE status IN ('Pending', 'Running');

-- +goose Down
DROP INDEX IF EXISTS operations_one_active_per_deployment;
ALTER TABLE operations DROP CONSTRAINT IF EXISTS operations_versions_valid;
ALTER TABLE operations DROP CONSTRAINT IF EXISTS operations_hashes_valid;
ALTER TABLE operations DROP CONSTRAINT IF EXISTS operations_status_valid;
ALTER TABLE operations DROP CONSTRAINT IF EXISTS operations_kind_valid;
ALTER TABLE deployments DROP CONSTRAINT IF EXISTS deployments_state_valid;
ALTER TABLE deployments DROP CONSTRAINT IF EXISTS deployments_versions_valid;
