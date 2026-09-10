-- +goose Up
ALTER TABLE storage_profiles
    DROP CONSTRAINT storage_profiles_binding_valid,
    ALTER COLUMN runtime_binding SET DEFAULT '';

COMMENT ON COLUMN storage_profiles.runtime_binding IS
    'Deprecated installation-wide binding retained for alpha schema compatibility; runtime selection uses cluster_storage_bindings.';

-- +goose Down
UPDATE storage_profiles SET enabled = false WHERE runtime_binding = '';
ALTER TABLE storage_profiles
    ALTER COLUMN runtime_binding DROP DEFAULT,
    ADD CONSTRAINT storage_profiles_binding_valid CHECK ((enabled AND runtime_binding <> '') OR (NOT enabled));
