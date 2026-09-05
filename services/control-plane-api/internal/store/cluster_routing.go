package store

import (
	"context"
	"encoding/hex"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/molejo-platform/molejo/services/control-plane-api/internal/domain"
)

// WorkspaceCluster is the explicit placement boundary between a logical
// workspace and one runtime cluster.
type WorkspaceCluster struct {
	ClusterID          string    `json:"clusterId"`
	ClusterName        string    `json:"clusterName"`
	Namespace          string    `json:"namespace"`
	State              string    `json:"state"`
	Message            string    `json:"message,omitempty"`
	ObservedGeneration int64     `json:"observedGeneration"`
	CreatedAt          time.Time `json:"createdAt"`
	UpdatedAt          time.Time `json:"updatedAt"`
}

func (s *Store) ListWorkspaceClusters(ctx context.Context, workspaceID int64) ([]WorkspaceCluster, error) {
	rows, err := s.Pool.Query(ctx, `SELECT i.public_id,i.name,wc.namespace_name,wc.state,wc.message,wc.observed_generation,wc.created_at,wc.updated_at
		FROM workspace_clusters wc JOIN agent_installations i ON i.id=wc.installation_id
		WHERE wc.workspace_id=$1 ORDER BY i.id`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]WorkspaceCluster, 0)
	for rows.Next() {
		var item WorkspaceCluster
		if err = rows.Scan(&item.ClusterID, &item.ClusterName, &item.Namespace, &item.State, &item.Message, &item.ObservedGeneration, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) AttachWorkspaceCluster(ctx context.Context, workspaceID, actorID int64, clusterPublicID, operationPublicID string, idempotencyHash, payloadHash []byte) (WorkspaceCluster, domain.Operation, bool, error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return WorkspaceCluster{}, domain.Operation{}, false, err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, "workspace-cluster-attach:"+hex.EncodeToString(idempotencyHash)); err != nil {
		return WorkspaceCluster{}, domain.Operation{}, false, err
	}
	if existing, found, findErr := operationByIdempotency(ctx, tx, workspaceID, actorID, idempotencyHash, payloadHash); findErr != nil {
		return WorkspaceCluster{}, domain.Operation{}, false, findErr
	} else if found {
		binding, bindingErr := workspaceClusterByOperation(ctx, tx, existing.ID)
		return binding, existing, true, bindingErr
	}
	var clusterID int64
	var clusterName, namespace string
	if err = tx.QueryRow(ctx, `SELECT i.id,i.name,w.namespace_name FROM agent_installations i CROSS JOIN workspaces w
		WHERE i.public_id=$1 AND i.status='Active' AND w.id=$2`, clusterPublicID, workspaceID).Scan(&clusterID, &clusterName, &namespace); errors.Is(err, pgx.ErrNoRows) {
		return WorkspaceCluster{}, domain.Operation{}, false, ErrAgentUnavailable
	}
	if err != nil {
		return WorkspaceCluster{}, domain.Operation{}, false, err
	}
	var binding WorkspaceCluster
	binding.ClusterID, binding.ClusterName = clusterPublicID, clusterName
	err = tx.QueryRow(ctx, `INSERT INTO workspace_clusters(workspace_id,installation_id,namespace_name)
		VALUES($1,$2,$3) RETURNING namespace_name,state,message,observed_generation,created_at,updated_at`, workspaceID, clusterID, namespace).
		Scan(&binding.Namespace, &binding.State, &binding.Message, &binding.ObservedGeneration, &binding.CreatedAt, &binding.UpdatedAt)
	if uniqueConstraint(err) != "" {
		return WorkspaceCluster{}, domain.Operation{}, false, ErrConflict
	}
	if err != nil {
		return WorkspaceCluster{}, domain.Operation{}, false, err
	}
	var operation domain.Operation
	err = tx.QueryRow(ctx, `INSERT INTO operations(public_id,workspace_id,requested_by_user_id,kind,status,idempotency_hash,payload_hash,desired_version,agent_installation_id)
		VALUES($1,$2,$3,'EnsureWorkspace','Pending',$4,$5,1,$6)
		RETURNING id,public_id,workspace_id,requested_by_user_id,kind,status,desired_version,attempts,created_at,updated_at`,
		operationPublicID, workspaceID, actorID, idempotencyHash, payloadHash, clusterID).
		Scan(&operation.ID, &operation.PublicID, &operation.WorkspaceID, &operation.ActorID, &operation.Kind, &operation.Status,
			&operation.DesiredVersion, &operation.Attempts, &operation.CreatedAt, &operation.UpdatedAt)
	if err != nil {
		return WorkspaceCluster{}, domain.Operation{}, false, translateDBError(err)
	}
	operation.ClusterID = clusterID
	return binding, operation, false, tx.Commit(ctx)
}

func workspaceClusterByOperation(ctx context.Context, query rowQuerier, operationID int64) (WorkspaceCluster, error) {
	var item WorkspaceCluster
	err := query.QueryRow(ctx, `SELECT i.public_id,i.name,wc.namespace_name,wc.state,wc.message,wc.observed_generation,wc.created_at,wc.updated_at
		FROM operations o JOIN workspace_clusters wc ON wc.workspace_id=o.workspace_id AND wc.installation_id=o.agent_installation_id
		JOIN agent_installations i ON i.id=wc.installation_id WHERE o.id=$1`, operationID).
		Scan(&item.ClusterID, &item.ClusterName, &item.Namespace, &item.State, &item.Message, &item.ObservedGeneration, &item.CreatedAt, &item.UpdatedAt)
	return item, err
}

func clusterByPublicID(ctx context.Context, query rowQuerier, publicID string) (int64, error) {
	var id int64
	err := query.QueryRow(ctx, `SELECT id FROM agent_installations WHERE public_id=$1 AND status='Active'`, publicID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, ErrAgentUnavailable
	}
	return id, err
}
