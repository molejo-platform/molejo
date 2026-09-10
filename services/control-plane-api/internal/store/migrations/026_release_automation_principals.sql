-- +goose Up
CREATE TABLE principals (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    public_id TEXT NOT NULL UNIQUE,
    kind TEXT NOT NULL,
    display_name TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'Active',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT principals_public_id_valid CHECK (public_id ~ '^(usr|svc|sys)-[a-z2-7]{20}$'),
    CONSTRAINT principals_kind_valid CHECK (kind IN ('User', 'ServiceAccount', 'System')),
    CONSTRAINT principals_display_name_valid CHECK (char_length(display_name) BETWEEN 1 AND 120),
    CONSTRAINT principals_status_valid CHECK (status IN ('Active', 'Disabled'))
);

ALTER TABLE users ADD COLUMN principal_id BIGINT;
INSERT INTO principals(public_id,kind,display_name,status,created_at,updated_at)
SELECT public_id,'User',display_name,
       CASE WHEN status='Active' THEN 'Active' ELSE 'Disabled' END,
       created_at,updated_at
FROM users;
UPDATE users u SET principal_id=p.id FROM principals p WHERE p.public_id=u.public_id;
ALTER TABLE users
    ALTER COLUMN principal_id SET NOT NULL,
    ADD CONSTRAINT users_principal_unique UNIQUE (principal_id),
    ADD CONSTRAINT users_principal_fk FOREIGN KEY (principal_id) REFERENCES principals(id);

-- +goose StatementBegin
CREATE FUNCTION synchronize_user_principal() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.principal_id IS NULL THEN
        INSERT INTO principals(public_id,kind,display_name,status)
        VALUES(NEW.public_id,'User',NEW.display_name,CASE WHEN NEW.status='Active' THEN 'Active' ELSE 'Disabled' END)
        RETURNING id INTO NEW.principal_id;
    ELSE
        UPDATE principals SET display_name=NEW.display_name,
            status=CASE WHEN NEW.status='Active' THEN 'Active' ELSE 'Disabled' END,
            updated_at=now()
        WHERE id=NEW.principal_id;
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER users_principal
    BEFORE INSERT OR UPDATE OF display_name,status ON users
    FOR EACH ROW EXECUTE FUNCTION synchronize_user_principal();

CREATE TABLE service_accounts (
    principal_id BIGINT PRIMARY KEY REFERENCES principals(id) ON DELETE CASCADE,
    workspace_id BIGINT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    project_id BIGINT NOT NULL,
    app_id BIGINT NOT NULL,
    name TEXT NOT NULL,
    created_by_user_id BIGINT NOT NULL REFERENCES users(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT service_accounts_project_workspace_fk
        FOREIGN KEY (project_id,workspace_id) REFERENCES projects(id,workspace_id),
    CONSTRAINT service_accounts_app_project_fk
        FOREIGN KEY (app_id,project_id) REFERENCES apps(id,project_id),
    CONSTRAINT service_accounts_name_valid CHECK (char_length(name) BETWEEN 1 AND 80),
    UNIQUE (app_id,name)
);

CREATE TABLE service_account_tokens (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    public_id TEXT NOT NULL UNIQUE,
    principal_id BIGINT NOT NULL REFERENCES service_accounts(principal_id) ON DELETE CASCADE,
    token_hash BYTEA NOT NULL UNIQUE,
    expires_at TIMESTAMPTZ NOT NULL,
    revoked_at TIMESTAMPTZ,
    last_used_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT service_account_tokens_public_id_valid CHECK (public_id ~ '^sat-[a-z2-7]{20}$')
);
CREATE INDEX service_account_tokens_principal ON service_account_tokens(principal_id);

CREATE TABLE service_account_grants (
    principal_id BIGINT NOT NULL REFERENCES service_accounts(principal_id) ON DELETE CASCADE,
    app_id BIGINT NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
    permission TEXT NOT NULL,
    app_environment_id BIGINT REFERENCES app_environments(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT service_account_grants_permission_valid
        CHECK (permission IN ('release.write','deployment.create')),
    CONSTRAINT service_account_grants_scope_valid CHECK (
        (permission='release.write' AND app_environment_id IS NULL)
        OR (permission='deployment.create' AND app_environment_id IS NOT NULL)
    ),
    UNIQUE NULLS NOT DISTINCT (principal_id,permission,app_environment_id)
);

ALTER TABLE audit_events ADD COLUMN actor_principal_id BIGINT REFERENCES principals(id);
ALTER TABLE audit_events DISABLE TRIGGER audit_events_immutable;
UPDATE audit_events ae SET actor_principal_id=u.principal_id
FROM users u WHERE u.id=ae.actor_user_id;
ALTER TABLE audit_events ENABLE TRIGGER audit_events_immutable;
CREATE INDEX audit_events_principal_cursor ON audit_events(actor_principal_id,id DESC);

ALTER TABLE deployments
    ADD COLUMN requested_by_principal_id BIGINT REFERENCES principals(id);
UPDATE deployments d SET requested_by_principal_id=u.principal_id
FROM users u WHERE u.id=d.requested_by_user_id;
ALTER TABLE deployments
    ALTER COLUMN requested_by_principal_id SET NOT NULL,
    ALTER COLUMN requested_by_user_id DROP NOT NULL;

-- +goose StatementBegin
CREATE FUNCTION populate_requested_by_principal() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.requested_by_principal_id IS NULL AND NEW.requested_by_user_id IS NOT NULL THEN
        SELECT principal_id INTO NEW.requested_by_principal_id FROM users WHERE id=NEW.requested_by_user_id;
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER deployments_requested_by_principal
    BEFORE INSERT ON deployments FOR EACH ROW EXECUTE FUNCTION populate_requested_by_principal();

ALTER TABLE operations
    ADD COLUMN requested_by_principal_id BIGINT REFERENCES principals(id);
UPDATE operations o SET requested_by_principal_id=u.principal_id
FROM users u WHERE u.id=o.requested_by_user_id;
ALTER TABLE operations
    ALTER COLUMN requested_by_principal_id SET NOT NULL,
    ALTER COLUMN requested_by_user_id DROP NOT NULL;
CREATE TRIGGER operations_requested_by_principal
    BEFORE INSERT ON operations FOR EACH ROW EXECUTE FUNCTION populate_requested_by_principal();
CREATE UNIQUE INDEX operations_principal_idempotency
    ON operations(workspace_id,requested_by_principal_id,idempotency_hash);

ALTER TABLE releases
    ALTER COLUMN build_id DROP NOT NULL,
    ALTER COLUMN app_environment_id DROP NOT NULL,
    ALTER COLUMN commit_sha DROP NOT NULL,
    ALTER COLUMN platform DROP NOT NULL,
    ADD COLUMN origin_kind TEXT NOT NULL DEFAULT 'ManagedBuild',
    ADD COLUMN source_provider TEXT,
    ADD COLUMN source_repository TEXT,
    ADD COLUMN source_revision TEXT,
    ADD COLUMN source_ref TEXT,
    ADD COLUMN producer_kind TEXT NOT NULL DEFAULT 'buildkit',
    ADD COLUMN producer_external_id TEXT,
    ADD COLUMN producer_url TEXT,
    ADD COLUMN created_by_principal_id BIGINT REFERENCES principals(id),
    ADD COLUMN idempotency_hash BYTEA,
    ADD COLUMN payload_hash BYTEA,
    ADD CONSTRAINT releases_origin_valid CHECK (origin_kind IN ('ManagedBuild','External')),
    ADD CONSTRAINT releases_origin_build_valid CHECK (
        (origin_kind='ManagedBuild' AND build_id IS NOT NULL)
        OR (origin_kind='External' AND build_id IS NULL)
    ),
    ADD CONSTRAINT releases_source_valid CHECK (
        (source_provider IS NULL AND source_repository IS NULL AND source_revision IS NULL AND source_ref IS NULL)
        OR (source_provider IS NOT NULL AND source_repository IS NOT NULL AND source_revision IS NOT NULL)
    ),
    ADD CONSTRAINT releases_idempotency_valid CHECK (
        (idempotency_hash IS NULL AND payload_hash IS NULL)
        OR (octet_length(idempotency_hash)=32 AND octet_length(payload_hash)=32)
    );

UPDATE releases r SET
    source_provider='GitHub',
    source_repository=b.repository_full_name,
    source_revision=b.commit_sha,
    source_ref=b.source_branch,
    producer_kind='buildkit',
    created_by_principal_id=u.principal_id
FROM builds b JOIN users u ON u.id=b.requested_by_user_id
WHERE b.id=r.build_id;
ALTER TABLE releases ALTER COLUMN created_by_principal_id SET NOT NULL;
CREATE UNIQUE INDEX releases_external_idempotency
    ON releases(app_id,created_by_principal_id,idempotency_hash)
    WHERE idempotency_hash IS NOT NULL;

-- +goose Down
-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS(SELECT 1 FROM releases WHERE build_id IS NULL)
       OR EXISTS(SELECT 1 FROM deployments WHERE requested_by_user_id IS NULL)
       OR EXISTS(SELECT 1 FROM operations WHERE requested_by_user_id IS NULL)
       OR EXISTS(SELECT 1 FROM audit_events ae JOIN principals p ON p.id=ae.actor_principal_id WHERE p.kind='ServiceAccount') THEN
        RAISE EXCEPTION 'migration 026 cannot be reverted after service account automation has been used';
    END IF;
END;
$$;
-- +goose StatementEnd
DROP INDEX IF EXISTS releases_external_idempotency;
ALTER TABLE releases
    DROP COLUMN IF EXISTS payload_hash,
    DROP COLUMN IF EXISTS idempotency_hash,
    DROP COLUMN IF EXISTS created_by_principal_id,
    DROP COLUMN IF EXISTS producer_url,
    DROP COLUMN IF EXISTS producer_external_id,
    DROP COLUMN IF EXISTS producer_kind,
    DROP COLUMN IF EXISTS source_ref,
    DROP COLUMN IF EXISTS source_revision,
    DROP COLUMN IF EXISTS source_repository,
    DROP COLUMN IF EXISTS source_provider,
    DROP COLUMN IF EXISTS origin_kind;
ALTER TABLE releases
    ALTER COLUMN build_id SET NOT NULL,
    ALTER COLUMN app_environment_id SET NOT NULL,
    ALTER COLUMN commit_sha SET NOT NULL,
    ALTER COLUMN platform SET NOT NULL;
DROP INDEX IF EXISTS operations_principal_idempotency;
DROP TRIGGER IF EXISTS operations_requested_by_principal ON operations;
DROP TRIGGER IF EXISTS deployments_requested_by_principal ON deployments;
DROP FUNCTION IF EXISTS populate_requested_by_principal;
ALTER TABLE operations ALTER COLUMN requested_by_user_id SET NOT NULL;
ALTER TABLE deployments ALTER COLUMN requested_by_user_id SET NOT NULL;
ALTER TABLE operations DROP COLUMN IF EXISTS requested_by_principal_id;
ALTER TABLE deployments DROP COLUMN IF EXISTS requested_by_principal_id;
DROP INDEX IF EXISTS audit_events_principal_cursor;
ALTER TABLE audit_events DROP COLUMN IF EXISTS actor_principal_id;
DROP TABLE IF EXISTS service_account_grants;
DROP TABLE IF EXISTS service_account_tokens;
DROP TABLE IF EXISTS service_accounts;
DROP TRIGGER IF EXISTS users_principal ON users;
DROP FUNCTION IF EXISTS synchronize_user_principal;
ALTER TABLE users DROP COLUMN IF EXISTS principal_id;
DROP TABLE IF EXISTS principals;
