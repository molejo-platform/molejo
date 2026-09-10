-- +goose Up
CREATE TABLE agent_installations (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    public_id TEXT NOT NULL UNIQUE,
    name TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'Pending',
    enrollment_attempt_id TEXT,
    csr_fingerprint BYTEA,
    certificate_pem BYTEA,
    ca_certificate_pem BYTEA,
    certificate_serial TEXT,
    certificate_fingerprint BYTEA,
    certificate_not_after TIMESTAMPTZ,
    last_seen_at TIMESTAMPTZ,
    created_by BIGINT NOT NULL REFERENCES users(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT agent_installations_public_id_valid CHECK (public_id ~ '^agi-[a-z2-7]{20}$'),
    CONSTRAINT agent_installations_name_valid CHECK (char_length(name) BETWEEN 1 AND 80),
    CONSTRAINT agent_installations_status_valid CHECK (status IN ('Pending', 'Active', 'Revoked')),
    CONSTRAINT agent_installations_certificate_complete CHECK (
        (certificate_pem IS NULL AND ca_certificate_pem IS NULL AND certificate_serial IS NULL AND certificate_fingerprint IS NULL AND certificate_not_after IS NULL)
        OR
        (certificate_pem IS NOT NULL AND ca_certificate_pem IS NOT NULL AND certificate_serial IS NOT NULL AND certificate_fingerprint IS NOT NULL AND certificate_not_after IS NOT NULL)
    )
);

CREATE TABLE agent_enrollment_tokens (
    installation_id BIGINT PRIMARY KEY REFERENCES agent_installations(id) ON DELETE CASCADE,
    token_hash BYTEA NOT NULL UNIQUE,
    expires_at TIMESTAMPTZ NOT NULL,
    consumed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX agent_installations_status ON agent_installations(status);
CREATE INDEX agent_installations_last_seen ON agent_installations(last_seen_at) WHERE status = 'Active';

-- +goose Down
DROP TABLE IF EXISTS agent_enrollment_tokens;
DROP TABLE IF EXISTS agent_installations;
