package store

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/molejo-platform/molejo/services/control-plane-api/internal/domain"
)

type GitHubBuildSource struct {
	WorkspaceID            int64
	ProjectID              int64
	ProjectPublicID        string
	AppID                  int64
	AppPublicID            string
	AppEnvironmentID       int64
	AppEnvironmentPublicID string
	InstallationID         int64
	InstallationExternalID int64
	RepositoryID           int64
	RepositoryFullName     string
	SourceBranch           string
}

func (s *Store) GitHubBuildSource(ctx context.Context, workspaceID int64, projectPublicID, appPublicID, appEnvironmentPublicID string) (GitHubBuildSource, error) {
	var source GitHubBuildSource
	err := s.Pool.QueryRow(ctx, `
		SELECT p.workspace_id,p.id,p.public_id,a.id,a.public_id,ae.id,ae.public_id,i.id,i.github_installation_id,
		       src.repository_id,src.repository_full_name,ae.source_branch
		FROM projects p
		JOIN apps a ON a.project_id=p.id
		JOIN app_environments ae ON ae.app_id=a.id
		JOIN app_github_sources src ON src.app_id=a.id
		JOIN github_installations i ON i.id=src.github_installation_id AND i.workspace_id=p.workspace_id
		WHERE p.workspace_id=$1 AND p.public_id=$2 AND a.public_id=$3 AND ae.public_id=$4
		  AND i.status='Active'
		  AND p.archived_at IS NULL AND a.archived_at IS NULL AND ae.archived_at IS NULL AND ae.deletion_requested_at IS NULL`, workspaceID, projectPublicID, appPublicID, appEnvironmentPublicID).
		Scan(&source.WorkspaceID, &source.ProjectID, &source.ProjectPublicID, &source.AppID, &source.AppPublicID,
			&source.AppEnvironmentID, &source.AppEnvironmentPublicID, &source.InstallationID, &source.InstallationExternalID,
			&source.RepositoryID, &source.RepositoryFullName, &source.SourceBranch)
	if errors.Is(err, pgx.ErrNoRows) {
		return GitHubBuildSource{}, ErrNotFound
	}
	return source, err
}

func (s *Store) FindBuildByIdempotency(ctx context.Context, workspaceID, actorID int64, idempotencyHash, payloadHash []byte) (domain.Build, bool, error) {
	build, storedPayload, err := s.buildByIdempotency(ctx, s.Pool, workspaceID, actorID, idempotencyHash)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Build{}, false, nil
	}
	if err != nil {
		return domain.Build{}, false, err
	}
	if string(storedPayload) != string(payloadHash) {
		return domain.Build{}, false, ErrConflict
	}
	return build, true, nil
}

func (s *Store) CreateBuild(ctx context.Context, workspaceID, actorID int64, publicID, projectPublicID, appPublicID, appEnvironmentPublicID, sourceBranch, commitSHA string, idempotencyHash, payloadHash []byte) (domain.Build, bool, error) {
	return s.CreateBuildWithMetadata(ctx, workspaceID, actorID, publicID, projectPublicID, appPublicID, appEnvironmentPublicID, sourceBranch, domain.CommitMetadata{SHA: commitSHA}, idempotencyHash, payloadHash)
}

func (s *Store) CreateBuildWithMetadata(ctx context.Context, workspaceID, actorID int64, publicID, projectPublicID, appPublicID, appEnvironmentPublicID, sourceBranch string, metadata domain.CommitMetadata, idempotencyHash, payloadHash []byte) (domain.Build, bool, error) {
	if _, err := domain.NormalizeSourceBranch(sourceBranch); err != nil {
		return domain.Build{}, false, err
	}
	if err := domain.ValidateCommitSHA(metadata.SHA); err != nil {
		return domain.Build{}, false, err
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return domain.Build{}, false, err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, "build-create:"+fmt.Sprintf("%x", idempotencyHash)); err != nil {
		return domain.Build{}, false, err
	}
	if existing, storedPayload, findErr := s.buildByIdempotency(ctx, tx, workspaceID, actorID, idempotencyHash); findErr == nil {
		if string(storedPayload) != string(payloadHash) {
			return domain.Build{}, false, ErrConflict
		}
		if err = tx.Commit(ctx); err != nil {
			return domain.Build{}, false, err
		}
		return existing, true, nil
	} else if !errors.Is(findErr, pgx.ErrNoRows) {
		return domain.Build{}, false, findErr
	}

	var build domain.Build
	err = tx.QueryRow(ctx, `
		INSERT INTO builds(public_id,workspace_id,project_id,app_id,app_environment_id,requested_by_user_id,
		  github_installation_id,github_installation_external_id,repository_id,repository_full_name,
		  source_branch,commit_sha,commit_title,commit_author_name,commit_author_login,committed_at,platform,idempotency_hash,payload_hash)
		SELECT $1,p.workspace_id,p.id,a.id,ae.id,$2,i.id,i.github_installation_id,src.repository_id,
		       src.repository_full_name,$7,$8,$9,$10,$11,$12,'linux/amd64',$13,$14
		FROM projects p
		JOIN apps a ON a.project_id=p.id
		JOIN app_environments ae ON ae.app_id=a.id
		JOIN app_github_sources src ON src.app_id=a.id
		JOIN github_installations i ON i.id=src.github_installation_id AND i.workspace_id=p.workspace_id
		WHERE p.workspace_id=$3 AND p.public_id=$4 AND a.public_id=$5 AND ae.public_id=$6
		  AND i.status='Active'
		  AND p.archived_at IS NULL AND a.archived_at IS NULL AND ae.archived_at IS NULL AND ae.deletion_requested_at IS NULL
			RETURNING id,public_id,workspace_id,project_id,app_id,app_environment_id,github_installation_external_id,
			          repository_id,repository_full_name,source_branch,commit_sha,commit_title,commit_author_name,
			          commit_author_login,committed_at,trigger_type,platform,status,attempts,COALESCE(worker_id,''),
		          fencing_token,lease_until,COALESCE(error_code,''),COALESCE(error_message,''),created_at,updated_at`,
		publicID, actorID, workspaceID, projectPublicID, appPublicID, appEnvironmentPublicID, sourceBranch,
		metadata.SHA, metadata.Title, metadata.AuthorName, metadata.AuthorLogin, metadata.CommittedAt, idempotencyHash, payloadHash).
		Scan(buildScanTargets(&build)...)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Build{}, false, ErrNotFound
	}
	if uniqueConstraint(err) == "builds_public_id_key" {
		return domain.Build{}, false, ErrPublicIDCollision
	}
	if err != nil {
		return domain.Build{}, false, translateDBError(err)
	}
	build.ProjectPublicID = projectPublicID
	build.AppPublicID = appPublicID
	build.AppEnvironmentPublicID = appEnvironmentPublicID
	if err = tx.Commit(ctx); err != nil {
		return domain.Build{}, false, err
	}
	return build, false, nil
}

type buildQuery interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func (s *Store) buildByIdempotency(ctx context.Context, query buildQuery, workspaceID, actorID int64, hash []byte) (domain.Build, []byte, error) {
	var build domain.Build
	var payload []byte
	err := query.QueryRow(ctx, `
		SELECT b.id,b.public_id,b.workspace_id,b.project_id,b.app_id,b.app_environment_id,b.github_installation_external_id,
		       b.repository_id,b.repository_full_name,b.source_branch,b.commit_sha,b.commit_title,b.commit_author_name,
		       b.commit_author_login,b.committed_at,b.trigger_type,b.platform,b.status,b.attempts,COALESCE(b.worker_id,''),
		       b.fencing_token,b.lease_until,COALESCE(b.error_code,''),COALESCE(b.error_message,''),b.created_at,b.updated_at,
		       p.public_id,a.public_id,ae.public_id,b.payload_hash
		FROM builds b JOIN projects p ON p.id=b.project_id JOIN apps a ON a.id=b.app_id JOIN app_environments ae ON ae.id=b.app_environment_id
		WHERE b.workspace_id=$1 AND b.requested_by_user_id=$2 AND b.idempotency_hash=$3`, workspaceID, actorID, hash).
		Scan(append(buildScanTargets(&build), &build.ProjectPublicID, &build.AppPublicID, &build.AppEnvironmentPublicID, &payload)...)
	return build, payload, err
}

func (s *Store) ClaimNextBuild(ctx context.Context, workerID string, lease time.Duration) (domain.Build, bool, error) {
	if strings.TrimSpace(workerID) == "" || lease <= 0 {
		return domain.Build{}, false, errors.New("worker ID and lease are required")
	}
	var build domain.Build
	err := s.Pool.QueryRow(ctx, `
		WITH candidate AS (
			SELECT b.id FROM builds b
			WHERE b.attempts < 3 AND (b.status='Pending' OR (b.status='Running' AND b.lease_until < now()))
			  AND (b.status='Running' OR NOT EXISTS (
			      SELECT 1 FROM builds active
			      WHERE active.app_environment_id=b.app_environment_id AND active.status='Running'
			  ))
			ORDER BY id FOR UPDATE SKIP LOCKED LIMIT 1
		), claimed AS (
			UPDATE builds b SET status='Running',attempts=b.attempts+1,worker_id=$1,
			  fencing_token=b.fencing_token+1,lease_until=now()+$2::interval,
			  started_at=COALESCE(b.started_at,now()),error_code=NULL,error_message=NULL,updated_at=now()
			FROM candidate c WHERE b.id=c.id
			RETURNING b.*
		)
		SELECT b.id,b.public_id,b.workspace_id,b.project_id,b.app_id,b.app_environment_id,b.github_installation_external_id,
		       b.repository_id,b.repository_full_name,b.source_branch,b.commit_sha,b.commit_title,b.commit_author_name,
		       b.commit_author_login,b.committed_at,b.trigger_type,b.platform,b.status,b.attempts,COALESCE(b.worker_id,''),
		       b.fencing_token,b.lease_until,COALESCE(b.error_code,''),COALESCE(b.error_message,''),b.created_at,b.updated_at,
		       p.public_id,a.public_id,ae.public_id
		FROM claimed b JOIN projects p ON p.id=b.project_id JOIN apps a ON a.id=b.app_id JOIN app_environments ae ON ae.id=b.app_environment_id`, workerID, lease.String()).
		Scan(append(buildScanTargets(&build), &build.ProjectPublicID, &build.AppPublicID, &build.AppEnvironmentPublicID)...)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Build{}, false, nil
	}
	return build, err == nil, err
}

func (s *Store) AppendBuildLog(ctx context.Context, build domain.Build, message string) error {
	message = strings.TrimSpace(message)
	if message == "" {
		return nil
	}
	if len(message) > 4000 {
		message = message[:4000]
	}
	command, err := s.Pool.Exec(ctx, `
		INSERT INTO build_logs(build_id,message)
		SELECT id,$2 FROM builds WHERE id=$1 AND status='Running' AND worker_id=$3 AND fencing_token=$4`,
		build.ID, message, build.WorkerID, build.FencingToken)
	if err != nil {
		return err
	}
	if command.RowsAffected() != 1 {
		return ErrConflict
	}
	return nil
}

func (s *Store) FailBuild(ctx context.Context, build domain.Build, code, message string, retryable bool) error {
	final := !retryable || build.Attempts >= 3
	status := domain.BuildPending
	if final {
		status = domain.BuildFailed
		if code == "build_timeout" {
			status = domain.BuildTimedOut
		}
	}
	command, err := s.Pool.Exec(ctx, `
		UPDATE builds SET status=$1,worker_id=NULL,lease_until=NULL,error_code=$2,error_message=$3,
		  completed_at=CASE WHEN $1 IN ('Failed','TimedOut') THEN now() ELSE NULL END,updated_at=now()
		WHERE id=$4 AND status='Running' AND worker_id=$5 AND fencing_token=$6`,
		status, code, message, build.ID, build.WorkerID, build.FencingToken)
	if err != nil {
		return err
	}
	if command.RowsAffected() != 1 {
		return ErrConflict
	}
	return nil
}

func (s *Store) CompleteBuild(ctx context.Context, build domain.Build, releasePublicID, image string) (domain.Release, error) {
	repository, digest, ok := strings.Cut(image, "@")
	if !ok {
		return domain.Release{}, errors.New("release image is invalid")
	}
	canonical, err := domain.ReleaseImageReference(repository, digest)
	if err != nil || canonical != image {
		return domain.Release{}, errors.New("release image is invalid")
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return domain.Release{}, err
	}
	defer tx.Rollback(ctx)
	command, err := tx.Exec(ctx, `
		UPDATE builds SET status='Succeeded',worker_id=NULL,lease_until=NULL,error_code=NULL,error_message=NULL,
		  completed_at=now(),updated_at=now()
		WHERE id=$1 AND status='Running' AND worker_id=$2 AND fencing_token=$3`, build.ID, build.WorkerID, build.FencingToken)
	if err != nil {
		return domain.Release{}, err
	}
	if command.RowsAffected() != 1 {
		return domain.Release{}, ErrConflict
	}
	var release domain.Release
	err = tx.QueryRow(ctx, `
		INSERT INTO releases(public_id,workspace_id,project_id,app_id,app_environment_id,build_id,commit_sha,image,platform,
			source_provider,source_repository,source_revision,source_ref,producer_kind,created_by_principal_id)
		SELECT $1,b.workspace_id,b.project_id,b.app_id,b.app_environment_id,b.id,b.commit_sha,$2,b.platform,
			'GitHub',b.repository_full_name,b.commit_sha,b.source_branch,'buildkit',u.principal_id
		FROM builds b JOIN users u ON u.id=b.requested_by_user_id WHERE b.id=$3
		RETURNING id,public_id,workspace_id,project_id,app_id,app_environment_id,commit_sha,image,platform,availability_status,created_at`,
		releasePublicID, image, build.ID).
		Scan(&release.ID, &release.PublicID, &release.WorkspaceID, &release.ProjectID, &release.AppID, &release.AppEnvironmentID, &release.CommitSHA, &release.Image, &release.Platform, &release.AvailabilityStatus, &release.CreatedAt)
	if uniqueConstraint(err) == "releases_public_id_key" {
		return domain.Release{}, ErrPublicIDCollision
	}
	if err != nil {
		return domain.Release{}, translateDBError(err)
	}
	release.ProjectPublicID = build.ProjectPublicID
	release.AppPublicID = build.AppPublicID
	release.AppEnvironmentPublicID = build.AppEnvironmentPublicID
	release.BuildPublicID = build.PublicID
	release.SourceBranch = build.SourceBranch
	release.CommitTitle = build.CommitTitle
	release.CommitAuthorName = build.CommitAuthorName
	release.CommitAuthorLogin = build.CommitAuthorLogin
	release.CommittedAt = build.CommittedAt
	release.TriggerType = build.TriggerType
	release.OriginKind = domain.ReleaseOriginManagedBuild
	release.SourceProvider = "GitHub"
	release.SourceRepository = build.RepositoryFullName
	release.SourceRevision = build.CommitSHA
	release.SourceRef = build.SourceBranch
	release.ProducerKind = "buildkit"
	release.ProvenanceStatus = domain.ReleaseProvenanceDeclared
	if err = tx.QueryRow(ctx, `SELECT p.public_id,p.kind,p.display_name FROM builds b JOIN users u ON u.id=b.requested_by_user_id JOIN principals p ON p.id=u.principal_id WHERE b.id=$1`, build.ID).
		Scan(&release.CreatedBy.ID, &release.CreatedBy.Kind, &release.CreatedBy.DisplayName); err != nil {
		return domain.Release{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return domain.Release{}, err
	}
	return release, nil
}

func (s *Store) ReleaseBuildClaims(ctx context.Context, workerID string) error {
	_, err := s.Pool.Exec(ctx, `UPDATE builds SET status='Pending',worker_id=NULL,lease_until=NULL,updated_at=now() WHERE status='Running' AND worker_id=$1`, workerID)
	return err
}

func (s *Store) ListBuilds(ctx context.Context, workspaceID int64, projectPublicID, appPublicID string, beforeID int64, limit int) ([]domain.Build, string, error) {
	if beforeID == 0 {
		beforeID = math.MaxInt64
	}
	rows, err := s.Pool.Query(ctx, `
		SELECT b.id,b.public_id,b.workspace_id,b.project_id,b.app_id,b.app_environment_id,b.github_installation_external_id,
		       b.repository_id,b.repository_full_name,b.source_branch,b.commit_sha,b.commit_title,b.commit_author_name,
		       b.commit_author_login,b.committed_at,b.trigger_type,b.platform,b.status,b.attempts,COALESCE(b.worker_id,''),
		       b.fencing_token,b.lease_until,COALESCE(b.error_code,''),COALESCE(b.error_message,''),b.created_at,b.updated_at,p.public_id,a.public_id,ae.public_id
		FROM builds b JOIN projects p ON p.id=b.project_id JOIN apps a ON a.id=b.app_id JOIN app_environments ae ON ae.id=b.app_environment_id
		WHERE b.workspace_id=$1 AND p.public_id=$2 AND a.public_id=$3 AND b.id < $4
		ORDER BY b.id DESC LIMIT $5`, workspaceID, projectPublicID, appPublicID, beforeID, limit+1)
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()
	items := []domain.Build{}
	for rows.Next() {
		var build domain.Build
		if err = rows.Scan(append(buildScanTargets(&build), &build.ProjectPublicID, &build.AppPublicID, &build.AppEnvironmentPublicID)...); err != nil {
			return nil, "", err
		}
		items = append(items, build)
	}
	if err = rows.Err(); err != nil {
		return nil, "", err
	}
	if len(items) > limit {
		items = items[:limit]
		return items, domain.EncodeCursor(items[len(items)-1].ID), nil
	}
	return items, "", nil
}

func (s *Store) FindBuild(ctx context.Context, workspaceID int64, publicID string) (domain.Build, error) {
	var build domain.Build
	err := s.Pool.QueryRow(ctx, `
		SELECT b.id,b.public_id,b.workspace_id,b.project_id,b.app_id,b.app_environment_id,b.github_installation_external_id,
		       b.repository_id,b.repository_full_name,b.source_branch,b.commit_sha,b.commit_title,b.commit_author_name,
		       b.commit_author_login,b.committed_at,b.trigger_type,b.platform,b.status,b.attempts,COALESCE(b.worker_id,''),
		       b.fencing_token,b.lease_until,COALESCE(b.error_code,''),COALESCE(b.error_message,''),b.created_at,b.updated_at,p.public_id,a.public_id,ae.public_id
		FROM builds b JOIN projects p ON p.id=b.project_id JOIN apps a ON a.id=b.app_id JOIN app_environments ae ON ae.id=b.app_environment_id
		WHERE b.workspace_id=$1 AND b.public_id=$2`, workspaceID, publicID).
		Scan(append(buildScanTargets(&build), &build.ProjectPublicID, &build.AppPublicID, &build.AppEnvironmentPublicID)...)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Build{}, ErrNotFound
	}
	return build, err
}

func (s *Store) ListBuildLogs(ctx context.Context, workspaceID int64, buildPublicID string) ([]domain.BuildLog, error) {
	rows, err := s.Pool.Query(ctx, `
		SELECT l.sequence,l.message,l.created_at FROM build_logs l
		JOIN builds b ON b.id=l.build_id WHERE b.workspace_id=$1 AND b.public_id=$2
		ORDER BY l.sequence`, workspaceID, buildPublicID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []domain.BuildLog{}
	for rows.Next() {
		var item domain.BuildLog
		if err = rows.Scan(&item.Sequence, &item.Message, &item.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func buildScanTargets(build *domain.Build) []any {
	return []any{
		&build.ID, &build.PublicID, &build.WorkspaceID, &build.ProjectID, &build.AppID, &build.AppEnvironmentID,
		&build.InstallationExternalID, &build.RepositoryID, &build.RepositoryFullName, &build.SourceBranch, &build.CommitSHA,
		&build.CommitTitle, &build.CommitAuthorName, &build.CommitAuthorLogin, &build.CommittedAt, &build.TriggerType,
		&build.Platform, &build.Status, &build.Attempts, &build.WorkerID, &build.FencingToken,
		&build.LeaseUntil, &build.ErrorCode, &build.ErrorMessage, &build.CreatedAt, &build.UpdatedAt,
	}
}
