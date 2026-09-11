-- +goose Up
-- Alpha installs start empty. No old HTTP configuration is translated.
CREATE TABLE publication_domains (
 id TEXT PRIMARY KEY CHECK(length(id) BETWEEN 1 AND 128),
 name TEXT NOT NULL CHECK(length(name) BETWEEN 1 AND 253),
 kind TEXT NOT NULL CHECK (kind IN ('Exact','SubdomainPool')),
 reserved_names JSONB NOT NULL DEFAULT '[]' CHECK (jsonb_typeof(reserved_names)='array' AND jsonb_array_length(reserved_names)<=100),
 version BIGINT NOT NULL DEFAULT 1 CHECK (version>0),
 created_by BIGINT NOT NULL REFERENCES users(id),
 updated_by BIGINT NOT NULL REFERENCES users(id),
 created_at TIMESTAMPTZ NOT NULL DEFAULT now(), updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 UNIQUE(name,kind)
);
DROP TABLE cluster_publication_bindings;
CREATE TABLE cluster_publication_bindings (
 id TEXT PRIMARY KEY CHECK(length(id) BETWEEN 1 AND 128),
 cluster_id BIGINT NOT NULL UNIQUE REFERENCES agent_installations(id),
 kind TEXT NOT NULL DEFAULT 'KubernetesHTTP' CHECK(kind='KubernetesHTTP'),
 configuration JSONB NOT NULL CHECK(jsonb_typeof(configuration)='object' AND configuration ? 'schemaVersion' AND configuration->>'schemaVersion'='kubernetes-http.v1alpha1'),
 version BIGINT NOT NULL DEFAULT 1 CHECK(version>0),
 observation JSONB NOT NULL DEFAULT '{}',
 observed_at TIMESTAMPTZ, expires_at TIMESTAMPTZ,
 observed_session_id TEXT NOT NULL DEFAULT '', observed_sequence BIGINT NOT NULL DEFAULT 0,
 created_by BIGINT NOT NULL REFERENCES users(id), updated_by BIGINT NOT NULL REFERENCES users(id),
 created_at TIMESTAMPTZ NOT NULL DEFAULT now(), updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE publication_grants (
 domain_id TEXT NOT NULL REFERENCES publication_domains(id),
 workspace_id BIGINT NOT NULL REFERENCES workspaces(id),
 binding_id TEXT NOT NULL REFERENCES cluster_publication_bindings(id),
 created_by BIGINT NOT NULL REFERENCES users(id), created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 PRIMARY KEY(domain_id,workspace_id,binding_id)
);
ALTER TABLE deployments ADD COLUMN publication_snapshot JSONB NOT NULL DEFAULT '{"schemaVersion":"publication.v1","addresses":[]}';
CREATE TABLE publication_execution_claims (
 deployment_id BIGINT NOT NULL REFERENCES deployments(id),
 hostname TEXT NOT NULL CHECK(length(hostname) BETWEEN 1 AND 253),
 domain_id TEXT NOT NULL REFERENCES publication_domains(id),
 binding_id TEXT NOT NULL REFERENCES cluster_publication_bindings(id),
 endpoint_name TEXT NOT NULL CHECK(length(endpoint_name) BETWEEN 1 AND 15),
 PRIMARY KEY(deployment_id,hostname)
);
ALTER TABLE publication_claims DROP CONSTRAINT publication_claims_reference_valid;
ALTER TABLE publication_claims DROP CONSTRAINT publication_claims_hostname_valid;
ALTER TABLE publication_claims ADD CONSTRAINT publication_claims_hostname_valid CHECK(length(hostname) BETWEEN 1 AND 253 AND hostname ~ '^[a-z0-9]([-a-z0-9.]*[a-z0-9])?$');
ALTER TABLE publication_claims ADD COLUMN domain_id TEXT REFERENCES publication_domains(id), ADD COLUMN binding_id TEXT REFERENCES cluster_publication_bindings(id);
CREATE TABLE operation_attempts (
 operation_id BIGINT NOT NULL REFERENCES operations(id),
 fencing_token BIGINT NOT NULL,
 session_id TEXT NOT NULL,
 deadline TIMESTAMPTZ NOT NULL,
 state TEXT NOT NULL CHECK(state IN ('Issued','Uncertain','Completed','Fenced')),
 runtime_uid TEXT NOT NULL DEFAULT '' CHECK(length(runtime_uid)<=128),
 withdrawal_confirmed BOOLEAN NOT NULL DEFAULT false CHECK(NOT withdrawal_confirmed OR runtime_uid<>''),
 created_at TIMESTAMPTZ NOT NULL DEFAULT now(), completed_at TIMESTAMPTZ,
 PRIMARY KEY(operation_id,fencing_token)
);
ALTER TABLE app_environments ADD COLUMN withdrawal_state TEXT NOT NULL DEFAULT 'None' CHECK(withdrawal_state IN ('None','Requested','Removing','Confirmed')),
 ADD COLUMN publication_observation JSONB NOT NULL DEFAULT '{}';
-- +goose Down
-- Downgrading would remove the durable withdrawal and allocation guarantees.
-- Alpha environments must be reset rather than converted to the former contract.
-- +goose StatementBegin
DO $$ BEGIN
 RAISE EXCEPTION 'HTTP publication contract cannot be downgraded; reset the alpha installation';
END $$;
-- +goose StatementEnd
