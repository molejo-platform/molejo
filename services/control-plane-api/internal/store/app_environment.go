package store

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"math"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/molejo-platform/molejo/packages/runtimecontract"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/domain"
)

type rowQuerier interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

const appEnvironmentColumns = `
	ae.id,ae.public_id,ae.workspace_id,COALESCE(ae.cluster_id,0),COALESCE(ai.public_id,''),COALESCE(ai.cluster_uid,''),ae.project_id,p.public_id,ae.app_id,a.public_id,a.name,
	ae.environment_id,e.public_id,e.name,ae.source_branch,ae.runtime_name,
	ae.workload_kind,ae.configuration_json::text,ae.configuration_version,ae.version,
	COALESCE(dd.public_id,''),COALESCE(cd.public_id,''),COALESCE(cr.public_id,''),
	COALESCE(dd.configuration_version,0),COALESCE(cd.configuration_version,0),
	ae.runtime_observed_generation,ae.runtime_observed_at,
	ae.last_state,ae.last_message,ae.deletion_requested_at,ae.archived_at,ae.created_at,ae.updated_at,ae.withdrawal_state,ae.publication_observation`

const appEnvironmentJoins = `
	JOIN projects p ON p.id=ae.project_id
	JOIN apps a ON a.id=ae.app_id
	JOIN environments e ON e.id=ae.environment_id
	LEFT JOIN agent_installations ai ON ai.id=ae.cluster_id
	LEFT JOIN deployments dd ON dd.id=ae.desired_deployment_id
	LEFT JOIN deployments cd ON cd.id=ae.current_deployment_id
	LEFT JOIN releases cr ON cr.id=ae.current_release_id`

func scanAppEnvironment(row pgx.Row) (domain.AppEnvironment, error) {
	var item domain.AppEnvironment
	var configuration []byte
	err := row.Scan(
		&item.ID, &item.PublicID, &item.WorkspaceID, &item.ClusterID, &item.ClusterPublicID, &item.ClusterUID, &item.ProjectID, &item.ProjectPublicID,
		&item.AppID, &item.AppPublicID, &item.AppName, &item.EnvironmentID, &item.EnvironmentPublicID,
		&item.EnvironmentName, &item.SourceBranch, &item.RuntimeName, &item.WorkloadKind, &configuration,
		&item.ConfigurationVersion, &item.Version, &item.DesiredDeploymentPublicID,
		&item.CurrentDeploymentPublicID, &item.CurrentReleasePublicID, &item.DesiredConfigurationVersion,
		&item.CurrentConfigurationVersion, &item.RuntimeObservedGeneration, &item.RuntimeObservedAt, &item.State, &item.Message,
		&item.DeletionRequestedAt, &item.ArchivedAt, &item.CreatedAt, &item.UpdatedAt, &item.WithdrawalState, &item.PublicationObservation,
	)
	if err != nil {
		return domain.AppEnvironment{}, err
	}
	if err = jsonUnmarshal(configuration, &item.Configuration); err != nil {
		return domain.AppEnvironment{}, err
	}
	item.Configuration = domain.NormalizeRuntimeConfig(item.Configuration)
	var observation runtimecontract.PublicationObservation
	if len(item.PublicationObservation) > 2 {
		if err = json.Unmarshal(item.PublicationObservation, &observation); err != nil {
			return item, err
		}
		observation.SetAggregate(time.Now())
		item.PublicationObservation, err = json.Marshal(observation)
		if err != nil {
			return item, err
		}
	}
	return item, nil
}

func (s *Store) CreateAppEnvironment(ctx context.Context, workspaceID, actorID int64, publicID, projectPublicID, appPublicID, environmentPublicID, branch string, configuration domain.RuntimeConfig) (domain.AppEnvironment, error) {
	item, _, err := s.CreateAppEnvironmentWithWorkload(ctx, workspaceID, actorID, publicID, projectPublicID, appPublicID, environmentPublicID, branch, domain.WorkloadStateless, configuration, nil)
	return item, err
}

func (s *Store) CreateAppEnvironmentWithWorkload(ctx context.Context, workspaceID, actorID int64, publicID, projectPublicID, appPublicID, environmentPublicID, branch string, workloadKind domain.WorkloadKind, configuration domain.RuntimeConfig, volumeRequest *domain.VolumeRequest) (domain.AppEnvironment, *domain.AppVolume, error) {
	clusterID, err := activeAgentInstallationID(ctx, s.Pool)
	if err != nil {
		return domain.AppEnvironment{}, nil, err
	}
	var clusterPublicID string
	if err = s.Pool.QueryRow(ctx, `SELECT public_id FROM agent_installations WHERE id=$1`, clusterID).Scan(&clusterPublicID); err != nil {
		return domain.AppEnvironment{}, nil, err
	}
	return s.CreateAppEnvironmentOnCluster(ctx, workspaceID, actorID, publicID, projectPublicID, appPublicID, environmentPublicID, clusterPublicID, branch, workloadKind, configuration, volumeRequest)
}

// CreateAppEnvironmentOnCluster is the product path: the caller selects the
// durable cluster explicitly instead of relying on a process-wide default.
func (s *Store) CreateAppEnvironmentOnCluster(ctx context.Context, workspaceID, actorID int64, publicID, projectPublicID, appPublicID, environmentPublicID, clusterPublicID, branch string, workloadKind domain.WorkloadKind, configuration domain.RuntimeConfig, volumeRequest *domain.VolumeRequest) (domain.AppEnvironment, *domain.AppVolume, error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return domain.AppEnvironment{}, nil, err
	}
	defer tx.Rollback(ctx)
	item, volume, err := s.createAppEnvironmentOnCluster(ctx, tx, workspaceID, actorID, publicID, projectPublicID, appPublicID, environmentPublicID, clusterPublicID, branch, workloadKind, configuration, volumeRequest)
	if err != nil {
		return domain.AppEnvironment{}, nil, err
	}
	return item, volume, tx.Commit(ctx)
}

func (s *Store) createAppEnvironmentOnCluster(ctx context.Context, tx pgx.Tx, workspaceID, actorID int64, publicID, projectPublicID, appPublicID, environmentPublicID, clusterPublicID, branch string, workloadKind domain.WorkloadKind, configuration domain.RuntimeConfig, volumeRequest *domain.VolumeRequest) (domain.AppEnvironment, *domain.AppVolume, error) {
	configuration = domain.NormalizeRuntimeConfig(configuration)
	if err := domain.ValidateWorkloadConfiguration(workloadKind, configuration, volumeRequest); err != nil {
		return domain.AppEnvironment{}, nil, err
	}
	configurationJSON, err := domain.CanonicalJSON(configuration)
	if err != nil {
		return domain.AppEnvironment{}, nil, err
	}
	var clusterReady bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(
		SELECT 1 FROM agent_installations i
		JOIN workspace_clusters wc ON wc.installation_id=i.id
		WHERE i.public_id=$1 AND i.status='Active' AND wc.workspace_id=$2 AND wc.state='Ready'
	)`, clusterPublicID, workspaceID).Scan(&clusterReady); err != nil {
		return domain.AppEnvironment{}, nil, err
	}
	if !clusterReady {
		return domain.AppEnvironment{}, nil, ErrAgentUnavailable
	}
	query := `WITH inserted AS (
		INSERT INTO app_environments(public_id,workspace_id,project_id,app_id,environment_id,cluster_id,source_branch,runtime_name,workload_kind,configuration_json)
		SELECT $1,p.workspace_id,p.id,a.id,e.id,i.id,$6,$7,$8,$9
		FROM projects p
		JOIN apps a ON a.project_id=p.id
		JOIN environments e ON e.project_id=p.id
		JOIN agent_installations i ON i.public_id=$10 AND i.status='Active'
		JOIN workspace_clusters wc ON wc.workspace_id=p.workspace_id AND wc.installation_id=i.id AND wc.state='Ready'
		WHERE p.workspace_id=$2 AND p.public_id=$3 AND a.public_id=$4 AND e.public_id=$5
		  AND p.archived_at IS NULL AND a.archived_at IS NULL AND e.archived_at IS NULL
		RETURNING *
	) SELECT ` + appEnvironmentColumns + ` FROM inserted ae ` + appEnvironmentJoins
	item, err := scanAppEnvironment(tx.QueryRow(ctx, query, publicID, workspaceID, projectPublicID, appPublicID, environmentPublicID, branch, domain.RuntimeName(publicID), workloadKind, configurationJSON, clusterPublicID))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.AppEnvironment{}, nil, ErrNotFound
	}
	if uniqueConstraint(err) == "app_environments_public_id_key" {
		return domain.AppEnvironment{}, nil, ErrPublicIDCollision
	}
	if err != nil {
		return domain.AppEnvironment{}, nil, translateDBError(err)
	}
	configuration, err = s.reservePublicationClaims(ctx, tx, item.ID, item.ConfigurationVersion, item.WorkloadKind, configuration, true)
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
	return item, volume, nil
}

func (s *Store) UpdateAppEnvironment(ctx context.Context, workspaceID, actorID int64, publicID, branch string, configuration domain.RuntimeConfig, version int64) (domain.AppEnvironment, error) {
	configuration = domain.NormalizeRuntimeConfig(configuration)
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return domain.AppEnvironment{}, err
	}
	defer tx.Rollback(ctx)
	if err := lockPublication(ctx, tx); err != nil {
		return domain.AppEnvironment{}, err
	}
	current, err := appEnvironmentForUpdate(ctx, tx, workspaceID, publicID)
	if err != nil {
		return domain.AppEnvironment{}, err
	}
	if current.Version != version || current.DeletionRequestedAt != nil {
		return domain.AppEnvironment{}, ErrVersionConflict
	}
	configuration = inheritAllocatedPorts(configuration, current.Configuration)
	configuration, _, err = s.resolveHTTPConfiguration(ctx, tx, current.ID, configuration)
	if err != nil {
		return domain.AppEnvironment{}, err
	}
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
		configuration, err = s.reservePublicationClaims(ctx, tx, item.ID, item.ConfigurationVersion, item.WorkloadKind, configuration, true)
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
	var otherActive bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM operations WHERE app_environment_id=$1 AND status IN ('Pending','Running') AND kind<>'ApplyDeployment')`, appEnvironment.ID).Scan(&otherActive); err != nil {
		return domain.Operation{}, err
	}
	if otherActive {
		return domain.Operation{}, ErrConflict
	}
	var active bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM operations WHERE app_environment_id=$1 AND status IN ('Pending','Running'))`, appEnvironment.ID).Scan(&active); err != nil {
		return domain.Operation{}, err
	}
	if active {
		// Superseding intent is durable; issued attempts retain their claims.
		if _, err = tx.Exec(ctx, `UPDATE deployments d SET status='Degraded',message=$2,completed_at=now(),updated_at=now() WHERE EXISTS(SELECT 1 FROM operations o WHERE o.deployment_id=d.id AND o.app_environment_id=$1 AND o.kind='ApplyDeployment' AND o.status IN ('Pending','Running'))`, appEnvironment.ID, "withdrawal requested"); err != nil {
			return domain.Operation{}, err
		}
		if _, err = tx.Exec(ctx, `UPDATE operations SET status='Failed',error_code='superseded',error_message='withdrawal requested',completed_at=now(),lease_until=NULL,worker_id=NULL WHERE app_environment_id=$1 AND kind='ApplyDeployment' AND status IN ('Pending','Running')`, appEnvironment.ID); err != nil {
			return domain.Operation{}, err
		}
	}
	var desiredVersion int64
	if err = tx.QueryRow(ctx, `UPDATE app_environments SET deletion_requested_at=now(),withdrawal_state='Requested',version=version+1,updated_at=now() WHERE id=$1 RETURNING version`, appEnvironment.ID).Scan(&desiredVersion); err != nil {
		return domain.Operation{}, err
	}
	operation, err := insertOperation(ctx, tx, workspaceID, appEnvironment.ID, 0, actorID, domain.OperationDeleteAppEnv, idempotencyHash, payloadHash, desiredVersion)
	if err != nil {
		return domain.Operation{}, translateDBError(err)
	}
	operation.AppEnvironmentPublicID = appEnvironment.PublicID
	return operation, tx.Commit(ctx)
}

func appEnvironmentForUpdate(ctx context.Context, tx pgx.Tx, workspaceID int64, publicID string) (domain.AppEnvironment, error) {
	if err := lockPublication(ctx, tx); err != nil {
		return domain.AppEnvironment{}, err
	}

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

func jsonUnmarshal(raw []byte, target any) error {
	return json.Unmarshal(raw, target)
}
