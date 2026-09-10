-- +goose Up
ALTER TABLE app_environments DROP CONSTRAINT app_environments_branch_valid;

-- +goose Down
UPDATE app_environments SET source_branch = 'main' WHERE source_branch = '';
ALTER TABLE app_environments
    ADD CONSTRAINT app_environments_branch_valid CHECK (char_length(source_branch) BETWEEN 1 AND 255);
