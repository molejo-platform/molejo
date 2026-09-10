-- +goose Up
ALTER TABLE releases
    ADD COLUMN provenance_status TEXT NOT NULL DEFAULT 'Declared',
    ADD CONSTRAINT releases_provenance_status_valid CHECK (provenance_status IN ('Declared'));

ALTER TABLE releases DROP CONSTRAINT releases_idempotency_valid;
ALTER TABLE releases ADD CONSTRAINT releases_idempotency_valid CHECK (
    (idempotency_hash IS NULL AND payload_hash IS NULL)
    OR (
        idempotency_hash IS NOT NULL
        AND payload_hash IS NOT NULL
        AND octet_length(idempotency_hash)=32
        AND octet_length(payload_hash)=32
    )
);

-- Keep managed-build writers from the previous release compatible while the
-- control-plane replicas are replaced during a rolling upgrade.
-- +goose StatementBegin
CREATE FUNCTION populate_release_principal() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.created_by_principal_id IS NULL AND NEW.build_id IS NOT NULL THEN
        SELECT u.principal_id INTO NEW.created_by_principal_id
        FROM builds b JOIN users u ON u.id=b.requested_by_user_id
        WHERE b.id=NEW.build_id;
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER releases_created_by_principal
    BEFORE INSERT ON releases FOR EACH ROW EXECUTE FUNCTION populate_release_principal();

-- +goose Down
DROP TRIGGER IF EXISTS releases_created_by_principal ON releases;
DROP FUNCTION IF EXISTS populate_release_principal;
ALTER TABLE releases DROP CONSTRAINT releases_idempotency_valid;
ALTER TABLE releases ADD CONSTRAINT releases_idempotency_valid CHECK (
    (idempotency_hash IS NULL AND payload_hash IS NULL)
    OR (octet_length(idempotency_hash)=32 AND octet_length(payload_hash)=32)
);
ALTER TABLE releases
    DROP CONSTRAINT releases_provenance_status_valid,
    DROP COLUMN provenance_status;
