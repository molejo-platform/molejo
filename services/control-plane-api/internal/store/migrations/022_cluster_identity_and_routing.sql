-- +goose Up
-- Keep agent_installations as the durable cluster record. The legacy table name is
-- intentionally preserved so existing installations can migrate without replacing
-- public identifiers or foreign keys.
ALTER TABLE agent_installations
    ADD COLUMN agent_version TEXT NOT NULL DEFAULT '',
    ADD COLUMN revoked_at TIMESTAMPTZ,
    ADD COLUMN revocation_reason TEXT NOT NULL DEFAULT '';

ALTER TABLE agent_installations
    DROP CONSTRAINT agent_installations_public_id_valid,
    ADD CONSTRAINT agent_installations_public_id_valid CHECK (public_id ~ '^(agi|cls)-[a-z2-7]{20}$');

CREATE TABLE agent_credentials (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    installation_id BIGINT NOT NULL REFERENCES agent_installations(id) ON DELETE CASCADE,
    status TEXT NOT NULL,
    enrollment_attempt_id TEXT,
    csr_fingerprint BYTEA,
    certificate_pem BYTEA NOT NULL,
    ca_certificate_pem BYTEA NOT NULL,
    server_ca_certificate_pem BYTEA NOT NULL,
    certificate_serial TEXT NOT NULL,
    certificate_fingerprint BYTEA NOT NULL UNIQUE,
    certificate_not_after TIMESTAMPTZ NOT NULL,
    overlap_not_after TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT agent_credentials_status_valid CHECK (status IN ('Active', 'Superseded', 'Revoked')),
    CONSTRAINT agent_credentials_attempt_complete CHECK (
        (enrollment_attempt_id IS NULL AND csr_fingerprint IS NULL)
        OR (enrollment_attempt_id IS NOT NULL AND csr_fingerprint IS NOT NULL)
    )
);

CREATE UNIQUE INDEX agent_credentials_active
    ON agent_credentials(installation_id)
    WHERE status = 'Active';
CREATE UNIQUE INDEX agent_credentials_attempt
    ON agent_credentials(installation_id, enrollment_attempt_id)
    WHERE enrollment_attempt_id IS NOT NULL;
CREATE INDEX agent_credentials_validity
    ON agent_credentials(installation_id, status, certificate_not_after);

INSERT INTO agent_credentials(
    installation_id,status,enrollment_attempt_id,csr_fingerprint,certificate_pem,ca_certificate_pem,server_ca_certificate_pem,
    certificate_serial,certificate_fingerprint,certificate_not_after,created_at,updated_at
)
SELECT id,
       CASE WHEN status='Revoked' THEN 'Revoked' ELSE 'Active' END,
       enrollment_attempt_id,csr_fingerprint,certificate_pem,ca_certificate_pem,ca_certificate_pem,certificate_serial,
       certificate_fingerprint,certificate_not_after,created_at,updated_at
FROM agent_installations
WHERE certificate_pem IS NOT NULL;

CREATE TABLE workspace_clusters (
    workspace_id BIGINT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    installation_id BIGINT NOT NULL REFERENCES agent_installations(id) ON DELETE RESTRICT,
    namespace_name TEXT NOT NULL,
    state TEXT NOT NULL DEFAULT 'Pending',
    message TEXT NOT NULL DEFAULT '',
    observed_generation BIGINT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (workspace_id, installation_id),
    CONSTRAINT workspace_clusters_namespace_valid CHECK (
        namespace_name ~ '^[a-z0-9](?:[-a-z0-9]*[a-z0-9])?$'
        AND char_length(namespace_name) <= 63
    ),
    CONSTRAINT workspace_clusters_state_valid CHECK (state IN ('Pending', 'Running', 'Ready', 'Failed')),
    CONSTRAINT workspace_clusters_generation_valid CHECK (observed_generation >= 0),
    UNIQUE (installation_id, namespace_name)
);

INSERT INTO workspace_clusters(workspace_id,installation_id,namespace_name,state,observed_generation,created_at,updated_at)
SELECT DISTINCT o.workspace_id,o.agent_installation_id,w.namespace_name,
       CASE w.bootstrap_state WHEN 'Ready' THEN 'Ready' WHEN 'Failed' THEN 'Failed' WHEN 'Running' THEN 'Running' ELSE 'Pending' END,
       CASE WHEN w.bootstrap_state='Ready' THEN 1 ELSE 0 END,w.created_at,w.updated_at
FROM operations o
JOIN workspaces w ON w.id=o.workspace_id
WHERE o.agent_installation_id IS NOT NULL;

ALTER TABLE app_environments
    ADD COLUMN cluster_id BIGINT REFERENCES agent_installations(id) ON DELETE RESTRICT,
    ADD COLUMN runtime_observed_generation BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN runtime_observed_at TIMESTAMPTZ,
    ADD COLUMN reconciliation_generation BIGINT NOT NULL DEFAULT 0,
    ADD CONSTRAINT app_environments_runtime_generations_valid CHECK (
        runtime_observed_generation >= 0 AND reconciliation_generation >= 0
    );

UPDATE app_environments ae
SET cluster_id=selected.installation_id
FROM (
    SELECT app_environment_id,min(agent_installation_id) AS installation_id
    FROM operations
    WHERE app_environment_id IS NOT NULL AND agent_installation_id IS NOT NULL
    GROUP BY app_environment_id
) selected
WHERE selected.app_environment_id=ae.id;

UPDATE app_environments ae
SET cluster_id=only_cluster.id
FROM (
    SELECT min(id) AS id FROM agent_installations HAVING count(*)=1
) only_cluster
WHERE ae.cluster_id IS NULL;

DROP INDEX operations_one_active_workspace_bootstrap;
CREATE UNIQUE INDEX operations_one_active_workspace_cluster_bootstrap
    ON operations(workspace_id,agent_installation_id)
    WHERE kind='EnsureWorkspace' AND status IN ('Pending','Running');

-- +goose Down
DROP INDEX IF EXISTS operations_one_active_workspace_cluster_bootstrap;
CREATE UNIQUE INDEX operations_one_active_workspace_bootstrap
    ON operations(workspace_id)
    WHERE kind='EnsureWorkspace' AND status IN ('Pending','Running');
ALTER TABLE app_environments
    DROP CONSTRAINT IF EXISTS app_environments_runtime_generations_valid,
    DROP COLUMN IF EXISTS reconciliation_generation,
    DROP COLUMN IF EXISTS runtime_observed_at,
    DROP COLUMN IF EXISTS runtime_observed_generation,
    DROP COLUMN IF EXISTS cluster_id;
DROP TABLE IF EXISTS workspace_clusters;
DROP TABLE IF EXISTS agent_credentials;
ALTER TABLE agent_installations
    DROP COLUMN IF EXISTS revocation_reason,
    DROP COLUMN IF EXISTS revoked_at,
    DROP COLUMN IF EXISTS agent_version;
ALTER TABLE agent_installations
    DROP CONSTRAINT agent_installations_public_id_valid,
    ADD CONSTRAINT agent_installations_public_id_valid CHECK (public_id ~ '^(agi|cls)-[a-z2-7]{20}$');
