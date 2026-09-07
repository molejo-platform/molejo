package store

import (
	"context"
	"errors"
	"math"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/molejo-platform/molejo/services/control-plane-api/internal/audit"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/domain"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/identity"
)

type InstallationUser struct {
	identity.User
	InstallationAdministrator bool `json:"installationAdministrator"`
}

type UserInvitation struct {
	PublicID  string
	TokenHash []byte
	ExpiresAt time.Time
}

func scanInstallationUser(row pgx.Row) (InstallationUser, error) {
	var item InstallationUser
	err := row.Scan(
		&item.ID,
		&item.PublicID,
		&item.Username,
		&item.DisplayName,
		&item.Status,
		&item.Version,
		&item.AuthVersion,
		&item.CreatedAt,
		&item.UpdatedAt,
		&item.InstallationAdministrator,
	)
	return item, err
}

const installationUserColumns = `u.id,u.public_id,u.username,u.display_name,u.status,u.version,u.auth_version,u.created_at,u.updated_at,
	EXISTS(SELECT 1 FROM installation_role_assignments ira WHERE ira.user_id=u.id AND ira.role='Administrator')`

func (s *Store) FindInstallationUser(ctx context.Context, publicID string) (InstallationUser, error) {
	item, err := scanInstallationUser(s.Pool.QueryRow(ctx, `SELECT `+installationUserColumns+` FROM users u WHERE u.public_id=$1`, publicID))
	if errors.Is(err, pgx.ErrNoRows) {
		return InstallationUser{}, ErrNotFound
	}
	return item, err
}

func (s *Store) ListInstallationUsers(ctx context.Context, beforeID int64, limit int) ([]InstallationUser, string, error) {
	if beforeID == 0 {
		beforeID = math.MaxInt64
	}
	rows, err := s.Pool.Query(ctx, `SELECT `+installationUserColumns+` FROM users u WHERE u.id<$1 ORDER BY u.id DESC LIMIT $2`, beforeID, limit+1)
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()
	items := make([]InstallationUser, 0, limit)
	for rows.Next() {
		item, scanErr := scanInstallationUser(rows)
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

func (s *Store) CreateInvitedUser(ctx context.Context, user identity.User, installationAdmin bool, invitation UserInvitation, createdByUserID int64, event audit.Event) (InstallationUser, error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return InstallationUser{}, err
	}
	defer tx.Rollback(ctx)
	created, err := scanUser(tx.QueryRow(ctx, `INSERT INTO users(public_id,username,username_key,display_name,status)
		VALUES($1,$2,$2,$3,'Invited') RETURNING `+userColumns, user.PublicID, user.Username, user.DisplayName))
	if err != nil {
		return InstallationUser{}, translateUserManagementDBError(err)
	}
	if installationAdmin {
		if _, err = tx.Exec(ctx, `INSERT INTO installation_role_assignments(user_id,role) VALUES($1,'Administrator')`, created.ID); err != nil {
			return InstallationUser{}, err
		}
	}
	if _, err = tx.Exec(ctx, `INSERT INTO user_invitations(public_id,user_id,token_hash,expires_at,created_by_user_id)
		VALUES($1,$2,$3,$4,$5)`, invitation.PublicID, created.ID, invitation.TokenHash, invitation.ExpiresAt, createdByUserID); err != nil {
		return InstallationUser{}, translateUserManagementDBError(err)
	}
	event.TargetPublicID = created.PublicID
	event.Metadata = map[string]any{"installationAdministrator": installationAdmin, "invitationId": invitation.PublicID}
	if err = insertAudit(ctx, tx, event); err != nil {
		return InstallationUser{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return InstallationUser{}, err
	}
	return InstallationUser{User: created, InstallationAdministrator: installationAdmin}, nil
}

func (s *Store) CreateUserInvitation(ctx context.Context, userPublicID string, invitation UserInvitation, createdByUserID int64, event audit.Event) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var userID int64
	if err = tx.QueryRow(ctx, `SELECT id FROM users WHERE public_id=$1 AND status='Invited' FOR UPDATE`, userPublicID).Scan(&userID); errors.Is(err, pgx.ErrNoRows) {
		return ErrConflict
	}
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE user_invitations SET revoked_at=now() WHERE user_id=$1 AND accepted_at IS NULL AND revoked_at IS NULL`, userID); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO user_invitations(public_id,user_id,token_hash,expires_at,created_by_user_id)
		VALUES($1,$2,$3,$4,$5)`, invitation.PublicID, userID, invitation.TokenHash, invitation.ExpiresAt, createdByUserID); err != nil {
		return translateUserManagementDBError(err)
	}
	event.TargetPublicID = userPublicID
	event.Metadata = map[string]any{"invitationId": invitation.PublicID}
	if err = insertAudit(ctx, tx, event); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func translateUserManagementDBError(err error) error {
	switch uniqueConstraint(err) {
	case "users_public_id_key", "user_invitations_public_id_key", "user_invitations_token_hash_key":
		return ErrPublicIDCollision
	default:
		return translateDBError(err)
	}
}

func (s *Store) AcceptUserInvitation(ctx context.Context, tokenHash []byte, passwordHash string, event audit.Event) (identity.User, error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return identity.User{}, err
	}
	defer tx.Rollback(ctx)
	var invitationID, userID int64
	err = tx.QueryRow(ctx, `SELECT ui.id,ui.user_id FROM user_invitations ui JOIN users u ON u.id=ui.user_id
		WHERE ui.token_hash=$1 AND ui.accepted_at IS NULL AND ui.revoked_at IS NULL AND ui.expires_at>now() AND u.status='Invited'
		FOR UPDATE OF ui,u`, tokenHash).Scan(&invitationID, &userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return identity.User{}, ErrInvitationInvalid
	}
	if err != nil {
		return identity.User{}, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO password_credentials(user_id,password_hash) VALUES($1,$2)`, userID, passwordHash); err != nil {
		return identity.User{}, translateDBError(err)
	}
	user, err := scanUser(tx.QueryRow(ctx, `UPDATE users SET status='Active',version=version+1,auth_version=auth_version+1,updated_at=now()
		WHERE id=$1 RETURNING `+userColumns, userID))
	if err != nil {
		return identity.User{}, err
	}
	if _, err = tx.Exec(ctx, `UPDATE user_invitations SET accepted_at=now() WHERE id=$1`, invitationID); err != nil {
		return identity.User{}, err
	}
	event.ActorUserID = &user.ID
	event.TargetPublicID = user.PublicID
	if err = insertAudit(ctx, tx, event); err != nil {
		return identity.User{}, err
	}
	return user, tx.Commit(ctx)
}

func (s *Store) SetInstallationAdministrator(ctx context.Context, publicID string, administrator bool, version int64, event audit.Event) (InstallationUser, error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return InstallationUser{}, err
	}
	defer tx.Rollback(ctx)
	if err = lockIdentityGovernance(ctx, tx); err != nil {
		return InstallationUser{}, err
	}
	var userID int64
	var status string
	var current bool
	err = tx.QueryRow(ctx, `SELECT u.id,u.status,EXISTS(SELECT 1 FROM installation_role_assignments r WHERE r.user_id=u.id AND r.role='Administrator')
		FROM users u WHERE u.public_id=$1 AND u.version=$2 FOR UPDATE`, publicID, version).Scan(&userID, &status, &current)
	if errors.Is(err, pgx.ErrNoRows) {
		return InstallationUser{}, ErrVersionConflict
	}
	if err != nil {
		return InstallationUser{}, err
	}
	if current == administrator {
		return scanInstallationUser(tx.QueryRow(ctx, `SELECT `+installationUserColumns+` FROM users u WHERE u.id=$1`, userID))
	}
	if current && status == identity.StatusActive {
		allowed, checkErr := hasAnotherActiveAdministrator(ctx, tx, userID)
		if checkErr != nil {
			return InstallationUser{}, checkErr
		}
		if !allowed {
			return InstallationUser{}, ErrGovernanceInvariant
		}
	}
	if administrator {
		_, err = tx.Exec(ctx, `INSERT INTO installation_role_assignments(user_id,role) VALUES($1,'Administrator')`, userID)
	} else {
		_, err = tx.Exec(ctx, `DELETE FROM installation_role_assignments WHERE user_id=$1 AND role='Administrator'`, userID)
	}
	if err != nil {
		return InstallationUser{}, err
	}
	if _, err = tx.Exec(ctx, `UPDATE users SET version=version+1,auth_version=auth_version+1,updated_at=now() WHERE id=$1`, userID); err != nil {
		return InstallationUser{}, err
	}
	if _, err = tx.Exec(ctx, `UPDATE sessions SET revoked_at=now() WHERE user_id=$1 AND revoked_at IS NULL`, userID); err != nil {
		return InstallationUser{}, err
	}
	event.TargetPublicID = publicID
	event.Metadata = map[string]any{"installationAdministrator": administrator}
	if err = insertAudit(ctx, tx, event); err != nil {
		return InstallationUser{}, err
	}
	item, err := scanInstallationUser(tx.QueryRow(ctx, `SELECT `+installationUserColumns+` FROM users u WHERE u.id=$1`, userID))
	if err != nil {
		return InstallationUser{}, err
	}
	return item, tx.Commit(ctx)
}

func lockIdentityGovernance(ctx context.Context, tx pgx.Tx) error {
	_, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('molejo:identity-governance'))`)
	return err
}

func hasAnotherActiveAdministrator(ctx context.Context, tx pgx.Tx, excludedUserID int64) (bool, error) {
	var exists bool
	err := tx.QueryRow(ctx, `SELECT EXISTS(
		SELECT 1 FROM installation_role_assignments r JOIN users u ON u.id=r.user_id
		WHERE r.role='Administrator' AND u.status='Active' AND u.id<>$1
	)`, excludedUserID).Scan(&exists)
	return exists, err
}

func wouldOrphanOwnedWorkspace(ctx context.Context, tx pgx.Tx, userID int64) (bool, error) {
	var orphaned bool
	err := tx.QueryRow(ctx, `SELECT EXISTS(
		SELECT 1 FROM workspace_memberships owned
		WHERE owned.user_id=$1 AND owned.role='Owner' AND owned.status='Active'
		  AND NOT EXISTS (
			SELECT 1 FROM workspace_memberships other JOIN users u ON u.id=other.user_id
			WHERE other.workspace_id=owned.workspace_id AND other.user_id<>$1
			  AND other.role='Owner' AND other.status='Active' AND u.status='Active'
		  )
	)`, userID).Scan(&orphaned)
	return orphaned, err
}

func hasAnotherActiveWorkspaceOwner(ctx context.Context, tx pgx.Tx, workspaceID, excludedUserID int64) (bool, error) {
	var exists bool
	err := tx.QueryRow(ctx, `SELECT EXISTS(
		SELECT 1 FROM workspace_memberships wm JOIN users u ON u.id=wm.user_id
		WHERE wm.workspace_id=$1 AND wm.user_id<>$2 AND wm.role='Owner' AND wm.status='Active' AND u.status='Active'
	)`, workspaceID, excludedUserID).Scan(&exists)
	return exists, err
}

func (s *Store) ListInstallationAuditEvents(ctx context.Context, beforeID int64, limit int) ([]AuditEventView, string, error) {
	if beforeID == 0 {
		beforeID = math.MaxInt64
	}
	rows, err := s.Pool.Query(ctx, `SELECT ae.id,ae.public_id,ae.occurred_at,COALESCE(u.public_id,''),COALESCE(p.public_id,''),COALESCE(p.kind,''),ae.action,ae.target_type,ae.target_public_id,ae.outcome,ae.reason,ae.request_id,ae.metadata_json
		FROM audit_events ae LEFT JOIN users u ON u.id=ae.actor_user_id LEFT JOIN principals p ON p.id=ae.actor_principal_id
		WHERE ae.workspace_id IS NULL AND ae.id<$1 ORDER BY ae.id DESC LIMIT $2`, beforeID, limit+1)
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()
	items := []AuditEventView{}
	var lastID int64
	for rows.Next() {
		var id int64
		var item AuditEventView
		if err = rows.Scan(&id, &item.PublicID, &item.OccurredAt, &item.ActorUserID, &item.ActorID, &item.ActorKind, &item.Action, &item.TargetType, &item.TargetPublicID, &item.Outcome, &item.Reason, &item.RequestID, &item.Metadata); err != nil {
			return nil, "", err
		}
		if len(items) == limit {
			return items, domain.EncodeCursor(lastID), nil
		}
		items = append(items, item)
		lastID = id
	}
	return items, "", rows.Err()
}
