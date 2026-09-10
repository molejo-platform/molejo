-- +goose Up
CREATE TABLE cluster_capability_observations (
    cluster_id BIGINT NOT NULL REFERENCES agent_installations(id) ON DELETE CASCADE,
    capability_id TEXT NOT NULL,
    contract_version TEXT NOT NULL,
    support TEXT NOT NULL CHECK (support IN ('Supported', 'Unsupported', 'Unknown')),
    health TEXT NOT NULL CHECK (health IN ('Healthy', 'Degraded', 'Unavailable', 'Unknown')),
    provider_kind TEXT NOT NULL DEFAULT '',
    reason_code TEXT NOT NULL DEFAULT '',
    sanitized_message TEXT NOT NULL DEFAULT '',
    limitations_json JSONB NOT NULL DEFAULT '[]'::jsonb CHECK (jsonb_typeof(limitations_json) = 'array'),
    sampled_at TIMESTAMPTZ NOT NULL,
    received_at TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    observed_session_id TEXT NOT NULL,
    snapshot_sequence BIGINT NOT NULL CHECK (snapshot_sequence > 0),
    PRIMARY KEY (cluster_id, capability_id, contract_version),
    CHECK (char_length(capability_id) BETWEEN 1 AND 128),
    CHECK (char_length(contract_version) BETWEEN 1 AND 32),
    CHECK (char_length(provider_kind) <= 128),
    CHECK (char_length(reason_code) <= 128),
    CHECK (char_length(sanitized_message) <= 256),
    CHECK (expires_at > received_at)
);

CREATE INDEX cluster_capability_observations_expiry
    ON cluster_capability_observations(cluster_id, expires_at);

-- +goose Down
DROP TABLE IF EXISTS cluster_capability_observations;
