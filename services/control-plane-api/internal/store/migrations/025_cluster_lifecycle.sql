-- +goose Up
DROP INDEX agent_installations_cluster_uid;
CREATE UNIQUE INDEX agent_installations_active_cluster_uid
    ON agent_installations(cluster_uid)
    WHERE cluster_uid IS NOT NULL AND status <> 'Revoked';

ALTER TABLE agent_installations
    ADD COLUMN trust_bundle_id TEXT NOT NULL DEFAULT '',
    ADD CONSTRAINT agent_installations_trust_bundle_id_valid CHECK (
        trust_bundle_id = '' OR trust_bundle_id ~ '^[0-9a-f]{64}$'
    );

ALTER TABLE agent_credentials
    ADD COLUMN trust_bundle_id TEXT NOT NULL DEFAULT '',
    ADD CONSTRAINT agent_credentials_trust_bundle_id_valid CHECK (
        trust_bundle_id = '' OR trust_bundle_id ~ '^[0-9a-f]{64}$'
    );

-- +goose Down
ALTER TABLE agent_credentials
    DROP CONSTRAINT IF EXISTS agent_credentials_trust_bundle_id_valid,
    DROP COLUMN IF EXISTS trust_bundle_id;
ALTER TABLE agent_installations
    DROP CONSTRAINT IF EXISTS agent_installations_trust_bundle_id_valid,
    DROP COLUMN IF EXISTS trust_bundle_id;
DROP INDEX agent_installations_active_cluster_uid;
CREATE UNIQUE INDEX agent_installations_cluster_uid
    ON agent_installations(cluster_uid)
    WHERE cluster_uid IS NOT NULL;
