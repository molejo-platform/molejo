package store

import (
	"bytes"
	"context"
	"crypto/md5"
	"crypto/sha256"
	"database/sql"
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
	storesqlc "github.com/fruto-platform/fruto/services/control-plane-api/internal/store/sqlc"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/lock"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

type Store struct {
	Pool    *pgxpool.Pool
	queries *storesqlc.Queries
}

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
	return &Store{Pool: pool, queries: storesqlc.New(pool)}, nil
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
	return s.migrateTo(ctx, 0)
}

func (s *Store) MigrateHierarchyExpand(ctx context.Context) error {
	return s.migrateTo(ctx, 5)
}

func (s *Store) MigrateHierarchyBackfill(ctx context.Context) error {
	return s.migrateTo(ctx, 6)
}

func (s *Store) MigrateHierarchyContract(ctx context.Context) error {
	return s.migrateTo(ctx, 7)
}

func (s *Store) migrateTo(ctx context.Context, targetVersion int64) error {
	migrations, err := embeddedMigrations()
	if err != nil {
		return err
	}
	db, err := sql.Open("pgx", s.Pool.Config().ConnString())
	if err != nil {
		return err
	}
	defer db.Close()
	if err = db.PingContext(ctx); err != nil {
		return err
	}
	migrationConn, err := db.Conn(ctx)
	if err != nil {
		return err
	}
	defer migrationConn.Close()
	if _, err = migrationConn.ExecContext(ctx, `SELECT pg_advisory_lock(hashtext('fruto-control-plane-goose'))`); err != nil {
		return err
	}
	defer func() {
		_, _ = migrationConn.ExecContext(context.Background(), `SELECT pg_advisory_unlock(hashtext('fruto-control-plane-goose'))`)
	}()
	if _, err = db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (version BIGINT PRIMARY KEY, checksum BYTEA, applied_at TIMESTAMPTZ NOT NULL DEFAULT now())`); err != nil {
		return err
	}
	if _, err = db.ExecContext(ctx, `ALTER TABLE schema_migrations ADD COLUMN IF NOT EXISTS checksum BYTEA`); err != nil {
		return err
	}
	applied, err := loadAndValidateChecksums(ctx, db, migrations, false)
	if err != nil {
		return err
	}
	migrationFiles, err := fs.Sub(migrationFS, "migrations")
	if err != nil {
		return err
	}
	locker, err := lock.NewPostgresSessionLocker()
	if err != nil {
		return err
	}
	provider, err := goose.NewProvider(goose.DialectPostgres, db, migrationFiles, goose.WithSessionLocker(locker))
	if err != nil {
		return err
	}
	currentVersion, err := provider.GetDBVersion(ctx)
	if err != nil {
		return err
	}
	if targetVersion > 0 && currentVersion > targetVersion {
		return fmt.Errorf("database schema version %d is newer than requested version %d", currentVersion, targetVersion)
	}
	if currentVersion == 0 {
		for version := range applied {
			if _, err = db.ExecContext(ctx, `INSERT INTO goose_db_version(version_id,is_applied) SELECT $1,true WHERE NOT EXISTS (SELECT 1 FROM goose_db_version WHERE version_id=$1 AND is_applied=true)`, version); err != nil {
				return err
			}
		}
	}
	if targetVersion > 0 {
		if _, err = provider.UpTo(ctx, targetVersion); err != nil {
			return err
		}
	} else if _, err = provider.Up(ctx); err != nil {
		return err
	}
	for _, migration := range migrations {
		if targetVersion > 0 && migration.version > targetVersion {
			continue
		}
		if _, err = db.ExecContext(ctx, `INSERT INTO schema_migrations(version,checksum) VALUES ($1,$2) ON CONFLICT(version) DO UPDATE SET checksum=EXCLUDED.checksum`, migration.version, migration.checksum[:]); err != nil {
			return err
		}
	}
	return nil
}

func loadAndValidateChecksums(ctx context.Context, db *sql.DB, migrations []migration, requireAll bool) (map[int64]struct{}, error) {
	rows, err := db.QueryContext(ctx, `SELECT version,checksum FROM schema_migrations`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	known := make(map[int64]migration, len(migrations))
	for _, item := range migrations {
		known[item.version] = item
	}
	applied := make(map[int64]struct{}, len(migrations))
	for rows.Next() {
		var version int64
		var checksum []byte
		if err = rows.Scan(&version, &checksum); err != nil {
			return nil, err
		}
		item, ok := known[version]
		if !ok {
			return nil, fmt.Errorf("unknown schema migration %d is applied", version)
		}
		if len(checksum) != 0 && !bytes.Equal(checksum, item.checksum[:]) {
			return nil, fmt.Errorf("migration %d checksum drift", version)
		}
		applied[version] = struct{}{}
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	if requireAll {
		for _, item := range migrations {
			if _, ok := applied[item.version]; !ok {
				return nil, fmt.Errorf("schema migration %d is not applied", item.version)
			}
		}
	}
	return applied, nil
}

func (s *Store) Bootstrap(ctx context.Context, workspace domain.Workspace, actors map[string]struct{ Role, PasswordHash string }) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, "workspace-bootstrap:"+workspace.Namespace); err != nil {
		return err
	}
	queries := s.queries.WithTx(tx)
	actorIDs := make([]int64, 0, len(actors))
	for key, actor := range actors {
		actorID, queryErr := queries.UpsertActor(ctx, storesqlc.UpsertActorParams{ActorKey: key, Role: actor.Role, PasswordHash: actor.PasswordHash})
		err = queryErr
		if err != nil {
			return err
		}
		actorIDs = append(actorIDs, actorID)
	}
	workspaceID, err := queries.UpsertWorkspace(ctx, storesqlc.UpsertWorkspaceParams{PublicID: workspace.PublicID, Name: workspace.Name, NamespaceName: workspace.Namespace})
	if err != nil {
		return err
	}
	for _, actorID := range actorIDs {
		if err = queries.AddWorkspaceActor(ctx, storesqlc.AddWorkspaceActorParams{WorkspaceID: workspaceID, ActorID: actorID}); err != nil {
			return err
		}
	}
	if len(actorIDs) == 0 {
		return errors.New("workspace bootstrap requires at least one actor")
	}
	sort.Slice(actorIDs, func(i, j int) bool { return actorIDs[i] < actorIDs[j] })
	operationID, err := domain.NewPublicID("op")
	if err != nil {
		return err
	}
	bootstrapHash := domain.SHA256([]byte(workspace.PublicID + ":ensure-workspace"))
	if _, err = tx.Exec(ctx, `INSERT INTO operations(public_id,workspace_id,deployment_id,actor_id,kind,status,idempotency_hash,payload_hash,intent_json,desired_version,sequence)
		SELECT $1,$2,NULL,$3,'EnsureWorkspace','Pending',$4,$4,'{}'::jsonb,1,1
		WHERE EXISTS (SELECT 1 FROM workspaces WHERE id=$2 AND bootstrap_state <> 'Ready')
		  AND NOT EXISTS (SELECT 1 FROM operations WHERE workspace_id=$2 AND kind='EnsureWorkspace' AND status IN ('Pending','Running'))`, operationID, workspaceID, actorIDs[0], bootstrapHash); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) Authenticate(ctx context.Context, key string) (domain.Actor, string, error) {
	row, err := s.queries.GetActorByKey(ctx, key)
	return domain.Actor{ID: row.ID, Key: row.ActorKey, Role: row.Role}, row.PasswordHash, err
}

func (s *Store) Actor(ctx context.Context, actorID int64) (domain.Actor, error) {
	row, err := s.queries.GetActorByID(ctx, actorID)
	return domain.Actor{ID: row.ID, Key: row.ActorKey, Role: row.Role}, err
}

func (s *Store) WorkspaceForActor(ctx context.Context, actorID int64) (domain.Workspace, error) {
	row, err := s.queries.GetWorkspaceForActor(ctx, actorID)
	return workspaceValue(row.ID, row.PublicID, row.Name, row.NamespaceName, row.Version, row.BootstrapState, row.CreatedAt, row.UpdatedAt), err
}

func (s *Store) Workspace(ctx context.Context, workspaceID int64) (domain.Workspace, error) {
	row, err := s.queries.GetWorkspaceByID(ctx, workspaceID)
	return workspaceValue(row.ID, row.PublicID, row.Name, row.NamespaceName, row.Version, row.BootstrapState, row.CreatedAt, row.UpdatedAt), err
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
	gooseRows, err := s.Pool.Query(ctx, `SELECT version_id FROM goose_db_version WHERE is_applied=true`)
	if err != nil {
		return fmt.Errorf("goose migration metadata: %w", err)
	}
	defer gooseRows.Close()
	gooseApplied := make(map[int64]struct{}, len(migrations))
	for gooseRows.Next() {
		var version int64
		if err = gooseRows.Scan(&version); err != nil {
			return err
		}
		if version != 0 {
			gooseApplied[version] = struct{}{}
		}
	}
	if err = gooseRows.Err(); err != nil {
		return err
	}
	for _, migration := range migrations {
		if _, ok := gooseApplied[migration.version]; !ok {
			return fmt.Errorf("goose migration %d is not applied", migration.version)
		}
		delete(gooseApplied, migration.version)
	}
	if len(gooseApplied) != 0 {
		return fmt.Errorf("unknown goose migration is applied")
	}
	return nil
}

func (s *Store) CreateSession(ctx context.Context, actorID int64, tokenHash, csrfHash []byte, expires time.Time) error {
	return s.queries.CreateSession(ctx, storesqlc.CreateSessionParams{TokenHash: tokenHash, ActorID: actorID, CsrfHash: csrfHash, ExpiresAt: pgtype.Timestamptz{Time: expires, Valid: true}})
}

func (s *Store) Session(ctx context.Context, tokenHash []byte) (int64, []byte, error) {
	row, err := s.queries.GetActiveSession(ctx, tokenHash)
	return row.ActorID, row.CsrfHash, err
}

func (s *Store) RevokeSession(ctx context.Context, tokenHash []byte) error {
	return s.queries.RevokeSession(ctx, tokenHash)
}

func (s *Store) RotateSession(ctx context.Context, actorID int64, oldTokenHash, newTokenHash, csrfHash []byte, expires time.Time) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	queries := s.queries.WithTx(tx)
	rowsAffected, err := queries.RevokeActiveSession(ctx, storesqlc.RevokeActiveSessionParams{TokenHash: oldTokenHash, ActorID: actorID})
	if err != nil {
		return err
	}
	if rowsAffected != 1 {
		return ErrSessionInvalid
	}
	if err = queries.CreateSession(ctx, storesqlc.CreateSessionParams{TokenHash: newTokenHash, ActorID: actorID, CsrfHash: csrfHash, ExpiresAt: pgtype.Timestamptz{Time: expires, Valid: true}}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) CreateDeployment(ctx context.Context, workspaceID, actorID int64, deploymentID string, intent domain.Intent, idempotencyHash, payloadHash []byte) (domain.Deployment, domain.Operation, bool, error) {
	return s.createDeploymentWithHierarchy(ctx, workspaceID, actorID, deploymentID, "", "", 0, "", intent, idempotencyHash, payloadHash)
}

func (s *Store) CreateDeploymentForApp(ctx context.Context, workspaceID, actorID int64, deploymentID, appPublicID, environmentPublicID string, intent domain.Intent, idempotencyHash, payloadHash []byte) (domain.Deployment, domain.Operation, bool, error) {
	return s.createDeploymentWithHierarchy(ctx, workspaceID, actorID, deploymentID, appPublicID, environmentPublicID, 0, "", intent, idempotencyHash, payloadHash)
}

func (s *Store) CreateDeploymentForRelease(ctx context.Context, workspaceID, actorID int64, deploymentID, appPublicID, environmentPublicID string, release domain.Release, intent domain.Intent, idempotencyHash, payloadHash []byte) (domain.Deployment, domain.Operation, bool, error) {
	return s.createDeploymentWithHierarchy(ctx, workspaceID, actorID, deploymentID, appPublicID, environmentPublicID, release.ID, release.PublicID, intent, idempotencyHash, payloadHash)
}

func (s *Store) createDeploymentWithHierarchy(ctx context.Context, workspaceID, actorID int64, deploymentID, appPublicID, environmentPublicID string, releaseID int64, releasePublicID string, intent domain.Intent, idempotencyHash, payloadHash []byte) (domain.Deployment, domain.Operation, bool, error) {
	for attempt := 0; attempt < 3; attempt++ {
		deployment, operation, reused, err := s.createDeployment(ctx, workspaceID, actorID, deploymentID, appPublicID, environmentPublicID, releaseID, releasePublicID, intent, idempotencyHash, payloadHash)
		if !errors.Is(err, errRetryCreate) {
			return deployment, operation, reused, err
		}
	}
	return domain.Deployment{}, domain.Operation{}, false, ErrConflict
}

var errRetryCreate = errors.New("retry concurrent create")

func (s *Store) createDeployment(ctx context.Context, workspaceID, actorID int64, deploymentID, appPublicID, environmentPublicID string, releaseID int64, releasePublicID string, intent domain.Intent, idempotencyHash, payloadHash []byte) (domain.Deployment, domain.Operation, bool, error) {
	intentJSON, err := domain.CanonicalJSON(intent)
	if err != nil {
		return domain.Deployment{}, domain.Operation{}, false, err
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return domain.Deployment{}, domain.Operation{}, false, err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, "deployment-create:"+fmt.Sprintf("%x", idempotencyHash)); err != nil {
		return domain.Deployment{}, domain.Operation{}, false, err
	}
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
	hierarchy, err := s.resolveDeploymentHierarchy(ctx, tx, workspaceID, deploymentID, appPublicID, environmentPublicID, intent.Name)
	if err != nil {
		return domain.Deployment{}, domain.Operation{}, false, err
	}
	var deployment domain.Deployment
	runtimeName := domain.RuntimeName(deploymentID)
	err = tx.QueryRow(ctx, `INSERT INTO deployments(public_id,workspace_id,project_id,app_id,environment_id,release_id,name,runtime_name,slug,intent_json) VALUES ($1,$2,$3,$4,$5,NULLIF($6,0),$7,$8,NULLIF($9,''),$10) RETURNING id,public_id,workspace_id,project_id,app_id,environment_id,runtime_name,intent_json::text,desired_version,observed_version,observed_release,last_state,last_message,created_at,updated_at`, deploymentID, workspaceID, hierarchy.ProjectID, hierarchy.AppID, hierarchy.EnvironmentID, releaseID, intent.Name, runtimeName, intent.Slug, intentJSON).Scan(&deployment.ID, &deployment.PublicID, &deployment.WorkspaceID, &deployment.ProjectID, &deployment.AppID, &deployment.EnvironmentID, &deployment.RuntimeName, &intentJSON, &deployment.DesiredVersion, &deployment.ObservedVersion, &deployment.ObservedRelease, &deployment.State, &deployment.Message, &deployment.CreatedAt, &deployment.UpdatedAt)
	if err != nil {
		if isUniqueViolation(err) {
			if uniqueConstraint(err) == "deployments_public_id_key" {
				return domain.Deployment{}, domain.Operation{}, false, ErrPublicIDCollision
			}
			return domain.Deployment{}, domain.Operation{}, false, errRetryCreate
		}
		return domain.Deployment{}, domain.Operation{}, false, translateDBError(err)
	}
	if err := jsonUnmarshal(intentJSON, &deployment.Intent); err != nil {
		return domain.Deployment{}, domain.Operation{}, false, err
	}
	deployment.ProjectPublicID = hierarchy.ProjectPublicID
	deployment.AppPublicID = hierarchy.AppPublicID
	deployment.EnvironmentPublicID = hierarchy.EnvironmentPublicID
	deployment.ReleasePublicID = releasePublicID
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

type deploymentHierarchy struct {
	ProjectID           int64
	ProjectPublicID     string
	AppID               int64
	AppPublicID         string
	EnvironmentID       int64
	EnvironmentPublicID string
}

func (s *Store) resolveDeploymentHierarchy(ctx context.Context, tx pgx.Tx, workspaceID int64, deploymentID, appPublicID, environmentPublicID, deploymentName string) (deploymentHierarchy, error) {
	queries := s.queries.WithTx(tx)
	if appPublicID != "" || environmentPublicID != "" {
		if err := domain.ValidateHierarchyReferences(appPublicID, environmentPublicID); err != nil {
			return deploymentHierarchy{}, ErrNotFound
		}
		row, err := queries.ResolveDeploymentHierarchy(ctx, storesqlc.ResolveDeploymentHierarchyParams{WorkspaceID: workspaceID, PublicID: appPublicID, PublicID_2: environmentPublicID})
		if errors.Is(err, pgx.ErrNoRows) {
			return deploymentHierarchy{}, ErrNotFound
		}
		return deploymentHierarchy(row), err
	}
	workspace, err := queries.GetWorkspaceByID(ctx, workspaceID)
	if err != nil {
		return deploymentHierarchy{}, err
	}
	project, err := queries.UpsertCompatibilityProject(ctx, storesqlc.UpsertCompatibilityProjectParams{PublicID: compatibilityPublicID("prj", "project", workspace.PublicID), WorkspaceID: workspaceID})
	if err != nil {
		return deploymentHierarchy{}, err
	}
	environment, err := queries.UpsertCompatibilityEnvironment(ctx, storesqlc.UpsertCompatibilityEnvironmentParams{PublicID: compatibilityPublicID("env", "environment", workspace.PublicID), ProjectID: project.ID})
	if err != nil {
		return deploymentHierarchy{}, err
	}
	app, err := queries.UpsertCompatibilityApp(ctx, storesqlc.UpsertCompatibilityAppParams{PublicID: compatibilityPublicID("app", "app", deploymentID), ProjectID: project.ID, Name: deploymentName, NameKey: strings.ToLower(strings.TrimSpace(deploymentName))})
	if err != nil {
		return deploymentHierarchy{}, err
	}
	return deploymentHierarchy{ProjectID: project.ID, ProjectPublicID: project.PublicID, AppID: app.ID, AppPublicID: app.PublicID, EnvironmentID: environment.ID, EnvironmentPublicID: environment.PublicID}, nil
}

func compatibilityPublicID(prefix, kind, source string) string {
	digest := fmt.Sprintf("%x", md5.Sum([]byte(kind+":"+source)))
	digest = strings.NewReplacer("0", "a", "1", "b", "8", "c", "9", "d").Replace(digest)
	return prefix + "-" + digest[:20]
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
	if intent.AppID != "" && (intent.AppID != current.AppPublicID || intent.EnvironmentID != current.EnvironmentPublicID) {
		return domain.Deployment{}, domain.Operation{}, ErrImmutableHierarchy
	}
	if current.ReleasePublicID != "" && intent.Image != current.Intent.Image {
		return domain.Deployment{}, domain.Operation{}, ErrImmutableRelease
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
	deployment.ProjectID = current.ProjectID
	deployment.ProjectPublicID = current.ProjectPublicID
	deployment.AppID = current.AppID
	deployment.AppPublicID = current.AppPublicID
	deployment.EnvironmentID = current.EnvironmentID
	deployment.EnvironmentPublicID = current.EnvironmentPublicID
	deployment.ReleasePublicID = current.ReleasePublicID
	if _, err = tx.Exec(ctx, `UPDATE operations SET status='Superseded',completed_at=now(),updated_at=now() WHERE deployment_id=$1 AND status='Pending'`, deploymentID); err != nil {
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
	if _, err = tx.Exec(ctx, `UPDATE operations SET status='Superseded',completed_at=now(),updated_at=now() WHERE deployment_id=$1 AND status='Pending'`, deploymentID); err != nil {
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
	for range 3 {
		if _, err := tx.Exec(ctx, `SAVEPOINT operation_public_id`); err != nil {
			return domain.Operation{}, err
		}
		id, err := domain.NewPublicID("op")
		if err != nil {
			return domain.Operation{}, err
		}
		var op domain.Operation
		err = tx.QueryRow(ctx, `INSERT INTO operations(public_id,workspace_id,deployment_id,actor_id,kind,status,idempotency_hash,payload_hash,intent_json,desired_version,sequence) VALUES($1,$2,$3,$4,$5,'Pending',$6,$7,$8,$9,$9) RETURNING id,public_id,kind,status,desired_version,attempts,created_at,updated_at`, id, workspaceID, deploymentID, actorID, kind, idem, payload, intentJSON, version).Scan(&op.ID, &op.PublicID, &op.Kind, &op.Status, &op.DesiredVersion, &op.Attempts, &op.CreatedAt, &op.UpdatedAt)
		if uniqueConstraint(err) == "operations_public_id_key" {
			if _, rollbackErr := tx.Exec(ctx, `ROLLBACK TO SAVEPOINT operation_public_id`); rollbackErr != nil {
				return domain.Operation{}, rollbackErr
			}
			continue
		}
		if err == nil {
			_, err = tx.Exec(ctx, `RELEASE SAVEPOINT operation_public_id`)
		}
		if err == nil {
			err = jsonUnmarshal(intentJSON, &op.Intent)
		}
		op.DeploymentID = deploymentID
		op.DeploymentPublicID = deploymentPublicID
		op.ActorID = actorID
		return op, err
	}
	return domain.Operation{}, ErrPublicIDCollision
}

func (s *Store) ListDeployments(ctx context.Context, workspaceID, beforeID int64, limit int) ([]domain.Deployment, string, error) {
	rows, err := s.Pool.Query(ctx, `SELECT d.id,d.public_id,d.workspace_id,d.project_id,p.public_id,d.app_id,a.public_id,d.environment_id,e.public_id,COALESCE(r.public_id,''),d.runtime_name,d.intent_json::text,d.desired_version,d.observed_version,d.observed_release,d.last_state,d.last_message,d.deletion_requested_at,d.created_at,d.updated_at FROM deployments d JOIN projects p ON p.id=d.project_id JOIN apps a ON a.id=d.app_id JOIN environments e ON e.id=d.environment_id LEFT JOIN releases r ON r.id=d.release_id WHERE d.workspace_id=$1 AND d.id < $2 AND d.deleted_at IS NULL ORDER BY d.id DESC LIMIT $3`, workspaceID, beforeID, limit+1)
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()
	out := []domain.Deployment{}
	for rows.Next() {
		var d domain.Deployment
		var raw []byte
		if err = rows.Scan(&d.ID, &d.PublicID, &d.WorkspaceID, &d.ProjectID, &d.ProjectPublicID, &d.AppID, &d.AppPublicID, &d.EnvironmentID, &d.EnvironmentPublicID, &d.ReleasePublicID, &d.RuntimeName, &raw, &d.DesiredVersion, &d.ObservedVersion, &d.ObservedRelease, &d.State, &d.Message, &d.DeletionRequestedAt, &d.CreatedAt, &d.UpdatedAt); err != nil {
			return nil, "", err
		}
		if err = jsonUnmarshal(raw, &d.Intent); err != nil {
			return nil, "", err
		}
		out = append(out, d)
	}
	if err = rows.Err(); err != nil {
		return nil, "", err
	}
	nextCursor := ""
	if len(out) > limit {
		out = out[:limit]
		nextCursor = domain.EncodeCursor(out[len(out)-1].ID)
	}
	return out, nextCursor, nil
}

func (s *Store) FindDeployment(ctx context.Context, workspaceID int64, publicID string) (domain.Deployment, error) {
	var d domain.Deployment
	var raw []byte
	err := s.Pool.QueryRow(ctx, `SELECT d.id,d.public_id,d.workspace_id,d.project_id,p.public_id,d.app_id,a.public_id,d.environment_id,e.public_id,COALESCE(r.public_id,''),d.runtime_name,d.intent_json::text,d.desired_version,d.observed_version,d.observed_release,d.last_state,d.last_message,d.deletion_requested_at,d.created_at,d.updated_at FROM deployments d JOIN projects p ON p.id=d.project_id JOIN apps a ON a.id=d.app_id JOIN environments e ON e.id=d.environment_id LEFT JOIN releases r ON r.id=d.release_id WHERE d.workspace_id=$1 AND d.public_id=$2 AND d.deleted_at IS NULL`, workspaceID, publicID).Scan(&d.ID, &d.PublicID, &d.WorkspaceID, &d.ProjectID, &d.ProjectPublicID, &d.AppID, &d.AppPublicID, &d.EnvironmentID, &d.EnvironmentPublicID, &d.ReleasePublicID, &d.RuntimeName, &raw, &d.DesiredVersion, &d.ObservedVersion, &d.ObservedRelease, &d.State, &d.Message, &d.DeletionRequestedAt, &d.CreatedAt, &d.UpdatedAt)
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
	err := s.Pool.QueryRow(ctx, `SELECT o.id,o.public_id,COALESCE(o.deployment_id,0),COALESCE(d.public_id,''),o.kind,o.status,o.desired_version,o.attempts,o.error_code,o.error_message,o.created_at,o.updated_at FROM operations o LEFT JOIN deployments d ON d.id=o.deployment_id WHERE o.workspace_id=$1 AND o.public_id=$2`, workspaceID, publicID).Scan(&op.ID, &op.PublicID, &op.DeploymentID, &op.DeploymentPublicID, &op.Kind, &op.Status, &op.DesiredVersion, &op.Attempts, &op.ErrorCode, &op.ErrorMessage, &op.CreatedAt, &op.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return op, ErrNotFound
	}
	return op, err
}

func (s *Store) GetOperationForActor(ctx context.Context, actorID int64, publicID string) (domain.Operation, error) {
	var op domain.Operation
	err := s.Pool.QueryRow(ctx, `SELECT o.id,o.public_id,COALESCE(o.deployment_id,0),COALESCE(d.public_id,''),o.workspace_id,o.kind,o.status,o.desired_version,o.attempts,o.error_code,o.error_message,o.created_at,o.updated_at FROM operations o LEFT JOIN deployments d ON d.id=o.deployment_id JOIN workspace_actors wa ON wa.workspace_id=o.workspace_id WHERE wa.actor_id=$1 AND o.public_id=$2`, actorID, publicID).Scan(&op.ID, &op.PublicID, &op.DeploymentID, &op.DeploymentPublicID, &op.WorkspaceID, &op.Kind, &op.Status, &op.DesiredVersion, &op.Attempts, &op.ErrorCode, &op.ErrorMessage, &op.CreatedAt, &op.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return op, ErrNotFound
	}
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
	err = tx.QueryRow(ctx, `WITH candidate AS (SELECT id FROM operations WHERE (status='Pending' OR (status='Running' AND lease_until < now())) AND next_attempt_at <= now() ORDER BY id FOR UPDATE SKIP LOCKED LIMIT 1) UPDATE operations o SET status='Running',attempts=attempts+1,started_at=COALESCE(started_at,now()),lease_until=now()+$1::interval,worker_id=$2,fencing_token=fencing_token+1,updated_at=now() FROM candidate c WHERE o.id=c.id RETURNING o.id,o.public_id,o.workspace_id,COALESCE(o.deployment_id,0),o.actor_id,o.kind,o.status,o.desired_version,o.attempts,o.intent_json::text,o.worker_id,o.fencing_token,o.lease_until,o.created_at,o.updated_at`, fmt.Sprintf("%f seconds", lease.Seconds()), worker).Scan(&op.ID, &op.PublicID, &op.WorkspaceID, &op.DeploymentID, &op.ActorID, &op.Kind, &op.Status, &op.DesiredVersion, &op.Attempts, &operationIntent, &op.WorkerID, &op.FencingToken, &op.LeaseUntil, &op.CreatedAt, &op.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Operation{}, domain.Deployment{}, false, nil
	}
	if err != nil {
		return domain.Operation{}, domain.Deployment{}, false, err
	}
	if op.Kind == domain.OperationEnsureWorkspace {
		if _, err = tx.Exec(ctx, `UPDATE workspaces SET bootstrap_state='Running',updated_at=now() WHERE id=$1`, op.WorkspaceID); err != nil {
			return domain.Operation{}, domain.Deployment{}, false, err
		}
		if err = tx.Commit(ctx); err != nil {
			return domain.Operation{}, domain.Deployment{}, false, err
		}
		return op, domain.Deployment{WorkspaceID: op.WorkspaceID}, true, nil
	}
	var d domain.Deployment
	var deploymentIntent []byte
	if err = tx.QueryRow(ctx, `SELECT d.id,d.public_id,d.workspace_id,d.project_id,p.public_id,d.app_id,a.public_id,d.environment_id,e.public_id,COALESCE(r.public_id,''),d.runtime_name,d.intent_json::text,d.desired_version,d.observed_version,d.observed_release,d.last_state,d.last_message,d.deletion_requested_at,d.created_at,d.updated_at FROM deployments d JOIN projects p ON p.id=d.project_id JOIN apps a ON a.id=d.app_id JOIN environments e ON e.id=d.environment_id LEFT JOIN releases r ON r.id=d.release_id WHERE d.id=$1`, op.DeploymentID).Scan(&d.ID, &d.PublicID, &d.WorkspaceID, &d.ProjectID, &d.ProjectPublicID, &d.AppID, &d.AppPublicID, &d.EnvironmentID, &d.EnvironmentPublicID, &d.ReleasePublicID, &d.RuntimeName, &deploymentIntent, &d.DesiredVersion, &d.ObservedVersion, &d.ObservedRelease, &d.State, &d.Message, &d.DeletionRequestedAt, &d.CreatedAt, &d.UpdatedAt); err != nil {
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
	commandTag, err = tx.Exec(ctx, `UPDATE operations SET status='Succeeded',completed_at=now(),lease_until=NULL,worker_id=NULL,error_code='',error_message='',updated_at=now() WHERE id=$1 AND status='Running' AND worker_id=$2 AND fencing_token=$3 AND lease_until > now()`, op.ID, op.WorkerID, op.FencingToken)
	if err != nil {
		return err
	}
	if commandTag.RowsAffected() != 1 {
		return ErrLeaseLost
	}
	return tx.Commit(ctx)
}

func (s *Store) CompleteWorkspace(ctx context.Context, op domain.Operation) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `UPDATE workspaces SET bootstrap_state='Ready',updated_at=now() WHERE id=$1`, op.WorkspaceID); err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, `UPDATE operations SET status='Succeeded',completed_at=now(),lease_until=NULL,worker_id=NULL,error_code='',error_message='',updated_at=now() WHERE id=$1 AND kind='EnsureWorkspace' AND status='Running' AND worker_id=$2 AND fencing_token=$3 AND lease_until > now()`, op.ID, op.WorkerID, op.FencingToken)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return ErrLeaseLost
	}
	return tx.Commit(ctx)
}

func (s *Store) Fail(ctx context.Context, op domain.Operation, code, message string, retry bool) error {
	status := "Failed"
	if retry {
		status = "Pending"
	}
	command := `UPDATE operations SET status=$1,next_attempt_at=CASE WHEN $1='Pending' THEN now()+make_interval(secs => LEAST(300, power(2, attempts)::int)) ELSE next_attempt_at END,completed_at=CASE WHEN $1='Failed' THEN now() ELSE NULL END,lease_until=NULL,worker_id=NULL,error_code=$2,error_message=$3,updated_at=now() WHERE id=$4 AND status='Running' AND worker_id=$5 AND fencing_token=$6 AND lease_until > now()`
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
	if op.Kind == domain.OperationEnsureWorkspace {
		workspaceState := "Pending"
		if status == "Failed" {
			workspaceState = "Failed"
		}
		if _, err = s.Pool.Exec(ctx, `UPDATE workspaces SET bootstrap_state=$1,updated_at=now() WHERE id=$2`, workspaceState, op.WorkspaceID); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) ReleaseClaims(ctx context.Context, workerID string) error {
	_, err := s.Pool.Exec(ctx, `UPDATE operations SET status='Pending',next_attempt_at=now(),lease_until=NULL,worker_id=NULL,updated_at=now() WHERE status='Running' AND worker_id=$1`, workerID)
	return err
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
	err := tx.QueryRow(ctx, `SELECT d.id,d.public_id,d.workspace_id,d.project_id,p.public_id,d.app_id,a.public_id,d.environment_id,e.public_id,COALESCE(r.public_id,''),d.runtime_name,d.intent_json::text,d.desired_version,d.observed_version,d.observed_release,d.last_state,d.last_message,d.deletion_requested_at,d.deleted_at,d.created_at,d.updated_at FROM deployments d JOIN projects p ON p.id=d.project_id JOIN apps a ON a.id=d.app_id JOIN environments e ON e.id=d.environment_id LEFT JOIN releases r ON r.id=d.release_id WHERE d.id=$1`, id).Scan(&d.ID, &d.PublicID, &d.WorkspaceID, &d.ProjectID, &d.ProjectPublicID, &d.AppID, &d.AppPublicID, &d.EnvironmentID, &d.EnvironmentPublicID, &d.ReleasePublicID, &d.RuntimeName, &raw, &d.DesiredVersion, &d.ObservedVersion, &d.ObservedRelease, &d.State, &d.Message, &d.DeletionRequestedAt, &d.DeletedAt, &d.CreatedAt, &d.UpdatedAt)
	if err != nil {
		return d, err
	}
	err = jsonUnmarshal(raw, &d.Intent)
	return d, err
}

var ErrConflict = errors.New("conflict")

var ErrVersionConflict = fmt.Errorf("version conflict: %w", ErrConflict)

var ErrNameConflict = fmt.Errorf("name conflict: %w", ErrConflict)

var ErrDependencyConflict = fmt.Errorf("dependency conflict: %w", ErrConflict)

var ErrImmutableName = errors.New("deployment name is immutable")

var ErrImmutableHierarchy = errors.New("deployment hierarchy is immutable")

var ErrImmutableRelease = errors.New("deployment release image is immutable")

var ErrLeaseLost = errors.New("operation lease lost")

var ErrNotFound = errors.New("not found")

var ErrSessionInvalid = errors.New("session is no longer valid")

var ErrPublicIDCollision = errors.New("public ID collision")

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

func uniqueConstraint(err error) string {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.ConstraintName
	}
	return ""
}

func jsonUnmarshal(raw []byte, target any) error { return json.Unmarshal(raw, target) }
