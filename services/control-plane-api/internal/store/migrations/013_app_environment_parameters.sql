-- +goose Up
CREATE TABLE app_environment_parameter_bindings (
    app_environment_id BIGINT NOT NULL REFERENCES app_environments(id) ON DELETE CASCADE,
    environment_name VARCHAR(253) NOT NULL,
    parameter_id BIGINT NOT NULL,
    parameter_version BIGINT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (app_environment_id, environment_name),
    FOREIGN KEY (parameter_id, parameter_version) REFERENCES parameter_versions(parameter_id, version),
    CONSTRAINT app_environment_parameter_binding_name_valid CHECK (environment_name ~ '^[A-Za-z_][A-Za-z0-9_]*$')
);

CREATE INDEX app_environment_parameter_bindings_parameter_version_idx
    ON app_environment_parameter_bindings (parameter_id, parameter_version);
