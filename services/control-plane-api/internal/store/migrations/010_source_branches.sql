-- +goose Up
ALTER TABLE app_github_sources ADD COLUMN primary_branch TEXT;

UPDATE app_github_sources SET primary_branch = default_branch;

ALTER TABLE app_github_sources
    ALTER COLUMN primary_branch SET NOT NULL,
    ADD CONSTRAINT app_github_sources_primary_branch_valid
        CHECK (char_length(primary_branch) BETWEEN 1 AND 255);

ALTER TABLE builds ADD COLUMN source_branch TEXT;

UPDATE builds b
SET source_branch = s.primary_branch
FROM app_github_sources s
WHERE s.app_id = b.app_id;

UPDATE builds SET source_branch = 'unknown' WHERE source_branch IS NULL;

ALTER TABLE builds
    ALTER COLUMN source_branch SET NOT NULL,
    ADD CONSTRAINT builds_source_branch_valid
        CHECK (char_length(source_branch) BETWEEN 1 AND 255);
