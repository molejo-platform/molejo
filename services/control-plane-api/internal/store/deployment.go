package store

import (
	"context"
	"errors"
	"fmt"
	"math"

	"github.com/jackc/pgx/v5"

	"github.com/molejo-platform/molejo/services/control-plane-api/internal/audit"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/domain"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/principal"
)

const deploymentSelectColumns = `d.id,d.public_id,d.workspace_id,d.app_environment_id,d.app_id,ae.public_id,r.public_id,r.image,
		d.configuration_version,d.configuration_json::text,d.workload_kind,COALESCE(av.public_id,''),
		p.public_id,p.kind,p.display_name,d.status,d.message,d.observed_release,d.created_at,d.updated_at`

const deploymentJoins = `JOIN app_environments ae ON ae.id=d.app_environment_id
		JOIN releases r ON r.id=d.release_id
		JOIN principals p ON p.id=d.requested_by_principal_id
		LEFT JOIN app_volumes av ON av.id=d.app_volume_id`

func (s *Store) CreateDeployment(ctx context.Context, userID int64, request domain.DeploymentRequest, event audit.Event) (domain.Deployment, domain.Operation, bool, error) {
	actor, err := s.principalForUser(ctx, userID)
	if err != nil {
		return domain.Deployment{}, domain.Operation{}, false, err
	}
	return s.createDeployment(ctx, &userID, actor, request, event)
}

func (s *Store) CreateDeploymentForPrincipal(ctx context.Context, actor principal.Principal, request domain.DeploymentRequest, event audit.Event) (domain.Deployment, domain.Operation, bool, error) {
	return s.createDeployment(ctx, nil, actor, request, event)
}

func (s *Store) createDeployment(ctx context.Context, actorUserID *int64, actor principal.Principal, request domain.DeploymentRequest, event audit.Event) (domain.Deployment, domain.Operation, bool, error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return domain.Deployment{}, domain.Operation{}, false, err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, "deployment:"+fmt.Sprintf("%x", request.IdempotencyHash)); err != nil {
		return domain.Deployment{}, domain.Operation{}, false, err
	}
	if existing, found, findErr := operationByPrincipalIdempotency(ctx, tx, request.WorkspaceID, actor.ID, request.IdempotencyHash, request.PayloadHash); findErr != nil {
		return domain.Deployment{}, domain.Operation{}, false, findErr
	} else if found {
		deployment, deploymentErr := deploymentByID(ctx, tx, existing.DeploymentID)
		if deploymentErr != nil {
			return domain.Deployment{}, domain.Operation{}, false, deploymentErr
		}
		if err = tx.Commit(ctx); err != nil {
			return domain.Deployment{}, domain.Operation{}, false, err
		}
		return deployment, existing, true, nil
	}
	appEnvironment, err := appEnvironmentForUpdate(ctx, tx, request.WorkspaceID, request.AppEnvironmentPublicID)
	if err != nil {
		return domain.Deployment{}, domain.Operation{}, false, err
	}
	if appEnvironment.Version != request.ExpectedVersion || appEnvironment.CurrentDeploymentPublicID != request.ExpectedCurrentDeploymentPublicID {
		return domain.Deployment{}, domain.Operation{}, false, ErrVersionConflict
	}
	var releaseID int64
	if err = tx.QueryRow(ctx, `SELECT r.id FROM releases r WHERE r.public_id=$1 AND r.workspace_id=$2 AND r.app_id=$3 AND r.availability_status='Available'`, request.ReleasePublicID, request.WorkspaceID, appEnvironment.AppID).Scan(&releaseID); errors.Is(err, pgx.ErrNoRows) {
		return domain.Deployment{}, domain.Operation{}, false, ErrNotFound
	} else if err != nil {
		return domain.Deployment{}, domain.Operation{}, false, err
	}
	var active bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM operations WHERE app_environment_id=$1 AND status IN ('Pending','Running'))`, appEnvironment.ID).Scan(&active); err != nil {
		return domain.Deployment{}, domain.Operation{}, false, err
	}
	if active || appEnvironment.DeletionRequestedAt != nil {
		return domain.Deployment{}, domain.Operation{}, false, ErrConflict
	}
	revision, err := configurationRevision(ctx, tx, request.WorkspaceID, appEnvironment.ID, request.ConfigurationVersion)
	if err != nil {
		return domain.Deployment{}, domain.Operation{}, false, err
	}
	revision.Configuration = inheritAllocatedPorts(revision.Configuration, appEnvironment.Configuration)
	revision.Configuration, err = s.reservePublicationClaims(ctx, tx, appEnvironment.ID, revision.Version, appEnvironment.WorkloadKind, revision.Configuration)
	if err != nil {
		return domain.Deployment{}, domain.Operation{}, false, err
	}
	configurationJSON, err := domain.CanonicalJSON(revision.Configuration)
	if err != nil {
		return domain.Deployment{}, domain.Operation{}, false, err
	}
	var deployment domain.Deployment
	var appVolumeID int64
	var appVolumePublicID string
	if appEnvironment.WorkloadKind == domain.WorkloadStateful {
		if err = tx.QueryRow(ctx, `SELECT id,public_id FROM app_volumes WHERE app_environment_id=$1 AND desired_state='Ready' AND observed_state IN ('Provisioning','Ready') AND deletion_requested_at IS NULL`, appEnvironment.ID).Scan(&appVolumeID, &appVolumePublicID); errors.Is(err, pgx.ErrNoRows) {
			return domain.Deployment{}, domain.Operation{}, false, ErrConflict
		} else if err != nil {
			return domain.Deployment{}, domain.Operation{}, false, err
		}
	}
	err = tx.QueryRow(ctx, `
		INSERT INTO deployments(public_id,workspace_id,app_environment_id,app_id,release_id,requested_by_user_id,requested_by_principal_id,configuration_version,configuration_json,workload_kind,app_volume_id)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,NULLIF($11,0))
		RETURNING id,public_id,workspace_id,app_environment_id,app_id,configuration_version,configuration_json::text,workload_kind,status,message,observed_release,created_at,updated_at`,
		request.DeploymentPublicID, request.WorkspaceID, appEnvironment.ID, appEnvironment.AppID, releaseID, actorUserID, actor.ID, revision.Version, configurationJSON, appEnvironment.WorkloadKind, appVolumeID).
		Scan(&deployment.ID, &deployment.PublicID, &deployment.WorkspaceID, &deployment.AppEnvironmentID, &deployment.AppID,
			&deployment.ConfigurationVersion, &configurationJSON, &deployment.WorkloadKind, &deployment.State, &deployment.Message, &deployment.ObservedRelease,
			&deployment.CreatedAt, &deployment.UpdatedAt)
	if uniqueConstraint(err) == "deployments_public_id_key" {
		return domain.Deployment{}, domain.Operation{}, false, ErrPublicIDCollision
	}
	if err != nil {
		return domain.Deployment{}, domain.Operation{}, false, translateDBError(err)
	}
	deployment.AppEnvironmentPublicID = appEnvironment.PublicID
	deployment.ReleasePublicID = request.ReleasePublicID
	deployment.Configuration = revision.Configuration
	deployment.RequestedBy = domain.ActorReference{ID: actor.PublicID, Kind: actor.Kind, DisplayName: actor.DisplayName}
	deployment.AppVolumePublicID = appVolumePublicID
	var desiredVersion int64
	if err = tx.QueryRow(ctx, `UPDATE app_environments SET desired_deployment_id=$1,last_state='Progressing',last_message='',version=version+1,updated_at=now() WHERE id=$2 RETURNING version`, deployment.ID, appEnvironment.ID).Scan(&desiredVersion); err != nil {
		return domain.Deployment{}, domain.Operation{}, false, err
	}
	operation, err := insertOperationForPrincipal(ctx, tx, request.WorkspaceID, appEnvironment.ID, deployment.ID, actorUserID, actor.ID, domain.OperationApplyDeployment, request.IdempotencyHash, request.PayloadHash, desiredVersion)
	if err != nil {
		return domain.Deployment{}, domain.Operation{}, false, translateDBError(err)
	}
	operation.AppEnvironmentPublicID = appEnvironment.PublicID
	operation.DeploymentPublicID = deployment.PublicID
	event.ActorUserID = actorUserID
	event.ActorPrincipalID = &actor.ID
	event.WorkspaceID = &request.WorkspaceID
	event.TargetPublicID = request.DeploymentPublicID
	if err = insertAudit(ctx, tx, event); err != nil {
		return domain.Deployment{}, domain.Operation{}, false, err
	}
	if err = tx.Commit(ctx); err != nil {
		return domain.Deployment{}, domain.Operation{}, false, err
	}
	return deployment, operation, false, nil
}

func (s *Store) ListDeployments(ctx context.Context, workspaceID, appEnvironmentID, beforeID int64, limit int) ([]domain.Deployment, string, error) {
	if beforeID == 0 {
		beforeID = math.MaxInt64
	}
	rows, err := s.Pool.Query(ctx, `SELECT `+deploymentSelectColumns+` FROM deployments d `+deploymentJoins+`
		WHERE d.workspace_id=$1 AND d.app_environment_id=$2 AND d.id < $3 ORDER BY d.id DESC LIMIT $4`, workspaceID, appEnvironmentID, beforeID, limit+1)
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()
	items := []domain.Deployment{}
	for rows.Next() {
		item, scanErr := scanDeployment(rows)
		if scanErr != nil {
			return nil, "", scanErr
		}
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		return nil, "", err
	}
	if len(items) > limit {
		items = items[:limit]
		return items, domain.EncodeCursor(items[len(items)-1].ID), nil
	}
	return items, "", nil
}

func (s *Store) FindDeployment(ctx context.Context, workspaceID, appEnvironmentID int64, publicID string) (domain.Deployment, error) {
	item, err := scanDeployment(s.Pool.QueryRow(ctx, `SELECT `+deploymentSelectColumns+` FROM deployments d `+deploymentJoins+`
		WHERE d.workspace_id=$1 AND d.app_environment_id=$2 AND d.public_id=$3`, workspaceID, appEnvironmentID, publicID))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Deployment{}, ErrNotFound
	}
	return item, err
}

func (s *Store) PreviewDeployment(ctx context.Context, workspaceID, appEnvironmentID int64, releasePublicID string, configurationVersion int64) (domain.DeploymentPreview, error) {
	var releaseAppID int64
	if err := s.Pool.QueryRow(ctx, `SELECT app_id FROM releases WHERE workspace_id=$1 AND public_id=$2`, workspaceID, releasePublicID).Scan(&releaseAppID); errors.Is(err, pgx.ErrNoRows) {
		return domain.DeploymentPreview{}, ErrNotFound
	} else if err != nil {
		return domain.DeploymentPreview{}, err
	}
	var appID, currentDeploymentID int64
	if err := s.Pool.QueryRow(ctx, `SELECT app_id,COALESCE(current_deployment_id,0) FROM app_environments WHERE workspace_id=$1 AND id=$2 AND archived_at IS NULL`, workspaceID, appEnvironmentID).Scan(&appID, &currentDeploymentID); errors.Is(err, pgx.ErrNoRows) {
		return domain.DeploymentPreview{}, ErrNotFound
	} else if err != nil {
		return domain.DeploymentPreview{}, err
	}
	if releaseAppID != appID {
		return domain.DeploymentPreview{}, ErrNotFound
	}
	revision, err := configurationRevision(ctx, s.Pool, workspaceID, appEnvironmentID, configurationVersion)
	if err != nil {
		return domain.DeploymentPreview{}, err
	}
	var current *domain.Deployment
	if currentDeploymentID != 0 {
		item, findErr := deploymentByID(ctx, s.Pool, currentDeploymentID)
		if findErr != nil {
			return domain.DeploymentPreview{}, findErr
		}
		current = &item
	}
	return domain.PreviewDeployment(current, releasePublicID, revision), nil
}

func scanDeployment(row pgx.Row) (domain.Deployment, error) {
	var item domain.Deployment
	var configuration []byte
	err := row.Scan(&item.ID, &item.PublicID, &item.WorkspaceID, &item.AppEnvironmentID, &item.AppID,
		&item.AppEnvironmentPublicID, &item.ReleasePublicID, &item.Image, &item.ConfigurationVersion, &configuration, &item.WorkloadKind, &item.AppVolumePublicID,
		&item.RequestedBy.ID, &item.RequestedBy.Kind, &item.RequestedBy.DisplayName,
		&item.State, &item.Message, &item.ObservedRelease, &item.CreatedAt, &item.UpdatedAt)
	if err != nil {
		return domain.Deployment{}, err
	}
	if err = jsonUnmarshal(configuration, &item.Configuration); err != nil {
		return domain.Deployment{}, err
	}
	item.Configuration = domain.NormalizeRuntimeConfig(item.Configuration)
	return item, nil
}

func deploymentByID(ctx context.Context, query rowQuerier, id int64) (domain.Deployment, error) {
	return scanDeployment(query.QueryRow(ctx, `SELECT `+deploymentSelectColumns+` FROM deployments d `+deploymentJoins+` WHERE d.id=$1`, id))
}

func (s *Store) principalForUser(ctx context.Context, userID int64) (principal.Principal, error) {
	var value principal.Principal
	err := s.Pool.QueryRow(ctx, `SELECT p.id,p.public_id,p.kind,p.display_name FROM users u JOIN principals p ON p.id=u.principal_id WHERE u.id=$1`, userID).
		Scan(&value.ID, &value.PublicID, &value.Kind, &value.DisplayName)
	return value, err
}
