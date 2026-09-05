package store

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/molejo-platform/molejo/services/control-plane-api/internal/audit"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/authorization"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/domain"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/identity"
)

type SessionPrincipal struct {
	SessionID      int64
	PublicID       string
	UserID         int64
	CSRFHash       []byte
	AssuranceLevel string
}

type Membership struct {
	WorkspaceID       int64     `json:"-"`
	WorkspacePublicID string    `json:"workspaceId"`
	UserID            int64     `json:"-"`
	UserPublicID      string    `json:"userId"`
	Username          string    `json:"username"`
	DisplayName       string    `json:"displayName"`
	Role              string    `json:"role"`
	Status            string    `json:"status"`
	Version           int64     `json:"version"`
	CreatedAt         time.Time `json:"createdAt"`
	UpdatedAt         time.Time `json:"updatedAt"`
}

type Group struct {
	ID          int64     `json:"-"`
	PublicID    string    `json:"id"`
	WorkspaceID int64     `json:"-"`
	Name        string    `json:"name"`
	Version     int64     `json:"version"`
	MemberCount int       `json:"memberCount"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

type AccessGrant struct {
	ID               int64     `json:"-"`
	PublicID         string    `json:"id"`
	WorkspaceID      int64     `json:"-"`
	SubjectType      string    `json:"subjectType"`
	SubjectID        int64     `json:"-"`
	SubjectPublicID  string    `json:"subjectId"`
	SubjectName      string    `json:"subjectName"`
	ResourceType     string    `json:"resourceType"`
	ResourcePublicID string    `json:"resourceId"`
	Relation         string    `json:"relation"`
	CreatedAt        time.Time `json:"createdAt"`
}

type AuditEventView struct {
	PublicID       string         `json:"id"`
	OccurredAt     time.Time      `json:"occurredAt"`
	ActorUserID    string         `json:"actorUserId,omitempty"`
	ActorID        string         `json:"actorId,omitempty"`
	ActorKind      string         `json:"actorKind,omitempty"`
	Action         string         `json:"action"`
	TargetType     string         `json:"targetType"`
	TargetPublicID string         `json:"targetId,omitempty"`
	Outcome        string         `json:"outcome"`
	Reason         string         `json:"reason,omitempty"`
	RequestID      string         `json:"requestId,omitempty"`
	Metadata       map[string]any `json:"metadata"`
}

type PasswordResetGrant struct {
	ID         int64
	PublicID   string
	UserID     int64
	CodeHash   []byte
	Attempts   int
	ExpiresAt  time.Time
	VerifiedAt *time.Time
	ConsumedAt *time.Time
}

type SessionInfo struct {
	PublicID       string    `json:"id"`
	AssuranceLevel string    `json:"assuranceLevel"`
	LastSeenAt     time.Time `json:"lastSeenAt"`
	ExpiresAt      time.Time `json:"expiresAt"`
	Current        bool      `json:"current"`
}

func scanUser(row pgx.Row) (identity.User, error) {
	var user identity.User
	err := row.Scan(&user.ID, &user.PublicID, &user.Username, &user.DisplayName, &user.Status, &user.Version, &user.AuthVersion, &user.CreatedAt, &user.UpdatedAt)
	return user, err
}

const userColumns = `id,public_id,username,display_name,status,version,auth_version,created_at,updated_at`

func (s *Store) AuthenticateUser(ctx context.Context, username string) (identity.User, string, error) {
	var user identity.User
	var passwordHash string
	err := s.Pool.QueryRow(ctx, `SELECT u.id,u.public_id,u.username,u.display_name,u.status,u.version,u.auth_version,u.created_at,u.updated_at,p.password_hash
		FROM users u JOIN password_credentials p ON p.user_id=u.id WHERE u.username_key=$1`, username).
		Scan(&user.ID, &user.PublicID, &user.Username, &user.DisplayName, &user.Status, &user.Version, &user.AuthVersion, &user.CreatedAt, &user.UpdatedAt, &passwordHash)
	return user, passwordHash, err
}

func (s *Store) User(ctx context.Context, userID int64) (identity.User, error) {
	return scanUser(s.Pool.QueryRow(ctx, `SELECT `+userColumns+` FROM users WHERE id=$1`, userID))
}

func (s *Store) FindUser(ctx context.Context, publicID string) (identity.User, error) {
	user, err := scanUser(s.Pool.QueryRow(ctx, `SELECT `+userColumns+` FROM users WHERE public_id=$1`, publicID))
	if errors.Is(err, pgx.ErrNoRows) {
		return identity.User{}, ErrNotFound
	}
	return user, err
}

func (s *Store) FindUserByUsername(ctx context.Context, username string) (identity.User, error) {
	user, err := scanUser(s.Pool.QueryRow(ctx, `SELECT `+userColumns+` FROM users WHERE username_key=$1 AND status<>'Disabled'`, username))
	if errors.Is(err, pgx.ErrNoRows) {
		return identity.User{}, ErrNotFound
	}
	return user, err
}

func (s *Store) ListUsers(ctx context.Context, beforeID int64, limit int) ([]identity.User, string, error) {
	if beforeID == 0 {
		beforeID = math.MaxInt64
	}
	rows, err := s.Pool.Query(ctx, `SELECT `+userColumns+` FROM users WHERE id<$1 ORDER BY id DESC LIMIT $2`, beforeID, limit+1)
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()
	items := make([]identity.User, 0, limit)
	for rows.Next() {
		var user identity.User
		if err = rows.Scan(&user.ID, &user.PublicID, &user.Username, &user.DisplayName, &user.Status, &user.Version, &user.AuthVersion, &user.CreatedAt, &user.UpdatedAt); err != nil {
			return nil, "", err
		}
		if len(items) == limit {
			return items, domain.EncodeCursor(items[len(items)-1].ID), nil
		}
		items = append(items, user)
	}
	return items, "", rows.Err()
}

func (s *Store) CreateUser(ctx context.Context, user identity.User, passwordHash string, installationAdmin bool, event audit.Event) (identity.User, error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return identity.User{}, err
	}
	defer tx.Rollback(ctx)
	created, err := scanUser(tx.QueryRow(ctx, `INSERT INTO users(public_id,username,username_key,display_name,status)
		VALUES($1,$2,$2,$3,'Active') RETURNING `+userColumns, user.PublicID, user.Username, user.DisplayName))
	if err != nil {
		return identity.User{}, translateDBError(err)
	}
	if _, err = tx.Exec(ctx, `INSERT INTO password_credentials(user_id,password_hash) VALUES($1,$2)`, created.ID, passwordHash); err != nil {
		return identity.User{}, err
	}
	if installationAdmin {
		if _, err = tx.Exec(ctx, `INSERT INTO installation_role_assignments(user_id,role) VALUES($1,'Administrator')`, created.ID); err != nil {
			return identity.User{}, err
		}
	}
	event.TargetPublicID = created.PublicID
	if err = insertAudit(ctx, tx, event); err != nil {
		return identity.User{}, err
	}
	return created, tx.Commit(ctx)
}

func (s *Store) UpdateOwnProfile(ctx context.Context, userID, version int64, displayName string, event audit.Event) (identity.User, error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return identity.User{}, err
	}
	defer tx.Rollback(ctx)
	user, err := scanUser(tx.QueryRow(ctx, `UPDATE users SET display_name=$3,version=version+1,updated_at=now()
		WHERE id=$1 AND version=$2 RETURNING `+userColumns, userID, version, displayName))
	if errors.Is(err, pgx.ErrNoRows) {
		return identity.User{}, ErrVersionConflict
	}
	if err != nil {
		return identity.User{}, err
	}
	event.TargetPublicID = user.PublicID
	if err = insertAudit(ctx, tx, event); err != nil {
		return identity.User{}, err
	}
	return user, tx.Commit(ctx)
}

func (s *Store) ChangePassword(ctx context.Context, userID int64, passwordHash string, event audit.Event) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `UPDATE password_credentials SET password_hash=$2,changed_at=now() WHERE user_id=$1`, userID, passwordHash); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE users SET auth_version=auth_version+1,updated_at=now() WHERE id=$1`, userID); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE sessions SET revoked_at=now() WHERE user_id=$1 AND revoked_at IS NULL`, userID); err != nil {
		return err
	}
	if err = insertAudit(ctx, tx, event); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) SetUserStatus(ctx context.Context, publicID, status string, version int64, event audit.Event) (identity.User, error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return identity.User{}, err
	}
	defer tx.Rollback(ctx)
	user, err := scanUser(tx.QueryRow(ctx, `UPDATE users SET status=$2,version=version+1,auth_version=auth_version+1,updated_at=now()
		WHERE public_id=$1 AND version=$3 RETURNING `+userColumns, publicID, status, version))
	if errors.Is(err, pgx.ErrNoRows) {
		return identity.User{}, ErrVersionConflict
	}
	if err != nil {
		return identity.User{}, err
	}
	if status != identity.StatusActive {
		if _, err = tx.Exec(ctx, `UPDATE sessions SET revoked_at=now() WHERE user_id=$1 AND revoked_at IS NULL`, user.ID); err != nil {
			return identity.User{}, err
		}
	}
	event.TargetPublicID = user.PublicID
	if err = insertAudit(ctx, tx, event); err != nil {
		return identity.User{}, err
	}
	return user, tx.Commit(ctx)
}

func (s *Store) IsInstallationAdministrator(ctx context.Context, userID int64) (bool, error) {
	var allowed bool
	err := s.Pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM installation_role_assignments WHERE user_id=$1 AND role='Administrator')`, userID).Scan(&allowed)
	return allowed, err
}

func (s *Store) UserWorkspaceRoles(ctx context.Context, userID int64) (map[string]string, error) {
	rows, err := s.Pool.Query(ctx, `SELECT w.public_id,wm.role FROM workspace_memberships wm JOIN workspaces w ON w.id=wm.workspace_id
		WHERE wm.user_id=$1 AND wm.status='Active' ORDER BY w.id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	roles := map[string]string{}
	for rows.Next() {
		var workspaceID, role string
		if err = rows.Scan(&workspaceID, &role); err != nil {
			return nil, err
		}
		roles[workspaceID] = role
	}
	return roles, rows.Err()
}

func (s *Store) AuthorizationContext(ctx context.Context, userID, workspaceID int64, resourceType, resourcePublicID string) (authorization.Context, error) {
	var result authorization.Context
	if err := s.Pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM installation_role_assignments WHERE user_id=$1 AND role='Administrator')`, userID).Scan(&result.InstallationAdministrator); err != nil {
		return result, err
	}
	var status string
	err := s.Pool.QueryRow(ctx, `SELECT role,status FROM workspace_memberships WHERE workspace_id=$1 AND user_id=$2`, workspaceID, userID).Scan(&result.MembershipRole, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		return result, nil
	}
	if err != nil {
		return result, err
	}
	result.MembershipSuspended = status == "Suspended"
	rows, err := s.Pool.Query(ctx, `SELECT DISTINCT ag.relation FROM access_grants ag
		WHERE ag.workspace_id=$1 AND ag.resource_type=$2 AND ag.resource_public_id=$3 AND (
			(ag.subject_type='User' AND ag.subject_id=$4) OR
			(ag.subject_type='Group' AND EXISTS(SELECT 1 FROM workspace_group_members gm WHERE gm.group_id=ag.subject_id AND gm.user_id=$4)))`, workspaceID, resourceType, resourcePublicID, userID)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	for rows.Next() {
		var relation authorization.Relation
		if err = rows.Scan(&relation); err != nil {
			return result, err
		}
		result.Relations = append(result.Relations, relation)
	}
	return result, rows.Err()
}

func (s *Store) ListWorkspaceMemberships(ctx context.Context, workspaceID int64) ([]Membership, error) {
	rows, err := s.Pool.Query(ctx, `SELECT wm.workspace_id,w.public_id,wm.user_id,u.public_id,u.username,u.display_name,wm.role,wm.status,wm.version,wm.created_at,wm.updated_at
		FROM workspace_memberships wm JOIN workspaces w ON w.id=wm.workspace_id JOIN users u ON u.id=wm.user_id
		WHERE wm.workspace_id=$1 ORDER BY u.username`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Membership{}
	for rows.Next() {
		var item Membership
		if err = rows.Scan(&item.WorkspaceID, &item.WorkspacePublicID, &item.UserID, &item.UserPublicID, &item.Username, &item.DisplayName, &item.Role, &item.Status, &item.Version, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) PutWorkspaceMembership(ctx context.Context, workspaceID int64, userPublicID, role, status string, expectedVersion *int64, event audit.Event) (Membership, error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return Membership{}, err
	}
	defer tx.Rollback(ctx)
	var userID int64
	if err = tx.QueryRow(ctx, `SELECT id FROM users WHERE public_id=$1 AND status<>'Disabled'`, userPublicID).Scan(&userID); errors.Is(err, pgx.ErrNoRows) {
		return Membership{}, ErrNotFound
	}
	if err != nil {
		return Membership{}, err
	}
	if expectedVersion == nil {
		_, err = tx.Exec(ctx, `INSERT INTO workspace_memberships(workspace_id,user_id,role,status) VALUES($1,$2,$3,$4)`, workspaceID, userID, role, status)
	} else {
		var affected int64
		result, updateErr := tx.Exec(ctx, `UPDATE workspace_memberships SET role=$3,status=$4,version=version+1,updated_at=now() WHERE workspace_id=$1 AND user_id=$2 AND version=$5`, workspaceID, userID, role, status, *expectedVersion)
		err = updateErr
		if err == nil {
			affected = result.RowsAffected()
		}
		if err == nil && affected != 1 {
			return Membership{}, ErrVersionConflict
		}
	}
	if err != nil {
		return Membership{}, translateDBError(err)
	}
	var item Membership
	err = tx.QueryRow(ctx, `SELECT wm.workspace_id,w.public_id,wm.user_id,u.public_id,u.username,u.display_name,wm.role,wm.status,wm.version,wm.created_at,wm.updated_at
		FROM workspace_memberships wm JOIN workspaces w ON w.id=wm.workspace_id JOIN users u ON u.id=wm.user_id WHERE wm.workspace_id=$1 AND wm.user_id=$2`, workspaceID, userID).
		Scan(&item.WorkspaceID, &item.WorkspacePublicID, &item.UserID, &item.UserPublicID, &item.Username, &item.DisplayName, &item.Role, &item.Status, &item.Version, &item.CreatedAt, &item.UpdatedAt)
	if err != nil {
		return Membership{}, err
	}
	event.TargetPublicID = userPublicID
	if err = insertAudit(ctx, tx, event); err != nil {
		return Membership{}, err
	}
	return item, tx.Commit(ctx)
}

func (s *Store) DeleteWorkspaceMembership(ctx context.Context, workspaceID int64, userPublicID string, event audit.Event) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	result, err := tx.Exec(ctx, `DELETE FROM workspace_memberships wm USING users u WHERE wm.workspace_id=$1 AND wm.user_id=u.id AND u.public_id=$2`, workspaceID, userPublicID)
	if err != nil {
		return err
	}
	if result.RowsAffected() != 1 {
		return ErrNotFound
	}
	event.TargetPublicID = userPublicID
	if err = insertAudit(ctx, tx, event); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) CreateWorkspaceGroup(ctx context.Context, group Group, nameKey string, event audit.Event) (Group, error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return Group{}, err
	}
	defer tx.Rollback(ctx)
	err = tx.QueryRow(ctx, `INSERT INTO workspace_groups(public_id,workspace_id,name,name_key) VALUES($1,$2,$3,$4)
		RETURNING id,public_id,workspace_id,name,version,0,created_at,updated_at`, group.PublicID, group.WorkspaceID, group.Name, nameKey).
		Scan(&group.ID, &group.PublicID, &group.WorkspaceID, &group.Name, &group.Version, &group.MemberCount, &group.CreatedAt, &group.UpdatedAt)
	if err != nil {
		return Group{}, translateDBError(err)
	}
	event.TargetPublicID = group.PublicID
	if err = insertAudit(ctx, tx, event); err != nil {
		return Group{}, err
	}
	return group, tx.Commit(ctx)
}

func (s *Store) ListWorkspaceGroups(ctx context.Context, workspaceID int64) ([]Group, error) {
	rows, err := s.Pool.Query(ctx, `SELECT g.id,g.public_id,g.workspace_id,g.name,g.version,count(gm.user_id),g.created_at,g.updated_at
		FROM workspace_groups g LEFT JOIN workspace_group_members gm ON gm.group_id=g.id WHERE g.workspace_id=$1 GROUP BY g.id ORDER BY g.name_key`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Group{}
	for rows.Next() {
		var item Group
		if err = rows.Scan(&item.ID, &item.PublicID, &item.WorkspaceID, &item.Name, &item.Version, &item.MemberCount, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) ListWorkspaceGroupMembers(ctx context.Context, workspaceID int64, groupPublicID string) ([]Membership, error) {
	rows, err := s.Pool.Query(ctx, `SELECT wm.workspace_id,w.public_id,wm.user_id,u.public_id,u.username,u.display_name,wm.role,wm.status,wm.version,wm.created_at,wm.updated_at
		FROM workspace_group_members gm
		JOIN workspace_groups g ON g.id=gm.group_id AND g.workspace_id=gm.workspace_id
		JOIN workspace_memberships wm ON wm.workspace_id=gm.workspace_id AND wm.user_id=gm.user_id
		JOIN workspaces w ON w.id=wm.workspace_id
		JOIN users u ON u.id=wm.user_id
		WHERE gm.workspace_id=$1 AND g.public_id=$2 ORDER BY u.username`, workspaceID, groupPublicID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Membership{}
	for rows.Next() {
		var item Membership
		if err = rows.Scan(&item.WorkspaceID, &item.WorkspacePublicID, &item.UserID, &item.UserPublicID, &item.Username, &item.DisplayName, &item.Role, &item.Status, &item.Version, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) ListWorkspaceAccessGrants(ctx context.Context, workspaceID int64) ([]AccessGrant, error) {
	rows, err := s.Pool.Query(ctx, `SELECT ag.id,ag.public_id,ag.workspace_id,ag.subject_type,ag.subject_id,
		CASE WHEN ag.subject_type='User' THEN u.public_id ELSE g.public_id END,
		CASE WHEN ag.subject_type='User' THEN u.display_name ELSE g.name END,
		ag.resource_type,ag.resource_public_id,ag.relation,ag.created_at
		FROM access_grants ag
		LEFT JOIN users u ON ag.subject_type='User' AND u.id=ag.subject_id
		LEFT JOIN workspace_groups g ON ag.subject_type='Group' AND g.id=ag.subject_id AND g.workspace_id=ag.workspace_id
		WHERE ag.workspace_id=$1 ORDER BY ag.id DESC LIMIT 500`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []AccessGrant{}
	for rows.Next() {
		var item AccessGrant
		if err = rows.Scan(&item.ID, &item.PublicID, &item.WorkspaceID, &item.SubjectType, &item.SubjectID, &item.SubjectPublicID, &item.SubjectName, &item.ResourceType, &item.ResourcePublicID, &item.Relation, &item.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) CreateWorkspaceAccessGrant(ctx context.Context, grant AccessGrant, createdByUserID int64, event audit.Event) (AccessGrant, error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return AccessGrant{}, err
	}
	defer tx.Rollback(ctx)
	subjectTable := "users"
	subjectPublicColumn := "public_id"
	if grant.SubjectType == "Group" {
		subjectTable = "workspace_groups"
	}
	subjectQuery := fmt.Sprintf(`SELECT id FROM %s WHERE %s=$1`, subjectTable, subjectPublicColumn)
	if grant.SubjectType == "User" {
		subjectQuery = `SELECT u.id FROM users u JOIN workspace_memberships wm ON wm.user_id=u.id WHERE u.public_id=$1 AND wm.workspace_id=$2 AND wm.status='Active'`
	} else {
		subjectQuery += ` AND workspace_id=$2`
	}
	if err = tx.QueryRow(ctx, subjectQuery, grant.SubjectPublicID, grant.WorkspaceID).Scan(&grant.SubjectID); errors.Is(err, pgx.ErrNoRows) {
		return AccessGrant{}, ErrNotFound
	}
	if err != nil {
		return AccessGrant{}, err
	}
	resourceExists, err := workspaceResourceExists(ctx, tx, grant.WorkspaceID, grant.ResourceType, grant.ResourcePublicID)
	if err != nil {
		return AccessGrant{}, err
	}
	if !resourceExists {
		return AccessGrant{}, ErrNotFound
	}
	err = tx.QueryRow(ctx, `INSERT INTO access_grants(public_id,workspace_id,subject_type,subject_id,resource_type,resource_public_id,relation,created_by_user_id)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8)
		RETURNING id,created_at`, grant.PublicID, grant.WorkspaceID, grant.SubjectType, grant.SubjectID, grant.ResourceType, grant.ResourcePublicID, grant.Relation, createdByUserID).
		Scan(&grant.ID, &grant.CreatedAt)
	if err != nil {
		return AccessGrant{}, translateDBError(err)
	}
	event.TargetPublicID = grant.PublicID
	event.Metadata = map[string]any{"subjectType": grant.SubjectType, "subjectId": grant.SubjectPublicID, "resourceType": grant.ResourceType, "resourceId": grant.ResourcePublicID, "relation": grant.Relation}
	if err = insertAudit(ctx, tx, event); err != nil {
		return AccessGrant{}, err
	}
	return grant, tx.Commit(ctx)
}

func (s *Store) DeleteWorkspaceAccessGrant(ctx context.Context, workspaceID int64, publicID string, event audit.Event) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	result, err := tx.Exec(ctx, `DELETE FROM access_grants WHERE workspace_id=$1 AND public_id=$2`, workspaceID, publicID)
	if err != nil {
		return err
	}
	if result.RowsAffected() != 1 {
		return ErrNotFound
	}
	event.TargetPublicID = publicID
	if err = insertAudit(ctx, tx, event); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func workspaceResourceExists(ctx context.Context, tx pgx.Tx, workspaceID int64, resourceType, publicID string) (bool, error) {
	query := ""
	switch resourceType {
	case "Workspace":
		query = `SELECT EXISTS(SELECT 1 FROM workspaces WHERE id=$1 AND public_id=$2)`
	case "Project":
		query = `SELECT EXISTS(SELECT 1 FROM projects WHERE workspace_id=$1 AND public_id=$2 AND archived_at IS NULL)`
	case "App":
		query = `SELECT EXISTS(SELECT 1 FROM apps a JOIN projects p ON p.id=a.project_id WHERE p.workspace_id=$1 AND a.public_id=$2 AND a.archived_at IS NULL)`
	case "AppEnvironment":
		query = `SELECT EXISTS(SELECT 1 FROM app_environments WHERE workspace_id=$1 AND public_id=$2 AND deletion_requested_at IS NULL AND archived_at IS NULL)`
	default:
		return false, nil
	}
	var exists bool
	err := tx.QueryRow(ctx, query, workspaceID, publicID).Scan(&exists)
	return exists, err
}

func (s *Store) AddWorkspaceGroupMember(ctx context.Context, workspaceID int64, groupPublicID, userPublicID string, event audit.Event) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	result, err := tx.Exec(ctx, `INSERT INTO workspace_group_members(workspace_id,group_id,user_id)
		SELECT $1,g.id,u.id FROM workspace_groups g JOIN users u ON u.public_id=$3
		WHERE g.workspace_id=$1 AND g.public_id=$2 ON CONFLICT DO NOTHING`, workspaceID, groupPublicID, userPublicID)
	if err != nil {
		return err
	}
	if result.RowsAffected() != 1 {
		return ErrConflict
	}
	event.TargetPublicID = groupPublicID
	if err = insertAudit(ctx, tx, event); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) RemoveWorkspaceGroupMember(ctx context.Context, workspaceID int64, groupPublicID, userPublicID string, event audit.Event) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	result, err := tx.Exec(ctx, `DELETE FROM workspace_group_members gm USING workspace_groups g,users u
		WHERE gm.workspace_id=$1 AND gm.group_id=g.id AND g.public_id=$2 AND gm.user_id=u.id AND u.public_id=$3`, workspaceID, groupPublicID, userPublicID)
	if err != nil {
		return err
	}
	if result.RowsAffected() != 1 {
		return ErrNotFound
	}
	event.TargetPublicID = groupPublicID
	if err = insertAudit(ctx, tx, event); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) ListAuditEvents(ctx context.Context, workspaceID, beforeID int64, limit int) ([]AuditEventView, string, error) {
	if beforeID == 0 {
		beforeID = math.MaxInt64
	}
	rows, err := s.Pool.Query(ctx, `SELECT ae.id,ae.public_id,ae.occurred_at,COALESCE(u.public_id,''),COALESCE(p.public_id,''),COALESCE(p.kind,''),ae.action,ae.target_type,ae.target_public_id,ae.outcome,ae.reason,ae.request_id,ae.metadata_json
		FROM audit_events ae LEFT JOIN users u ON u.id=ae.actor_user_id LEFT JOIN principals p ON p.id=ae.actor_principal_id
		WHERE ae.workspace_id=$1 AND ae.id<$2 ORDER BY ae.id DESC LIMIT $3`, workspaceID, beforeID, limit+1)
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

func (s *Store) CreateUserSession(ctx context.Context, publicID string, user identity.User, tokenHash, csrfHash []byte, assuranceLevel string, idleExpiresAt, absoluteExpiresAt time.Time, event audit.Event) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var sessionID int64
	if err = tx.QueryRow(ctx, `INSERT INTO sessions(public_id,token_hash,user_id,csrf_hash,expires_at,idle_expires_at,auth_version,assurance_level)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8) RETURNING id`, publicID, tokenHash, user.ID, csrfHash, absoluteExpiresAt, idleExpiresAt, user.AuthVersion, assuranceLevel).Scan(&sessionID); err != nil {
		return err
	}
	event.SessionID = &sessionID
	if err = insertAudit(ctx, tx, event); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) UserSession(ctx context.Context, tokenHash []byte, idleTTL time.Duration) (SessionPrincipal, error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return SessionPrincipal{}, err
	}
	defer tx.Rollback(ctx)
	var principal SessionPrincipal
	var lastSeen time.Time
	err = tx.QueryRow(ctx, `SELECT s.id,s.public_id,s.user_id,s.csrf_hash,s.assurance_level,s.last_seen_at
		FROM sessions s JOIN users u ON u.id=s.user_id
		WHERE s.token_hash=$1 AND s.revoked_at IS NULL AND s.expires_at>now() AND s.idle_expires_at>now()
		  AND u.status='Active' AND u.auth_version=s.auth_version
		FOR UPDATE OF s`, tokenHash).Scan(&principal.SessionID, &principal.PublicID, &principal.UserID, &principal.CSRFHash, &principal.AssuranceLevel, &lastSeen)
	if err != nil {
		return SessionPrincipal{}, ErrSessionInvalid
	}
	if time.Since(lastSeen) >= time.Minute {
		if _, err = tx.Exec(ctx, `UPDATE sessions SET last_seen_at=now(),idle_expires_at=LEAST(expires_at,now()+$2::interval) WHERE id=$1`, principal.SessionID, fmt.Sprintf("%f seconds", idleTTL.Seconds())); err != nil {
			return SessionPrincipal{}, err
		}
	}
	return principal, tx.Commit(ctx)
}

func (s *Store) ListUserSessions(ctx context.Context, userID, currentSessionID int64) ([]SessionInfo, error) {
	rows, err := s.Pool.Query(ctx, `SELECT public_id,assurance_level,last_seen_at,expires_at,id=$2 FROM sessions
		WHERE user_id=$1 AND revoked_at IS NULL AND expires_at>now() AND idle_expires_at>now() ORDER BY last_seen_at DESC`, userID, currentSessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []SessionInfo{}
	for rows.Next() {
		var item SessionInfo
		if err = rows.Scan(&item.PublicID, &item.AssuranceLevel, &item.LastSeenAt, &item.ExpiresAt, &item.Current); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) RevokeUserSession(ctx context.Context, userID int64, publicID string, event audit.Event) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	result, err := tx.Exec(ctx, `UPDATE sessions SET revoked_at=now() WHERE public_id=$1 AND user_id=$2 AND revoked_at IS NULL`, publicID, userID)
	if err != nil {
		return err
	}
	if result.RowsAffected() != 1 {
		return ErrNotFound
	}
	event.TargetPublicID = publicID
	if err = insertAudit(ctx, tx, event); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) RevokeSessionToken(ctx context.Context, tokenHash []byte, event audit.Event) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var publicID string
	if err = tx.QueryRow(ctx, `UPDATE sessions SET revoked_at=now() WHERE token_hash=$1 AND revoked_at IS NULL RETURNING public_id`, tokenHash).Scan(&publicID); errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	event.TargetPublicID = publicID
	if err = insertAudit(ctx, tx, event); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) AuthenticationAllowed(ctx context.Context, keyHash []byte) (bool, error) {
	var allowed bool
	err := s.Pool.QueryRow(ctx, `SELECT COALESCE((SELECT blocked_until IS NULL OR blocked_until<=now() FROM authentication_rate_limits WHERE key_hash=$1),true)`, keyHash).Scan(&allowed)
	return allowed, err
}

func (s *Store) RecordAuthenticationFailure(ctx context.Context, keyHash []byte) error {
	_, err := s.Pool.Exec(ctx, `INSERT INTO authentication_rate_limits(key_hash,failures,blocked_until)
		VALUES($1,1,NULL)
		ON CONFLICT(key_hash) DO UPDATE SET failures=authentication_rate_limits.failures+1,
		blocked_until=CASE WHEN authentication_rate_limits.failures+1>=8 THEN now()+interval '15 minutes' ELSE authentication_rate_limits.blocked_until END,
		updated_at=now()`, keyHash)
	return err
}

func (s *Store) ClearAuthenticationFailures(ctx context.Context, keyHash []byte) error {
	_, err := s.Pool.Exec(ctx, `DELETE FROM authentication_rate_limits WHERE key_hash=$1`, keyHash)
	return err
}

func (s *Store) CreatePasswordResetGrant(ctx context.Context, grant PasswordResetGrant, createdByUserID int64, event audit.Event) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `UPDATE password_reset_grants SET consumed_at=now() WHERE user_id=$1 AND consumed_at IS NULL`, grant.UserID); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO password_reset_grants(public_id,user_id,code_hash,expires_at,created_by_user_id) VALUES($1,$2,$3,$4,$5)`, grant.PublicID, grant.UserID, grant.CodeHash, grant.ExpiresAt, createdByUserID); err != nil {
		return err
	}
	if err = insertAudit(ctx, tx, event); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) VerifyPasswordResetGrant(ctx context.Context, username string, codeHash []byte) (identity.User, string, error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return identity.User{}, "", err
	}
	defer tx.Rollback(ctx)
	var grantID int64
	var attempts int
	var expected []byte
	user, err := scanUser(tx.QueryRow(ctx, `SELECT u.id,u.public_id,u.username,u.display_name,u.status,u.version,u.auth_version,u.created_at,u.updated_at
		FROM users u WHERE u.username_key=$1 AND u.status='Active'`, username))
	if err != nil {
		return identity.User{}, "", ErrNotFound
	}
	var publicID string
	err = tx.QueryRow(ctx, `SELECT id,public_id,attempts,code_hash FROM password_reset_grants
		WHERE user_id=$1 AND verified_at IS NULL AND consumed_at IS NULL AND expires_at>now() ORDER BY id DESC LIMIT 1 FOR UPDATE`, user.ID).Scan(&grantID, &publicID, &attempts, &expected)
	if err != nil {
		return identity.User{}, "", ErrNotFound
	}
	if attempts >= 5 || !equalBytes(expected, codeHash) {
		_, _ = tx.Exec(ctx, `UPDATE password_reset_grants SET attempts=LEAST(attempts+1,5) WHERE id=$1`, grantID)
		if commitErr := tx.Commit(ctx); commitErr != nil {
			return identity.User{}, "", commitErr
		}
		return identity.User{}, "", ErrResetCodeInvalid
	}
	if _, err = tx.Exec(ctx, `UPDATE password_reset_grants SET verified_at=now() WHERE id=$1`, grantID); err != nil {
		return identity.User{}, "", err
	}
	return user, publicID, tx.Commit(ctx)
}

func (s *Store) CompletePasswordReset(ctx context.Context, username, grantPublicID, passwordHash string, event audit.Event) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var userID int64
	err = tx.QueryRow(ctx, `SELECT u.id FROM users u JOIN password_reset_grants g ON g.user_id=u.id
		WHERE u.username_key=$1 AND g.public_id=$2 AND g.verified_at IS NOT NULL AND g.consumed_at IS NULL AND g.expires_at>now() FOR UPDATE OF g`, username, grantPublicID).Scan(&userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrResetCodeInvalid
	}
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE password_credentials SET password_hash=$2,changed_at=now() WHERE user_id=$1`, userID, passwordHash); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE users SET auth_version=auth_version+1,status='Active',updated_at=now() WHERE id=$1`, userID); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE sessions SET revoked_at=now() WHERE user_id=$1 AND revoked_at IS NULL`, userID); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE password_reset_grants SET consumed_at=now() WHERE public_id=$1`, grantPublicID); err != nil {
		return err
	}
	if err = insertAudit(ctx, tx, event); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func equalBytes(left, right []byte) bool {
	if len(left) != len(right) {
		return false
	}
	var difference byte
	for index := range left {
		difference |= left[index] ^ right[index]
	}
	return difference == 0
}

func insertAudit(ctx context.Context, tx pgx.Tx, event audit.Event) error {
	_, err := tx.Exec(ctx, `INSERT INTO audit_events(public_id,actor_user_id,actor_principal_id,session_id,workspace_id,action,target_type,target_public_id,outcome,reason,request_id,trace_id,source_hash,user_agent_hash,metadata_json)
		VALUES($1,$2,COALESCE($3,(SELECT principal_id FROM users WHERE id=$2)),$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)`, event.PublicID, event.ActorUserID, event.ActorPrincipalID, event.SessionID, event.WorkspaceID, event.Action, event.TargetType, event.TargetPublicID, event.Outcome, event.Reason, event.RequestID, event.TraceID, event.SourceHash, event.UserAgentHash, event.MetadataJSON())
	return err
}

func (s *Store) RecordAudit(ctx context.Context, event audit.Event) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err = insertAudit(ctx, tx, event); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
