-- name: GetActorByID :one
SELECT id, actor_key, role
FROM actors
WHERE id = $1;

-- name: UpsertActor :one
INSERT INTO actors (actor_key, role, password_hash)
VALUES ($1, $2, $3)
ON CONFLICT (actor_key) DO UPDATE
SET role = EXCLUDED.role,
    password_hash = EXCLUDED.password_hash
RETURNING id;

-- name: GetActorByKey :one
SELECT id, actor_key, role, password_hash
FROM actors
WHERE actor_key = $1;

-- name: UpsertWorkspace :one
INSERT INTO workspaces (public_id, name, namespace_name)
VALUES ($1, $2, $3)
ON CONFLICT (namespace_name) DO UPDATE
SET name = EXCLUDED.name
RETURNING id;

-- name: AddWorkspaceActor :exec
INSERT INTO workspace_actors (workspace_id, actor_id)
VALUES ($1, $2)
ON CONFLICT DO NOTHING;

-- name: GetWorkspaceForActor :one
SELECT w.id, w.public_id, w.name, w.namespace_name, w.version, w.bootstrap_state, w.created_at, w.updated_at
FROM workspaces AS w
JOIN workspace_actors AS wa ON wa.workspace_id = w.id
WHERE wa.actor_id = $1
ORDER BY w.id
LIMIT 1;

-- name: GetWorkspaceByID :one
SELECT id, public_id, name, namespace_name, version, bootstrap_state, created_at, updated_at
FROM workspaces
WHERE id = $1;

-- name: ListWorkspacesForActor :many
SELECT w.id, w.public_id, w.name, w.namespace_name, w.version, w.bootstrap_state, w.created_at, w.updated_at
FROM workspaces AS w
JOIN workspace_actors AS wa ON wa.workspace_id = w.id
WHERE wa.actor_id = $1 AND w.id < $2
ORDER BY w.id DESC
LIMIT $3;

-- name: FindWorkspaceForActor :one
SELECT w.id, w.public_id, w.name, w.namespace_name, w.version, w.bootstrap_state, w.created_at, w.updated_at
FROM workspaces AS w
JOIN workspace_actors AS wa ON wa.workspace_id = w.id
WHERE wa.actor_id = $1 AND w.public_id = $2;

-- name: InsertWorkspace :one
INSERT INTO workspaces(public_id, name, namespace_name)
VALUES ($1, $2, $3)
RETURNING id, public_id, name, namespace_name, version, bootstrap_state, created_at, updated_at;

-- name: UpdateWorkspaceName :one
UPDATE workspaces
SET name = $3, version = version + 1, updated_at = now()
WHERE id = $1 AND version = $2
RETURNING id, public_id, name, namespace_name, version, bootstrap_state, created_at, updated_at;

-- name: FindWorkspaceOperationByIdempotency :one
SELECT o.id, o.public_id, o.workspace_id, o.actor_id, o.kind, o.status,
       o.desired_version, o.attempts, o.created_at, o.updated_at,
       o.error_code, o.error_message, o.payload_hash
FROM operations o
WHERE o.actor_id = $1
  AND o.kind = 'EnsureWorkspace'
  AND o.app_environment_id IS NULL
  AND o.deployment_id IS NULL
  AND o.idempotency_hash = $2;

-- name: InsertWorkspaceOperation :one
INSERT INTO operations(
    public_id, workspace_id, app_environment_id, deployment_id, actor_id, kind, status,
    idempotency_hash, payload_hash, desired_version
)
VALUES ($1, $2, NULL, NULL, $3, 'EnsureWorkspace', 'Pending', $4, $5, 1)
RETURNING id, public_id, workspace_id, actor_id, kind, status,
          desired_version, attempts, created_at, updated_at, error_code, error_message;

-- name: CreateProject :one
INSERT INTO projects(public_id, workspace_id, name, name_key)
VALUES ($1, $2, $3, $4)
RETURNING id, public_id, workspace_id, name, version, created_at, updated_at, archived_at;

-- name: FindProject :one
SELECT id, public_id, workspace_id, name, version, created_at, updated_at, archived_at
FROM projects
WHERE workspace_id = $1 AND public_id = $2;

-- name: FindActiveProjectForUpdate :one
SELECT id, public_id, workspace_id, name, version, created_at, updated_at, archived_at
FROM projects
WHERE workspace_id = $1 AND public_id = $2 AND archived_at IS NULL
FOR UPDATE;

-- name: ListProjects :many
SELECT id, public_id, workspace_id, name, version, created_at, updated_at, archived_at
FROM projects
WHERE workspace_id = $1
  AND id < $2
  AND ($4::boolean OR archived_at IS NULL)
ORDER BY id DESC
LIMIT $3;

-- name: UpdateProject :one
UPDATE projects
SET name = $4, name_key = $5, version = version + 1, updated_at = now()
WHERE workspace_id = $1 AND public_id = $2 AND version = $3 AND archived_at IS NULL
RETURNING id, public_id, workspace_id, name, version, created_at, updated_at, archived_at;

-- name: ArchiveProject :one
UPDATE projects p
SET archived_at = now(), version = version + 1, updated_at = now()
WHERE p.workspace_id = $1 AND p.public_id = $2 AND p.version = $3 AND p.archived_at IS NULL
  AND NOT EXISTS (SELECT 1 FROM environments e WHERE e.project_id = p.id AND e.archived_at IS NULL)
  AND NOT EXISTS (SELECT 1 FROM apps a WHERE a.project_id = p.id AND a.archived_at IS NULL)
RETURNING p.id, p.public_id, p.workspace_id, p.name, p.version, p.created_at, p.updated_at, p.archived_at;

-- name: CreateEnvironment :one
INSERT INTO environments(public_id, project_id, name, name_key)
VALUES ($1, $2, $3, $4)
RETURNING id, public_id, project_id, name, version, created_at, updated_at, archived_at;

-- name: FindEnvironment :one
SELECT e.id, e.public_id, e.project_id, e.name, e.version, e.created_at, e.updated_at, e.archived_at
FROM environments e
JOIN projects p ON p.id = e.project_id
WHERE p.workspace_id = $1 AND p.public_id = $2 AND e.public_id = $3;

-- name: FindActiveEnvironmentForUpdate :one
SELECT e.id, e.public_id, e.project_id, e.name, e.version, e.created_at, e.updated_at, e.archived_at
FROM environments e
JOIN projects p ON p.id = e.project_id
WHERE p.workspace_id = $1 AND p.public_id = $2 AND e.public_id = $3 AND e.archived_at IS NULL
FOR UPDATE OF e;

-- name: ListEnvironments :many
SELECT e.id, e.public_id, e.project_id, e.name, e.version, e.created_at, e.updated_at, e.archived_at
FROM environments e
JOIN projects p ON p.id = e.project_id
WHERE p.workspace_id = $1 AND p.public_id = $2
  AND e.id < $3
  AND ($5::boolean OR e.archived_at IS NULL)
ORDER BY e.id DESC
LIMIT $4;

-- name: UpdateEnvironment :one
UPDATE environments e
SET name = $5, name_key = $6, version = e.version + 1, updated_at = now()
FROM projects p
WHERE e.project_id = p.id AND p.workspace_id = $1 AND p.public_id = $2
  AND e.public_id = $3 AND e.version = $4 AND e.archived_at IS NULL
RETURNING e.id, e.public_id, e.project_id, e.name, e.version, e.created_at, e.updated_at, e.archived_at;

-- name: ArchiveEnvironment :one
UPDATE environments e
SET archived_at = now(), version = e.version + 1, updated_at = now()
FROM projects p
WHERE e.project_id = p.id AND p.workspace_id = $1 AND p.public_id = $2
  AND e.public_id = $3 AND e.version = $4 AND e.archived_at IS NULL
  AND NOT EXISTS (SELECT 1 FROM app_environments ae WHERE ae.environment_id = e.id AND ae.archived_at IS NULL)
RETURNING e.id, e.public_id, e.project_id, e.name, e.version, e.created_at, e.updated_at, e.archived_at;

-- name: CreateApp :one
INSERT INTO apps(public_id, project_id, name, name_key)
VALUES ($1, $2, $3, $4)
RETURNING id, public_id, project_id, name, version, created_at, updated_at, archived_at;

-- name: FindApp :one
SELECT a.id, a.public_id, a.project_id, a.name, a.version, a.created_at, a.updated_at, a.archived_at
FROM apps a
JOIN projects p ON p.id = a.project_id
WHERE p.workspace_id = $1 AND p.public_id = $2 AND a.public_id = $3;

-- name: FindActiveAppForUpdate :one
SELECT a.id, a.public_id, a.project_id, a.name, a.version, a.created_at, a.updated_at, a.archived_at
FROM apps a
JOIN projects p ON p.id = a.project_id
WHERE p.workspace_id = $1 AND p.public_id = $2 AND a.public_id = $3 AND a.archived_at IS NULL
FOR UPDATE OF a;

-- name: ListApps :many
SELECT a.id, a.public_id, a.project_id, a.name, a.version, a.created_at, a.updated_at, a.archived_at
FROM apps a
JOIN projects p ON p.id = a.project_id
WHERE p.workspace_id = $1 AND p.public_id = $2
  AND a.id < $3
  AND ($5::boolean OR a.archived_at IS NULL)
ORDER BY a.id DESC
LIMIT $4;

-- name: UpdateApp :one
UPDATE apps a
SET name = $5, name_key = $6, version = a.version + 1, updated_at = now()
FROM projects p
WHERE a.project_id = p.id AND p.workspace_id = $1 AND p.public_id = $2
  AND a.public_id = $3 AND a.version = $4 AND a.archived_at IS NULL
RETURNING a.id, a.public_id, a.project_id, a.name, a.version, a.created_at, a.updated_at, a.archived_at;

-- name: ArchiveApp :one
UPDATE apps a
SET archived_at = now(), version = a.version + 1, updated_at = now()
FROM projects p
WHERE a.project_id = p.id AND p.workspace_id = $1 AND p.public_id = $2
  AND a.public_id = $3 AND a.version = $4 AND a.archived_at IS NULL
  AND NOT EXISTS (SELECT 1 FROM app_environments ae WHERE ae.app_id = a.id AND ae.archived_at IS NULL)
RETURNING a.id, a.public_id, a.project_id, a.name, a.version, a.created_at, a.updated_at, a.archived_at;

-- name: CreateSession :exec
INSERT INTO sessions (token_hash, actor_id, csrf_hash, expires_at)
VALUES ($1, $2, $3, $4);

-- name: GetActiveSession :one
SELECT actor_id, csrf_hash
FROM sessions
WHERE token_hash = $1
  AND revoked_at IS NULL
  AND expires_at > now();

-- name: RevokeSession :exec
UPDATE sessions
SET revoked_at = now()
WHERE token_hash = $1;

-- name: RevokeActiveSession :execrows
UPDATE sessions
SET revoked_at = now()
WHERE token_hash = $1
  AND actor_id = $2
  AND revoked_at IS NULL
  AND expires_at > now();
