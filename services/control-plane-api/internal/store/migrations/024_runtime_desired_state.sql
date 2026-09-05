-- +goose Up
ALTER TABLE app_environments
    ADD COLUMN runtime_desired_version BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN runtime_spec_hash TEXT,
    ADD CONSTRAINT app_environments_runtime_desired_state_valid CHECK (
        (runtime_desired_version = 0 AND runtime_spec_hash IS NULL)
        OR (runtime_desired_version > 0 AND runtime_spec_hash ~ '^[0-9a-f]{64}$')
    );

ALTER TABLE app_volumes
    ADD COLUMN runtime_desired_version BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN runtime_spec_hash TEXT,
    ADD COLUMN runtime_reconciliation_generation BIGINT NOT NULL DEFAULT 0,
    ADD CONSTRAINT app_volumes_runtime_desired_state_valid CHECK (
        (runtime_desired_version = 0 AND runtime_spec_hash IS NULL)
        OR (runtime_desired_version > 0 AND runtime_spec_hash ~ '^[0-9a-f]{64}$')
    ),
    ADD CONSTRAINT app_volumes_runtime_reconciliation_generation_valid CHECK (runtime_reconciliation_generation >= 0);

-- +goose Down
ALTER TABLE app_volumes
    DROP CONSTRAINT IF EXISTS app_volumes_runtime_desired_state_valid,
    DROP CONSTRAINT IF EXISTS app_volumes_runtime_reconciliation_generation_valid,
    DROP COLUMN IF EXISTS runtime_reconciliation_generation,
    DROP COLUMN IF EXISTS runtime_spec_hash,
    DROP COLUMN IF EXISTS runtime_desired_version;

ALTER TABLE app_environments
    DROP CONSTRAINT IF EXISTS app_environments_runtime_desired_state_valid,
    DROP COLUMN IF EXISTS runtime_spec_hash,
    DROP COLUMN IF EXISTS runtime_desired_version;
