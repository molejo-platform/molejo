package store

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/fruto-platform/fruto/services/control-plane-api/internal/domain"
	"github.com/jackc/pgx/v5"
)

type rowQuerier interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

const appEnvironmentColumns = `
	ae.id,ae.public_id,ae.workspace_id,ae.project_id,p.public_id,ae.app_id,a.public_id,a.name,
	ae.environment_id,e.public_id,e.name,ae.source_branch,ae.runtime_name,
	ae.workload_kind,ae.configuration_json::text,ae.configuration_version,ae.version,
	COALESCE(dd.public_id,''),COALESCE(cd.public_id,''),COALESCE(cr.public_id,''),
	COALESCE(dd.configuration_version,0),COALESCE(cd.configuration_version,0),
	ae.last_state,ae.last_message,ae.deletion_requested_at,ae.archived_at,ae.created_at,ae.updated_at`

const appEnvironmentJoins = `
	JOIN projects p ON p.id=ae.project_id
	JOIN apps a ON a.id=ae.app_id
	JOIN environments e ON e.id=ae.environment_id
	LEFT JOIN deployments dd ON dd.id=ae.desired_deployment_id
	LEFT JOIN deployments cd ON cd.id=ae.current_deployment_id
	LEFT JOIN releases cr ON cr.id=ae.current_release_id`

func scanAppEnvironment(row pgx.Row) (domain.AppEnvironment, error) {
	var item domain.AppEnvironment
	var configuration []byte
	err := row.Scan(
		&item.ID, &item.PublicID, &item.WorkspaceID, &item.ProjectID, &item.ProjectPublicID,
		&item.AppID, &item.AppPublicID, &item.AppName, &item.EnvironmentID, &item.EnvironmentPublicID,
		&item.EnvironmentName, &item.SourceBranch, &item.RuntimeName, &item.WorkloadKind, &configuration,
		&item.ConfigurationVersion, &item.Version, &item.DesiredDeploymentPublicID,
		&item.CurrentDeploymentPublicID, &item.CurrentReleasePublicID, &item.DesiredConfigurationVersion,
		&item.CurrentConfigurationVersion, &item.State, &item.Message,
		&item.DeletionRequestedAt, &item.ArchivedAt, &item.CreatedAt, &item.UpdatedAt,
	)
	if err != nil {
		return domain.AppEnvironment{}, err
	}
	if err = jsonUnmarshal(configuration, &item.Configuration); err != nil {
		return domain.AppEnvironment{}, err
	}
	item.Configuration = domain.NormalizeRuntimeConfig(item.Configuration)
	return item, nil
}

func (s *Store) CreateAppEnvironment(ctx context.Context, workspaceID, actorID int64, publicID, projectPublicID, appPublicID, environmentPublicID, branch string, configuration domain.RuntimeConfig) (domain.AppEnvironment, error) {
	item, _, err := s.CreateAppEnvironmentWithWorkload(ctx, workspaceID, actorID, publicID, projectPublicID, appPublicID, environmentPublicID, branch, domain.WorkloadStateless, configuration, nil)
	return item, err
}

func (s *Store) CreateAppEnvironmentWithWorkload(ctx context.Context, workspaceID, actorID int64, publicID, projectPublicID, appPublicID, environmentPublicID, branch string, workloadKind domain.WorkloadKind, configuration domain.RuntimeConfig, volumeRequest *domain.VolumeRequest) (domain.AppEnvironment, *domain.AppVolume, error) {
	configuration = domain.NormalizeRuntimeConfig(configuration)
	if err := domain.ValidateWorkloadConfiguration(workloadKind, configuration, volumeRequest); err != nil {
		return domain.AppEnvironment{}, nil, err
	}
	configurationJSON, err := domain.CanonicalJSON(configuration)
	if err != nil {
		return domain.AppEnvironment{}, nil, err
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return domain.AppEnvironment{}, nil, err
	}
	defer tx.Rollback(ctx)
	query := `WITH inserted AS (
		INSERT INTO app_environments(public_id,workspace_id,project_id,app_id,environment_id,source_branch,runtime_name,workload_kind,configuration_json)
		SELECT $1,p.workspace_id,p.id,a.id,e.id,$6,$7,$8,$9
		FROM projects p
		JOIN apps a ON a.project_id=p.id
		JOIN environments e ON e.project_id=p.id
		WHERE p.workspace_id=$2 AND p.public_id=$3 AND a.public_id=$4 AND e.public_id=$5
		  AND p.archived_at IS NULL AND a.archived_at IS NULL AND e.archived_at IS NULL
		RETURNING *
	) SELECT ` + appEnvironmentColumns + ` FROM inserted ae ` + appEnvironmentJoins
	item, err := scanAppEnvironment(tx.QueryRow(ctx, query, publicID, workspaceID, projectPublicID, appPublicID, environmentPublicID, branch, domain.RuntimeName(publicID), workloadKind, configurationJSON))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.AppEnvironment{}, nil, ErrNotFound
	}
	if uniqueConstraint(err) == "app_environments_public_id_key" {
		return domain.AppEnvironment{}, nil, ErrPublicIDCollision
	}
	if err != nil {
		return domain.AppEnvironment{}, nil, translateDBError(err)
	}
	configuration, err = s.reservePublicationClaims(ctx, tx, item.ID, item.ConfigurationVersion, configuration)
	if err != nil {
		return domain.AppEnvironment{}, nil, err
	}
	configurationJSON, err = domain.CanonicalJSON(configuration)
	if err != nil {
		return domain.AppEnvironment{}, nil, err
	}
	if _, err = tx.Exec(ctx, `UPDATE app_environments SET configuration_json=$2 WHERE id=$1`, item.ID, configurationJSON); err != nil {
		return domain.AppEnvironment{}, nil, err
	}
	item.Configuration = configuration
	if err = replaceParameterBindings(ctx, tx, item.ID, workspaceID, configuration.Parameters); err != nil {
		return domain.AppEnvironment{}, nil, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO app_environment_configuration_revisions(app_environment_id,version,configuration_json,created_by_user_id) VALUES($1,$2,$3,$4)`, item.ID, item.ConfigurationVersion, configurationJSON, actorID); err != nil {
		return domain.AppEnvironment{}, nil, err
	}
	var volume *domain.AppVolume
	if workloadKind == domain.WorkloadStateful {
		created, createErr := createAppVolume(ctx, tx, workspaceID, actorID, item, *volumeRequest)
		if createErr != nil {
			return domain.AppEnvironment{}, nil, createErr
		}
		volume = &created
		item.State = domain.StateProgressing
		item.Message = "persistent storage is being prepared"
	}
	return item, volume, tx.Commit(ctx)
}

func (s *Store) UpdateAppEnvironment(ctx context.Context, workspaceID, actorID int64, publicID, branch string, configuration domain.RuntimeConfig, version int64) (domain.AppEnvironment, error) {
	configuration = domain.NormalizeRuntimeConfig(configuration)
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return domain.AppEnvironment{}, err
	}
	defer tx.Rollback(ctx)
	current, err := appEnvironmentForUpdate(ctx, tx, workspaceID, publicID)
	if err != nil {
		return domain.AppEnvironment{}, err
	}
	if current.Version != version || current.DeletionRequestedAt != nil {
		return domain.AppEnvironment{}, ErrVersionConflict
	}
	configuration = inheritAllocatedPorts(configuration, current.Configuration)
	configurationJSON, err := domain.CanonicalJSON(configuration)
	if err != nil {
		return domain.AppEnvironment{}, err
	}
	currentJSON, err := domain.CanonicalJSON(current.Configuration)
	if err != nil {
		return domain.AppEnvironment{}, err
	}
	configurationChanged := !bytes.Equal(currentJSON, configurationJSON)
	branchChanged := current.SourceBranch != branch
	if !configurationChanged && !branchChanged {
		return current, tx.Commit(ctx)
	}
	query := `WITH updated AS (
		UPDATE app_environments SET source_branch=$3,configuration_json=$4,
		  configuration_version=configuration_version+$5,version=version+1,updated_at=now()
		WHERE workspace_id=$1 AND public_id=$2 AND version=$6
		  AND archived_at IS NULL AND deletion_requested_at IS NULL
		RETURNING *
	) SELECT ` + appEnvironmentColumns + ` FROM updated ae ` + appEnvironmentJoins
	configurationIncrement := 0
	if configurationChanged {
		configurationIncrement = 1
	}
	item, err := scanAppEnvironment(tx.QueryRow(ctx, query, workspaceID, publicID, branch, configurationJSON, configurationIncrement, version))
	if errors.Is(err, pgx.ErrNoRows) {
		var exists bool
		if findErr := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM app_environments WHERE workspace_id=$1 AND public_id=$2 AND archived_at IS NULL)`, workspaceID, publicID).Scan(&exists); findErr != nil {
			return domain.AppEnvironment{}, findErr
		}
		if !exists {
			return domain.AppEnvironment{}, ErrNotFound
		}
		return domain.AppEnvironment{}, ErrVersionConflict
	}
	if err != nil {
		return domain.AppEnvironment{}, translateDBError(err)
	}
	if configurationChanged {
		configuration, err = s.reservePublicationClaims(ctx, tx, item.ID, item.ConfigurationVersion, configuration)
		if err != nil {
			return domain.AppEnvironment{}, err
		}
		configurationJSON, err = domain.CanonicalJSON(configuration)
		if err != nil {
			return domain.AppEnvironment{}, err
		}
		if _, err = tx.Exec(ctx, `UPDATE app_environments SET configuration_json=$2 WHERE id=$1`, item.ID, configurationJSON); err != nil {
			return domain.AppEnvironment{}, err
		}
		item.Configuration = configuration
		if err = replaceParameterBindings(ctx, tx, item.ID, workspaceID, configuration.Parameters); err != nil {
			return domain.AppEnvironment{}, err
		}
		if _, err = tx.Exec(ctx, `INSERT INTO app_environment_configuration_revisions(app_environment_id,version,configuration_json,created_by_user_id) VALUES($1,$2,$3,$4)`, item.ID, item.ConfigurationVersion, configurationJSON, actorID); err != nil {
			return domain.AppEnvironment{}, err
		}
	}
	return item, tx.Commit(ctx)
}

func (s *Store) ListConfigurationRevisions(ctx context.Context, workspaceID, appEnvironmentID, beforeVersion int64, limit int) ([]domain.ConfigurationRevision, string, error) {
	if beforeVersion == 0 {
		beforeVersion = math.MaxInt64
	}
	rows, err := s.Pool.Query(ctx, `SELECT r.app_environment_id,ae.public_id,r.version,r.configuration_json::text,COALESCE(u.username,'system'),r.created_at
		FROM app_environment_configuration_revisions r
		JOIN app_environments ae ON ae.id=r.app_environment_id
		LEFT JOIN users u ON u.id=r.created_by_user_id
		WHERE ae.workspace_id=$1 AND r.app_environment_id=$2 AND r.version < $3 ORDER BY r.version DESC LIMIT $4`, workspaceID, appEnvironmentID, beforeVersion, limit+1)
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()
	items := []domain.ConfigurationRevision{}
	for rows.Next() {
		item, scanErr := scanConfigurationRevision(rows)
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
		return items, domain.EncodeCursor(items[len(items)-1].Version), nil
	}
	return items, "", nil
}

func configurationRevision(ctx context.Context, query rowQuerier, workspaceID, appEnvironmentID, version int64) (domain.ConfigurationRevision, error) {
	item, err := scanConfigurationRevision(query.QueryRow(ctx, `SELECT r.app_environment_id,ae.public_id,r.version,r.configuration_json::text,COALESCE(u.username,'system'),r.created_at
		FROM app_environment_configuration_revisions r
		JOIN app_environments ae ON ae.id=r.app_environment_id
		LEFT JOIN users u ON u.id=r.created_by_user_id
		WHERE ae.workspace_id=$1 AND r.app_environment_id=$2 AND r.version=$3`, workspaceID, appEnvironmentID, version))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ConfigurationRevision{}, ErrNotFound
	}
	return item, err
}

func scanConfigurationRevision(row pgx.Row) (domain.ConfigurationRevision, error) {
	var item domain.ConfigurationRevision
	var configuration []byte
	if err := row.Scan(&item.AppEnvironmentID, &item.AppEnvironmentPublicID, &item.Version, &configuration, &item.CreatedBy, &item.CreatedAt); err != nil {
		return domain.ConfigurationRevision{}, err
	}
	if err := jsonUnmarshal(configuration, &item.Configuration); err != nil {
		return domain.ConfigurationRevision{}, err
	}
	item.Configuration = domain.NormalizeRuntimeConfig(item.Configuration)
	return item, nil
}

func replaceParameterBindings(ctx context.Context, tx pgx.Tx, appEnvironmentID, workspaceID int64, bindings []domain.ParameterBinding) error {
	if _, err := tx.Exec(ctx, `DELETE FROM app_environment_parameter_bindings WHERE app_environment_id=$1`, appEnvironmentID); err != nil {
		return err
	}
	for _, binding := range bindings {
		tag, err := tx.Exec(ctx, `INSERT INTO app_environment_parameter_bindings(app_environment_id,environment_name,parameter_id,parameter_version)
			SELECT $1,$2,p.id,$4 FROM parameters p JOIN parameter_versions pv ON pv.parameter_id=p.id AND pv.version=$4
			WHERE p.workspace_id=$3 AND p.public_id=$5 AND p.archived_at IS NULL`, appEnvironmentID, binding.Name, workspaceID, binding.ParameterVersion, binding.ParameterPublicID)
		if err != nil {
			return translateDBError(err)
		}
		if tag.RowsAffected() != 1 {
			return ErrParameterBinding
		}
	}
	return nil
}

func (s *Store) ResolveParameterBindings(ctx context.Context, workspaceID int64, bindings []domain.ParameterBinding) ([]domain.ResolvedParameter, error) {
	resolved := make([]domain.ResolvedParameter, 0, len(bindings))
	for _, binding := range bindings {
		var item domain.ResolvedParameter
		item.Binding = binding
		var plainText *string
		err := s.Pool.QueryRow(ctx, `SELECT p.kind,pv.plaintext_value,COALESCE(pv.secret_reference,''),COALESCE(pv.secret_backend_version,0)
			FROM parameters p JOIN parameter_versions pv ON pv.parameter_id=p.id
			WHERE p.workspace_id=$1 AND p.public_id=$2 AND pv.version=$3`, workspaceID, binding.ParameterPublicID, binding.ParameterVersion).
			Scan(&item.Kind, &plainText, &item.SecretReference, &item.SecretBackendVersion)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		if err != nil {
			return nil, err
		}
		if plainText != nil {
			item.PlainTextValue = *plainText
		}
		resolved = append(resolved, item)
	}
	return resolved, nil
}

func (s *Store) ListAppEnvironments(ctx context.Context, workspaceID int64, projectPublicID, appPublicID string, beforeID int64, limit int) ([]domain.AppEnvironment, string, error) {
	if beforeID == 0 {
		beforeID = math.MaxInt64
	}
	query := `SELECT ` + appEnvironmentColumns + ` FROM app_environments ae ` + appEnvironmentJoins + `
		WHERE ae.workspace_id=$1 AND p.public_id=$2 AND a.public_id=$3
		  AND ae.id < $4 AND ae.archived_at IS NULL
		ORDER BY ae.id DESC LIMIT $5`
	rows, err := s.Pool.Query(ctx, query, workspaceID, projectPublicID, appPublicID, beforeID, limit+1)
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()
	items := []domain.AppEnvironment{}
	for rows.Next() {
		item, scanErr := scanAppEnvironment(rows)
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

func (s *Store) ListEnvironmentApps(ctx context.Context, workspaceID int64, projectPublicID, environmentPublicID string, beforeID int64, limit int) ([]domain.AppEnvironment, string, error) {
	if beforeID == 0 {
		beforeID = math.MaxInt64
	}
	query := `SELECT ` + appEnvironmentColumns + ` FROM app_environments ae ` + appEnvironmentJoins + `
		WHERE ae.workspace_id=$1 AND p.public_id=$2 AND e.public_id=$3
		  AND ae.id < $4 AND ae.archived_at IS NULL
		ORDER BY ae.id DESC LIMIT $5`
	rows, err := s.Pool.Query(ctx, query, workspaceID, projectPublicID, environmentPublicID, beforeID, limit+1)
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()
	items := []domain.AppEnvironment{}
	for rows.Next() {
		item, scanErr := scanAppEnvironment(rows)
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

func (s *Store) FindAppEnvironment(ctx context.Context, workspaceID int64, publicID string) (domain.AppEnvironment, error) {
	query := `SELECT ` + appEnvironmentColumns + ` FROM app_environments ae ` + appEnvironmentJoins + `
		WHERE ae.workspace_id=$1 AND ae.public_id=$2 AND ae.archived_at IS NULL`
	item, err := scanAppEnvironment(s.Pool.QueryRow(ctx, query, workspaceID, publicID))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.AppEnvironment{}, ErrNotFound
	}
	return item, err
}

func (s *Store) FindAppEnvironmentForApp(ctx context.Context, workspaceID int64, projectPublicID, appPublicID, publicID string) (domain.AppEnvironment, error) {
	query := `SELECT ` + appEnvironmentColumns + ` FROM app_environments ae ` + appEnvironmentJoins + `
		WHERE ae.workspace_id=$1 AND p.public_id=$2 AND a.public_id=$3 AND ae.public_id=$4 AND ae.archived_at IS NULL`
	item, err := scanAppEnvironment(s.Pool.QueryRow(ctx, query, workspaceID, projectPublicID, appPublicID, publicID))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.AppEnvironment{}, ErrNotFound
	}
	return item, err
}

func (s *Store) CreateDeployment(ctx context.Context, workspaceID, actorID int64, appEnvironmentPublicID, deploymentPublicID, releasePublicID string, configurationVersion, expectedVersion int64, expectedCurrentDeploymentPublicID string, idempotencyHash, payloadHash []byte) (domain.Deployment, domain.Operation, bool, error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return domain.Deployment{}, domain.Operation{}, false, err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, "deployment:"+fmt.Sprintf("%x", idempotencyHash)); err != nil {
		return domain.Deployment{}, domain.Operation{}, false, err
	}
	if existing, found, findErr := operationByIdempotency(ctx, tx, workspaceID, actorID, idempotencyHash, payloadHash); findErr != nil {
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
	appEnvironment, err := appEnvironmentForUpdate(ctx, tx, workspaceID, appEnvironmentPublicID)
	if err != nil {
		return domain.Deployment{}, domain.Operation{}, false, err
	}
	if appEnvironment.Version != expectedVersion || appEnvironment.CurrentDeploymentPublicID != expectedCurrentDeploymentPublicID {
		return domain.Deployment{}, domain.Operation{}, false, ErrVersionConflict
	}
	var releaseID int64
	var requestedBy string
	if err = tx.QueryRow(ctx, `SELECT r.id,u.username FROM releases r JOIN users u ON u.id=$4 WHERE r.public_id=$1 AND r.workspace_id=$2 AND r.app_id=$3 AND r.availability_status='Available'`, releasePublicID, workspaceID, appEnvironment.AppID, actorID).Scan(&releaseID, &requestedBy); errors.Is(err, pgx.ErrNoRows) {
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
	revision, err := configurationRevision(ctx, tx, workspaceID, appEnvironment.ID, configurationVersion)
	if err != nil {
		return domain.Deployment{}, domain.Operation{}, false, err
	}
	revision.Configuration = inheritAllocatedPorts(revision.Configuration, appEnvironment.Configuration)
	revision.Configuration, err = s.reservePublicationClaims(ctx, tx, appEnvironment.ID, revision.Version, revision.Configuration)
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
		INSERT INTO deployments(public_id,workspace_id,app_environment_id,app_id,release_id,requested_by_user_id,configuration_version,configuration_json,workload_kind,app_volume_id)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,NULLIF($10,0))
		RETURNING id,public_id,workspace_id,app_environment_id,app_id,configuration_version,configuration_json::text,workload_kind,status,message,observed_release,created_at,updated_at`,
		deploymentPublicID, workspaceID, appEnvironment.ID, appEnvironment.AppID, releaseID, actorID, revision.Version, configurationJSON, appEnvironment.WorkloadKind, appVolumeID).
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
	deployment.ReleasePublicID = releasePublicID
	deployment.Configuration = revision.Configuration
	deployment.RequestedBy = requestedBy
	deployment.AppVolumePublicID = appVolumePublicID
	var desiredVersion int64
	if err = tx.QueryRow(ctx, `UPDATE app_environments SET desired_deployment_id=$1,last_state='Progressing',last_message='',version=version+1,updated_at=now() WHERE id=$2 RETURNING version`, deployment.ID, appEnvironment.ID).Scan(&desiredVersion); err != nil {
		return domain.Deployment{}, domain.Operation{}, false, err
	}
	operation, err := insertOperation(ctx, tx, workspaceID, appEnvironment.ID, deployment.ID, actorID, domain.OperationApplyDeployment, idempotencyHash, payloadHash, desiredVersion)
	if err != nil {
		return domain.Deployment{}, domain.Operation{}, false, translateDBError(err)
	}
	operation.AppEnvironmentPublicID = appEnvironment.PublicID
	operation.DeploymentPublicID = deployment.PublicID
	if err = tx.Commit(ctx); err != nil {
		return domain.Deployment{}, domain.Operation{}, false, err
	}
	return deployment, operation, false, nil
}

func (s *Store) DeleteAppEnvironment(ctx context.Context, workspaceID, actorID int64, appEnvironmentPublicID string, version int64, idempotencyHash, payloadHash []byte) (domain.Operation, error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return domain.Operation{}, err
	}
	defer tx.Rollback(ctx)
	if existing, found, findErr := operationByIdempotency(ctx, tx, workspaceID, actorID, idempotencyHash, payloadHash); findErr != nil {
		return domain.Operation{}, findErr
	} else if found {
		return existing, tx.Commit(ctx)
	}
	appEnvironment, err := appEnvironmentForUpdate(ctx, tx, workspaceID, appEnvironmentPublicID)
	if err != nil {
		return domain.Operation{}, err
	}
	if appEnvironment.Version != version || appEnvironment.DeletionRequestedAt != nil {
		return domain.Operation{}, ErrVersionConflict
	}
	var active bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM operations WHERE app_environment_id=$1 AND status IN ('Pending','Running'))`, appEnvironment.ID).Scan(&active); err != nil {
		return domain.Operation{}, err
	}
	if active {
		return domain.Operation{}, ErrConflict
	}
	var desiredVersion int64
	if err = tx.QueryRow(ctx, `UPDATE app_environments SET deletion_requested_at=now(),version=version+1,updated_at=now() WHERE id=$1 RETURNING version`, appEnvironment.ID).Scan(&desiredVersion); err != nil {
		return domain.Operation{}, err
	}
	operation, err := insertOperation(ctx, tx, workspaceID, appEnvironment.ID, 0, actorID, domain.OperationDeleteAppEnv, idempotencyHash, payloadHash, desiredVersion)
	if err != nil {
		return domain.Operation{}, translateDBError(err)
	}
	operation.AppEnvironmentPublicID = appEnvironment.PublicID
	return operation, tx.Commit(ctx)
}

func (s *Store) ListDeployments(ctx context.Context, workspaceID int64, appEnvironmentID int64, beforeID int64, limit int) ([]domain.Deployment, string, error) {
	if beforeID == 0 {
		beforeID = math.MaxInt64
	}
	rows, err := s.Pool.Query(ctx, `SELECT d.id,d.public_id,d.workspace_id,d.app_environment_id,d.app_id,ae.public_id,r.public_id,r.image,d.configuration_version,d.configuration_json::text,d.workload_kind,COALESCE(av.public_id,''),u.username,d.status,d.message,d.observed_release,d.created_at,d.updated_at
		FROM deployments d JOIN app_environments ae ON ae.id=d.app_environment_id JOIN releases r ON r.id=d.release_id JOIN users u ON u.id=d.requested_by_user_id LEFT JOIN app_volumes av ON av.id=d.app_volume_id
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
	item, err := scanDeployment(s.Pool.QueryRow(ctx, `SELECT d.id,d.public_id,d.workspace_id,d.app_environment_id,d.app_id,ae.public_id,r.public_id,r.image,d.configuration_version,d.configuration_json::text,d.workload_kind,COALESCE(av.public_id,''),u.username,d.status,d.message,d.observed_release,d.created_at,d.updated_at
		FROM deployments d JOIN app_environments ae ON ae.id=d.app_environment_id JOIN releases r ON r.id=d.release_id JOIN users u ON u.id=d.requested_by_user_id LEFT JOIN app_volumes av ON av.id=d.app_volume_id
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
		&item.AppEnvironmentPublicID, &item.ReleasePublicID, &item.Image, &item.ConfigurationVersion, &configuration, &item.WorkloadKind, &item.AppVolumePublicID, &item.RequestedBy,
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

func (s *Store) GetOperationForUser(ctx context.Context, userID int64, publicID string) (domain.Operation, error) {
	var item domain.Operation
	err := s.Pool.QueryRow(ctx, `SELECT o.id,o.public_id,o.workspace_id,COALESCE(o.app_environment_id,0),COALESCE(ae.public_id,''),COALESCE(o.deployment_id,0),COALESCE(d.public_id,''),COALESCE(o.app_volume_id,0),COALESCE(av.public_id,''),o.requested_by_user_id,o.kind,o.status,o.desired_version,o.attempts,o.error_code,o.error_message,o.created_at,o.updated_at
		FROM operations o LEFT JOIN app_environments ae ON ae.id=o.app_environment_id LEFT JOIN deployments d ON d.id=o.deployment_id LEFT JOIN app_volumes av ON av.id=o.app_volume_id
		JOIN workspace_memberships wm ON wm.workspace_id=o.workspace_id WHERE wm.user_id=$1 AND wm.status='Active' AND o.public_id=$2`, userID, publicID).
		Scan(&item.ID, &item.PublicID, &item.WorkspaceID, &item.AppEnvironmentID, &item.AppEnvironmentPublicID,
			&item.DeploymentID, &item.DeploymentPublicID, &item.AppVolumeID, &item.AppVolumePublicID, &item.ActorID, &item.Kind, &item.Status,
			&item.DesiredVersion, &item.Attempts, &item.ErrorCode, &item.ErrorMessage, &item.CreatedAt, &item.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Operation{}, ErrNotFound
	}
	return item, err
}

func (s *Store) ListAppEnvironmentOperations(ctx context.Context, workspaceID, appEnvironmentID int64, from, to time.Time, limit int) ([]domain.Operation, error) {
	rows, err := s.Pool.Query(ctx, `SELECT o.id,o.public_id,o.workspace_id,o.app_environment_id,ae.public_id,COALESCE(o.deployment_id,0),COALESCE(d.public_id,''),COALESCE(o.app_volume_id,0),COALESCE(av.public_id,''),o.requested_by_user_id,o.kind,o.status,o.desired_version,o.attempts,o.error_code,o.error_message,o.created_at,o.updated_at
		FROM operations o JOIN app_environments ae ON ae.id=o.app_environment_id LEFT JOIN deployments d ON d.id=o.deployment_id LEFT JOIN app_volumes av ON av.id=o.app_volume_id
		WHERE o.workspace_id=$1 AND o.app_environment_id=$2 AND o.created_at >= $3 AND o.created_at <= $4
		ORDER BY o.created_at DESC,o.id DESC LIMIT $5`, workspaceID, appEnvironmentID, from, to, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]domain.Operation, 0)
	for rows.Next() {
		var item domain.Operation
		if err = rows.Scan(&item.ID, &item.PublicID, &item.WorkspaceID, &item.AppEnvironmentID, &item.AppEnvironmentPublicID,
			&item.DeploymentID, &item.DeploymentPublicID, &item.AppVolumeID, &item.AppVolumePublicID, &item.ActorID, &item.Kind, &item.Status,
			&item.DesiredVersion, &item.Attempts, &item.ErrorCode, &item.ErrorMessage, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) ClaimNext(ctx context.Context, worker string, lease time.Duration) (domain.Operation, domain.AppEnvironment, domain.Deployment, bool, error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return domain.Operation{}, domain.AppEnvironment{}, domain.Deployment{}, false, err
	}
	defer tx.Rollback(ctx)
	var operation domain.Operation
	err = tx.QueryRow(ctx, `WITH candidate AS (
		SELECT id FROM operations WHERE (status='Pending' OR (status='Running' AND lease_until < now())) AND next_attempt_at <= now()
		ORDER BY id FOR UPDATE SKIP LOCKED LIMIT 1
	) UPDATE operations o SET status='Running',attempts=attempts+1,started_at=COALESCE(started_at,now()),
		lease_until=now()+$1::interval,worker_id=$2,fencing_token=fencing_token+1,updated_at=now()
	FROM candidate c WHERE o.id=c.id
	RETURNING o.id,o.public_id,o.workspace_id,COALESCE(o.app_environment_id,0),COALESCE(o.deployment_id,0),COALESCE(o.app_volume_id,0),o.requested_by_user_id,o.kind,o.status,o.desired_version,o.attempts,o.worker_id,o.fencing_token,o.lease_until,o.created_at,o.updated_at`, fmt.Sprintf("%f seconds", lease.Seconds()), worker).
		Scan(&operation.ID, &operation.PublicID, &operation.WorkspaceID, &operation.AppEnvironmentID, &operation.DeploymentID, &operation.AppVolumeID,
			&operation.ActorID, &operation.Kind, &operation.Status, &operation.DesiredVersion, &operation.Attempts,
			&operation.WorkerID, &operation.FencingToken, &operation.LeaseUntil, &operation.CreatedAt, &operation.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Operation{}, domain.AppEnvironment{}, domain.Deployment{}, false, nil
	}
	if err != nil {
		return domain.Operation{}, domain.AppEnvironment{}, domain.Deployment{}, false, err
	}
	if operation.Kind == domain.OperationEnsureWorkspace {
		if _, err = tx.Exec(ctx, `UPDATE workspaces SET bootstrap_state='Running',updated_at=now() WHERE id=$1`, operation.WorkspaceID); err != nil {
			return domain.Operation{}, domain.AppEnvironment{}, domain.Deployment{}, false, err
		}
		return operation, domain.AppEnvironment{WorkspaceID: operation.WorkspaceID}, domain.Deployment{}, true, tx.Commit(ctx)
	}
	appEnvironment, err := appEnvironmentByID(ctx, tx, operation.AppEnvironmentID)
	if err != nil {
		return domain.Operation{}, domain.AppEnvironment{}, domain.Deployment{}, false, err
	}
	operation.AppEnvironmentPublicID = appEnvironment.PublicID
	if operation.AppVolumeID != 0 {
		volume, volumeErr := appVolumeByID(ctx, tx, operation.AppVolumeID)
		if volumeErr != nil {
			return domain.Operation{}, domain.AppEnvironment{}, domain.Deployment{}, false, volumeErr
		}
		operation.AppVolumePublicID = volume.PublicID
		state := domain.VolumeStateProvisioning
		if operation.Kind == domain.OperationExpandVolume {
			state = domain.VolumeStateExpanding
		}
		if _, err = tx.Exec(ctx, `UPDATE app_volumes SET observed_state=$1,updated_at=now() WHERE id=$2`, state, volume.ID); err != nil {
			return domain.Operation{}, domain.AppEnvironment{}, domain.Deployment{}, false, err
		}
	}
	var deployment domain.Deployment
	if operation.Kind == domain.OperationApplyDeployment {
		deployment, err = deploymentByID(ctx, tx, operation.DeploymentID)
		if err != nil {
			return domain.Operation{}, domain.AppEnvironment{}, domain.Deployment{}, false, err
		}
		operation.DeploymentPublicID = deployment.PublicID
		if _, err = tx.Exec(ctx, `UPDATE deployments SET status='Progressing',started_at=COALESCE(started_at,now()),updated_at=now() WHERE id=$1`, deployment.ID); err != nil {
			return domain.Operation{}, domain.AppEnvironment{}, domain.Deployment{}, false, err
		}
	}
	return operation, appEnvironment, deployment, true, tx.Commit(ctx)
}

func (s *Store) CompleteDeployment(ctx context.Context, operation domain.Operation, message, observedRelease string) error {
	return s.completeDeployment(ctx, operation, message, observedRelease, nil)
}

func (s *Store) CompleteStatefulDeployment(ctx context.Context, operation domain.Operation, message, observedRelease, volumeMessage string, observedSizeGiB int64) error {
	volume := &deploymentVolumeCompletion{message: volumeMessage, observedSizeGiB: observedSizeGiB}
	return s.completeDeployment(ctx, operation, message, observedRelease, volume)
}

type deploymentVolumeCompletion struct {
	message         string
	observedSizeGiB int64
}

func (s *Store) completeDeployment(ctx context.Context, operation domain.Operation, message, observedRelease string, volume *deploymentVolumeCompletion) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err = completeOperationLease(ctx, tx, operation); err != nil {
		return err
	}
	deployed, err := deploymentByID(ctx, tx, operation.DeploymentID)
	if err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, `UPDATE deployments SET status='Ready',message=$1,observed_release=$2,completed_at=now(),updated_at=now() WHERE id=$3 AND app_environment_id=$4`, message, observedRelease, operation.DeploymentID, operation.AppEnvironmentID)
	if err != nil || tag.RowsAffected() != 1 {
		return ErrLeaseLost
	}
	_, err = tx.Exec(ctx, `UPDATE app_environments ae SET current_deployment_id=d.id,current_release_id=d.release_id,last_state='Ready',last_message=$1,updated_at=now()
		FROM deployments d WHERE ae.id=$2 AND d.id=$3 AND ae.desired_deployment_id=d.id`, message, operation.AppEnvironmentID, operation.DeploymentID)
	if err != nil {
		return err
	}
	if volume != nil {
		tag, err = tx.Exec(ctx, `UPDATE app_volumes av SET observed_state='Ready',message=$1,observed_size_gib=$2,updated_at=now()
			FROM deployments d WHERE d.id=$3 AND d.app_volume_id=av.id AND av.app_environment_id=$4`, volume.message, volume.observedSizeGiB, operation.DeploymentID, operation.AppEnvironmentID)
		if err != nil || tag.RowsAffected() != 1 {
			return ErrLeaseLost
		}
	}
	if err = activatePublicationClaims(ctx, tx, operation.AppEnvironmentID, deployed.ConfigurationVersion, deployed.Configuration, s.Publication.Domain); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) CompleteAppEnvironmentDeletion(ctx context.Context, operation domain.Operation, message string) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err = completeOperationLease(ctx, tx, operation); err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, `UPDATE app_environments SET archived_at=now(),last_message=$1,updated_at=now() WHERE id=$2 AND deletion_requested_at IS NOT NULL AND archived_at IS NULL`, message, operation.AppEnvironmentID)
	if err != nil || tag.RowsAffected() != 1 {
		return ErrLeaseLost
	}
	if _, err = tx.Exec(ctx, `DELETE FROM publication_claims WHERE app_environment_id=$1`, operation.AppEnvironmentID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) CompleteWorkspace(ctx context.Context, operation domain.Operation) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err = completeOperationLease(ctx, tx, operation); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE workspaces SET bootstrap_state='Ready',updated_at=now() WHERE id=$1`, operation.WorkspaceID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func completeOperationLease(ctx context.Context, tx pgx.Tx, operation domain.Operation) error {
	tag, err := tx.Exec(ctx, `UPDATE operations SET status='Succeeded',completed_at=now(),lease_until=NULL,worker_id=NULL,error_code='',error_message='',updated_at=now()
		WHERE id=$1 AND status='Running' AND worker_id=$2 AND fencing_token=$3 AND lease_until > now()`, operation.ID, operation.WorkerID, operation.FencingToken)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return ErrLeaseLost
	}
	return nil
}

func (s *Store) Fail(ctx context.Context, operation domain.Operation, code, message string, retry bool) error {
	status := domain.OperationFailed
	if retry && operation.Attempts < 8 {
		status = domain.OperationPending
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	tag, err := tx.Exec(ctx, `UPDATE operations SET status=$1,next_attempt_at=CASE WHEN $1='Pending' THEN now()+make_interval(secs => LEAST(300,power(2,attempts)::int)) ELSE next_attempt_at END,
		completed_at=CASE WHEN $1='Failed' THEN now() ELSE NULL END,lease_until=NULL,worker_id=NULL,error_code=$2,error_message=$3,updated_at=now()
		WHERE id=$4 AND status='Running' AND worker_id=$5 AND fencing_token=$6 AND lease_until > now()`, status, code, message, operation.ID, operation.WorkerID, operation.FencingToken)
	if err != nil || tag.RowsAffected() != 1 {
		return ErrLeaseLost
	}
	if status == domain.OperationFailed {
		switch operation.Kind {
		case domain.OperationEnsureWorkspace:
			_, err = tx.Exec(ctx, `UPDATE workspaces SET bootstrap_state='Failed',updated_at=now() WHERE id=$1`, operation.WorkspaceID)
		case domain.OperationApplyDeployment:
			_, err = tx.Exec(ctx, `UPDATE deployments SET status='Degraded',message=$1,completed_at=now(),updated_at=now() WHERE id=$2`, message, operation.DeploymentID)
			if err == nil {
				_, err = tx.Exec(ctx, `UPDATE app_environments SET last_state='Degraded',last_message=$1,updated_at=now() WHERE id=$2 AND desired_deployment_id=$3`, message, operation.AppEnvironmentID, operation.DeploymentID)
			}
		case domain.OperationDeleteAppEnv:
			_, err = tx.Exec(ctx, `UPDATE app_environments SET last_state='Degraded',last_message=$1,updated_at=now() WHERE id=$2`, message, operation.AppEnvironmentID)
		case domain.OperationEnsureVolume, domain.OperationExpandVolume, domain.OperationDeleteVolume:
			_, err = tx.Exec(ctx, `UPDATE app_volumes SET observed_state='Degraded',message=$1,updated_at=now() WHERE id=$2`, message, operation.AppVolumeID)
			if err == nil {
				_, err = tx.Exec(ctx, `UPDATE app_environments SET last_state='Degraded',last_message=$1,updated_at=now() WHERE id=$2 AND archived_at IS NULL`, message, operation.AppEnvironmentID)
			}
		}
		if err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (s *Store) ReleaseClaims(ctx context.Context, workerID string) error {
	_, err := s.Pool.Exec(ctx, `UPDATE operations SET status='Pending',next_attempt_at=now(),lease_until=NULL,worker_id=NULL,updated_at=now() WHERE status='Running' AND worker_id=$1`, workerID)
	return err
}

func insertOperation(ctx context.Context, tx pgx.Tx, workspaceID, appEnvironmentID, deploymentID, actorID int64, kind string, idempotencyHash, payloadHash []byte, desiredVersion int64) (domain.Operation, error) {
	for range 3 {
		if _, err := tx.Exec(ctx, `SAVEPOINT operation_public_id`); err != nil {
			return domain.Operation{}, err
		}
		publicID, err := domain.NewPublicID("op")
		if err != nil {
			return domain.Operation{}, err
		}
		var item domain.Operation
		err = tx.QueryRow(ctx, `INSERT INTO operations(public_id,workspace_id,app_environment_id,deployment_id,requested_by_user_id,kind,status,idempotency_hash,payload_hash,desired_version)
			VALUES($1,$2,NULLIF($3,0),NULLIF($4,0),$5,$6,'Pending',$7,$8,$9)
			RETURNING id,public_id,workspace_id,COALESCE(app_environment_id,0),COALESCE(deployment_id,0),requested_by_user_id,kind,status,desired_version,attempts,created_at,updated_at`,
			publicID, workspaceID, appEnvironmentID, deploymentID, actorID, kind, idempotencyHash, payloadHash, desiredVersion).
			Scan(&item.ID, &item.PublicID, &item.WorkspaceID, &item.AppEnvironmentID, &item.DeploymentID,
				&item.ActorID, &item.Kind, &item.Status, &item.DesiredVersion, &item.Attempts, &item.CreatedAt, &item.UpdatedAt)
		if uniqueConstraint(err) == "operations_public_id_key" {
			if _, rollbackErr := tx.Exec(ctx, `ROLLBACK TO SAVEPOINT operation_public_id`); rollbackErr != nil {
				return domain.Operation{}, rollbackErr
			}
			if _, releaseErr := tx.Exec(ctx, `RELEASE SAVEPOINT operation_public_id`); releaseErr != nil {
				return domain.Operation{}, releaseErr
			}
			continue
		}
		if err == nil {
			_, err = tx.Exec(ctx, `RELEASE SAVEPOINT operation_public_id`)
		}
		return item, err
	}
	return domain.Operation{}, ErrPublicIDCollision
}

func operationByIdempotency(ctx context.Context, tx pgx.Tx, workspaceID, actorID int64, idempotencyHash, payloadHash []byte) (domain.Operation, bool, error) {
	var item domain.Operation
	var storedPayload []byte
	err := tx.QueryRow(ctx, `SELECT o.id,o.public_id,o.workspace_id,COALESCE(o.app_environment_id,0),COALESCE(ae.public_id,''),COALESCE(o.deployment_id,0),COALESCE(d.public_id,''),COALESCE(o.app_volume_id,0),COALESCE(av.public_id,''),o.requested_by_user_id,o.kind,o.status,o.desired_version,o.attempts,o.error_code,o.error_message,o.created_at,o.updated_at,o.payload_hash
		FROM operations o LEFT JOIN app_environments ae ON ae.id=o.app_environment_id LEFT JOIN deployments d ON d.id=o.deployment_id LEFT JOIN app_volumes av ON av.id=o.app_volume_id
		WHERE o.workspace_id=$1 AND o.requested_by_user_id=$2 AND o.idempotency_hash=$3`, workspaceID, actorID, idempotencyHash).
		Scan(&item.ID, &item.PublicID, &item.WorkspaceID, &item.AppEnvironmentID, &item.AppEnvironmentPublicID,
			&item.DeploymentID, &item.DeploymentPublicID, &item.AppVolumeID, &item.AppVolumePublicID, &item.ActorID, &item.Kind, &item.Status,
			&item.DesiredVersion, &item.Attempts, &item.ErrorCode, &item.ErrorMessage, &item.CreatedAt, &item.UpdatedAt, &storedPayload)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Operation{}, false, nil
	}
	if err != nil {
		return domain.Operation{}, false, err
	}
	if string(storedPayload) != string(payloadHash) {
		return domain.Operation{}, false, ErrConflict
	}
	return item, true, nil
}

func appEnvironmentForUpdate(ctx context.Context, tx pgx.Tx, workspaceID int64, publicID string) (domain.AppEnvironment, error) {
	query := `SELECT ` + appEnvironmentColumns + ` FROM app_environments ae ` + appEnvironmentJoins + `
		WHERE ae.workspace_id=$1 AND ae.public_id=$2 AND ae.archived_at IS NULL FOR UPDATE OF ae`
	item, err := scanAppEnvironment(tx.QueryRow(ctx, query, workspaceID, publicID))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.AppEnvironment{}, ErrNotFound
	}
	return item, err
}

func appEnvironmentByID(ctx context.Context, query rowQuerier, id int64) (domain.AppEnvironment, error) {
	sql := `SELECT ` + appEnvironmentColumns + ` FROM app_environments ae ` + appEnvironmentJoins + ` WHERE ae.id=$1`
	return scanAppEnvironment(query.QueryRow(ctx, sql, id))
}

func deploymentByID(ctx context.Context, query rowQuerier, id int64) (domain.Deployment, error) {
	return scanDeployment(query.QueryRow(ctx, `SELECT d.id,d.public_id,d.workspace_id,d.app_environment_id,d.app_id,ae.public_id,r.public_id,r.image,d.configuration_version,d.configuration_json::text,d.workload_kind,COALESCE(av.public_id,''),u.username,d.status,d.message,d.observed_release,d.created_at,d.updated_at
		FROM deployments d JOIN app_environments ae ON ae.id=d.app_environment_id JOIN releases r ON r.id=d.release_id JOIN users u ON u.id=d.requested_by_user_id LEFT JOIN app_volumes av ON av.id=d.app_volume_id WHERE d.id=$1`, id))
}

func jsonUnmarshal(raw []byte, target any) error {
	return json.Unmarshal(raw, target)
}
