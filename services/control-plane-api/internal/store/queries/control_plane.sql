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
SELECT w.id, w.public_id, w.name, w.namespace_name
FROM workspaces AS w
JOIN workspace_actors AS wa ON wa.workspace_id = w.id
WHERE wa.actor_id = $1
ORDER BY w.id
LIMIT 1;

-- name: GetWorkspaceByID :one
SELECT id, public_id, name, namespace_name
FROM workspaces
WHERE id = $1;

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
