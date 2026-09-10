-- +goose Up
ALTER TABLE users DROP CONSTRAINT users_status_valid;
ALTER TABLE users
    ADD CONSTRAINT users_status_valid CHECK (status IN ('Invited', 'Active', 'Disabled', 'Locked'));

CREATE TABLE user_invitations (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    public_id TEXT NOT NULL UNIQUE,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash BYTEA NOT NULL UNIQUE,
    expires_at TIMESTAMPTZ NOT NULL,
    accepted_at TIMESTAMPTZ,
    revoked_at TIMESTAMPTZ,
    created_by_user_id BIGINT NOT NULL REFERENCES users(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT user_invitations_public_id_valid CHECK (public_id ~ '^uin-[a-z2-7]{20}$')
);
CREATE UNIQUE INDEX user_invitations_one_active
    ON user_invitations(user_id)
    WHERE accepted_at IS NULL AND revoked_at IS NULL;

-- +goose Down
DROP TABLE IF EXISTS user_invitations;
ALTER TABLE users DROP CONSTRAINT users_status_valid;
ALTER TABLE users
    ADD CONSTRAINT users_status_valid CHECK (status IN ('Active', 'Disabled', 'Locked'));
