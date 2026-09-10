-- +goose Up
CREATE TABLE app_environment_configuration_revisions (
    app_environment_id BIGINT NOT NULL REFERENCES app_environments(id) ON DELETE CASCADE,
    version BIGINT NOT NULL,
    configuration_json JSONB NOT NULL,
    created_by_actor_id BIGINT REFERENCES actors(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (app_environment_id, version),
    CONSTRAINT app_environment_configuration_revision_version_valid CHECK (version > 0)
);

INSERT INTO app_environment_configuration_revisions(
    app_environment_id, version, configuration_json, created_by_actor_id, created_at
)
SELECT ae.id, ae.configuration_version, ae.configuration_json,
       (SELECT wa.actor_id FROM workspace_actors wa WHERE wa.workspace_id=ae.workspace_id ORDER BY wa.actor_id LIMIT 1),
       ae.updated_at
FROM app_environments ae;

CREATE INDEX app_environment_configuration_revisions_created_at
    ON app_environment_configuration_revisions(app_environment_id, created_at DESC);

-- +goose Down
DROP TABLE IF EXISTS app_environment_configuration_revisions;
