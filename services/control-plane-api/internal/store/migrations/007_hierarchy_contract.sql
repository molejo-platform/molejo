-- +goose Up
ALTER TABLE deployments
    ALTER COLUMN project_id SET NOT NULL,
    ALTER COLUMN app_id SET NOT NULL,
    ALTER COLUMN environment_id SET NOT NULL;

ALTER TABLE deployments
    ADD CONSTRAINT deployments_project_workspace_fk
        FOREIGN KEY (project_id, workspace_id) REFERENCES projects(id, workspace_id),
    ADD CONSTRAINT deployments_app_project_fk
        FOREIGN KEY (app_id, project_id) REFERENCES apps(id, project_id),
    ADD CONSTRAINT deployments_environment_project_fk
        FOREIGN KEY (environment_id, project_id) REFERENCES environments(id, project_id);

CREATE UNIQUE INDEX deployments_app_environment_active
    ON deployments(app_id, environment_id)
    WHERE deleted_at IS NULL;
