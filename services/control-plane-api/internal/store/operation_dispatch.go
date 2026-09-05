package store

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/molejo-platform/molejo/services/control-plane-api/internal/domain"
)

// ClaimedOperationForAgent reloads the authoritative lease before a result is
// accepted. This intentionally does not depend on process memory, so fencing
// remains correct across API replicas and restarts.
func (s *Store) ClaimedOperationForAgent(ctx context.Context, installationPublicID, operationPublicID string, fencingToken int64) (domain.Operation, error) {
	var item domain.Operation
	err := s.Pool.QueryRow(ctx, `SELECT o.id,o.public_id,o.workspace_id,COALESCE(o.agent_installation_id,0),
		COALESCE(o.app_environment_id,0),COALESCE(o.deployment_id,0),COALESCE(o.app_volume_id,0),
		o.requested_by_user_id,o.kind,o.status,o.desired_version,o.attempts,o.worker_id,o.fencing_token,o.lease_until,o.created_at,o.updated_at
		FROM operations o JOIN agent_installations i ON i.id=o.agent_installation_id
		WHERE i.public_id=$1 AND o.public_id=$2 AND o.status='Running' AND o.worker_id=$3
		  AND o.fencing_token=$4 AND o.lease_until>now()`, installationPublicID, operationPublicID,
		"agent:"+installationPublicID, fencingToken).
		Scan(&item.ID, &item.PublicID, &item.WorkspaceID, &item.ClusterID, &item.AppEnvironmentID, &item.DeploymentID,
			&item.AppVolumeID, &item.ActorID, &item.Kind, &item.Status, &item.DesiredVersion, &item.Attempts,
			&item.WorkerID, &item.FencingToken, &item.LeaseUntil, &item.CreatedAt, &item.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Operation{}, ErrLeaseLost
	}
	return item, err
}
