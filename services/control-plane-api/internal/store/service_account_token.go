package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/molejo-platform/molejo/services/control-plane-api/internal/audit"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/automation"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/principal"
)

type CreateServiceAccountTokenParams struct {
	WorkspaceID            int64
	UserID                 int64
	ProjectPublicID        string
	AppPublicID            string
	ServiceAccountPublicID string
	TokenPublicID          string
	TokenHash              []byte
	ExpiresAt              time.Time
	AuditEvent             audit.Event
}

func (s *Store) CreateServiceAccountToken(ctx context.Context, params CreateServiceAccountTokenParams) (automation.Token, error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return automation.Token{}, err
	}
	defer tx.Rollback(ctx)
	var principalID int64
	err = tx.QueryRow(ctx, `SELECT sa.principal_id FROM service_accounts sa
		JOIN principals p ON p.id=sa.principal_id
		JOIN projects pr ON pr.id=sa.project_id JOIN apps a ON a.id=sa.app_id
		WHERE sa.workspace_id=$1 AND pr.public_id=$2 AND a.public_id=$3 AND p.public_id=$4
		  AND p.status='Active'`, params.WorkspaceID, params.ProjectPublicID, params.AppPublicID, params.ServiceAccountPublicID).Scan(&principalID)
	if errors.Is(err, pgx.ErrNoRows) {
		return automation.Token{}, ErrNotFound
	}
	if err != nil {
		return automation.Token{}, err
	}
	var token automation.Token
	err = tx.QueryRow(ctx, `INSERT INTO service_account_tokens(public_id,principal_id,token_hash,expires_at)
		VALUES($1,$2,$3,$4) RETURNING public_id,expires_at,revoked_at,last_used_at,created_at`, params.TokenPublicID, principalID, params.TokenHash, params.ExpiresAt).
		Scan(&token.PublicID, &token.ExpiresAt, &token.RevokedAt, &token.LastUsedAt, &token.CreatedAt)
	if uniqueConstraint(err) == "service_account_tokens_public_id_key" {
		return automation.Token{}, ErrPublicIDCollision
	}
	if err != nil {
		return automation.Token{}, translateDBError(err)
	}
	event := params.AuditEvent
	event.ActorUserID = &params.UserID
	event.WorkspaceID = &params.WorkspaceID
	event.TargetPublicID = params.TokenPublicID
	if err = insertAudit(ctx, tx, event); err != nil {
		return automation.Token{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return automation.Token{}, err
	}
	return token, nil
}

func (s *Store) ListServiceAccountTokens(ctx context.Context, workspaceID int64, projectPublicID, appPublicID, serviceAccountPublicID string) ([]automation.Token, error) {
	var exists bool
	if err := s.Pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM service_accounts sa
		JOIN principals p ON p.id=sa.principal_id JOIN projects pr ON pr.id=sa.project_id JOIN apps a ON a.id=sa.app_id
		WHERE sa.workspace_id=$1 AND pr.public_id=$2 AND a.public_id=$3 AND p.public_id=$4)`,
		workspaceID, projectPublicID, appPublicID, serviceAccountPublicID).Scan(&exists); err != nil {
		return nil, err
	}
	if !exists {
		return nil, ErrNotFound
	}
	rows, err := s.Pool.Query(ctx, `SELECT t.public_id,t.expires_at,t.revoked_at,t.last_used_at,t.created_at
		FROM service_account_tokens t JOIN principals p ON p.id=t.principal_id
		JOIN service_accounts sa ON sa.principal_id=p.id
		JOIN projects pr ON pr.id=sa.project_id JOIN apps a ON a.id=sa.app_id
		WHERE sa.workspace_id=$1 AND pr.public_id=$2 AND a.public_id=$3 AND p.public_id=$4
		ORDER BY t.created_at,t.public_id`, workspaceID, projectPublicID, appPublicID, serviceAccountPublicID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []automation.Token{}
	for rows.Next() {
		var item automation.Token
		if err = rows.Scan(&item.PublicID, &item.ExpiresAt, &item.RevokedAt, &item.LastUsedAt, &item.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) RevokeServiceAccountToken(ctx context.Context, workspaceID, userID int64, projectPublicID, appPublicID, serviceAccountPublicID, tokenPublicID string, event audit.Event) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	result, err := tx.Exec(ctx, `UPDATE service_account_tokens t SET revoked_at=COALESCE(t.revoked_at,now())
		FROM principals p,service_accounts sa,projects pr,apps a
		WHERE p.id=t.principal_id AND sa.principal_id=p.id AND pr.id=sa.project_id AND a.id=sa.app_id
		  AND sa.workspace_id=$1 AND pr.public_id=$2 AND a.public_id=$3 AND p.public_id=$4 AND t.public_id=$5`,
		workspaceID, projectPublicID, appPublicID, serviceAccountPublicID, tokenPublicID)
	if err != nil {
		return err
	}
	if result.RowsAffected() != 1 {
		return ErrNotFound
	}
	event.ActorUserID = &userID
	event.WorkspaceID = &workspaceID
	event.TargetPublicID = tokenPublicID
	if err = insertAudit(ctx, tx, event); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) AuthenticateServiceAccount(ctx context.Context, tokenHash []byte) (principal.Principal, error) {
	var value principal.Principal
	err := s.Pool.QueryRow(ctx, `SELECT p.id,p.public_id,p.kind,p.display_name,sa.workspace_id,sa.project_id,sa.app_id
		FROM service_account_tokens t JOIN principals p ON p.id=t.principal_id
		JOIN service_accounts sa ON sa.principal_id=p.id
		WHERE t.token_hash=$1 AND t.revoked_at IS NULL AND t.expires_at>now() AND p.status='Active'`, tokenHash).
		Scan(&value.ID, &value.PublicID, &value.Kind, &value.DisplayName, &value.WorkspaceID, &value.ProjectID, &value.AppID)
	if errors.Is(err, pgx.ErrNoRows) {
		return principal.Principal{}, ErrAutomationAuthentication
	}
	if err != nil {
		return principal.Principal{}, err
	}
	if _, err = s.Pool.Exec(ctx, `UPDATE service_account_tokens SET last_used_at=now()
		WHERE token_hash=$1 AND (last_used_at IS NULL OR last_used_at < now()-interval '5 minutes')`, tokenHash); err != nil {
		return principal.Principal{}, err
	}
	return value, nil
}
