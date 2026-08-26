-- +goose Up
INSERT INTO projects(public_id, workspace_id, name, name_key)
SELECT
    'prj-' || translate(substring(md5('project:' || w.public_id), 1, 20), '0189', 'abcd'),
    w.id,
    'Imported',
    'imported'
FROM workspaces w
WHERE EXISTS (SELECT 1 FROM deployments d WHERE d.workspace_id = w.id)
ON CONFLICT (public_id) DO NOTHING;

INSERT INTO environments(public_id, project_id, name, name_key)
SELECT
    'env-' || translate(substring(md5('environment:' || w.public_id), 1, 20), '0189', 'abcd'),
    p.id,
    'Imported',
    'imported'
FROM workspaces w
JOIN projects p
  ON p.public_id = 'prj-' || translate(substring(md5('project:' || w.public_id), 1, 20), '0189', 'abcd')
WHERE EXISTS (SELECT 1 FROM deployments d WHERE d.workspace_id = w.id)
ON CONFLICT (public_id) DO NOTHING;

INSERT INTO apps(public_id, project_id, name, name_key, created_at, updated_at, archived_at)
SELECT
    'app-' || translate(substring(md5('app:' || d.public_id), 1, 20), '0189', 'abcd'),
    p.id,
    d.name,
    lower(btrim(d.name)),
    d.created_at,
    d.updated_at,
    d.deleted_at
FROM deployments d
JOIN workspaces w ON w.id = d.workspace_id
JOIN projects p
  ON p.public_id = 'prj-' || translate(substring(md5('project:' || w.public_id), 1, 20), '0189', 'abcd')
ON CONFLICT (public_id) DO NOTHING;

UPDATE deployments d
SET project_id = p.id,
    app_id = a.id,
    environment_id = e.id
FROM workspaces w
JOIN projects p
  ON p.public_id = 'prj-' || translate(substring(md5('project:' || w.public_id), 1, 20), '0189', 'abcd')
JOIN environments e
  ON e.project_id = p.id
 AND e.public_id = 'env-' || translate(substring(md5('environment:' || w.public_id), 1, 20), '0189', 'abcd')
JOIN apps a ON a.project_id = p.id
WHERE d.workspace_id = w.id
  AND a.public_id = 'app-' || translate(substring(md5('app:' || d.public_id), 1, 20), '0189', 'abcd')
  AND (d.project_id IS NULL OR d.app_id IS NULL OR d.environment_id IS NULL);
