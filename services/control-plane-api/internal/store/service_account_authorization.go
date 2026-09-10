package store

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/molejo-platform/molejo/services/control-plane-api/internal/automation"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/domain"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/principal"
)

var (
	ErrAutomationAuthentication = errors.New("automation credential is invalid")
	ErrAutomationAuthorization  = errors.New("automation principal is not authorized")
)

func (s *Store) AuthorizeServiceAccount(ctx context.Context, value principal.Principal, workspacePublicID, projectPublicID, appPublicID, appEnvironmentPublicID string, permission automation.Permission) (domain.Workspace, error) {
	var workspace domain.Workspace
	var allowed bool
	err := s.Pool.QueryRow(ctx, `SELECT w.id,w.public_id,w.name,w.namespace_name,w.version,w.bootstrap_state,w.created_at,w.updated_at,
		EXISTS(SELECT 1 FROM service_account_grants g
			WHERE g.principal_id=$1 AND g.app_id=a.id AND g.permission=$5
			  AND (($5='release.write' AND g.app_environment_id IS NULL)
			    OR ($5='deployment.create' AND g.app_environment_id=ae.id)))
		FROM service_accounts sa JOIN workspaces w ON w.id=sa.workspace_id
		JOIN projects pr ON pr.id=sa.project_id JOIN apps a ON a.id=sa.app_id
		LEFT JOIN app_environments ae ON ae.app_id=a.id AND ae.public_id=NULLIF($4,'') AND ae.archived_at IS NULL
		WHERE sa.principal_id=$1 AND w.public_id=$2 AND pr.public_id=$3 AND a.public_id=$6`,
		value.ID, workspacePublicID, projectPublicID, appEnvironmentPublicID, permission, appPublicID).
		Scan(&workspace.ID, &workspace.PublicID, &workspace.Name, &workspace.Namespace, &workspace.Version, &workspace.BootstrapState, &workspace.CreatedAt, &workspace.UpdatedAt, &allowed)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Workspace{}, ErrAutomationAuthorization
	}
	if err != nil {
		return domain.Workspace{}, err
	}
	if !allowed {
		return domain.Workspace{}, ErrAutomationAuthorization
	}
	return workspace, nil
}
