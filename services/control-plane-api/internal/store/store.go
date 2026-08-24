package store

import (
	"bytes"
	"context"
	"crypto/sha256"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/fruto-platform/fruto/services/control-plane-api/internal/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

type Store struct{ Pool *pgxpool.Pool }

type migration struct {
	version  int64
	contents []byte
	checksum [sha256.Size]byte
}

func New(ctx context.Context, dsn string) (*Store, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return &Store{Pool: pool}, nil
}

func (s *Store) Close() { s.Pool.Close() }

func embeddedMigrations() ([]migration, error) {
	entries, err := fs.ReadDir(migrationFS, "migrations")
	if err != nil {
		return nil, err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	migrations := make([]migration, 0, len(entries))
	seenVersions := make(map[int64]string)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		version, err := strconv.ParseInt(strings.SplitN(entry.Name(), "_", 2)[0], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid migration name %q: %w", entry.Name(), err)
		}
		if previous, exists := seenVersions[version]; exists {
			return nil, fmt.Errorf("duplicate migration version %d in %q and %q", version, previous, entry.Name())
		}
		seenVersions[version] = entry.Name()
		contents, err := migrationFS.ReadFile("migrations/" + entry.Name())
		if err != nil {
			return nil, err
		}
		if parts := strings.SplitN(string(contents), "-- +goose Down", 2); len(parts) == 2 {
			contents = []byte(parts[0])
		}
		migrations = append(migrations, migration{version: version, contents: contents, checksum: sha256.Sum256(contents)})
	}
	if len(migrations) == 0 {
		return nil, errors.New("no migrations found")
	}
	return migrations, nil
}

func (s *Store) Migrate(ctx context.Context) error {
	migrations, err := embeddedMigrations()
	if err != nil {
		return err
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('fruto-control-plane-migrations'))`); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (version BIGINT PRIMARY KEY, checksum BYTEA, applied_at TIMESTAMPTZ NOT NULL DEFAULT now())`); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `ALTER TABLE schema_migrations ADD COLUMN IF NOT EXISTS checksum BYTEA`); err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	for _, migration := range migrations {
		tx, err := s.Pool.Begin(ctx)
		if err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('fruto-control-plane-migrations'))`); err != nil {
			_ = tx.Rollback(ctx)
			return err
		}
		var stored []byte
		err = tx.QueryRow(ctx, `SELECT checksum FROM schema_migrations WHERE version=$1`, migration.version).Scan(&stored)
		switch {
		case err == nil && len(stored) == 0:
			// Databases created by the original MVP did not record checksums.
			// Backfill the checksum once so subsequent runs detect drift.
			_, err = tx.Exec(ctx, `UPDATE schema_migrations SET checksum=$1 WHERE version=$2`, migration.checksum[:], migration.version)
		case err == nil && !bytes.Equal(stored, migration.checksum[:]):
			err = fmt.Errorf("migration %d checksum drift", migration.version)
		case errors.Is(err, pgx.ErrNoRows):
			if _, err = tx.Exec(ctx, string(migration.contents)); err == nil {
				_, err = tx.Exec(ctx, `INSERT INTO schema_migrations(version,checksum) VALUES ($1,$2)`, migration.version, migration.checksum[:])
			}
		}
		if err != nil {
			_ = tx.Rollback(ctx)
			return err
		}
		if err = tx.Commit(ctx); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) Bootstrap(ctx context.Context, workspace domain.Workspace, actors map[string]struct{ Role, PasswordHash string }) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	for key, actor := range actors {
		_, err = tx.Exec(ctx, `INSERT INTO actors(actor_key, role, password_hash) VALUES ($1,$2,$3) ON CONFLICT(actor_key) DO UPDATE SET role=EXCLUDED.role, password_hash=EXCLUDED.password_hash`, key, actor.Role, actor.PasswordHash)
		if err != nil {
			return err
		}
	}
	var workspaceID int64
	err = tx.QueryRow(ctx, `INSERT INTO workspaces(public_id,name,namespace_name) VALUES ($1,$2,$3) ON CONFLICT(namespace_name) DO UPDATE SET name=EXCLUDED.name RETURNING id`, workspace.PublicID, workspace.Name, workspace.Namespace).Scan(&workspaceID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			err = tx.QueryRow(ctx, `SELECT id FROM workspaces WHERE namespace_name=$1`, workspace.Namespace).Scan(&workspaceID)
		}
		if err != nil {
			return err
		}
	}
	for key := range actors {
		if _, err = tx.Exec(ctx, `INSERT INTO workspace_actors(workspace_id,actor_id) SELECT $1,id FROM actors WHERE actor_key=$2 ON CONFLICT DO NOTHING`, workspaceID, key); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (s *Store) Authenticate(ctx context.Context, key string) (domain.Actor, string, error) {
	var actor domain.Actor
	var hash string
	err := s.Pool.QueryRow(ctx, `SELECT id,actor_key,role,password_hash FROM actors WHERE actor_key=$1`, key).Scan(&actor.ID, &actor.Key, &actor.Role, &hash)
	return actor, hash, err
}

func (s *Store) WorkspaceForActor(ctx context.Context, actorID int64) (domain.Workspace, error) {
	var workspace domain.Workspace
	err := s.Pool.QueryRow(ctx, `SELECT w.id,w.public_id,w.name,w.namespace_name FROM workspaces w JOIN workspace_actors wa ON wa.workspace_id=w.id WHERE wa.actor_id=$1 ORDER BY w.id LIMIT 1`, actorID).Scan(&workspace.ID, &workspace.PublicID, &workspace.Name, &workspace.Namespace)
	return workspace, err
}

func (s *Store) Workspace(ctx context.Context, workspaceID int64) (domain.Workspace, error) {
	var workspace domain.Workspace
	err := s.Pool.QueryRow(ctx, `SELECT id,public_id,name,namespace_name FROM workspaces WHERE id=$1`, workspaceID).Scan(&workspace.ID, &workspace.PublicID, &workspace.Name, &workspace.Namespace)
	return workspace, err
}

func (s *Store) SchemaReady(ctx context.Context) error {
	migrations, err := embeddedMigrations()
	if err != nil {
		return err
	}
	rows, err := s.Pool.Query(ctx, `SELECT version,checksum FROM schema_migrations`)
	if err != nil {
		return err
	}
	defer rows.Close()
	applied := make(map[int64][]byte, len(migrations))
	for rows.Next() {
		var version int64
		var checksum []byte
		if err = rows.Scan(&version, &checksum); err != nil {
			return err
		}
		applied[version] = checksum
	}
	if err = rows.Err(); err != nil {
		return err
	}
	for _, migration := range migrations {
		checksum, ok := applied[migration.version]
		if !ok {
			return fmt.Errorf("schema migration %d is not applied", migration.version)
		}
		if !bytes.Equal(checksum, migration.checksum[:]) {
			return fmt.Errorf("schema migration %d checksum drift", migration.version)
		}
		delete(applied, migration.version)
	}
	if len(applied) != 0 {
		versions := make([]int64, 0, len(applied))
		for version := range applied {
			versions = append(versions, version)
		}
		sort.Slice(versions, func(i, j int) bool { return versions[i] < versions[j] })
		return fmt.Errorf("unknown schema migration %d is applied", versions[0])
	}
	return nil
}

func (s *Store) CreateSession(ctx context.Context, actorID int64, tokenHash, csrfHash []byte, expires time.Time) error {
	_, err := s.Pool.Exec(ctx, `INSERT INTO sessions(token_hash,actor_id,csrf_hash,expires_at) VALUES ($1,$2,$3,$4)`, tokenHash, actorID, csrfHash, expires)
	return err
}

func (s *Store) Session(ctx context.Context, tokenHash []byte) (int64, []byte, error) {
	var actorID int64
	var csrf []byte
	err := s.Pool.QueryRow(ctx, `SELECT actor_id,csrf_hash FROM sessions WHERE token_hash=$1 AND revoked_at IS NULL AND expires_at > now()`, tokenHash).Scan(&actorID, &csrf)
	return actorID, csrf, err
}

func (s *Store) RevokeSession(ctx context.Context, tokenHash []byte) error {
	_, err := s.Pool.Exec(ctx, `UPDATE sessions SET revoked_at=now() WHERE token_hash=$1`, tokenHash)
	return err
}

func (s *Store) RotateSession(ctx context.Context, actorID int64, oldTokenHash, newTokenHash, csrfHash []byte, expires time.Time) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	result, err := tx.Exec(ctx, `UPDATE sessions SET revoked_at=now() WHERE token_hash=$1 AND actor_id=$2 AND revoked_at IS NULL AND expires_at > now()`, oldTokenHash, actorID)
	if err != nil {
		return err
	}
	if result.RowsAffected() != 1 {
		return ErrSessionInvalid
	}
	if _, err = tx.Exec(ctx, `INSERT INTO sessions(token_hash,actor_id,csrf_hash,expires_at) VALUES ($1,$2,$3,$4)`, newTokenHash, actorID, csrfHash, expires); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) CreateDeployment(ctx context.Context, workspaceID, actorID int64, deploymentID string, intent domain.Intent, idempotencyHash, payloadHash []byte) (domain.Deployment, domain.Operation, bool, error) {
	for attempt := 0; attempt < 3; attempt++ {
		deployment, operation, reused, err := s.createDeployment(ctx, workspaceID, actorID, deploymentID, intent, idempotencyHash, payloadHash)
		if !errors.Is(err, errRetryCreate) {
			return deployment, operation, reused, err
		}
	}
	return domain.Deployment{}, domain.Operation{}, false, ErrConflict
}

var errRetryCreate = errors.New("retry concurrent create")

func (s *Store) createDeployment(ctx context.Context, workspaceID, actorID int64, deploymentID string, intent domain.Intent, idempotencyHash, payloadHash []byte) (domain.Deployment, domain.Operation, bool, error) {
	intentJSON, err := domain.CanonicalJSON(intent)
	if err != nil {
		return domain.Deployment{}, domain.Operation{}, false, err
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return domain.Deployment{}, domain.Operation{}, false, err
	}
	defer tx.Rollback(ctx)
	var existing domain.Operation
	var existingIntent []byte
	err = tx.QueryRow(ctx, `SELECT o.id,o.public_id,o.deployment_id,o.actor_id,o.kind,o.status,o.desired_version,o.attempts,o.intent_json::text,o.created_at,o.updated_at FROM operations o WHERE o.workspace_id=$1 AND o.kind='CreateDeployment' AND o.idempotency_hash=$2`, workspaceID, idempotencyHash).Scan(&existing.ID, &existing.PublicID, &existing.DeploymentID, &existing.ActorID, &existing.Kind, &existing.Status, &existing.DesiredVersion, &existing.Attempts, &existingIntent, &existing.CreatedAt, &existing.UpdatedAt)
	if err == nil {
		var storedPayload []byte
		if err = tx.QueryRow(ctx, `SELECT payload_hash FROM operations WHERE id=$1`, existing.ID).Scan(&storedPayload); err != nil {
			return domain.Deployment{}, domain.Operation{}, false, err
		}
		if string(storedPayload) != string(payloadHash) {
			return domain.Deployment{}, domain.Operation{}, false, ErrConflict
		}
		dep, depErr := deploymentByID(ctx, tx, existing.DeploymentID)
		if depErr != nil {
			return domain.Deployment{}, domain.Operation{}, false, depErr
		}
		existing.DeploymentPublicID = dep.PublicID
		if err = tx.Commit(ctx); err != nil {
			return domain.Deployment{}, domain.Operation{}, false, err
		}
		return dep, existing, true, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return domain.Deployment{}, domain.Operation{}, false, err
	}
	var deployment domain.Deployment
	runtimeName := domain.RuntimeName(deploymentID)
	err = tx.QueryRow(ctx, `INSERT INTO deployments(public_id,workspace_id,name,runtime_name,slug,intent_json) VALUES ($1,$2,$3,$4,NULLIF($5,''),$6) RETURNING id,public_id,workspace_id,runtime_name,intent_json::text,desired_version,observed_version,observed_release,last_state,last_message,created_at,updated_at`, deploymentID, workspaceID, intent.Name, runtimeName, intent.Slug, intentJSON).Scan(&deployment.ID, &deployment.PublicID, &deployment.WorkspaceID, &deployment.RuntimeName, &intentJSON, &deployment.DesiredVersion, &deployment.ObservedVersion, &deployment.ObservedRelease, &deployment.State, &deployment.Message, &deployment.CreatedAt, &deployment.UpdatedAt)
	if err != nil {
		if isUniqueViolation(err) {
			return domain.Deployment{}, domain.Operation{}, false, errRetryCreate
		}
		return domain.Deployment{}, domain.Operation{}, false, translateDBError(err)
	}
	if err := jsonUnmarshal(intentJSON, &deployment.Intent); err != nil {
		return domain.Deployment{}, domain.Operation{}, false, err
	}
	op, err := insertOperation(ctx, tx, workspaceID, deployment.ID, deployment.PublicID, actorID, "CreateDeployment", idempotencyHash, payloadHash, intentJSON, deployment.DesiredVersion)
	if err != nil {
		if isUniqueViolation(err) {
			return domain.Deployment{}, domain.Operation{}, false, errRetryCreate
		}
		return domain.Deployment{}, domain.Operation{}, false, translateDBError(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.Deployment{}, domain.Operation{}, false, err
	}
	return deployment, op, false, nil
}

func (s *Store) UpdateDeployment(ctx context.Context, workspaceID, actorID, deploymentID int64, intent domain.Intent, version int64, idem, payload []byte) (domain.Deployment, domain.Operation, error) {
	intentJSON, err := domain.CanonicalJSON(intent)
	if err != nil {
		return domain.Deployment{}, domain.Operation{}, err
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return domain.Deployment{}, domain.Operation{}, err
	}
	defer tx.Rollback(ctx)
	if existing, found, err := existingOperation(ctx, tx, workspaceID, deploymentID, "UpdateDeployment", idem, payload); err != nil {
		return domain.Deployment{}, domain.Operation{}, err
	} else if found {
		dep, depErr := deploymentByID(ctx, tx, deploymentID)
		if depErr != nil {
			return domain.Deployment{}, domain.Operation{}, depErr
		}
		existing.DeploymentPublicID = dep.PublicID
		if err = tx.Commit(ctx); err != nil {
			return domain.Deployment{}, domain.Operation{}, err
		}
		return dep, existing, nil
	}
	current, err := deploymentByID(ctx, tx, deploymentID)
	if err != nil {
		return domain.Deployment{}, domain.Operation{}, err
	}
	if current.WorkspaceID != workspaceID || current.DeletionRequestedAt != nil {
		return domain.Deployment{}, domain.Operation{}, ErrConflict
	}
	if intent.Name != current.Intent.Name {
		return domain.Deployment{}, domain.Operation{}, ErrImmutableName
	}
	var running bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM operations WHERE deployment_id=$1 AND status='Running')`, deploymentID).Scan(&running); err != nil {
		return domain.Deployment{}, domain.Operation{}, err
	}
	if running {
		return domain.Deployment{}, domain.Operation{}, ErrConflict
	}
	var deployment domain.Deployment
	var storedJSON []byte
	err = tx.QueryRow(ctx, `UPDATE deployments SET slug=NULLIF($1,''),intent_json=$2,desired_version=desired_version+1,updated_at=now() WHERE id=$3 AND workspace_id=$4 AND deleted_at IS NULL AND deletion_requested_at IS NULL AND desired_version=$5 RETURNING id,public_id,workspace_id,runtime_name,intent_json::text,desired_version,observed_version,observed_release,last_state,last_message,created_at,updated_at`, intent.Slug, intentJSON, deploymentID, workspaceID, version).Scan(&deployment.ID, &deployment.PublicID, &deployment.WorkspaceID, &deployment.RuntimeName, &storedJSON, &deployment.DesiredVersion, &deployment.ObservedVersion, &deployment.ObservedRelease, &deployment.State, &deployment.Message, &deployment.CreatedAt, &deployment.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.Deployment{}, domain.Operation{}, ErrConflict
		}
		return domain.Deployment{}, domain.Operation{}, translateDBError(err)
	}
	if err = jsonUnmarshal(storedJSON, &deployment.Intent); err != nil {
		return domain.Deployment{}, domain.Operation{}, err
	}
	if _, err = tx.Exec(ctx, `UPDATE operations SET status='Superseded',updated_at=now() WHERE deployment_id=$1 AND status='Pending'`, deploymentID); err != nil {
		return domain.Deployment{}, domain.Operation{}, err
	}
	op, err := insertOperation(ctx, tx, workspaceID, deployment.ID, deployment.PublicID, actorID, "UpdateDeployment", idem, payload, intentJSON, deployment.DesiredVersion)
	if err != nil {
		return domain.Deployment{}, domain.Operation{}, translateDBError(err)
	}
	if err = tx.Commit(ctx); err != nil {
		return domain.Deployment{}, domain.Operation{}, err
	}
	return deployment, op, nil
}

func (s *Store) DeleteDeployment(ctx context.Context, workspaceID, actorID, deploymentID, version int64, idem, payload []byte) (domain.Operation, error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return domain.Operation{}, err
	}
	defer tx.Rollback(ctx)
	if existing, found, err := existingOperation(ctx, tx, workspaceID, deploymentID, "DeleteDeployment", idem, payload); err != nil {
		return domain.Operation{}, err
	} else if found {
		_ = tx.QueryRow(ctx, `SELECT public_id FROM deployments WHERE id=$1`, deploymentID).Scan(&existing.DeploymentPublicID)
		if err = tx.Commit(ctx); err != nil {
			return domain.Operation{}, err
		}
		return existing, nil
	}
	current, err := deploymentByID(ctx, tx, deploymentID)
	if err != nil {
		return domain.Operation{}, err
	}
	if current.WorkspaceID != workspaceID || current.DeletedAt != nil || current.DeletionRequestedAt != nil {
		return domain.Operation{}, ErrConflict
	}
	var running bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM operations WHERE deployment_id=$1 AND status='Running')`, deploymentID).Scan(&running); err != nil {
		return domain.Operation{}, err
	}
	if running {
		return domain.Operation{}, ErrConflict
	}
	var publicID string
	var intentJSON []byte
	var newVersion int64
	err = tx.QueryRow(ctx, `UPDATE deployments SET desired_version=desired_version+1,deletion_requested_at=now(),updated_at=now() WHERE id=$1 AND workspace_id=$2 AND deleted_at IS NULL AND deletion_requested_at IS NULL AND desired_version=$3 RETURNING public_id,intent_json::text,desired_version`, deploymentID, workspaceID, version).Scan(&publicID, &intentJSON, &newVersion)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.Operation{}, ErrConflict
		}
		return domain.Operation{}, translateDBError(err)
	}
	if _, err = tx.Exec(ctx, `UPDATE operations SET status='Superseded',updated_at=now() WHERE deployment_id=$1 AND status='Pending'`, deploymentID); err != nil {
		return domain.Operation{}, err
	}
	op, err := insertOperation(ctx, tx, workspaceID, deploymentID, publicID, actorID, "DeleteDeployment", idem, payload, intentJSON, newVersion)
	if err != nil {
		return domain.Operation{}, translateDBError(err)
	}
	op.DeploymentPublicID = publicID
	if err = tx.Commit(ctx); err != nil {
		return domain.Operation{}, err
	}
	return op, nil
}

func insertOperation(ctx context.Context, tx pgx.Tx, workspaceID, deploymentID int64, deploymentPublicID string, actorID int64, kind string, idem, payload, intentJSON []byte, version int64) (domain.Operation, error) {
	id, err := domain.NewPublicID("op")
	if err != nil {
		return domain.Operation{}, err
	}
	var op domain.Operation
	err = tx.QueryRow(ctx, `INSERT INTO operations(public_id,workspace_id,deployment_id,actor_id,kind,status,idempotency_hash,payload_hash,intent_json,desired_version,sequence) VALUES($1,$2,$3,$4,$5,'Pending',$6,$7,$8,$9,$9) RETURNING id,public_id,kind,status,desired_version,attempts,created_at,updated_at`, id, workspaceID, deploymentID, actorID, kind, idem, payload, intentJSON, version).Scan(&op.ID, &op.PublicID, &op.Kind, &op.Status, &op.DesiredVersion, &op.Attempts, &op.CreatedAt, &op.UpdatedAt)
	if err == nil {
		err = jsonUnmarshal(intentJSON, &op.Intent)
	}
	op.DeploymentID = deploymentID
	op.DeploymentPublicID = deploymentPublicID
	op.ActorID = actorID
	return op, err
}

func (s *Store) ListDeployments(ctx context.Context, workspaceID int64, limit int) ([]domain.Deployment, error) {
	rows, err := s.Pool.Query(ctx, `SELECT id,public_id,workspace_id,runtime_name,intent_json::text,desired_version,observed_version,observed_release,last_state,last_message,deletion_requested_at,created_at,updated_at FROM deployments WHERE workspace_id=$1 AND deleted_at IS NULL ORDER BY id DESC LIMIT $2`, workspaceID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Deployment{}
	for rows.Next() {
		var d domain.Deployment
		var raw []byte
		if err = rows.Scan(&d.ID, &d.PublicID, &d.WorkspaceID, &d.RuntimeName, &raw, &d.DesiredVersion, &d.ObservedVersion, &d.ObservedRelease, &d.State, &d.Message, &d.DeletionRequestedAt, &d.CreatedAt, &d.UpdatedAt); err != nil {
			return nil, err
		}
		if err = jsonUnmarshal(raw, &d.Intent); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (s *Store) FindDeployment(ctx context.Context, workspaceID int64, publicID string) (domain.Deployment, error) {
	var d domain.Deployment
	var raw []byte
	err := s.Pool.QueryRow(ctx, `SELECT id,public_id,workspace_id,runtime_name,intent_json::text,desired_version,observed_version,observed_release,last_state,last_message,deletion_requested_at,created_at,updated_at FROM deployments WHERE workspace_id=$1 AND public_id=$2 AND deleted_at IS NULL`, workspaceID, publicID).Scan(&d.ID, &d.PublicID, &d.WorkspaceID, &d.RuntimeName, &raw, &d.DesiredVersion, &d.ObservedVersion, &d.ObservedRelease, &d.State, &d.Message, &d.DeletionRequestedAt, &d.CreatedAt, &d.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return d, ErrNotFound
	}
	if err != nil {
		return d, err
	}
	err = jsonUnmarshal(raw, &d.Intent)
	return d, err
}

func (s *Store) ListOperations(ctx context.Context, workspaceID, deploymentID int64) ([]domain.Operation, error) {
	rows, err := s.Pool.Query(ctx, `SELECT o.id,o.public_id,o.deployment_id,d.public_id,o.actor_id,o.kind,o.status,o.desired_version,o.attempts,o.error_code,o.error_message,o.created_at,o.updated_at FROM operations o JOIN deployments d ON d.id=o.deployment_id WHERE o.workspace_id=$1 AND o.deployment_id=$2 ORDER BY o.id DESC`, workspaceID, deploymentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Operation{}
	for rows.Next() {
		var op domain.Operation
		if err = rows.Scan(&op.ID, &op.PublicID, &op.DeploymentID, &op.DeploymentPublicID, &op.ActorID, &op.Kind, &op.Status, &op.DesiredVersion, &op.Attempts, &op.ErrorCode, &op.ErrorMessage, &op.CreatedAt, &op.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, op)
	}
	return out, rows.Err()
}

func (s *Store) GetOperation(ctx context.Context, workspaceID int64, publicID string) (domain.Operation, error) {
	var op domain.Operation
	err := s.Pool.QueryRow(ctx, `SELECT o.id,o.public_id,o.deployment_id,d.public_id,o.kind,o.status,o.desired_version,o.attempts,o.error_code,o.error_message,o.created_at,o.updated_at FROM operations o JOIN deployments d ON d.id=o.deployment_id WHERE o.workspace_id=$1 AND o.public_id=$2`, workspaceID, publicID).Scan(&op.ID, &op.PublicID, &op.DeploymentID, &op.DeploymentPublicID, &op.Kind, &op.Status, &op.DesiredVersion, &op.Attempts, &op.ErrorCode, &op.ErrorMessage, &op.CreatedAt, &op.UpdatedAt)
	return op, err
}

func (s *Store) ClaimNext(ctx context.Context, worker string, lease time.Duration) (domain.Operation, domain.Deployment, bool, error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return domain.Operation{}, domain.Deployment{}, false, err
	}
	defer tx.Rollback(ctx)
	var op domain.Operation
	var operationIntent []byte
	err = tx.QueryRow(ctx, `WITH candidate AS (SELECT id FROM operations WHERE (status='Pending' OR (status='Running' AND lease_until < now())) AND next_attempt_at <= now() ORDER BY id FOR UPDATE SKIP LOCKED LIMIT 1) UPDATE operations o SET status='Running',attempts=attempts+1,lease_until=now()+$1::interval,worker_id=$2,fencing_token=fencing_token+1,updated_at=now() FROM candidate c WHERE o.id=c.id RETURNING o.id,o.public_id,o.workspace_id,o.deployment_id,o.actor_id,o.kind,o.status,o.desired_version,o.attempts,o.intent_json::text,o.worker_id,o.fencing_token,o.lease_until,o.created_at,o.updated_at`, fmt.Sprintf("%f seconds", lease.Seconds()), worker).Scan(&op.ID, &op.PublicID, &op.WorkspaceID, &op.DeploymentID, &op.ActorID, &op.Kind, &op.Status, &op.DesiredVersion, &op.Attempts, &operationIntent, &op.WorkerID, &op.FencingToken, &op.LeaseUntil, &op.CreatedAt, &op.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Operation{}, domain.Deployment{}, false, nil
	}
	if err != nil {
		return domain.Operation{}, domain.Deployment{}, false, err
	}
	var d domain.Deployment
	var deploymentIntent []byte
	if err = tx.QueryRow(ctx, `SELECT id,public_id,workspace_id,runtime_name,intent_json::text,desired_version,observed_version,observed_release,last_state,last_message,deletion_requested_at,created_at,updated_at FROM deployments WHERE id=$1`, op.DeploymentID).Scan(&d.ID, &d.PublicID, &d.WorkspaceID, &d.RuntimeName, &deploymentIntent, &d.DesiredVersion, &d.ObservedVersion, &d.ObservedRelease, &d.State, &d.Message, &d.DeletionRequestedAt, &d.CreatedAt, &d.UpdatedAt); err != nil {
		return domain.Operation{}, domain.Deployment{}, false, err
	}
	if err = jsonUnmarshal(deploymentIntent, &d.Intent); err != nil {
		return domain.Operation{}, domain.Deployment{}, false, err
	}
	if err = tx.Commit(ctx); err != nil {
		return domain.Operation{}, domain.Deployment{}, false, err
	}
	if err = jsonUnmarshal(operationIntent, &op.Intent); err != nil {
		return domain.Operation{}, domain.Deployment{}, false, err
	}
	op.DeploymentPublicID = d.PublicID
	return op, d, true, nil
}

func (s *Store) Complete(ctx context.Context, op domain.Operation, state, message string, observedVersion int64, release string, deleted bool) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var commandTag pgconn.CommandTag
	if deleted {
		commandTag, err = tx.Exec(ctx, `UPDATE deployments SET deleted_at=now(),last_message=$1,updated_at=now() WHERE id=$2 AND desired_version=$3 AND deletion_requested_at IS NOT NULL AND deleted_at IS NULL`, message, op.DeploymentID, op.DesiredVersion)
	} else {
		commandTag, err = tx.Exec(ctx, `UPDATE deployments SET observed_version=$1,observed_release=$2,last_state=$3,last_message=$4,updated_at=now() WHERE id=$5 AND desired_version=$6 AND deleted_at IS NULL`, observedVersion, release, state, message, op.DeploymentID, op.DesiredVersion)
	}
	if err != nil {
		return err
	}
	if commandTag.RowsAffected() != 1 {
		return ErrLeaseLost
	}
	commandTag, err = tx.Exec(ctx, `UPDATE operations SET status='Succeeded',lease_until=NULL,worker_id=NULL,updated_at=now() WHERE id=$1 AND status='Running' AND worker_id=$2 AND fencing_token=$3 AND lease_until > now()`, op.ID, op.WorkerID, op.FencingToken)
	if err != nil {
		return err
	}
	if commandTag.RowsAffected() != 1 {
		return ErrLeaseLost
	}
	return tx.Commit(ctx)
}

func (s *Store) Fail(ctx context.Context, op domain.Operation, code, message string, retry bool) error {
	status := "Failed"
	if retry {
		status = "Pending"
	}
	command := `UPDATE operations SET status=$1,next_attempt_at=CASE WHEN $1='Pending' THEN now()+make_interval(secs => LEAST(300, power(2, attempts)::int)) ELSE next_attempt_at END,lease_until=NULL,worker_id=NULL,error_code=$2,error_message=$3,updated_at=now() WHERE id=$4 AND status='Running' AND worker_id=$5 AND fencing_token=$6 AND lease_until > now()`
	if retry && op.Attempts >= 8 {
		status = "Failed"
	}
	tag, err := s.Pool.Exec(ctx, command, status, code, message, op.ID, op.WorkerID, op.FencingToken)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return ErrLeaseLost
	}
	return nil
}

func existingOperation(ctx context.Context, tx pgx.Tx, workspaceID, deploymentID int64, kind string, idem, payload []byte) (domain.Operation, bool, error) {
	var op domain.Operation
	var storedPayload []byte
	err := tx.QueryRow(ctx, `SELECT id,public_id,deployment_id,workspace_id,actor_id,kind,status,desired_version,attempts,error_code,error_message,created_at,updated_at,payload_hash FROM operations WHERE workspace_id=$1 AND deployment_id=$2 AND kind=$3 AND idempotency_hash=$4`, workspaceID, deploymentID, kind, idem).Scan(&op.ID, &op.PublicID, &op.DeploymentID, &op.WorkspaceID, &op.ActorID, &op.Kind, &op.Status, &op.DesiredVersion, &op.Attempts, &op.ErrorCode, &op.ErrorMessage, &op.CreatedAt, &op.UpdatedAt, &storedPayload)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Operation{}, false, nil
	}
	if err != nil {
		return domain.Operation{}, false, err
	}
	if string(storedPayload) != string(payload) {
		return domain.Operation{}, false, ErrConflict
	}
	return op, true, nil
}

func deploymentByID(ctx context.Context, tx pgx.Tx, id int64) (domain.Deployment, error) {
	var d domain.Deployment
	var raw []byte
	err := tx.QueryRow(ctx, `SELECT id,public_id,workspace_id,runtime_name,intent_json::text,desired_version,observed_version,observed_release,last_state,last_message,deletion_requested_at,deleted_at,created_at,updated_at FROM deployments WHERE id=$1`, id).Scan(&d.ID, &d.PublicID, &d.WorkspaceID, &d.RuntimeName, &raw, &d.DesiredVersion, &d.ObservedVersion, &d.ObservedRelease, &d.State, &d.Message, &d.DeletionRequestedAt, &d.DeletedAt, &d.CreatedAt, &d.UpdatedAt)
	if err != nil {
		return d, err
	}
	err = jsonUnmarshal(raw, &d.Intent)
	return d, err
}

var ErrConflict = errors.New("conflict")

var ErrImmutableName = errors.New("deployment name is immutable")

var ErrLeaseLost = errors.New("operation lease lost")

var ErrNotFound = errors.New("not found")

var ErrSessionInvalid = errors.New("session is no longer valid")

var ErrNoRows = pgx.ErrNoRows

func translateDBError(err error) error {
	if isUniqueViolation(err) {
		return ErrConflict
	}
	return err
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

func jsonUnmarshal(raw []byte, target any) error { return json.Unmarshal(raw, target) }
