package store

import (
	"context"

	"github.com/molejo-platform/molejo/services/control-plane-api/internal/domain"
)

func (s *Store) WorkspaceSummary(ctx context.Context, workspaceID int64) (domain.WorkspaceSummary, error) {
	var summary domain.WorkspaceSummary
	err := s.Pool.QueryRow(ctx, `SELECT
		(SELECT count(*) FROM projects WHERE workspace_id=$1 AND archived_at IS NULL),
		(SELECT count(*) FROM environments e JOIN projects p ON p.id=e.project_id WHERE p.workspace_id=$1 AND p.archived_at IS NULL AND e.archived_at IS NULL),
		(SELECT count(*) FROM apps a JOIN projects p ON p.id=a.project_id WHERE p.workspace_id=$1 AND p.archived_at IS NULL AND a.archived_at IS NULL),
		(SELECT count(*) FROM app_environments WHERE workspace_id=$1 AND archived_at IS NULL),
		(SELECT count(DISTINCT a.id) FROM apps a JOIN projects p ON p.id=a.project_id JOIN app_github_sources s ON s.app_id=a.id WHERE p.workspace_id=$1 AND p.archived_at IS NULL AND a.archived_at IS NULL),
		(SELECT count(DISTINCT a.id) FROM apps a JOIN projects p ON p.id=a.project_id JOIN releases r ON r.app_id=a.id WHERE p.workspace_id=$1 AND p.archived_at IS NULL AND a.archived_at IS NULL),
		(SELECT count(*) FROM app_environments WHERE workspace_id=$1 AND archived_at IS NULL AND current_deployment_id IS NOT NULL),
		(SELECT count(*) FROM workspace_clusters WHERE workspace_id=$1),
		(SELECT count(*) FROM workspace_clusters WHERE workspace_id=$1 AND state='Ready'),
		(SELECT count(*) FROM app_environments WHERE workspace_id=$1 AND archived_at IS NULL AND last_state='Ready'),
		(SELECT count(*) FROM app_environments WHERE workspace_id=$1 AND archived_at IS NULL AND last_state IN ('Pending','Progressing')),
		(SELECT count(*) FROM app_environments WHERE workspace_id=$1 AND archived_at IS NULL AND last_state IN ('Degraded','Unknown')),
		(SELECT count(*) FROM operations WHERE workspace_id=$1 AND status IN ('Pending','Running')),
		(SELECT count(*) FROM operations WHERE workspace_id=$1 AND status='Failed')`, workspaceID).Scan(
		&summary.Counts.Projects,
		&summary.Counts.Environments,
		&summary.Counts.Apps,
		&summary.Counts.AppEnvironments,
		&summary.Counts.AppsWithSource,
		&summary.Counts.AppsWithRelease,
		&summary.Counts.DeployedAppEnvironments,
		&summary.Clusters.Total,
		&summary.Clusters.Ready,
		&summary.Runtime.Ready,
		&summary.Runtime.Progressing,
		&summary.Runtime.Degraded,
		&summary.Operations.Active,
		&summary.Operations.Failed,
	)
	return summary, err
}
