package store

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"math"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/molejo-platform/molejo/services/control-plane-api/internal/domain"
	storesqlc "github.com/molejo-platform/molejo/services/control-plane-api/internal/store/sqlc"
)

func (s *Store) ListWorkspaces(ctx context.Context, userID, beforeID int64, limit int) ([]domain.Workspace, string, error) {
	if beforeID == 0 {
		beforeID = math.MaxInt64
	}
	rows, err := s.queries.ListWorkspacesForUser(ctx, storesqlc.ListWorkspacesForUserParams{UserID: userID, ID: beforeID, Limit: int32(limit + 1)})
	if err != nil {
		return nil, "", err
	}
	items := make([]domain.Workspace, 0, min(len(rows), limit))
	for index, row := range rows {
		if index == limit {
			return items, domain.EncodeCursor(items[len(items)-1].ID), nil
		}
		items = append(items, workspaceValue(row.ID, row.PublicID, row.Name, row.NamespaceName, row.Version, row.BootstrapState, row.CreatedAt, row.UpdatedAt))
	}
	return items, "", nil
}

func (s *Store) FindWorkspaceForUser(ctx context.Context, userID int64, publicID string) (domain.Workspace, error) {
	row, err := s.queries.FindWorkspaceForUser(ctx, storesqlc.FindWorkspaceForUserParams{UserID: userID, PublicID: publicID})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Workspace{}, ErrNotFound
	}
	return workspaceValue(row.ID, row.PublicID, row.Name, row.NamespaceName, row.Version, row.BootstrapState, row.CreatedAt, row.UpdatedAt), err
}

func (s *Store) CreateWorkspace(ctx context.Context, userID int64, publicID, operationID, name string, idempotencyHash, payloadHash []byte) (domain.Workspace, domain.Operation, bool, error) {
	clusterID, err := activeAgentInstallationID(ctx, s.Pool)
	if err != nil {
		return domain.Workspace{}, domain.Operation{}, false, err
	}
	return s.createWorkspaceForCluster(ctx, userID, clusterID, publicID, operationID, name, idempotencyHash, payloadHash)
}

func (s *Store) CreateWorkspaceOnCluster(ctx context.Context, userID int64, clusterPublicID, publicID, operationID, name string, idempotencyHash, payloadHash []byte) (domain.Workspace, domain.Operation, bool, error) {
	var clusterID int64
	if err := s.Pool.QueryRow(ctx, `SELECT id FROM agent_installations WHERE public_id=$1 AND status='Active'`, clusterPublicID).Scan(&clusterID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.Workspace{}, domain.Operation{}, false, ErrAgentUnavailable
		}
		return domain.Workspace{}, domain.Operation{}, false, err
	}
	return s.createWorkspaceForCluster(ctx, userID, clusterID, publicID, operationID, name, idempotencyHash, payloadHash)
}

func (s *Store) createWorkspaceForCluster(ctx context.Context, userID, clusterID int64, publicID, operationID, name string, idempotencyHash, payloadHash []byte) (domain.Workspace, domain.Operation, bool, error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return domain.Workspace{}, domain.Operation{}, false, err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, "workspace-create:"+hex.EncodeToString(idempotencyHash)); err != nil {
		return domain.Workspace{}, domain.Operation{}, false, err
	}
	queries := s.queries.WithTx(tx)
	existing, err := queries.FindWorkspaceOperationByIdempotency(ctx, storesqlc.FindWorkspaceOperationByIdempotencyParams{RequestedByUserID: userID, IdempotencyHash: idempotencyHash})
	if err == nil {
		if !bytes.Equal(existing.PayloadHash, payloadHash) {
			return domain.Workspace{}, domain.Operation{}, false, ErrConflict
		}
		workspace, getErr := queries.GetWorkspaceByID(ctx, existing.WorkspaceID)
		if getErr != nil {
			return domain.Workspace{}, domain.Operation{}, false, getErr
		}
		return workspaceValue(workspace.ID, workspace.PublicID, workspace.Name, workspace.NamespaceName, workspace.Version, workspace.BootstrapState, workspace.CreatedAt, workspace.UpdatedAt), operationValue(existing.ID, existing.PublicID, existing.WorkspaceID, existing.ActorID, existing.Kind, existing.Status, existing.DesiredVersion, existing.Attempts, existing.ErrorCode, existing.ErrorMessage, existing.CreatedAt, existing.UpdatedAt), true, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return domain.Workspace{}, domain.Operation{}, false, err
	}
	workspace, err := queries.InsertWorkspace(ctx, storesqlc.InsertWorkspaceParams{PublicID: publicID, Name: name, NamespaceName: publicID})
	if err != nil {
		return domain.Workspace{}, domain.Operation{}, false, hierarchyWriteError(err)
	}
	if _, err = tx.Exec(ctx, `INSERT INTO workspace_memberships(workspace_id,user_id,role,status) VALUES($1,$2,'Owner','Active')`, workspace.ID, userID); err != nil {
		return domain.Workspace{}, domain.Operation{}, false, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO workspace_clusters(workspace_id,installation_id,namespace_name) VALUES($1,$2,$3)`, workspace.ID, clusterID, workspace.NamespaceName); err != nil {
		return domain.Workspace{}, domain.Operation{}, false, err
	}
	operation, err := queries.InsertWorkspaceOperation(ctx, storesqlc.InsertWorkspaceOperationParams{
		PublicID: operationID, WorkspaceID: workspace.ID, RequestedByUserID: userID,
		IdempotencyHash: idempotencyHash, PayloadHash: payloadHash, AgentInstallationID: pgtype.Int8{Int64: clusterID, Valid: true},
	})
	if err != nil {
		return domain.Workspace{}, domain.Operation{}, false, hierarchyWriteError(err)
	}
	if err = tx.Commit(ctx); err != nil {
		return domain.Workspace{}, domain.Operation{}, false, err
	}
	return workspaceValue(workspace.ID, workspace.PublicID, workspace.Name, workspace.NamespaceName, workspace.Version, workspace.BootstrapState, workspace.CreatedAt, workspace.UpdatedAt), operationValue(operation.ID, operation.PublicID, operation.WorkspaceID, operation.ActorID, operation.Kind, operation.Status, operation.DesiredVersion, operation.Attempts, operation.ErrorCode, operation.ErrorMessage, operation.CreatedAt, operation.UpdatedAt), false, nil
}

func (s *Store) UpdateWorkspace(ctx context.Context, workspaceID, version int64, name string) (domain.Workspace, error) {
	row, err := s.queries.UpdateWorkspaceName(ctx, storesqlc.UpdateWorkspaceNameParams{ID: workspaceID, Version: version, Name: name})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Workspace{}, ErrVersionConflict
	}
	return workspaceValue(row.ID, row.PublicID, row.Name, row.NamespaceName, row.Version, row.BootstrapState, row.CreatedAt, row.UpdatedAt), err
}

func (s *Store) CreateProject(ctx context.Context, workspaceID int64, publicID, name, nameKey string) (domain.Project, error) {
	row, err := s.queries.CreateProject(ctx, storesqlc.CreateProjectParams{PublicID: publicID, WorkspaceID: workspaceID, Name: name, NameKey: nameKey})
	if err != nil {
		return domain.Project{}, hierarchyWriteError(err)
	}
	return projectValue(row.ID, row.PublicID, row.WorkspaceID, row.Name, row.Version, row.CreatedAt, row.UpdatedAt, row.ArchivedAt), nil
}

func (s *Store) FindProject(ctx context.Context, workspaceID int64, publicID string) (domain.Project, error) {
	row, err := s.queries.FindProject(ctx, storesqlc.FindProjectParams{WorkspaceID: workspaceID, PublicID: publicID})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Project{}, ErrNotFound
	}
	return projectValue(row.ID, row.PublicID, row.WorkspaceID, row.Name, row.Version, row.CreatedAt, row.UpdatedAt, row.ArchivedAt), err
}

func (s *Store) ListProjects(ctx context.Context, workspaceID, beforeID int64, limit int, includeArchived bool) ([]domain.Project, string, error) {
	if beforeID == 0 {
		beforeID = math.MaxInt64
	}
	rows, err := s.queries.ListProjects(ctx, storesqlc.ListProjectsParams{WorkspaceID: workspaceID, ID: beforeID, Limit: int32(limit + 1), Column4: includeArchived})
	if err != nil {
		return nil, "", err
	}
	items := make([]domain.Project, 0, min(len(rows), limit))
	for index, row := range rows {
		if index == limit {
			return items, domain.EncodeCursor(items[len(items)-1].ID), nil
		}
		items = append(items, projectValue(row.ID, row.PublicID, row.WorkspaceID, row.Name, row.Version, row.CreatedAt, row.UpdatedAt, row.ArchivedAt))
	}
	return items, "", nil
}

func (s *Store) UpdateProject(ctx context.Context, workspaceID int64, publicID string, version int64, name, nameKey string) (domain.Project, error) {
	if _, err := s.FindProject(ctx, workspaceID, publicID); err != nil {
		return domain.Project{}, err
	}
	row, err := s.queries.UpdateProject(ctx, storesqlc.UpdateProjectParams{WorkspaceID: workspaceID, PublicID: publicID, Version: version, Name: name, NameKey: nameKey})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Project{}, ErrVersionConflict
	}
	if err != nil {
		return domain.Project{}, hierarchyWriteError(err)
	}
	return projectValue(row.ID, row.PublicID, row.WorkspaceID, row.Name, row.Version, row.CreatedAt, row.UpdatedAt, row.ArchivedAt), nil
}

func (s *Store) ArchiveProject(ctx context.Context, workspaceID int64, publicID string, version int64) (domain.Project, error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return domain.Project{}, err
	}
	defer tx.Rollback(ctx)
	queries := s.queries.WithTx(tx)
	current, err := queries.FindActiveProjectForUpdate(ctx, storesqlc.FindActiveProjectForUpdateParams{WorkspaceID: workspaceID, PublicID: publicID})
	if errors.Is(err, pgx.ErrNoRows) {
		if _, findErr := queries.FindProject(ctx, storesqlc.FindProjectParams{WorkspaceID: workspaceID, PublicID: publicID}); errors.Is(findErr, pgx.ErrNoRows) {
			return domain.Project{}, ErrNotFound
		}
		return domain.Project{}, ErrVersionConflict
	}
	if err != nil {
		return domain.Project{}, err
	}
	if current.Version != version {
		return domain.Project{}, ErrVersionConflict
	}
	row, err := queries.ArchiveProject(ctx, storesqlc.ArchiveProjectParams{WorkspaceID: workspaceID, PublicID: publicID, Version: version})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Project{}, ErrDependencyConflict
	}
	if err != nil {
		return domain.Project{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return domain.Project{}, err
	}
	return projectValue(row.ID, row.PublicID, row.WorkspaceID, row.Name, row.Version, row.CreatedAt, row.UpdatedAt, row.ArchivedAt), nil
}

func (s *Store) CreateEnvironment(ctx context.Context, workspaceID int64, projectPublicID, publicID, name, nameKey string) (domain.Environment, error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return domain.Environment{}, err
	}
	defer tx.Rollback(ctx)
	queries := s.queries.WithTx(tx)
	project, err := queries.FindActiveProjectForUpdate(ctx, storesqlc.FindActiveProjectForUpdateParams{WorkspaceID: workspaceID, PublicID: projectPublicID})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Environment{}, ErrNotFound
	}
	if err != nil {
		return domain.Environment{}, err
	}
	row, err := queries.CreateEnvironment(ctx, storesqlc.CreateEnvironmentParams{PublicID: publicID, ProjectID: project.ID, Name: name, NameKey: nameKey})
	if err != nil {
		return domain.Environment{}, hierarchyWriteError(err)
	}
	if err = tx.Commit(ctx); err != nil {
		return domain.Environment{}, err
	}
	return environmentValue(row.ID, row.PublicID, row.ProjectID, row.Name, row.Version, row.CreatedAt, row.UpdatedAt, row.ArchivedAt), nil
}

func (s *Store) FindEnvironment(ctx context.Context, workspaceID int64, projectPublicID, publicID string) (domain.Environment, error) {
	row, err := s.queries.FindEnvironment(ctx, storesqlc.FindEnvironmentParams{WorkspaceID: workspaceID, PublicID: projectPublicID, PublicID_2: publicID})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Environment{}, ErrNotFound
	}
	return environmentValue(row.ID, row.PublicID, row.ProjectID, row.Name, row.Version, row.CreatedAt, row.UpdatedAt, row.ArchivedAt), err
}

func (s *Store) ListEnvironments(ctx context.Context, workspaceID int64, projectPublicID string, beforeID int64, limit int, includeArchived bool) ([]domain.Environment, string, error) {
	if beforeID == 0 {
		beforeID = math.MaxInt64
	}
	rows, err := s.queries.ListEnvironments(ctx, storesqlc.ListEnvironmentsParams{WorkspaceID: workspaceID, PublicID: projectPublicID, ID: beforeID, Limit: int32(limit + 1), Column5: includeArchived})
	if err != nil {
		return nil, "", err
	}
	items := make([]domain.Environment, 0, min(len(rows), limit))
	for index, row := range rows {
		if index == limit {
			return items, domain.EncodeCursor(items[len(items)-1].ID), nil
		}
		items = append(items, environmentValue(row.ID, row.PublicID, row.ProjectID, row.Name, row.Version, row.CreatedAt, row.UpdatedAt, row.ArchivedAt))
	}
	return items, "", nil
}

func (s *Store) UpdateEnvironment(ctx context.Context, workspaceID int64, projectPublicID, publicID string, version int64, name, nameKey string) (domain.Environment, error) {
	if _, err := s.FindEnvironment(ctx, workspaceID, projectPublicID, publicID); err != nil {
		return domain.Environment{}, err
	}
	row, err := s.queries.UpdateEnvironment(ctx, storesqlc.UpdateEnvironmentParams{WorkspaceID: workspaceID, PublicID: projectPublicID, PublicID_2: publicID, Version: version, Name: name, NameKey: nameKey})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Environment{}, ErrVersionConflict
	}
	if err != nil {
		return domain.Environment{}, hierarchyWriteError(err)
	}
	return environmentValue(row.ID, row.PublicID, row.ProjectID, row.Name, row.Version, row.CreatedAt, row.UpdatedAt, row.ArchivedAt), nil
}

func (s *Store) ArchiveEnvironment(ctx context.Context, workspaceID int64, projectPublicID, publicID string, version int64) (domain.Environment, error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return domain.Environment{}, err
	}
	defer tx.Rollback(ctx)
	queries := s.queries.WithTx(tx)
	current, err := queries.FindActiveEnvironmentForUpdate(ctx, storesqlc.FindActiveEnvironmentForUpdateParams{WorkspaceID: workspaceID, PublicID: projectPublicID, PublicID_2: publicID})
	if errors.Is(err, pgx.ErrNoRows) {
		if _, findErr := queries.FindEnvironment(ctx, storesqlc.FindEnvironmentParams{WorkspaceID: workspaceID, PublicID: projectPublicID, PublicID_2: publicID}); errors.Is(findErr, pgx.ErrNoRows) {
			return domain.Environment{}, ErrNotFound
		}
		return domain.Environment{}, ErrVersionConflict
	}
	if err != nil {
		return domain.Environment{}, err
	}
	if current.Version != version {
		return domain.Environment{}, ErrVersionConflict
	}
	row, err := queries.ArchiveEnvironment(ctx, storesqlc.ArchiveEnvironmentParams{WorkspaceID: workspaceID, PublicID: projectPublicID, PublicID_2: publicID, Version: version})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Environment{}, ErrDependencyConflict
	}
	if err != nil {
		return domain.Environment{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return domain.Environment{}, err
	}
	return environmentValue(row.ID, row.PublicID, row.ProjectID, row.Name, row.Version, row.CreatedAt, row.UpdatedAt, row.ArchivedAt), nil
}

func (s *Store) CreateApp(ctx context.Context, workspaceID int64, projectPublicID, publicID, name, nameKey string) (domain.App, error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return domain.App{}, err
	}
	defer tx.Rollback(ctx)
	queries := s.queries.WithTx(tx)
	project, err := queries.FindActiveProjectForUpdate(ctx, storesqlc.FindActiveProjectForUpdateParams{WorkspaceID: workspaceID, PublicID: projectPublicID})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.App{}, ErrNotFound
	}
	if err != nil {
		return domain.App{}, err
	}
	row, err := queries.CreateApp(ctx, storesqlc.CreateAppParams{PublicID: publicID, ProjectID: project.ID, Name: name, NameKey: nameKey})
	if err != nil {
		return domain.App{}, hierarchyWriteError(err)
	}
	if err = tx.Commit(ctx); err != nil {
		return domain.App{}, err
	}
	return appValue(row.ID, row.PublicID, row.ProjectID, row.Name, row.Version, row.CreatedAt, row.UpdatedAt, row.ArchivedAt), nil
}

func (s *Store) FindApp(ctx context.Context, workspaceID int64, projectPublicID, publicID string) (domain.App, error) {
	row, err := s.queries.FindApp(ctx, storesqlc.FindAppParams{WorkspaceID: workspaceID, PublicID: projectPublicID, PublicID_2: publicID})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.App{}, ErrNotFound
	}
	return appValue(row.ID, row.PublicID, row.ProjectID, row.Name, row.Version, row.CreatedAt, row.UpdatedAt, row.ArchivedAt), err
}

func (s *Store) ListApps(ctx context.Context, workspaceID int64, projectPublicID string, beforeID int64, limit int, includeArchived bool) ([]domain.App, string, error) {
	if beforeID == 0 {
		beforeID = math.MaxInt64
	}
	rows, err := s.queries.ListApps(ctx, storesqlc.ListAppsParams{WorkspaceID: workspaceID, PublicID: projectPublicID, ID: beforeID, Limit: int32(limit + 1), Column5: includeArchived})
	if err != nil {
		return nil, "", err
	}
	items := make([]domain.App, 0, min(len(rows), limit))
	for index, row := range rows {
		if index == limit {
			return items, domain.EncodeCursor(items[len(items)-1].ID), nil
		}
		items = append(items, appValue(row.ID, row.PublicID, row.ProjectID, row.Name, row.Version, row.CreatedAt, row.UpdatedAt, row.ArchivedAt))
	}
	return items, "", nil
}

func (s *Store) UpdateApp(ctx context.Context, workspaceID int64, projectPublicID, publicID string, version int64, name, nameKey string) (domain.App, error) {
	if _, err := s.FindApp(ctx, workspaceID, projectPublicID, publicID); err != nil {
		return domain.App{}, err
	}
	row, err := s.queries.UpdateApp(ctx, storesqlc.UpdateAppParams{WorkspaceID: workspaceID, PublicID: projectPublicID, PublicID_2: publicID, Version: version, Name: name, NameKey: nameKey})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.App{}, ErrVersionConflict
	}
	if err != nil {
		return domain.App{}, hierarchyWriteError(err)
	}
	return appValue(row.ID, row.PublicID, row.ProjectID, row.Name, row.Version, row.CreatedAt, row.UpdatedAt, row.ArchivedAt), nil
}

func (s *Store) ArchiveApp(ctx context.Context, workspaceID int64, projectPublicID, publicID string, version int64) (domain.App, error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return domain.App{}, err
	}
	defer tx.Rollback(ctx)
	queries := s.queries.WithTx(tx)
	current, err := queries.FindActiveAppForUpdate(ctx, storesqlc.FindActiveAppForUpdateParams{WorkspaceID: workspaceID, PublicID: projectPublicID, PublicID_2: publicID})
	if errors.Is(err, pgx.ErrNoRows) {
		if _, findErr := queries.FindApp(ctx, storesqlc.FindAppParams{WorkspaceID: workspaceID, PublicID: projectPublicID, PublicID_2: publicID}); errors.Is(findErr, pgx.ErrNoRows) {
			return domain.App{}, ErrNotFound
		}
		return domain.App{}, ErrVersionConflict
	}
	if err != nil {
		return domain.App{}, err
	}
	if current.Version != version {
		return domain.App{}, ErrVersionConflict
	}
	row, err := queries.ArchiveApp(ctx, storesqlc.ArchiveAppParams{WorkspaceID: workspaceID, PublicID: projectPublicID, PublicID_2: publicID, Version: version})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.App{}, ErrDependencyConflict
	}
	if err != nil {
		return domain.App{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return domain.App{}, err
	}
	return appValue(row.ID, row.PublicID, row.ProjectID, row.Name, row.Version, row.CreatedAt, row.UpdatedAt, row.ArchivedAt), nil
}

func workspaceValue(id int64, publicID, name, namespace string, version int64, state string, createdAt, updatedAt pgtype.Timestamptz) domain.Workspace {
	return domain.Workspace{ID: id, PublicID: publicID, Name: name, Namespace: namespace, Version: version, BootstrapState: state, CreatedAt: createdAt.Time, UpdatedAt: updatedAt.Time}
}

func projectValue(id int64, publicID string, workspaceID int64, name string, version int64, createdAt, updatedAt, archivedAt pgtype.Timestamptz) domain.Project {
	return domain.Project{ID: id, PublicID: publicID, WorkspaceID: workspaceID, Name: name, Version: version, CreatedAt: createdAt.Time, UpdatedAt: updatedAt.Time, ArchivedAt: optionalTime(archivedAt)}
}

func environmentValue(id int64, publicID string, projectID int64, name string, version int64, createdAt, updatedAt, archivedAt pgtype.Timestamptz) domain.Environment {
	return domain.Environment{ID: id, PublicID: publicID, ProjectID: projectID, Name: name, Version: version, CreatedAt: createdAt.Time, UpdatedAt: updatedAt.Time, ArchivedAt: optionalTime(archivedAt)}
}

func appValue(id int64, publicID string, projectID int64, name string, version int64, createdAt, updatedAt, archivedAt pgtype.Timestamptz) domain.App {
	return domain.App{ID: id, PublicID: publicID, ProjectID: projectID, Name: name, Version: version, CreatedAt: createdAt.Time, UpdatedAt: updatedAt.Time, ArchivedAt: optionalTime(archivedAt)}
}

func operationValue(id int64, publicID string, workspaceID, actorID int64, kind, status string, desiredVersion int64, attempts int32, errorCode, errorMessage string, createdAt, updatedAt pgtype.Timestamptz) domain.Operation {
	return domain.Operation{ID: id, PublicID: publicID, WorkspaceID: workspaceID, ActorID: actorID, Kind: kind, Status: status, DesiredVersion: desiredVersion, Attempts: int(attempts), ErrorCode: errorCode, ErrorMessage: errorMessage, CreatedAt: createdAt.Time, UpdatedAt: updatedAt.Time}
}

func optionalTime(value pgtype.Timestamptz) *time.Time {
	if !value.Valid {
		return nil
	}
	result := value.Time
	return &result
}

func hierarchyWriteError(err error) error {
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) && postgresError.Code == "23505" {
		if postgresError.ConstraintName == "projects_public_id_key" || postgresError.ConstraintName == "environments_public_id_key" || postgresError.ConstraintName == "apps_public_id_key" || postgresError.ConstraintName == "workspaces_public_id_key" || postgresError.ConstraintName == "operations_public_id_key" || postgresError.ConstraintName == "github_installations_public_id_key" {
			return ErrPublicIDCollision
		}
		return ErrNameConflict
	}
	return err
}
