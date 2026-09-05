package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/molejo-platform/molejo/services/control-plane-api/internal/audit"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/automation"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/domain"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/principal"
)

var (
	ErrAutomationAuthentication = errors.New("automation credential is invalid")
	ErrAutomationAuthorization  = errors.New("automation principal is not authorized")
)

func (s *Store) CreateServiceAccount(ctx context.Context, workspaceID, userID int64, projectPublicID, appPublicID, serviceAccountPublicID, tokenPublicID, name string, deploymentEnvironmentIDs []string, tokenHash []byte, expiresAt time.Time, event audit.Event) (automation.ServiceAccount, error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return automation.ServiceAccount{}, err
	}
	defer tx.Rollback(ctx)

	var projectID, appID int64
	var workspacePublicID string
	err = tx.QueryRow(ctx, `SELECT p.id,a.id,w.public_id FROM projects p
		JOIN apps a ON a.project_id=p.id JOIN workspaces w ON w.id=p.workspace_id
		WHERE p.workspace_id=$1 AND p.public_id=$2 AND a.public_id=$3
		  AND p.archived_at IS NULL AND a.archived_at IS NULL`, workspaceID, projectPublicID, appPublicID).
		Scan(&projectID, &appID, &workspacePublicID)
	if errors.Is(err, pgx.ErrNoRows) {
		return automation.ServiceAccount{}, ErrNotFound
	}
	if err != nil {
		return automation.ServiceAccount{}, err
	}

	var principalID int64
	if err = tx.QueryRow(ctx, `INSERT INTO principals(public_id,kind,display_name)
		VALUES($1,'ServiceAccount',$2) RETURNING id`, serviceAccountPublicID, name).Scan(&principalID); err != nil {
		if uniqueConstraint(err) == "principals_public_id_key" {
			return automation.ServiceAccount{}, ErrPublicIDCollision
		}
		return automation.ServiceAccount{}, translateDBError(err)
	}
	if _, err = tx.Exec(ctx, `INSERT INTO service_accounts(principal_id,workspace_id,project_id,app_id,name,created_by_user_id)
		VALUES($1,$2,$3,$4,$5,$6)`, principalID, workspaceID, projectID, appID, name, userID); err != nil {
		return automation.ServiceAccount{}, translateDBError(err)
	}
	if _, err = tx.Exec(ctx, `INSERT INTO service_account_tokens(public_id,principal_id,token_hash,expires_at)
		VALUES($1,$2,$3,$4)`, tokenPublicID, principalID, tokenHash, expiresAt); err != nil {
		if uniqueConstraint(err) == "service_account_tokens_public_id_key" {
			return automation.ServiceAccount{}, ErrPublicIDCollision
		}
		return automation.ServiceAccount{}, translateDBError(err)
	}
	if _, err = tx.Exec(ctx, `INSERT INTO service_account_grants(principal_id,app_id,permission)
		VALUES($1,$2,$3)`, principalID, appID, automation.PermissionReleaseWrite); err != nil {
		return automation.ServiceAccount{}, err
	}
	for _, publicID := range deploymentEnvironmentIDs {
		result, insertErr := tx.Exec(ctx, `INSERT INTO service_account_grants(principal_id,app_id,permission,app_environment_id)
			SELECT $1,$2,$3,ae.id FROM app_environments ae
			WHERE ae.workspace_id=$4 AND ae.app_id=$5 AND ae.public_id=$6 AND ae.archived_at IS NULL`,
			principalID, appID, automation.PermissionDeploymentCreate, workspaceID, appID, publicID)
		if insertErr != nil {
			return automation.ServiceAccount{}, translateDBError(insertErr)
		}
		if result.RowsAffected() != 1 {
			return automation.ServiceAccount{}, ErrNotFound
		}
	}
	var createdAt, updatedAt time.Time
	if err = tx.QueryRow(ctx, `SELECT sa.created_at,p.updated_at FROM service_accounts sa JOIN principals p ON p.id=sa.principal_id WHERE sa.principal_id=$1`, principalID).Scan(&createdAt, &updatedAt); err != nil {
		return automation.ServiceAccount{}, err
	}
	event.ActorUserID = &userID
	event.TargetPublicID = serviceAccountPublicID
	if err = insertAudit(ctx, tx, event); err != nil {
		return automation.ServiceAccount{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return automation.ServiceAccount{}, err
	}
	return automation.ServiceAccount{
		PublicID: serviceAccountPublicID, WorkspacePublicID: workspacePublicID, ProjectPublicID: projectPublicID,
		AppPublicID: appPublicID, Name: name, Status: "Active", DeploymentEnvironmentIDs: deploymentEnvironmentIDs,
		CreatedAt: createdAt, UpdatedAt: updatedAt,
	}, nil
}

func (s *Store) ListServiceAccounts(ctx context.Context, workspaceID int64, projectPublicID, appPublicID string) ([]automation.ServiceAccount, error) {
	rows, err := s.Pool.Query(ctx, `SELECT p.public_id,w.public_id,pr.public_id,a.public_id,sa.name,p.status,sa.created_at,p.updated_at,
		COALESCE(array_agg(ae.public_id ORDER BY ae.public_id) FILTER (WHERE g.permission='deployment.create'),'{}')
		FROM service_accounts sa JOIN principals p ON p.id=sa.principal_id
		JOIN workspaces w ON w.id=sa.workspace_id JOIN projects pr ON pr.id=sa.project_id JOIN apps a ON a.id=sa.app_id
		LEFT JOIN service_account_grants g ON g.principal_id=sa.principal_id
		LEFT JOIN app_environments ae ON ae.id=g.app_environment_id
		WHERE sa.workspace_id=$1 AND pr.public_id=$2 AND a.public_id=$3
		GROUP BY p.public_id,w.public_id,pr.public_id,a.public_id,sa.name,p.status,sa.created_at,p.updated_at
		ORDER BY sa.created_at,p.public_id`, workspaceID, projectPublicID, appPublicID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []automation.ServiceAccount{}
	for rows.Next() {
		var item automation.ServiceAccount
		if err = rows.Scan(&item.PublicID, &item.WorkspacePublicID, &item.ProjectPublicID, &item.AppPublicID, &item.Name, &item.Status, &item.CreatedAt, &item.UpdatedAt, &item.DeploymentEnvironmentIDs); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) RevokeServiceAccount(ctx context.Context, workspaceID, userID int64, projectPublicID, appPublicID, serviceAccountPublicID string, event audit.Event) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	result, err := tx.Exec(ctx, `UPDATE principals p SET status='Disabled',updated_at=now()
		FROM service_accounts sa JOIN projects pr ON pr.id=sa.project_id JOIN apps a ON a.id=sa.app_id
		WHERE p.id=sa.principal_id AND p.public_id=$1 AND sa.workspace_id=$2 AND pr.public_id=$3 AND a.public_id=$4`,
		serviceAccountPublicID, workspaceID, projectPublicID, appPublicID)
	if err != nil {
		return err
	}
	if result.RowsAffected() != 1 {
		return ErrNotFound
	}
	if _, err = tx.Exec(ctx, `UPDATE service_account_tokens t SET revoked_at=COALESCE(revoked_at,now())
		FROM principals p WHERE p.id=t.principal_id AND p.public_id=$1`, serviceAccountPublicID); err != nil {
		return err
	}
	event.ActorUserID = &userID
	event.TargetPublicID = serviceAccountPublicID
	if err = insertAudit(ctx, tx, event); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) AuthenticateServiceAccount(ctx context.Context, tokenHash []byte) (principal.Principal, error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return principal.Principal{}, err
	}
	defer tx.Rollback(ctx)
	var value principal.Principal
	err = tx.QueryRow(ctx, `SELECT p.id,p.public_id,p.kind,p.display_name,sa.workspace_id,sa.project_id,sa.app_id
		FROM service_account_tokens t JOIN principals p ON p.id=t.principal_id
		JOIN service_accounts sa ON sa.principal_id=p.id
		WHERE t.token_hash=$1 AND t.revoked_at IS NULL AND t.expires_at>now() AND p.status='Active'
		FOR UPDATE OF t`, tokenHash).
		Scan(&value.ID, &value.PublicID, &value.Kind, &value.DisplayName, &value.WorkspaceID, &value.ProjectID, &value.AppID)
	if errors.Is(err, pgx.ErrNoRows) {
		return principal.Principal{}, ErrAutomationAuthentication
	}
	if err != nil {
		return principal.Principal{}, err
	}
	value.ServiceAccountID = value.ID
	if _, err = tx.Exec(ctx, `UPDATE service_account_tokens SET last_used_at=now() WHERE token_hash=$1`, tokenHash); err != nil {
		return principal.Principal{}, err
	}
	return value, tx.Commit(ctx)
}

func (s *Store) AuthorizeServiceAccount(ctx context.Context, value principal.Principal, workspacePublicID, projectPublicID, appPublicID, appEnvironmentPublicID, permission string) (domain.Workspace, error) {
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
	if errors.Is(err, pgx.ErrNoRows) || err == nil && !allowed {
		return domain.Workspace{}, ErrAutomationAuthorization
	}
	return workspace, err
}
