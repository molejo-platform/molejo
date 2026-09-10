-- +goose Up
ALTER TABLE agent_installations
    ADD COLUMN cluster_uid TEXT,
    ADD COLUMN kubernetes_version TEXT NOT NULL DEFAULT '',
    ADD COLUMN capabilities_json JSONB NOT NULL DEFAULT '[]';

CREATE UNIQUE INDEX agent_installations_cluster_uid
    ON agent_installations(cluster_uid)
    WHERE cluster_uid IS NOT NULL;

ALTER TABLE operations
    ADD COLUMN agent_installation_id BIGINT REFERENCES agent_installations(id);

CREATE INDEX operations_agent_claim
    ON operations(agent_installation_id, status, next_attempt_at, id);

-- +goose Down
DROP INDEX IF EXISTS operations_agent_claim;
ALTER TABLE operations DROP COLUMN IF EXISTS agent_installation_id;
DROP INDEX IF EXISTS agent_installations_cluster_uid;
ALTER TABLE agent_installations
    DROP COLUMN IF EXISTS capabilities_json,
    DROP COLUMN IF EXISTS kubernetes_version,
    DROP COLUMN IF EXISTS cluster_uid;
