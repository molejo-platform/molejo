package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

type AvailabilityClusterFacts struct {
	ClusterID    string
	Attached     bool
	LastSeenAt   *time.Time
	Capabilities []string
}

func (s *Store) FeatureAvailabilityClusterFacts(ctx context.Context, workspaceID int64, scopeType, scopeID string) (AvailabilityClusterFacts, error) {
	if err := s.validateAvailabilityScope(ctx, workspaceID, scopeType, scopeID); err != nil {
		return AvailabilityClusterFacts{}, err
	}
	query := `SELECT i.public_id,i.last_seen_at,i.capabilities_json
		FROM workspace_clusters wc JOIN agent_installations i ON i.id=wc.installation_id
		WHERE wc.workspace_id=$1 AND i.status='Active' ORDER BY i.id LIMIT 1`
	arguments := []any{workspaceID}
	if scopeType == "AppEnvironment" {
		query = `SELECT i.public_id,i.last_seen_at,i.capabilities_json
			FROM app_environments ae JOIN agent_installations i ON i.id=ae.cluster_id
			WHERE ae.workspace_id=$1 AND ae.public_id=$2 AND ae.archived_at IS NULL AND i.status='Active'`
		arguments = append(arguments, scopeID)
	}
	var facts AvailabilityClusterFacts
	var capabilities []byte
	err := s.Pool.QueryRow(ctx, query, arguments...).Scan(&facts.ClusterID, &facts.LastSeenAt, &capabilities)
	if errors.Is(err, pgx.ErrNoRows) {
		return facts, nil
	}
	if err != nil {
		return AvailabilityClusterFacts{}, err
	}
	if len(capabilities) > 0 {
		if err = json.Unmarshal(capabilities, &facts.Capabilities); err != nil {
			return AvailabilityClusterFacts{}, err
		}
	}
	facts.Attached = true
	if facts.Capabilities == nil {
		facts.Capabilities = []string{}
	}
	return facts, nil
}

func (s *Store) validateAvailabilityScope(ctx context.Context, workspaceID int64, scopeType, scopeID string) error {
	var exists bool
	var err error
	switch scopeType {
	case "Workspace":
		err = s.Pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM workspaces WHERE id=$1 AND public_id=$2)`, workspaceID, scopeID).Scan(&exists)
	case "App":
		err = s.Pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM apps a JOIN projects p ON p.id=a.project_id WHERE p.workspace_id=$1 AND a.public_id=$2 AND a.archived_at IS NULL)`, workspaceID, scopeID).Scan(&exists)
	case "AppEnvironment":
		err = s.Pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM app_environments WHERE workspace_id=$1 AND public_id=$2 AND archived_at IS NULL)`, workspaceID, scopeID).Scan(&exists)
	default:
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if !exists {
		return ErrNotFound
	}
	return nil
}
