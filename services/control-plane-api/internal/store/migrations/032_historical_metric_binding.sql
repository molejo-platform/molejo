-- +goose Up
CREATE TABLE cluster_historical_metric_bindings (
    cluster_id BIGINT PRIMARY KEY REFERENCES agent_installations(id) ON DELETE CASCADE,
    provider_type TEXT NOT NULL CHECK (provider_type = 'PrometheusCompatible'),
    endpoint TEXT NOT NULL,
    health TEXT NOT NULL CHECK (health IN ('Unknown', 'Healthy', 'Degraded', 'Unavailable')),
    conformant BOOLEAN NOT NULL DEFAULT false,
    reason_code TEXT NOT NULL DEFAULT '',
    limitations_json JSONB NOT NULL DEFAULT '[]'::jsonb,
    observed_at TIMESTAMPTZ,
    version BIGINT NOT NULL DEFAULT 1 CHECK (version > 0),
    created_by BIGINT NOT NULL REFERENCES users(id),
    updated_by BIGINT NOT NULL REFERENCES users(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE cluster_historical_metric_bindings;
