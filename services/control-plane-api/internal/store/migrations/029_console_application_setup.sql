-- +goose Up
CREATE TABLE app_environment_setups (
    workspace_id BIGINT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    requested_by_user_id BIGINT NOT NULL REFERENCES users(id),
    idempotency_hash BYTEA NOT NULL,
    payload_hash BYTEA NOT NULL,
    app_id BIGINT NOT NULL REFERENCES apps(id),
    app_environment_id BIGINT NOT NULL REFERENCES app_environments(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT app_environment_setups_hashes_valid CHECK (
        octet_length(idempotency_hash) = 32 AND octet_length(payload_hash) = 32
    ),
    PRIMARY KEY (workspace_id, requested_by_user_id, idempotency_hash),
    UNIQUE (app_environment_id)
);

-- +goose Down
DROP TABLE app_environment_setups;
