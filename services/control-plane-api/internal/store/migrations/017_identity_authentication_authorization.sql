-- +goose Up
ALTER TABLE actors RENAME TO users;
ALTER TABLE users RENAME COLUMN actor_key TO username;

ALTER TABLE users
    ADD COLUMN public_id TEXT,
    ADD COLUMN username_key TEXT,
    ADD COLUMN display_name TEXT,
    ADD COLUMN status TEXT NOT NULL DEFAULT 'Active',
    ADD COLUMN version BIGINT NOT NULL DEFAULT 1,
    ADD COLUMN auth_version BIGINT NOT NULL DEFAULT 1,
    ADD COLUMN updated_at TIMESTAMPTZ NOT NULL DEFAULT now();

UPDATE users
SET public_id = 'usr-' || translate(substr(md5(username || ':' || id::text), 1, 20), '0123456789abcdef', 'abcdefghijklmnop'),
    username_key = lower(trim(username)),
    display_name = username;

ALTER TABLE users
    ALTER COLUMN public_id SET NOT NULL,
    ALTER COLUMN username_key SET NOT NULL,
    ALTER COLUMN display_name SET NOT NULL,
    ADD CONSTRAINT users_public_id_unique UNIQUE (public_id),
    ADD CONSTRAINT users_username_key_unique UNIQUE (username_key),
    ADD CONSTRAINT users_public_id_valid CHECK (public_id ~ '^usr-[a-z2-7]{20}$'),
    ADD CONSTRAINT users_username_valid CHECK (username = username_key AND username ~ '^[a-z0-9][a-z0-9._-]{2,63}$'),
    ADD CONSTRAINT users_display_name_valid CHECK (char_length(display_name) BETWEEN 1 AND 120),
    ADD CONSTRAINT users_status_valid CHECK (status IN ('Active', 'Disabled', 'Locked')),
    ADD CONSTRAINT users_version_valid CHECK (version > 0),
    ADD CONSTRAINT users_auth_version_valid CHECK (auth_version > 0);

CREATE TABLE password_credentials (
    user_id BIGINT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    password_hash TEXT NOT NULL,
    algorithm TEXT NOT NULL DEFAULT 'argon2id',
    changed_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT password_credentials_algorithm_valid CHECK (algorithm = 'argon2id')
);

INSERT INTO password_credentials(user_id, password_hash)
SELECT id, password_hash FROM users;

ALTER TABLE workspace_actors RENAME TO workspace_memberships;
ALTER TABLE workspace_memberships RENAME COLUMN actor_id TO user_id;
ALTER TABLE workspace_memberships
    ADD COLUMN role TEXT,
    ADD COLUMN status TEXT NOT NULL DEFAULT 'Active',
    ADD COLUMN version BIGINT NOT NULL DEFAULT 1,
    ADD COLUMN created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    ADD COLUMN updated_at TIMESTAMPTZ NOT NULL DEFAULT now();

UPDATE workspace_memberships wm
SET role = CASE WHEN u.role = 'owner' THEN 'Owner' ELSE 'Viewer' END
FROM users u
WHERE u.id = wm.user_id;

ALTER TABLE workspace_memberships
    ALTER COLUMN role SET NOT NULL,
    ADD CONSTRAINT workspace_memberships_role_valid CHECK (role IN ('Owner', 'Member', 'Viewer')),
    ADD CONSTRAINT workspace_memberships_status_valid CHECK (status IN ('Active', 'Suspended')),
    ADD CONSTRAINT workspace_memberships_version_valid CHECK (version > 0),
    ADD CONSTRAINT workspace_memberships_workspace_user_unique UNIQUE (workspace_id, user_id);

CREATE TABLE installation_role_assignments (
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, role),
    CONSTRAINT installation_role_assignments_role_valid CHECK (role = 'Administrator')
);

INSERT INTO installation_role_assignments(user_id, role)
SELECT id, 'Administrator' FROM users WHERE role = 'owner';

ALTER TABLE users DROP COLUMN password_hash;
ALTER TABLE users DROP COLUMN role;

ALTER TABLE operations RENAME COLUMN actor_id TO requested_by_user_id;
ALTER TABLE sessions RENAME COLUMN actor_id TO user_id;
ALTER TABLE builds RENAME COLUMN requested_by_actor_id TO requested_by_user_id;
ALTER TABLE deployments RENAME COLUMN requested_by_actor_id TO requested_by_user_id;
ALTER TABLE github_connection_states RENAME COLUMN actor_id TO user_id;
ALTER TABLE github_installations RENAME COLUMN connected_by_actor_id TO connected_by_user_id;
ALTER TABLE parameter_versions RENAME COLUMN created_by_actor_id TO created_by_user_id;
ALTER TABLE app_environment_configuration_revisions RENAME COLUMN created_by_actor_id TO created_by_user_id;
ALTER TABLE app_environment_delivery_policies RENAME COLUMN updated_by_actor_id TO updated_by_user_id;
ALTER TABLE delivery_targets RENAME COLUMN requested_by_actor_id TO requested_by_user_id;

ALTER TABLE sessions
    ADD COLUMN public_id TEXT,
    ADD COLUMN auth_version BIGINT NOT NULL DEFAULT 1,
    ADD COLUMN assurance_level TEXT NOT NULL DEFAULT 'AAL1',
    ADD COLUMN last_seen_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    ADD COLUMN idle_expires_at TIMESTAMPTZ;

UPDATE sessions
SET public_id = 'ses-' || translate(substr(md5(encode(token_hash, 'hex') || ':' || id::text), 1, 20), '0123456789abcdef', 'abcdefghijklmnop'),
    idle_expires_at = expires_at;

ALTER TABLE sessions
    ALTER COLUMN public_id SET NOT NULL,
    ALTER COLUMN idle_expires_at SET NOT NULL,
    ADD CONSTRAINT sessions_public_id_unique UNIQUE (public_id),
    ADD CONSTRAINT sessions_public_id_valid CHECK (public_id ~ '^ses-[a-z2-7]{20}$'),
    ADD CONSTRAINT sessions_assurance_level_valid CHECK (assurance_level IN ('AAL1', 'AAL2'));

CREATE TABLE password_reset_grants (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    public_id TEXT NOT NULL UNIQUE,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    code_hash BYTEA NOT NULL,
    attempts INTEGER NOT NULL DEFAULT 0,
    expires_at TIMESTAMPTZ NOT NULL,
    verified_at TIMESTAMPTZ,
    consumed_at TIMESTAMPTZ,
    created_by_user_id BIGINT NOT NULL REFERENCES users(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT password_reset_grants_public_id_valid CHECK (public_id ~ '^prg-[a-z2-7]{20}$'),
    CONSTRAINT password_reset_grants_attempts_valid CHECK (attempts BETWEEN 0 AND 5)
);
CREATE UNIQUE INDEX password_reset_grants_one_active
    ON password_reset_grants(user_id)
    WHERE consumed_at IS NULL;

CREATE TABLE workspace_groups (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    public_id TEXT NOT NULL UNIQUE,
    workspace_id BIGINT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    name_key TEXT NOT NULL,
    version BIGINT NOT NULL DEFAULT 1,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT workspace_groups_public_id_valid CHECK (public_id ~ '^grp-[a-z2-7]{20}$'),
    CONSTRAINT workspace_groups_name_valid CHECK (char_length(name) BETWEEN 1 AND 80),
    CONSTRAINT workspace_groups_version_valid CHECK (version > 0),
    UNIQUE (workspace_id, name_key),
    UNIQUE (workspace_id, id)
);

CREATE TABLE workspace_group_members (
    workspace_id BIGINT NOT NULL,
    group_id BIGINT NOT NULL,
    user_id BIGINT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (group_id, user_id),
    FOREIGN KEY (workspace_id, group_id) REFERENCES workspace_groups(workspace_id, id) ON DELETE CASCADE,
    FOREIGN KEY (workspace_id, user_id) REFERENCES workspace_memberships(workspace_id, user_id) ON DELETE CASCADE
);

CREATE TABLE access_grants (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    public_id TEXT NOT NULL UNIQUE,
    workspace_id BIGINT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    subject_type TEXT NOT NULL,
    subject_id BIGINT NOT NULL,
    resource_type TEXT NOT NULL,
    resource_public_id TEXT NOT NULL,
    relation TEXT NOT NULL,
    version BIGINT NOT NULL DEFAULT 1,
    created_by_user_id BIGINT NOT NULL REFERENCES users(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT access_grants_public_id_valid CHECK (public_id ~ '^agr-[a-z2-7]{20}$'),
    CONSTRAINT access_grants_subject_type_valid CHECK (subject_type IN ('User', 'Group')),
    CONSTRAINT access_grants_resource_type_valid CHECK (resource_type IN ('Workspace', 'Project', 'App', 'AppEnvironment')),
    CONSTRAINT access_grants_relation_valid CHECK (relation IN ('Viewer', 'Editor', 'Deployer', 'Manager')),
    CONSTRAINT access_grants_version_valid CHECK (version > 0),
    UNIQUE (workspace_id, subject_type, subject_id, resource_type, resource_public_id, relation)
);

CREATE TABLE webauthn_credentials (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    public_id TEXT NOT NULL UNIQUE,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    credential_id BYTEA NOT NULL UNIQUE,
    credential_json JSONB NOT NULL,
    name TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_used_at TIMESTAMPTZ,
    CONSTRAINT webauthn_credentials_public_id_valid CHECK (public_id ~ '^wac-[a-z2-7]{20}$'),
    CONSTRAINT webauthn_credentials_name_valid CHECK (char_length(name) BETWEEN 1 AND 80)
);

CREATE TABLE totp_credentials (
    user_id BIGINT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    secret_reference TEXT NOT NULL,
    secret_version BIGINT NOT NULL,
    enabled_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_used_step BIGINT NOT NULL DEFAULT 0,
    CONSTRAINT totp_credentials_secret_version_valid CHECK (secret_version > 0)
);

CREATE TABLE recovery_codes (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    code_hash BYTEA NOT NULL,
    used_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE authentication_challenges (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    challenge_hash BYTEA NOT NULL UNIQUE,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    kind TEXT NOT NULL,
    payload_json JSONB NOT NULL DEFAULT '{}',
    attempts INTEGER NOT NULL DEFAULT 0,
    expires_at TIMESTAMPTZ NOT NULL,
    consumed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT authentication_challenges_kind_valid CHECK (kind IN ('Login', 'WebAuthnRegistration', 'TOTPEnrollment')),
    CONSTRAINT authentication_challenges_attempts_valid CHECK (attempts BETWEEN 0 AND 5)
);

CREATE TABLE authentication_rate_limits (
    key_hash BYTEA PRIMARY KEY,
    failures INTEGER NOT NULL DEFAULT 0,
    blocked_until TIMESTAMPTZ,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT authentication_rate_limits_failures_valid CHECK (failures >= 0)
);

CREATE TABLE audit_events (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    public_id TEXT NOT NULL UNIQUE,
    occurred_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    actor_user_id BIGINT REFERENCES users(id),
    session_id BIGINT REFERENCES sessions(id),
    workspace_id BIGINT REFERENCES workspaces(id),
    action TEXT NOT NULL,
    target_type TEXT NOT NULL,
    target_public_id TEXT NOT NULL DEFAULT '',
    outcome TEXT NOT NULL,
    reason TEXT NOT NULL DEFAULT '',
    request_id TEXT NOT NULL DEFAULT '',
    trace_id TEXT NOT NULL DEFAULT '',
    source_hash BYTEA,
    user_agent_hash BYTEA,
    metadata_json JSONB NOT NULL DEFAULT '{}',
    CONSTRAINT audit_events_public_id_valid CHECK (public_id ~ '^aud-[a-z2-7]{20}$'),
    CONSTRAINT audit_events_action_valid CHECK (action ~ '^[a-z][a-z0-9_.]{2,127}$'),
    CONSTRAINT audit_events_outcome_valid CHECK (outcome IN ('Succeeded', 'Failed', 'Denied'))
);
CREATE INDEX audit_events_workspace_cursor ON audit_events(workspace_id, id DESC);
CREATE INDEX audit_events_actor_cursor ON audit_events(actor_user_id, id DESC);

-- Audit events are append-only at the database boundary.
-- +goose StatementBegin
CREATE FUNCTION prevent_audit_event_mutation() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'audit events are immutable';
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER audit_events_immutable
    BEFORE UPDATE OR DELETE ON audit_events
    FOR EACH ROW EXECUTE FUNCTION prevent_audit_event_mutation();

-- +goose Down
DROP TRIGGER IF EXISTS audit_events_immutable ON audit_events;
DROP FUNCTION IF EXISTS prevent_audit_event_mutation;
DROP TABLE IF EXISTS audit_events;
DROP TABLE IF EXISTS authentication_rate_limits;
DROP TABLE IF EXISTS authentication_challenges;
DROP TABLE IF EXISTS recovery_codes;
DROP TABLE IF EXISTS totp_credentials;
DROP TABLE IF EXISTS webauthn_credentials;
DROP TABLE IF EXISTS access_grants;
DROP TABLE IF EXISTS workspace_group_members;
DROP TABLE IF EXISTS workspace_groups;
DROP TABLE IF EXISTS password_reset_grants;
DROP TABLE IF EXISTS installation_role_assignments;
DROP TABLE IF EXISTS password_credentials;
-- This migration is an intentional pre-alpha cutover and is not reversible.
