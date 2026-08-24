-- +goose Up
ALTER TABLE deployments ADD COLUMN IF NOT EXISTS runtime_name TEXT;
ALTER TABLE deployments ADD COLUMN IF NOT EXISTS deletion_requested_at TIMESTAMPTZ;

UPDATE deployments
SET runtime_name = 'ap-' || regexp_replace(public_id, '^dep-', '')
WHERE runtime_name IS NULL OR runtime_name = '';

ALTER TABLE deployments ALTER COLUMN runtime_name SET NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS deployments_workspace_runtime_name ON deployments(workspace_id, runtime_name);

-- +goose Down
DROP INDEX IF EXISTS deployments_workspace_runtime_name;
ALTER TABLE deployments DROP COLUMN IF EXISTS deletion_requested_at;
ALTER TABLE deployments DROP COLUMN IF EXISTS runtime_name;
