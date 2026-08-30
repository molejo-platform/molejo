-- +goose Up
DROP INDEX IF EXISTS app_environments_slug_active;

CREATE OR REPLACE FUNCTION molejo_runtime_configuration_v2(configuration JSONB)
RETURNS JSONB
LANGUAGE SQL
IMMUTABLE
AS $$
    SELECT jsonb_build_object(
        'replicas', configuration->'replicas',
        'ports', jsonb_build_array(jsonb_build_object(
            'name', 'http',
            'containerPort', configuration->'port',
            'protocol', 'TCP'
        )),
        'resources', configuration->'resources',
        'probes', jsonb_build_object(
            'startup', jsonb_build_object('type', 'HTTP', 'portName', 'http', 'path', configuration->'probes'->'readiness'->'path'),
            'readiness', jsonb_build_object('type', 'HTTP', 'portName', 'http', 'path', configuration->'probes'->'readiness'->'path'),
            'liveness', jsonb_build_object('type', 'HTTP', 'portName', 'http', 'path', configuration->'probes'->'liveness'->'path')
        ),
        'publicEndpoints', CASE WHEN configuration->>'exposure' = 'Public'
            THEN jsonb_build_array(jsonb_build_object('name', 'web', 'type', 'HTTP', 'portName', 'http', 'hostnameLabel', configuration->>'slug'))
            ELSE '[]'::jsonb END,
        'variables', COALESCE(configuration->'variables', '[]'::jsonb),
        'parameters', COALESCE(configuration->'parameters', '[]'::jsonb)
    )
$$;

UPDATE app_environments
SET configuration_json = molejo_runtime_configuration_v2(configuration_json);
UPDATE deployments
SET configuration_json = molejo_runtime_configuration_v2(configuration_json);
UPDATE app_environment_configuration_revisions
SET configuration_json = molejo_runtime_configuration_v2(configuration_json);

DROP FUNCTION molejo_runtime_configuration_v2(JSONB);

CREATE TABLE publication_claims (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    app_environment_id BIGINT NOT NULL REFERENCES app_environments(id) ON DELETE CASCADE,
    endpoint_name TEXT NOT NULL,
    endpoint_type TEXT NOT NULL,
    hostname TEXT NOT NULL,
    external_port INTEGER,
    desired_configuration_version BIGINT,
    current_configuration_version BIGINT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT publication_claims_name_valid CHECK (endpoint_name ~ '^[a-z0-9](?:[-a-z0-9]*[a-z0-9])?$' AND char_length(endpoint_name) <= 15),
    CONSTRAINT publication_claims_type_valid CHECK (endpoint_type IN ('HTTP', 'TCP')),
    CONSTRAINT publication_claims_hostname_valid CHECK (hostname ~ '^[a-z0-9](?:[-a-z0-9]*[a-z0-9])?\.[a-z0-9.-]+$' AND char_length(hostname) <= 253),
    CONSTRAINT publication_claims_port_valid CHECK ((endpoint_type = 'HTTP' AND external_port IS NULL) OR (endpoint_type = 'TCP' AND external_port BETWEEN 1 AND 65535)),
    CONSTRAINT publication_claims_reference_valid CHECK (desired_configuration_version IS NOT NULL OR current_configuration_version IS NOT NULL),
    UNIQUE (hostname)
);
CREATE UNIQUE INDEX publication_claims_external_port
    ON publication_claims(external_port) WHERE external_port IS NOT NULL;
CREATE INDEX publication_claims_app_environment
    ON publication_claims(app_environment_id);

-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (
        WITH existing_claims AS (
            SELECT ae.id AS app_environment_id,(endpoint->>'hostnameLabel') || '.molejo.dev' AS hostname
            FROM app_environments ae
            CROSS JOIN LATERAL jsonb_array_elements(ae.configuration_json->'publicEndpoints') endpoint
            UNION ALL
            SELECT ae.id,(endpoint->>'hostnameLabel') || '.molejo.dev'
            FROM app_environments ae
            JOIN deployments d ON d.id=ae.current_deployment_id
            CROSS JOIN LATERAL jsonb_array_elements(d.configuration_json->'publicEndpoints') endpoint
        )
        SELECT 1 FROM existing_claims GROUP BY hostname HAVING count(DISTINCT app_environment_id) > 1
    ) THEN
        RAISE EXCEPTION 'migration 019 found a public hostname claimed by different AppEnvironments';
    END IF;
END $$;
-- +goose StatementEnd

INSERT INTO publication_claims(app_environment_id,endpoint_name,endpoint_type,hostname,desired_configuration_version)
SELECT ae.id,endpoint->>'name',endpoint->>'type',(endpoint->>'hostnameLabel') || '.molejo.dev',ae.configuration_version
FROM app_environments ae
CROSS JOIN LATERAL jsonb_array_elements(ae.configuration_json->'publicEndpoints') endpoint;

INSERT INTO publication_claims(app_environment_id,endpoint_name,endpoint_type,hostname,current_configuration_version)
SELECT ae.id,endpoint->>'name',endpoint->>'type',(endpoint->>'hostnameLabel') || '.molejo.dev',d.configuration_version
FROM app_environments ae
JOIN deployments d ON d.id=ae.current_deployment_id
CROSS JOIN LATERAL jsonb_array_elements(d.configuration_json->'publicEndpoints') endpoint
ON CONFLICT(hostname) DO UPDATE
SET current_configuration_version=EXCLUDED.current_configuration_version,updated_at=now()
WHERE publication_claims.app_environment_id=EXCLUDED.app_environment_id;

-- +goose Down
DROP TABLE IF EXISTS publication_claims;
