-- +goose Up
ALTER TABLE parameters
    ADD COLUMN value_state TEXT NOT NULL DEFAULT 'Ready',
    ADD COLUMN purge_after TIMESTAMPTZ,
    ADD COLUMN purged_at TIMESTAMPTZ,
    ADD CONSTRAINT parameters_value_state_valid CHECK (value_state IN ('Pending', 'Ready', 'Purged'));

UPDATE parameters
SET purge_after = now() + interval '7 days'
WHERE archived_at IS NOT NULL;

ALTER TABLE parameters
    ADD CONSTRAINT parameters_purge_valid CHECK (
        (archived_at IS NULL AND purge_after IS NULL AND purged_at IS NULL)
        OR (archived_at IS NOT NULL AND purge_after IS NOT NULL)
    );

ALTER TABLE parameter_versions
    ADD COLUMN value_state TEXT NOT NULL DEFAULT 'Ready',
    ADD COLUMN expected_secret_backend_version BIGINT,
    ADD COLUMN idempotency_hash BYTEA,
    ADD COLUMN payload_hash BYTEA,
    ADD COLUMN requested_path TEXT,
    ADD COLUMN requested_description TEXT,
    ADD COLUMN completed_at TIMESTAMPTZ,
    ADD CONSTRAINT parameter_versions_state_valid CHECK (value_state IN ('Pending', 'Ready', 'Purged')),
    ADD CONSTRAINT parameter_versions_pending_valid CHECK (
        (value_state IN ('Ready', 'Purged') AND expected_secret_backend_version IS NULL)
        OR (
            value_state = 'Pending'
            AND plaintext_value IS NULL
            AND secret_reference IS NOT NULL
            AND secret_backend_version IS NOT NULL
            AND expected_secret_backend_version >= 0
            AND octet_length(idempotency_hash) = 32
            AND octet_length(payload_hash) = 32
            AND requested_path ~ '^/[a-zA-Z0-9._/-]{1,254}$'
            AND requested_path !~ '//'
            AND char_length(requested_description) <= 500
            AND completed_at IS NULL
        )
    );

ALTER TABLE parameter_versions
    DROP CONSTRAINT parameter_versions_value_valid,
    ADD CONSTRAINT parameter_versions_value_valid CHECK (
        (
            value_state = 'Purged'
            AND plaintext_value IS NULL
            AND secret_reference IS NULL
            AND secret_backend_version IS NULL
            AND fingerprint IS NULL
        )
        OR (
            value_state <> 'Purged'
            AND (
                (plaintext_value IS NOT NULL AND secret_reference IS NULL AND secret_backend_version IS NULL AND fingerprint IS NULL)
                OR
                (plaintext_value IS NULL AND secret_reference IS NOT NULL AND secret_backend_version IS NOT NULL AND secret_backend_version > 0 AND octet_length(fingerprint) = 32)
            )
        )
    );

CREATE UNIQUE INDEX parameter_versions_actor_idempotency
    ON parameter_versions(created_by_actor_id, idempotency_hash)
    WHERE idempotency_hash IS NOT NULL;

CREATE UNIQUE INDEX parameter_versions_one_pending
    ON parameter_versions(parameter_id)
    WHERE value_state = 'Pending';

CREATE TABLE parameter_path_reservations (
    workspace_id BIGINT NOT NULL REFERENCES workspaces(id),
    path TEXT NOT NULL,
    parameter_id BIGINT NOT NULL REFERENCES parameters(id) ON DELETE CASCADE,
    parameter_version BIGINT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (workspace_id, path),
    UNIQUE (parameter_id, parameter_version),
    CONSTRAINT parameter_path_reservations_path_valid CHECK (
        path ~ '^/[a-zA-Z0-9._/-]{1,254}$' AND path !~ '//'
    )
);

CREATE INDEX parameters_purge_queue
    ON parameters(purge_after, id)
    WHERE archived_at IS NOT NULL AND purged_at IS NULL;
