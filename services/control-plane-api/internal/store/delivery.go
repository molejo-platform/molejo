package store

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/fruto-platform/fruto/services/control-plane-api/internal/domain"
	"github.com/jackc/pgx/v5"
)

func (s *Store) DeliveryPolicy(ctx context.Context, workspaceID int64, projectPublicID, appPublicID, appEnvironmentPublicID string) (domain.DeliveryPolicy, error) {
	var policy domain.DeliveryPolicy
	err := s.Pool.QueryRow(ctx, `
		SELECT ae.public_id,COALESCE(dp.push_enabled,false),COALESCE(dp.release_enabled,false),
		       COALESCE(dp.version,0),COALESCE(dp.updated_at,ae.updated_at)
		FROM app_environments ae
		JOIN apps a ON a.id=ae.app_id JOIN projects p ON p.id=ae.project_id
		LEFT JOIN app_environment_delivery_policies dp ON dp.app_environment_id=ae.id
		WHERE ae.workspace_id=$1 AND p.public_id=$2 AND a.public_id=$3 AND ae.public_id=$4
		  AND p.archived_at IS NULL AND a.archived_at IS NULL AND ae.archived_at IS NULL`,
		workspaceID, projectPublicID, appPublicID, appEnvironmentPublicID).
		Scan(&policy.AppEnvironmentPublicID, &policy.PushEnabled, &policy.ReleaseEnabled, &policy.Version, &policy.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.DeliveryPolicy{}, ErrNotFound
	}
	return policy, err
}

func (s *Store) PutDeliveryPolicy(ctx context.Context, workspaceID, actorID int64, projectPublicID, appPublicID, appEnvironmentPublicID string, expectedVersion int64, pushEnabled, releaseEnabled bool) (domain.DeliveryPolicy, error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return domain.DeliveryPolicy{}, err
	}
	defer tx.Rollback(ctx)
	var appEnvironmentID int64
	if err = tx.QueryRow(ctx, `
		SELECT ae.id FROM app_environments ae
		JOIN apps a ON a.id=ae.app_id JOIN projects p ON p.id=ae.project_id
		WHERE ae.workspace_id=$1 AND p.public_id=$2 AND a.public_id=$3 AND ae.public_id=$4
		  AND p.archived_at IS NULL AND a.archived_at IS NULL AND ae.archived_at IS NULL
		FOR UPDATE`, workspaceID, projectPublicID, appPublicID, appEnvironmentPublicID).Scan(&appEnvironmentID); errors.Is(err, pgx.ErrNoRows) {
		return domain.DeliveryPolicy{}, ErrNotFound
	} else if err != nil {
		return domain.DeliveryPolicy{}, err
	}
	var policy domain.DeliveryPolicy
	if expectedVersion == 0 {
		err = tx.QueryRow(ctx, `
			INSERT INTO app_environment_delivery_policies(app_environment_id,workspace_id,push_enabled,release_enabled,updated_by_user_id)
			VALUES($1,$2,$3,$4,$5)
			ON CONFLICT (app_environment_id) DO NOTHING
			RETURNING $6,push_enabled,release_enabled,version,updated_at`,
			appEnvironmentID, workspaceID, pushEnabled, releaseEnabled, actorID, appEnvironmentPublicID).
			Scan(&policy.AppEnvironmentPublicID, &policy.PushEnabled, &policy.ReleaseEnabled, &policy.Version, &policy.UpdatedAt)
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.DeliveryPolicy{}, ErrVersionConflict
		}
	} else {
		err = tx.QueryRow(ctx, `
			UPDATE app_environment_delivery_policies
			SET push_enabled=$1,release_enabled=$2,updated_by_user_id=$3,version=version+1,updated_at=now()
			WHERE app_environment_id=$4 AND workspace_id=$5 AND version=$6
			RETURNING $7,push_enabled,release_enabled,version,updated_at`,
			pushEnabled, releaseEnabled, actorID, appEnvironmentID, workspaceID, expectedVersion, appEnvironmentPublicID).
			Scan(&policy.AppEnvironmentPublicID, &policy.PushEnabled, &policy.ReleaseEnabled, &policy.Version, &policy.UpdatedAt)
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.DeliveryPolicy{}, ErrVersionConflict
		}
	}
	if err != nil {
		return domain.DeliveryPolicy{}, translateDBError(err)
	}
	return policy, tx.Commit(ctx)
}

func (s *Store) AcceptGitHubDelivery(ctx context.Context, delivery domain.GitHubDelivery) (domain.GitHubDelivery, bool, error) {
	var stored domain.GitHubDelivery
	err := s.Pool.QueryRow(ctx, `
		INSERT INTO github_deliveries(delivery_id,event_type,action,installation_external_id,repository_id,
		  repository_full_name,source_branch,source_ref,commit_sha,tag_name,repository_ids,payload_hash)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,COALESCE($11,'{}'::bigint[]),$12)
		ON CONFLICT (delivery_id) DO NOTHING
		RETURNING id,delivery_id,event_type,action,installation_external_id,repository_id,repository_full_name,
		          source_branch,source_ref,commit_sha,tag_name,repository_ids,payload_hash,status,attempts,
		          COALESCE(worker_id,''),fencing_token,lease_until`,
		delivery.DeliveryID, delivery.EventType, delivery.Action, delivery.InstallationExternalID, delivery.RepositoryID,
		delivery.RepositoryFullName, delivery.SourceBranch, delivery.SourceRef, delivery.CommitSHA, delivery.TagName,
		delivery.RepositoryIDs, delivery.PayloadHash).Scan(deliveryScanTargets(&stored)...)
	if err == nil {
		return stored, false, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return domain.GitHubDelivery{}, false, translateDBError(err)
	}
	err = s.Pool.QueryRow(ctx, `
		SELECT id,delivery_id,event_type,action,installation_external_id,repository_id,repository_full_name,
		       source_branch,source_ref,commit_sha,tag_name,repository_ids,payload_hash,status,attempts,
		       COALESCE(worker_id,''),fencing_token,lease_until
		FROM github_deliveries WHERE delivery_id=$1`, delivery.DeliveryID).Scan(deliveryScanTargets(&stored)...)
	if err != nil {
		return domain.GitHubDelivery{}, false, err
	}
	if string(stored.PayloadHash) != string(delivery.PayloadHash) {
		return domain.GitHubDelivery{}, false, ErrConflict
	}
	return stored, true, nil
}

func (s *Store) ClaimNextGitHubDelivery(ctx context.Context, workerID string, lease time.Duration) (domain.GitHubDelivery, bool, error) {
	if strings.TrimSpace(workerID) == "" || lease <= 0 {
		return domain.GitHubDelivery{}, false, errors.New("worker ID and lease are required")
	}
	var delivery domain.GitHubDelivery
	err := s.Pool.QueryRow(ctx, `
		WITH candidate AS (
			SELECT id FROM github_deliveries
			WHERE attempts < 10 AND (status='Pending' OR (status='Running' AND lease_until < now()))
			ORDER BY id FOR UPDATE SKIP LOCKED LIMIT 1
		), claimed AS (
			UPDATE github_deliveries d SET status='Running',attempts=d.attempts+1,worker_id=$1,
			  fencing_token=d.fencing_token+1,lease_until=now()+$2::interval,error_code='',error_message='',updated_at=now()
			FROM candidate c WHERE d.id=c.id RETURNING d.*
		)
		SELECT id,delivery_id,event_type,action,installation_external_id,repository_id,repository_full_name,
		       source_branch,source_ref,commit_sha,tag_name,repository_ids,payload_hash,status,attempts,
		       COALESCE(worker_id,''),fencing_token,lease_until FROM claimed`, workerID, lease.String()).
		Scan(deliveryScanTargets(&delivery)...)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.GitHubDelivery{}, false, nil
	}
	return delivery, err == nil, err
}

func (s *Store) CompleteGitHubDelivery(ctx context.Context, delivery domain.GitHubDelivery, ignored bool) error {
	status := "Succeeded"
	if ignored {
		status = "Ignored"
	}
	command, err := s.Pool.Exec(ctx, `
		UPDATE github_deliveries SET status=$1,worker_id=NULL,lease_until=NULL,processed_at=now(),updated_at=now()
		WHERE id=$2 AND status='Running' AND worker_id=$3 AND fencing_token=$4`,
		status, delivery.ID, delivery.WorkerID, delivery.FencingToken)
	if err != nil {
		return err
	}
	if command.RowsAffected() != 1 {
		return ErrConflict
	}
	return nil
}

func (s *Store) FailGitHubDelivery(ctx context.Context, delivery domain.GitHubDelivery, code, message string, retryable bool) error {
	status := "Pending"
	if !retryable || delivery.Attempts >= 10 {
		status = "Failed"
	}
	command, err := s.Pool.Exec(ctx, `
		UPDATE github_deliveries SET status=$1,worker_id=NULL,lease_until=NULL,error_code=$2,error_message=$3,
		  processed_at=CASE WHEN $1='Failed' THEN now() ELSE NULL END,updated_at=now()
		WHERE id=$4 AND status='Running' AND worker_id=$5 AND fencing_token=$6`,
		status, code, message, delivery.ID, delivery.WorkerID, delivery.FencingToken)
	if err != nil {
		return err
	}
	if command.RowsAffected() != 1 {
		return ErrConflict
	}
	return nil
}

func (s *Store) ReleaseGitHubDeliveryClaims(ctx context.Context, workerID string) error {
	_, err := s.Pool.Exec(ctx, `UPDATE github_deliveries SET status='Pending',worker_id=NULL,lease_until=NULL,updated_at=now() WHERE status='Running' AND worker_id=$1`, workerID)
	return err
}

func (s *Store) DeliveryCandidates(ctx context.Context, delivery domain.GitHubDelivery, trigger string) ([]domain.DeliveryCandidate, error) {
	rows, err := s.Pool.Query(ctx, `
		SELECT p.workspace_id,p.id,p.public_id,a.id,a.public_id,ae.id,ae.public_id,ae.source_branch,
		       dp.updated_by_user_id,dp.version
		FROM github_installations i
		JOIN app_github_sources src ON src.github_installation_id=i.id
		JOIN apps a ON a.id=src.app_id JOIN projects p ON p.id=a.project_id
		JOIN app_environments ae ON ae.app_id=a.id
		JOIN app_environment_delivery_policies dp ON dp.app_environment_id=ae.id AND dp.workspace_id=p.workspace_id
		WHERE i.github_installation_id=$1 AND i.status='Active' AND src.repository_id=$2
		  AND (($3='Push' AND dp.push_enabled) OR ($3='Release' AND dp.release_enabled))
		  AND ae.source_branch=$4
		  AND p.archived_at IS NULL AND a.archived_at IS NULL AND ae.archived_at IS NULL AND ae.deletion_requested_at IS NULL
		ORDER BY ae.id`, delivery.InstallationExternalID, delivery.RepositoryID, trigger, delivery.SourceBranch)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []domain.DeliveryCandidate{}
	for rows.Next() {
		var item domain.DeliveryCandidate
		if err = rows.Scan(&item.WorkspaceID, &item.ProjectID, &item.ProjectPublicID, &item.AppID, &item.AppPublicID,
			&item.AppEnvironmentID, &item.AppEnvironmentPublicID, &item.SourceBranch, &item.RequestedByActorID, &item.PolicyVersion); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) CreateDeliveryTargetBuild(ctx context.Context, delivery domain.GitHubDelivery, candidate domain.DeliveryCandidate, metadata domain.CommitMetadata, trigger, targetPublicID, buildPublicID string) (domain.DeliveryTarget, bool, error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return domain.DeliveryTarget{}, false, err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, candidate.AppEnvironmentID); err != nil {
		return domain.DeliveryTarget{}, false, err
	}
	var target domain.DeliveryTarget
	err = tx.QueryRow(ctx, `
		SELECT t.id,t.public_id,t.github_delivery_id,t.workspace_id,t.project_id,t.app_id,t.app_environment_id,
		       ae.public_id,t.requested_by_user_id,t.trigger_type,t.source_branch,t.commit_sha,COALESCE(t.build_id,0),
		       COALESCE(b.public_id,''),COALESCE(r.public_id,''),COALESCE(t.deployment_id,0),COALESCE(d.public_id,''),t.status
		FROM delivery_targets t JOIN app_environments ae ON ae.id=t.app_environment_id
		LEFT JOIN builds b ON b.id=t.build_id LEFT JOIN releases r ON r.build_id=b.id LEFT JOIN deployments d ON d.id=t.deployment_id
		WHERE t.github_delivery_id=$1 AND t.app_environment_id=$2`, delivery.ID, candidate.AppEnvironmentID).
		Scan(deliveryTargetScanTargets(&target)...)
	if err == nil {
		return target, true, tx.Commit(ctx)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return domain.DeliveryTarget{}, false, err
	}
	if _, err = tx.Exec(ctx, `
		WITH superseded AS (
			UPDATE builds SET status='Superseded',completed_at=now(),updated_at=now()
			WHERE app_environment_id=$1 AND status='Pending' AND trigger_type IN ('Push','Release') RETURNING id
		)
		UPDATE delivery_targets SET status='Superseded',completed_at=now(),updated_at=now()
		WHERE build_id IN (SELECT id FROM superseded)`, candidate.AppEnvironmentID); err != nil {
		return domain.DeliveryTarget{}, false, err
	}
	err = tx.QueryRow(ctx, `
		INSERT INTO delivery_targets(public_id,github_delivery_id,workspace_id,project_id,app_id,app_environment_id,
		  requested_by_user_id,policy_version,trigger_type,source_branch,commit_sha)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
		RETURNING id`, targetPublicID, delivery.ID, candidate.WorkspaceID, candidate.ProjectID, candidate.AppID,
		candidate.AppEnvironmentID, candidate.RequestedByActorID, candidate.PolicyVersion, trigger, candidate.SourceBranch, metadata.SHA).
		Scan(&target.ID)
	if uniqueConstraint(err) == "delivery_targets_public_id_key" {
		return domain.DeliveryTarget{}, false, ErrPublicIDCollision
	}
	if err != nil {
		return domain.DeliveryTarget{}, false, translateDBError(err)
	}
	idempotencyHash := domain.SHA256([]byte("github-delivery\n" + delivery.DeliveryID + "\n" + candidate.AppEnvironmentPublicID))
	payloadHash := domain.SHA256([]byte(trigger + "\n" + metadata.SHA + "\n" + candidate.SourceBranch))
	var buildID int64
	err = tx.QueryRow(ctx, `
		INSERT INTO builds(public_id,workspace_id,project_id,app_id,app_environment_id,requested_by_user_id,
		  github_installation_id,github_installation_external_id,repository_id,repository_full_name,source_branch,
		  commit_sha,commit_title,commit_author_name,commit_author_login,committed_at,trigger_type,github_delivery_id,
		  platform,idempotency_hash,payload_hash)
		SELECT $1,$2,$3,$4,$5,$6,i.id,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,'linux/amd64',$18,$19
		FROM github_installations i WHERE i.github_installation_id=$7 AND i.status='Active'
		RETURNING id`, buildPublicID, candidate.WorkspaceID, candidate.ProjectID, candidate.AppID, candidate.AppEnvironmentID,
		candidate.RequestedByActorID, delivery.InstallationExternalID, delivery.RepositoryID, delivery.RepositoryFullName,
		candidate.SourceBranch, metadata.SHA, metadata.Title, metadata.AuthorName, metadata.AuthorLogin, metadata.CommittedAt,
		trigger, delivery.ID, idempotencyHash, payloadHash).Scan(&buildID)
	if uniqueConstraint(err) == "builds_public_id_key" {
		return domain.DeliveryTarget{}, false, ErrPublicIDCollision
	}
	if err != nil {
		return domain.DeliveryTarget{}, false, translateDBError(err)
	}
	if _, err = tx.Exec(ctx, `UPDATE delivery_targets SET build_id=$1,updated_at=now() WHERE id=$2`, buildID, target.ID); err != nil {
		return domain.DeliveryTarget{}, false, err
	}
	target = domain.DeliveryTarget{ID: target.ID, PublicID: targetPublicID, GitHubDeliveryID: delivery.ID,
		WorkspaceID: candidate.WorkspaceID, ProjectID: candidate.ProjectID, AppID: candidate.AppID,
		AppEnvironmentID: candidate.AppEnvironmentID, AppEnvironmentPublicID: candidate.AppEnvironmentPublicID,
		RequestedByActorID: candidate.RequestedByActorID, TriggerType: trigger, SourceBranch: candidate.SourceBranch,
		CommitSHA: metadata.SHA, BuildID: buildID, BuildPublicID: buildPublicID, Status: "Building"}
	return target, false, tx.Commit(ctx)
}

func (s *Store) ApplyGitHubInstallationDelivery(ctx context.Context, delivery domain.GitHubDelivery) error {
	switch delivery.EventType {
	case "installation":
		status := "Active"
		if delivery.Action == "suspend" {
			status = "Suspended"
		}
		if delivery.Action == "deleted" {
			status = "Deleted"
		}
		_, err := s.Pool.Exec(ctx, `UPDATE github_installations SET status=$1,updated_at=now() WHERE github_installation_id=$2`, status, delivery.InstallationExternalID)
		if err == nil && status == "Deleted" {
			_, err = s.Pool.Exec(ctx, `DELETE FROM app_github_sources src USING github_installations i WHERE src.github_installation_id=i.id AND i.github_installation_id=$1`, delivery.InstallationExternalID)
		}
		return err
	case "installation_repositories":
		if delivery.Action != "removed" || len(delivery.RepositoryIDs) == 0 {
			return nil
		}
		_, err := s.Pool.Exec(ctx, `DELETE FROM app_github_sources src USING github_installations i WHERE src.github_installation_id=i.id AND i.github_installation_id=$1 AND src.repository_id=ANY($2)`, delivery.InstallationExternalID, delivery.RepositoryIDs)
		return err
	default:
		return nil
	}
}

func (s *Store) DeliveryTargetsToAdvance(ctx context.Context, limit int) ([]domain.DeliveryTarget, error) {
	rows, err := s.Pool.Query(ctx, `
		SELECT t.id,t.public_id,t.github_delivery_id,t.workspace_id,t.project_id,t.app_id,t.app_environment_id,
		       ae.public_id,t.requested_by_user_id,t.trigger_type,t.source_branch,t.commit_sha,COALESCE(t.build_id,0),
		       COALESCE(b.public_id,''),COALESCE(r.public_id,''),COALESCE(t.deployment_id,0),COALESCE(d.public_id,''),t.status,
		       COALESCE(b.status,''),COALESCE(d.status,'')
		FROM delivery_targets t JOIN app_environments ae ON ae.id=t.app_environment_id
		LEFT JOIN builds b ON b.id=t.build_id LEFT JOIN releases r ON r.build_id=b.id LEFT JOIN deployments d ON d.id=t.deployment_id
		WHERE t.status IN ('Building','DeployPending','Deploying') ORDER BY t.id LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []domain.DeliveryTarget{}
	for rows.Next() {
		var target domain.DeliveryTarget
		targets := append(deliveryTargetScanTargets(&target), &target.BuildStatus, &target.DeploymentStatus)
		if err = rows.Scan(targets...); err != nil {
			return nil, err
		}
		items = append(items, target)
	}
	return items, rows.Err()
}

func (s *Store) MarkDeliveryTarget(ctx context.Context, targetID int64, status, code, message string) error {
	terminal := status == "Succeeded" || status == "Failed" || status == "Superseded"
	command, err := s.Pool.Exec(ctx, `UPDATE delivery_targets SET status=$1,error_code=$2,error_message=$3,
		completed_at=CASE WHEN $4 THEN now() ELSE NULL END,updated_at=now() WHERE id=$5`, status, code, message, terminal, targetID)
	if err != nil {
		return err
	}
	if command.RowsAffected() != 1 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) AttachDeliveryDeployment(ctx context.Context, targetID, deploymentID int64) error {
	command, err := s.Pool.Exec(ctx, `UPDATE delivery_targets SET deployment_id=$1,status='Deploying',updated_at=now() WHERE id=$2 AND status='DeployPending'`, deploymentID, targetID)
	if err != nil {
		return err
	}
	if command.RowsAffected() != 1 {
		return ErrConflict
	}
	return nil
}

func deliveryScanTargets(delivery *domain.GitHubDelivery) []any {
	return []any{&delivery.ID, &delivery.DeliveryID, &delivery.EventType, &delivery.Action, &delivery.InstallationExternalID,
		&delivery.RepositoryID, &delivery.RepositoryFullName, &delivery.SourceBranch, &delivery.SourceRef, &delivery.CommitSHA,
		&delivery.TagName, &delivery.RepositoryIDs, &delivery.PayloadHash, &delivery.Status, &delivery.Attempts,
		&delivery.WorkerID, &delivery.FencingToken, &delivery.LeaseUntil}
}

func deliveryTargetScanTargets(target *domain.DeliveryTarget) []any {
	return []any{&target.ID, &target.PublicID, &target.GitHubDeliveryID, &target.WorkspaceID, &target.ProjectID,
		&target.AppID, &target.AppEnvironmentID, &target.AppEnvironmentPublicID, &target.RequestedByActorID,
		&target.TriggerType, &target.SourceBranch, &target.CommitSHA, &target.BuildID, &target.BuildPublicID,
		&target.ReleasePublicID, &target.DeploymentID, &target.DeploymentPublicID, &target.Status}
}
