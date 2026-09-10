-- +goose Up
CREATE TABLE parameters (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    public_id TEXT NOT NULL UNIQUE,
    workspace_id BIGINT NOT NULL REFERENCES workspaces(id),
    path TEXT NOT NULL,
    kind TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    current_version BIGINT NOT NULL DEFAULT 1,
    version BIGINT NOT NULL DEFAULT 1,
    archived_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT parameters_public_id_valid CHECK (public_id ~ '^par-[a-z2-7]{20}$'),
    CONSTRAINT parameters_path_valid CHECK (path ~ '^/[a-zA-Z0-9._/-]{1,254}$' AND path !~ '//'),
    CONSTRAINT parameters_kind_valid CHECK (kind IN ('PlainText','Secret')),
    CONSTRAINT parameters_description_valid CHECK (char_length(description) <= 500),
    CONSTRAINT parameters_versions_valid CHECK (current_version > 0 AND version > 0),
    UNIQUE (id, workspace_id)
);
CREATE UNIQUE INDEX parameters_workspace_path_active
    ON parameters(workspace_id, path)
    WHERE archived_at IS NULL;
CREATE INDEX parameters_workspace_cursor
    ON parameters(workspace_id, id DESC);

CREATE TABLE parameter_versions (
    parameter_id BIGINT NOT NULL REFERENCES parameters(id) ON DELETE RESTRICT,
    version BIGINT NOT NULL,
    created_by_actor_id BIGINT NOT NULL REFERENCES actors(id),
    plaintext_value TEXT,
    secret_reference TEXT,
    secret_backend_version BIGINT,
    fingerprint BYTEA,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (parameter_id, version),
    CONSTRAINT parameter_versions_value_valid CHECK (
        (plaintext_value IS NOT NULL AND secret_reference IS NULL AND secret_backend_version IS NULL AND fingerprint IS NULL)
        OR
        (plaintext_value IS NULL AND secret_reference IS NOT NULL AND secret_backend_version IS NOT NULL AND secret_backend_version > 0 AND octet_length(fingerprint) = 32)
    ),
    CONSTRAINT parameter_versions_plaintext_size CHECK (plaintext_value IS NULL OR octet_length(plaintext_value) <= 65536),
    CONSTRAINT parameter_versions_secret_reference_valid CHECK (secret_reference IS NULL OR char_length(secret_reference) BETWEEN 1 AND 512)
);
