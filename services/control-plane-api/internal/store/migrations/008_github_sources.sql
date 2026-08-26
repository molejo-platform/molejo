-- +goose Up
CREATE TABLE github_connection_states (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    state_hash BYTEA NOT NULL UNIQUE,
    browser_hash BYTEA NOT NULL,
    actor_id BIGINT NOT NULL REFERENCES actors(id),
    workspace_id BIGINT NOT NULL REFERENCES workspaces(id),
    step TEXT NOT NULL,
    github_installation_id BIGINT,
    expires_at TIMESTAMPTZ NOT NULL,
    consumed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT github_connection_states_step_valid
        CHECK (step IN ('installation', 'authorization')),
    CONSTRAINT github_connection_states_installation_valid
        CHECK ((step = 'installation' AND github_installation_id IS NULL)
            OR (step = 'authorization' AND github_installation_id > 0))
);

CREATE INDEX github_connection_states_expiry
    ON github_connection_states(expires_at)
    WHERE consumed_at IS NULL;

CREATE TABLE github_installations (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    public_id TEXT NOT NULL UNIQUE,
    workspace_id BIGINT NOT NULL REFERENCES workspaces(id),
    connected_by_actor_id BIGINT NOT NULL REFERENCES actors(id),
    github_installation_id BIGINT NOT NULL UNIQUE,
    account_id BIGINT NOT NULL,
    account_login TEXT NOT NULL,
    account_type TEXT NOT NULL,
    repository_selection TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT github_installations_public_id_valid
        CHECK (public_id ~ '^ghi-[a-z2-7]{20}$'),
    CONSTRAINT github_installations_account_valid
        CHECK (account_id > 0 AND char_length(account_login) > 0),
    CONSTRAINT github_installations_account_type_valid
        CHECK (account_type IN ('User', 'Organization', 'Enterprise')),
    CONSTRAINT github_installations_selection_valid
        CHECK (repository_selection IN ('all', 'selected'))
);

CREATE INDEX github_installations_workspace
    ON github_installations(workspace_id, id DESC);

CREATE TABLE app_github_sources (
    app_id BIGINT PRIMARY KEY REFERENCES apps(id),
    github_installation_id BIGINT NOT NULL REFERENCES github_installations(id) ON DELETE RESTRICT,
    repository_id BIGINT NOT NULL,
    repository_name TEXT NOT NULL,
    repository_full_name TEXT NOT NULL,
    repository_private BOOLEAN NOT NULL,
    default_branch TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT app_github_sources_repository_valid
        CHECK (repository_id > 0 AND char_length(repository_name) > 0
            AND char_length(repository_full_name) > 2 AND char_length(default_branch) > 0)
);

CREATE INDEX app_github_sources_installation
    ON app_github_sources(github_installation_id);
