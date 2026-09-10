-- +goose Up
CREATE TABLE cluster_storage_bindings (
    cluster_id BIGINT NOT NULL REFERENCES agent_installations(id) ON DELETE CASCADE,
    storage_profile_id TEXT NOT NULL REFERENCES storage_profiles(id) ON DELETE CASCADE,
    storage_class_name TEXT NOT NULL,
    provisioner TEXT NOT NULL DEFAULT '',
    access_modes_json JSONB NOT NULL DEFAULT '[]'::jsonb CHECK (jsonb_typeof(access_modes_json) = 'array'),
    allow_expansion BOOLEAN NOT NULL DEFAULT false,
    volume_binding_mode TEXT NOT NULL DEFAULT '',
    health TEXT NOT NULL DEFAULT 'Unknown' CHECK (health IN ('Unknown', 'Healthy', 'Degraded', 'Unavailable')),
    reason_code TEXT NOT NULL DEFAULT 'binding_observation_pending',
    observed_at TIMESTAMPTZ,
    expires_at TIMESTAMPTZ,
    observed_session_id TEXT NOT NULL DEFAULT '',
    observed_sequence BIGINT NOT NULL DEFAULT 0 CHECK (observed_sequence >= 0),
    version BIGINT NOT NULL DEFAULT 1 CHECK (version > 0),
    created_by BIGINT NOT NULL REFERENCES users(id),
    updated_by BIGINT NOT NULL REFERENCES users(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (cluster_id, storage_profile_id),
    CHECK (char_length(storage_class_name) BETWEEN 1 AND 253),
    CHECK (char_length(provisioner) <= 253),
    CHECK (char_length(volume_binding_mode) <= 64),
    CHECK (char_length(reason_code) <= 128)
);

CREATE TABLE cluster_publication_bindings (
    cluster_id BIGINT PRIMARY KEY REFERENCES agent_installations(id) ON DELETE CASCADE,
    gateway_namespace TEXT NOT NULL,
    gateway_name TEXT NOT NULL,
    section_name TEXT NOT NULL,
    gateway_class_name TEXT NOT NULL DEFAULT '',
    gateway_class_accepted BOOLEAN NOT NULL DEFAULT false,
    gateway_programmed BOOLEAN NOT NULL DEFAULT false,
    listener_ready BOOLEAN NOT NULL DEFAULT false,
    supported_route_kinds_json JSONB NOT NULL DEFAULT '[]'::jsonb CHECK (jsonb_typeof(supported_route_kinds_json) = 'array'),
    health TEXT NOT NULL DEFAULT 'Unknown' CHECK (health IN ('Unknown', 'Healthy', 'Degraded', 'Unavailable')),
    reason_code TEXT NOT NULL DEFAULT 'binding_observation_pending',
    observed_at TIMESTAMPTZ,
    expires_at TIMESTAMPTZ,
    observed_session_id TEXT NOT NULL DEFAULT '',
    observed_sequence BIGINT NOT NULL DEFAULT 0 CHECK (observed_sequence >= 0),
    version BIGINT NOT NULL DEFAULT 1 CHECK (version > 0),
    created_by BIGINT NOT NULL REFERENCES users(id),
    updated_by BIGINT NOT NULL REFERENCES users(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (char_length(gateway_namespace) BETWEEN 1 AND 63),
    CHECK (char_length(gateway_name) BETWEEN 1 AND 253),
    CHECK (char_length(section_name) BETWEEN 1 AND 63),
    CHECK (char_length(gateway_class_name) <= 253),
    CHECK (char_length(reason_code) <= 128)
);

ALTER TABLE app_volumes
    ADD COLUMN runtime_storage_class_name TEXT,
    ADD COLUMN storage_binding_version BIGINT;

UPDATE app_volumes av
SET runtime_storage_class_name = sp.runtime_binding,
    storage_binding_version = 0
FROM storage_profiles sp
WHERE sp.id = av.storage_profile_id;

ALTER TABLE app_volumes
    ALTER COLUMN runtime_storage_class_name SET NOT NULL,
    ALTER COLUMN storage_binding_version SET NOT NULL,
    ADD CONSTRAINT app_volumes_storage_binding_version_valid CHECK (storage_binding_version >= 0);

-- +goose Down
ALTER TABLE app_volumes
    DROP CONSTRAINT app_volumes_storage_binding_version_valid,
    DROP COLUMN storage_binding_version,
    DROP COLUMN runtime_storage_class_name;
DROP TABLE cluster_publication_bindings;
DROP TABLE cluster_storage_bindings;
