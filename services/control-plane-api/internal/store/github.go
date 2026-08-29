package store

import (
	"context"
	"errors"
	"time"

	"github.com/fruto-platform/fruto/services/control-plane-api/internal/domain"
	"github.com/jackc/pgx/v5"
)

type GitHubConnectionState struct {
	ActorID        int64
	WorkspaceID    int64
	Step           string
	InstallationID int64
}

func (s *Store) GitHubConnectionAuthorized(ctx context.Context, actorID, workspaceID int64) (bool, error) {
	var authorized bool
	err := s.Pool.QueryRow(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM actors a
			JOIN workspace_actors wa ON wa.actor_id=a.id
			WHERE a.id=$1 AND a.role='owner' AND wa.workspace_id=$2
		)`, actorID, workspaceID).Scan(&authorized)
	return authorized, err
}

func (s *Store) CreateGitHubConnectionState(ctx context.Context, stateHash, browserHash []byte, actorID, workspaceID int64, step string, installationID int64, expiresAt time.Time) error {
	var externalID any
	if installationID > 0 {
		externalID = installationID
	}
	_, err := s.Pool.Exec(ctx, `
		INSERT INTO github_connection_states(state_hash,browser_hash,actor_id,workspace_id,step,github_installation_id,expires_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7)`, stateHash, browserHash, actorID, workspaceID, step, externalID, expiresAt)
	return err
}

func (s *Store) ConsumeGitHubConnectionState(ctx context.Context, stateHash, browserHash []byte, step string) (GitHubConnectionState, error) {
	var value GitHubConnectionState
	err := s.Pool.QueryRow(ctx, `
		UPDATE github_connection_states
		SET consumed_at=now()
		WHERE state_hash=$1 AND browser_hash=$2 AND step=$3
		  AND consumed_at IS NULL AND expires_at > now()
		RETURNING actor_id,workspace_id,step,COALESCE(github_installation_id,0)`, stateHash, browserHash, step).
		Scan(&value.ActorID, &value.WorkspaceID, &value.Step, &value.InstallationID)
	if errors.Is(err, pgx.ErrNoRows) {
		return GitHubConnectionState{}, ErrNotFound
	}
	return value, err
}

func (s *Store) ConnectGitHubInstallation(ctx context.Context, publicID string, workspaceID, actorID, externalID, accountID int64, accountLogin, accountType, repositorySelection string) (domain.GitHubInstallation, error) {
	var value domain.GitHubInstallation
	err := s.Pool.QueryRow(ctx, `
		INSERT INTO github_installations(public_id,workspace_id,connected_by_actor_id,github_installation_id,account_id,account_login,account_type,repository_selection)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
		ON CONFLICT (github_installation_id) DO UPDATE SET
			connected_by_actor_id=EXCLUDED.connected_by_actor_id,
			account_id=EXCLUDED.account_id,
			account_login=EXCLUDED.account_login,
			account_type=EXCLUDED.account_type,
			repository_selection=EXCLUDED.repository_selection,
			status='Active',
			updated_at=now()
		WHERE github_installations.workspace_id=EXCLUDED.workspace_id
		RETURNING id,public_id,workspace_id,github_installation_id,account_id,account_login,account_type,repository_selection,created_at`,
		publicID, workspaceID, actorID, externalID, accountID, accountLogin, accountType, repositorySelection).
		Scan(&value.ID, &value.PublicID, &value.WorkspaceID, &value.ExternalID, &value.AccountID, &value.AccountLogin, &value.AccountType, &value.RepositorySelection, &value.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.GitHubInstallation{}, ErrConflict
	}
	return value, hierarchyWriteError(err)
}

func (s *Store) ListGitHubInstallations(ctx context.Context, workspaceID int64) ([]domain.GitHubInstallation, error) {
	rows, err := s.Pool.Query(ctx, `
		SELECT id,public_id,workspace_id,github_installation_id,account_id,account_login,account_type,repository_selection,created_at
		FROM github_installations WHERE workspace_id=$1 AND status<>'Deleted' ORDER BY id DESC`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []domain.GitHubInstallation{}
	for rows.Next() {
		var value domain.GitHubInstallation
		if err = rows.Scan(&value.ID, &value.PublicID, &value.WorkspaceID, &value.ExternalID, &value.AccountID, &value.AccountLogin, &value.AccountType, &value.RepositorySelection, &value.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, value)
	}
	return items, rows.Err()
}

func (s *Store) FindGitHubInstallation(ctx context.Context, workspaceID int64, publicID string) (domain.GitHubInstallation, error) {
	var value domain.GitHubInstallation
	err := s.Pool.QueryRow(ctx, `
		SELECT id,public_id,workspace_id,github_installation_id,account_id,account_login,account_type,repository_selection,created_at
		FROM github_installations WHERE workspace_id=$1 AND public_id=$2`, workspaceID, publicID).
		Scan(&value.ID, &value.PublicID, &value.WorkspaceID, &value.ExternalID, &value.AccountID, &value.AccountLogin, &value.AccountType, &value.RepositorySelection, &value.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.GitHubInstallation{}, ErrNotFound
	}
	return value, err
}

func (s *Store) DeleteGitHubInstallation(ctx context.Context, workspaceID int64, publicID string) error {
	command, err := s.Pool.Exec(ctx, `
		DELETE FROM github_installations i
		WHERE i.workspace_id=$1 AND i.public_id=$2
		  AND NOT EXISTS (SELECT 1 FROM app_github_sources s WHERE s.github_installation_id=i.id)`, workspaceID, publicID)
	if err != nil {
		return err
	}
	if command.RowsAffected() == 1 {
		return nil
	}
	if _, err = s.FindGitHubInstallation(ctx, workspaceID, publicID); err == nil {
		return ErrDependencyConflict
	}
	return ErrNotFound
}

func (s *Store) GitHubInstallationInUse(ctx context.Context, installationID int64) (bool, error) {
	var inUse bool
	err := s.Pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM app_github_sources WHERE github_installation_id=$1) OR EXISTS(SELECT 1 FROM builds WHERE github_installation_id=$1 AND status IN ('Pending','Running'))`, installationID).Scan(&inUse)
	return inUse, err
}

func (s *Store) SetAppGitHubSource(ctx context.Context, workspaceID int64, projectPublicID, appPublicID, installationPublicID string, repository domain.GitHubRepository) (domain.GitHubSource, error) {
	var source domain.GitHubSource
	err := s.Pool.QueryRow(ctx, `
		INSERT INTO app_github_sources(app_id,github_installation_id,repository_id,repository_name,repository_full_name,repository_private,default_branch)
		SELECT a.id,i.id,$5,$6,$7,$8,$9
		FROM apps a
		JOIN projects p ON p.id=a.project_id
		JOIN github_installations i ON i.workspace_id=p.workspace_id AND i.public_id=$4 AND i.status='Active'
		WHERE p.workspace_id=$1 AND p.public_id=$2 AND a.public_id=$3
		  AND p.archived_at IS NULL AND a.archived_at IS NULL
		ON CONFLICT (app_id) DO UPDATE SET
			github_installation_id=EXCLUDED.github_installation_id,
			repository_id=EXCLUDED.repository_id,
			repository_name=EXCLUDED.repository_name,
			repository_full_name=EXCLUDED.repository_full_name,
			repository_private=EXCLUDED.repository_private,
			default_branch=EXCLUDED.default_branch,
			updated_at=now()
		RETURNING $4,repository_id::text,repository_name,repository_full_name,repository_private,default_branch,updated_at`,
		workspaceID, projectPublicID, appPublicID, installationPublicID, repository.ID, repository.Name, repository.FullName, repository.Private, repository.DefaultBranch).
		Scan(&source.InstallationID, &source.Repository.ID, &source.Repository.Name, &source.Repository.FullName, &source.Repository.Private, &source.Repository.DefaultBranch, &source.ConnectedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.GitHubSource{}, ErrNotFound
	}
	return source, err
}

func (s *Store) GetAppGitHubSource(ctx context.Context, workspaceID int64, projectPublicID, appPublicID string) (*domain.GitHubSource, error) {
	if _, err := s.FindApp(ctx, workspaceID, projectPublicID, appPublicID); err != nil {
		return nil, err
	}
	var source domain.GitHubSource
	err := s.Pool.QueryRow(ctx, `
		SELECT i.public_id,s.repository_id::text,s.repository_name,s.repository_full_name,s.repository_private,s.default_branch,s.updated_at
		FROM apps a
		JOIN projects p ON p.id=a.project_id
		JOIN app_github_sources s ON s.app_id=a.id
		JOIN github_installations i ON i.id=s.github_installation_id
		WHERE p.workspace_id=$1 AND p.public_id=$2 AND a.public_id=$3
		  AND p.archived_at IS NULL AND a.archived_at IS NULL`, workspaceID, projectPublicID, appPublicID).
		Scan(&source.InstallationID, &source.Repository.ID, &source.Repository.Name, &source.Repository.FullName, &source.Repository.Private, &source.Repository.DefaultBranch, &source.ConnectedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &source, nil
}

func (s *Store) ClearAppGitHubSource(ctx context.Context, workspaceID int64, projectPublicID, appPublicID string) error {
	command, err := s.Pool.Exec(ctx, `
		DELETE FROM app_github_sources s USING apps a,projects p
		WHERE s.app_id=a.id AND a.project_id=p.id AND p.workspace_id=$1
		  AND p.public_id=$2 AND a.public_id=$3`, workspaceID, projectPublicID, appPublicID)
	if err != nil {
		return err
	}
	if command.RowsAffected() == 0 {
		if _, err = s.FindApp(ctx, workspaceID, projectPublicID, appPublicID); err != nil {
			return err
		}
	}
	return nil
}
