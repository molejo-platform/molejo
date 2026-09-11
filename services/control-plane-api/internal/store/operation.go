package store

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/molejo-platform/molejo/services/control-plane-api/internal/domain"
)

const operationSelectColumns = `o.id,o.public_id,o.workspace_id,COALESCE(o.app_environment_id,0),COALESCE(ae.public_id,''),
		COALESCE(o.deployment_id,0),COALESCE(d.public_id,''),COALESCE(o.app_volume_id,0),COALESCE(av.public_id,''),
		o.requested_by_principal_id,o.kind,o.status,o.desired_version,o.attempts,o.error_code,o.error_message,o.created_at,o.updated_at`

const operationJoins = `LEFT JOIN app_environments ae ON ae.id=o.app_environment_id
		LEFT JOIN deployments d ON d.id=o.deployment_id
		LEFT JOIN app_volumes av ON av.id=o.app_volume_id`

func (s *Store) GetOperationForUser(ctx context.Context, userID int64, publicID string) (domain.Operation, error) {
	item, err := scanOperation(s.Pool.QueryRow(ctx, `SELECT `+operationSelectColumns+` FROM operations o `+operationJoins+`
		JOIN workspace_memberships wm ON wm.workspace_id=o.workspace_id
		WHERE wm.user_id=$1 AND wm.status='Active' AND o.public_id=$2`, userID, publicID))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Operation{}, ErrNotFound
	}
	return item, err
}

func (s *Store) GetOperationForPrincipal(ctx context.Context, principalID int64, publicID string) (domain.Operation, error) {
	item, err := scanOperation(s.Pool.QueryRow(ctx, `SELECT `+operationSelectColumns+` FROM operations o `+operationJoins+`
		WHERE o.requested_by_principal_id=$1 AND o.public_id=$2`, principalID, publicID))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Operation{}, ErrNotFound
	}
	return item, err
}

func (s *Store) FindOperationByIdempotency(ctx context.Context, workspaceID, actorID int64, idempotencyHash, payloadHash []byte) (domain.Operation, bool, error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return domain.Operation{}, false, err
	}
	defer tx.Rollback(ctx)
	operation, found, err := operationByIdempotency(ctx, tx, workspaceID, actorID, idempotencyHash, payloadHash)
	if err != nil {
		return domain.Operation{}, false, err
	}
	return operation, found, tx.Commit(ctx)
}

func (s *Store) ListAppEnvironmentOperations(ctx context.Context, workspaceID, appEnvironmentID int64, from, to time.Time, limit int) ([]domain.Operation, error) {
	rows, err := s.Pool.Query(ctx, `SELECT `+operationSelectColumns+` FROM operations o `+operationJoins+`
		WHERE o.workspace_id=$1 AND o.app_environment_id=$2 AND o.created_at >= $3 AND o.created_at <= $4
		ORDER BY o.created_at DESC,o.id DESC LIMIT $5`, workspaceID, appEnvironmentID, from, to, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]domain.Operation, 0)
	for rows.Next() {
		item, scanErr := scanOperation(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) ListWorkspaceOperations(ctx context.Context, workspaceID, beforeID int64, limit int, status, kind, appEnvironmentPublicID string) ([]domain.Operation, string, error) {
	if beforeID == 0 {
		beforeID = math.MaxInt64
	}
	rows, err := s.Pool.Query(ctx, `SELECT `+operationSelectColumns+` FROM operations o `+operationJoins+`
		WHERE o.workspace_id=$1 AND o.id < $2
		  AND ($3='' OR o.status=$3)
		  AND ($4='' OR o.kind=$4)
		  AND ($5='' OR ae.public_id=$5)
		ORDER BY o.id DESC LIMIT $6`, workspaceID, beforeID, status, kind, appEnvironmentPublicID, limit+1)
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()
	items := make([]domain.Operation, 0, limit)
	for rows.Next() {
		item, scanErr := scanOperation(rows)
		if scanErr != nil {
			return nil, "", scanErr
		}
		if len(items) == limit {
			return items, domain.EncodeCursor(items[len(items)-1].ID), nil
		}
		items = append(items, item)
	}
	return items, "", rows.Err()
}

func (s *Store) ClaimNext(ctx context.Context, worker string, lease time.Duration) (domain.Operation, domain.AppEnvironment, domain.Deployment, bool, error) {
	return s.claimNext(ctx, worker, "", lease)
}

func (s *Store) ClaimNextForAgent(ctx context.Context, worker, installationID string, lease time.Duration) (domain.Operation, domain.AppEnvironment, domain.Deployment, bool, error) {
	if installationID == "" {
		return domain.Operation{}, domain.AppEnvironment{}, domain.Deployment{}, false, ErrAgentUnavailable
	}
	return s.claimNext(ctx, worker, installationID, lease)
}

func (s *Store) claimNext(ctx context.Context, worker, installationID string, lease time.Duration) (domain.Operation, domain.AppEnvironment, domain.Deployment, bool, error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return domain.Operation{}, domain.AppEnvironment{}, domain.Deployment{}, false, err
	}
	defer tx.Rollback(ctx)
	if err = lockPublication(ctx, tx); err != nil {
		return domain.Operation{}, domain.AppEnvironment{}, domain.Deployment{}, false, err
	}
	if installationID != "" {
		if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, "agent-dispatch:"+installationID); err != nil {
			return domain.Operation{}, domain.AppEnvironment{}, domain.Deployment{}, false, err
		}
	}
	var operation domain.Operation
	err = tx.QueryRow(ctx, `WITH candidate AS (
		SELECT o.id FROM operations o LEFT JOIN agent_installations ai ON ai.id=o.agent_installation_id
		WHERE (o.status='Pending' OR (o.status='Running' AND o.lease_until < now())) AND o.next_attempt_at <= now()
		AND ($3='' OR ai.public_id=$3)
		AND NOT EXISTS (
			SELECT 1 FROM operations active
			WHERE active.agent_installation_id=o.agent_installation_id AND active.status='Running'
			  AND active.lease_until >= now() AND active.id<>o.id
		)
		ORDER BY o.id FOR UPDATE OF o SKIP LOCKED LIMIT 1
	) UPDATE operations o SET status='Running',attempts=attempts+1,started_at=COALESCE(started_at,now()),
		lease_until=now()+$1::interval,worker_id=$2,fencing_token=fencing_token+1,updated_at=now()
	FROM candidate c WHERE o.id=c.id
	RETURNING o.id,o.public_id,o.workspace_id,COALESCE(o.agent_installation_id,0),COALESCE(o.app_environment_id,0),COALESCE(o.deployment_id,0),COALESCE(o.app_volume_id,0),o.requested_by_principal_id,o.kind,o.status,o.desired_version,o.attempts,o.worker_id,o.fencing_token,o.lease_until,o.created_at,o.updated_at`, fmt.Sprintf("%f seconds", lease.Seconds()), worker, installationID).
		Scan(&operation.ID, &operation.PublicID, &operation.WorkspaceID, &operation.ClusterID, &operation.AppEnvironmentID, &operation.DeploymentID, &operation.AppVolumeID,
			&operation.ActorID, &operation.Kind, &operation.Status, &operation.DesiredVersion, &operation.Attempts,
			&operation.WorkerID, &operation.FencingToken, &operation.LeaseUntil, &operation.CreatedAt, &operation.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Operation{}, domain.AppEnvironment{}, domain.Deployment{}, false, nil
	}
	if err != nil {
		return domain.Operation{}, domain.AppEnvironment{}, domain.Deployment{}, false, err
	}
	if operation.Kind == domain.OperationEnsureWorkspace {
		if _, err = tx.Exec(ctx, `UPDATE workspace_clusters SET state='Running',message='',updated_at=now()
			WHERE workspace_id=$1 AND installation_id=$2`, operation.WorkspaceID, operation.ClusterID); err != nil {
			return domain.Operation{}, domain.AppEnvironment{}, domain.Deployment{}, false, err
		}
		_, _ = tx.Exec(ctx, `UPDATE workspaces SET bootstrap_state='Running',updated_at=now() WHERE id=$1 AND bootstrap_state='Pending'`, operation.WorkspaceID)
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

func (s *Store) CompleteDeployment(ctx context.Context, operation domain.Operation, message, observedRelease, specHash string) error {
	return s.completeDeployment(ctx, operation, message, observedRelease, specHash, nil)
}

func (s *Store) CompleteStatefulDeployment(ctx context.Context, operation domain.Operation, message, observedRelease, volumeMessage string, observedSizeGiB int64, specHash string) error {
	volume := &deploymentVolumeCompletion{message: volumeMessage, observedSizeGiB: observedSizeGiB}
	return s.completeDeployment(ctx, operation, message, observedRelease, specHash, volume)
}

type deploymentVolumeCompletion struct {
	message         string
	observedSizeGiB int64
}

func (s *Store) completeDeployment(ctx context.Context, operation domain.Operation, message, observedRelease, specHash string, volume *deploymentVolumeCompletion) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err = lockPublication(ctx, tx); err != nil {
		return err
	}
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
	_, err = tx.Exec(ctx, `UPDATE app_environments ae SET current_deployment_id=d.id,current_release_id=d.release_id,last_state='Ready',last_message=$1,
		runtime_desired_version=$4,runtime_spec_hash=$5,updated_at=now()
		FROM deployments d WHERE ae.id=$2 AND d.id=$3 AND ae.desired_deployment_id=d.id`, message, operation.AppEnvironmentID, operation.DeploymentID, operation.DesiredVersion, specHash)
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
	// A newer observed runtime version fences older publication snapshots.
	if _, err = tx.Exec(ctx, `DELETE FROM publication_execution_claims ec USING deployments d WHERE ec.deployment_id=d.id AND d.app_environment_id=$1 AND d.id<>$2`, operation.AppEnvironmentID, operation.DeploymentID); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE operation_attempts a SET state='Fenced',completed_at=now() FROM operations o WHERE a.operation_id=o.id AND o.app_environment_id=$1 AND o.desired_version<$2 AND a.state IN ('Issued','Uncertain')`, operation.AppEnvironmentID, operation.DesiredVersion); err != nil {
		return err
	}
	if err = activatePublicationClaims(ctx, tx, operation.AppEnvironmentID, deployed.ConfigurationVersion, deployed.WorkloadKind, deployed.Configuration, s.publication); err != nil {
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
	if err = lockPublication(ctx, tx); err != nil {
		return err
	}
	if err = completeOperationLease(ctx, tx, operation); err != nil {
		return err
	}
	var confirmed bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM operation_attempts WHERE operation_id=$1 AND fencing_token=$2 AND state='Completed' AND withdrawal_confirmed AND runtime_uid<>'')`, operation.ID, operation.FencingToken).Scan(&confirmed); err != nil {
		return err
	}
	if !confirmed {
		return ErrLeaseLost
	}
	tag, err := tx.Exec(ctx, `UPDATE app_environments SET archived_at=now(),withdrawal_state='Confirmed',publication_observation='{}',last_message=$1,updated_at=now() WHERE id=$2 AND deletion_requested_at IS NOT NULL AND archived_at IS NULL AND withdrawal_state='Removing'`, message, operation.AppEnvironmentID)
	if err != nil || tag.RowsAffected() != 1 {
		return ErrLeaseLost
	}
	if _, err = tx.Exec(ctx, `DELETE FROM publication_execution_claims ec USING deployments d WHERE ec.deployment_id=d.id AND d.app_environment_id=$1`, operation.AppEnvironmentID); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE operation_attempts a SET state='Fenced',completed_at=now() FROM operations o WHERE a.operation_id=o.id AND o.app_environment_id=$1 AND a.state IN ('Issued','Uncertain')`, operation.AppEnvironmentID); err != nil {
		return err
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
	if err = lockPublication(ctx, tx); err != nil {
		return err
	}
	if err = completeOperationLease(ctx, tx, operation); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE workspace_clusters SET state='Ready',message='',observed_generation=GREATEST(observed_generation,1),updated_at=now()
		WHERE workspace_id=$1 AND installation_id=$2`, operation.WorkspaceID, operation.ClusterID); err != nil {
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
			_, err = tx.Exec(ctx, `UPDATE workspace_clusters SET state='Failed',message=$1,updated_at=now()
				WHERE workspace_id=$2 AND installation_id=$3`, message, operation.WorkspaceID, operation.ClusterID)
			if err == nil {
				_, err = tx.Exec(ctx, `UPDATE workspaces SET bootstrap_state=CASE WHEN EXISTS(
					SELECT 1 FROM workspace_clusters WHERE workspace_id=$1 AND state='Ready') THEN 'Ready' ELSE 'Failed' END,updated_at=now() WHERE id=$1`, operation.WorkspaceID)
			}
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
	principalID, err := principalIDForUser(ctx, tx, actorID)
	if err != nil {
		return domain.Operation{}, err
	}
	return insertOperationForPrincipal(ctx, tx, workspaceID, appEnvironmentID, deploymentID, &actorID, principalID, kind, idempotencyHash, payloadHash, desiredVersion)
}

func insertOperationForPrincipal(ctx context.Context, tx pgx.Tx, workspaceID, appEnvironmentID, deploymentID int64, actorUserID *int64, principalID int64, kind string, idempotencyHash, payloadHash []byte, desiredVersion int64) (domain.Operation, error) {
	var clusterID int64
	if err := tx.QueryRow(ctx, `SELECT cluster_id FROM app_environments WHERE id=$1 AND cluster_id IS NOT NULL`, appEnvironmentID).Scan(&clusterID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.Operation{}, ErrAgentUnavailable
		}
		return domain.Operation{}, err
	}
	for range 3 {
		if _, err := tx.Exec(ctx, `SAVEPOINT operation_public_id`); err != nil {
			return domain.Operation{}, err
		}
		publicID, err := domain.NewPublicID("op")
		if err != nil {
			return domain.Operation{}, err
		}
		var item domain.Operation
		err = tx.QueryRow(ctx, `INSERT INTO operations(public_id,workspace_id,app_environment_id,deployment_id,requested_by_user_id,requested_by_principal_id,kind,status,idempotency_hash,payload_hash,desired_version,agent_installation_id)
			VALUES($1,$2,NULLIF($3,0),NULLIF($4,0),$5,$6,$7,'Pending',$8,$9,$10,$11)
			RETURNING id,public_id,workspace_id,COALESCE(app_environment_id,0),COALESCE(deployment_id,0),requested_by_principal_id,kind,status,desired_version,attempts,created_at,updated_at`,
			publicID, workspaceID, appEnvironmentID, deploymentID, actorUserID, principalID, kind, idempotencyHash, payloadHash, desiredVersion, clusterID).
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
	principalID, err := principalIDForUser(ctx, tx, actorID)
	if err != nil {
		return domain.Operation{}, false, err
	}
	return operationByPrincipalIdempotency(ctx, tx, workspaceID, principalID, idempotencyHash, payloadHash)
}

func operationByPrincipalIdempotency(ctx context.Context, tx pgx.Tx, workspaceID, principalID int64, idempotencyHash, payloadHash []byte) (domain.Operation, bool, error) {
	var storedPayload []byte
	item, err := scanOperationWithPayload(tx.QueryRow(ctx, `SELECT `+operationSelectColumns+`,o.payload_hash FROM operations o `+operationJoins+`
		WHERE o.workspace_id=$1 AND o.requested_by_principal_id=$2 AND o.idempotency_hash=$3`, workspaceID, principalID, idempotencyHash), &storedPayload)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Operation{}, false, nil
	}
	if err != nil {
		return domain.Operation{}, false, err
	}
	if !bytes.Equal(storedPayload, payloadHash) {
		return domain.Operation{}, false, ErrIdempotencyConflict
	}
	return item, true, nil
}

func scanOperation(row pgx.Row) (domain.Operation, error) {
	return scanOperationWithPayload(row, nil)
}

func scanOperationWithPayload(row pgx.Row, payloadHash *[]byte) (domain.Operation, error) {
	var item domain.Operation
	targets := []any{
		&item.ID, &item.PublicID, &item.WorkspaceID, &item.AppEnvironmentID, &item.AppEnvironmentPublicID,
		&item.DeploymentID, &item.DeploymentPublicID, &item.AppVolumeID, &item.AppVolumePublicID, &item.ActorID,
		&item.Kind, &item.Status, &item.DesiredVersion, &item.Attempts, &item.ErrorCode, &item.ErrorMessage,
		&item.CreatedAt, &item.UpdatedAt,
	}
	if payloadHash != nil {
		targets = append(targets, payloadHash)
	}
	err := row.Scan(targets...)
	return item, err
}

func principalIDForUser(ctx context.Context, query rowQuerier, userID int64) (int64, error) {
	var principalID int64
	err := query.QueryRow(ctx, `SELECT principal_id FROM users WHERE id=$1`, userID).Scan(&principalID)
	return principalID, err
}
