package store

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math"

	"github.com/jackc/pgx/v5"

	"github.com/molejo-platform/molejo/services/control-plane-api/internal/audit"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/domain"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/principal"
	releasecontract "github.com/molejo-platform/molejo/services/control-plane-api/internal/release"
)

const releaseSelectColumns = `r.id,r.public_id,r.workspace_id,r.project_id,r.app_id,COALESCE(r.app_environment_id,0),
	COALESCE(b.public_id,''),r.origin_kind,COALESCE(r.source_provider,''),COALESCE(r.source_repository,''),
	COALESCE(r.source_revision,''),COALESCE(r.source_ref,''),r.producer_kind,COALESCE(r.producer_external_id,''),
	COALESCE(r.producer_url,''),r.provenance_status,principal.public_id,principal.kind,principal.display_name,COALESCE(b.source_branch,''),COALESCE(r.commit_sha,''),
	COALESCE(b.commit_title,''),COALESCE(b.commit_author_name,''),COALESCE(b.commit_author_login,''),b.committed_at,
	COALESCE(b.trigger_type,''),r.image,COALESCE(r.platform,''),r.availability_status,r.expired_at,r.created_at,
	project.public_id,app.public_id,COALESCE(app_environment.public_id,'')`

const releaseJoins = `JOIN projects project ON project.id=r.project_id
	JOIN apps app ON app.id=r.app_id
	JOIN principals principal ON principal.id=r.created_by_principal_id
	LEFT JOIN builds b ON b.id=r.build_id
	LEFT JOIN app_environments app_environment ON app_environment.id=r.app_environment_id`

type RegisterExternalReleaseParams struct {
	WorkspaceID     int64
	ProjectPublicID string
	AppPublicID     string
	ReleasePublicID string
	Command         releasecontract.RegisterCommand
	IdempotencyHash []byte
	PayloadHash     []byte
	AuditEvent      audit.Event
}

func (s *Store) RegisterExternalRelease(ctx context.Context, actor principal.Principal, params RegisterExternalReleaseParams) (domain.Release, bool, error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return domain.Release{}, false, err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, "release:"+fmt.Sprintf("%x", params.IdempotencyHash)); err != nil {
		return domain.Release{}, false, err
	}

	var projectID, appID int64
	err = tx.QueryRow(ctx, `SELECT project.id,app.id FROM projects project JOIN apps app ON app.project_id=project.id
		WHERE project.workspace_id=$1 AND project.public_id=$2 AND app.public_id=$3
		  AND project.archived_at IS NULL AND app.archived_at IS NULL`, params.WorkspaceID, params.ProjectPublicID, params.AppPublicID).
		Scan(&projectID, &appID)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Release{}, false, ErrNotFound
	}
	if err != nil {
		return domain.Release{}, false, err
	}
	if actor.WorkspaceID != params.WorkspaceID || actor.ProjectID != projectID || actor.AppID != appID {
		return domain.Release{}, false, ErrAutomationAuthorization
	}

	existing, storedPayload, found, err := releaseByIdempotency(ctx, tx, appID, actor.ID, params.IdempotencyHash)
	if err != nil {
		return domain.Release{}, false, err
	}
	if found {
		if !bytes.Equal(storedPayload, params.PayloadHash) {
			return domain.Release{}, false, ErrIdempotencyConflict
		}
		if err = tx.Commit(ctx); err != nil {
			return domain.Release{}, false, err
		}
		return existing, true, nil
	}

	_, err = tx.Exec(ctx, `INSERT INTO releases(
		public_id,workspace_id,project_id,app_id,origin_kind,source_provider,source_repository,source_revision,source_ref,
		producer_kind,producer_external_id,producer_url,created_by_principal_id,idempotency_hash,payload_hash,image)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,NULLIF($9,''),$10,NULLIF($11,''),NULLIF($12,''),$13,$14,$15,$16)`,
		params.ReleasePublicID, params.WorkspaceID, projectID, appID, domain.ReleaseOriginExternal, params.Command.Source.Provider, params.Command.Source.Repository, params.Command.Source.Revision,
		params.Command.Source.Ref, params.Command.Provenance.Producer, params.Command.Provenance.ExternalRunID, params.Command.Provenance.URL, actor.ID,
		params.IdempotencyHash, params.PayloadHash, params.Command.Artifact.Reference)
	if uniqueConstraint(err) == "releases_public_id_key" {
		return domain.Release{}, false, ErrPublicIDCollision
	}
	if err != nil {
		return domain.Release{}, false, translateDBError(err)
	}
	item, err := releaseByPublicID(ctx, tx, params.WorkspaceID, params.ProjectPublicID, params.AppPublicID, params.ReleasePublicID)
	if err != nil {
		return domain.Release{}, false, err
	}
	event := params.AuditEvent
	event.ActorPrincipalID = &actor.ID
	event.WorkspaceID = &params.WorkspaceID
	event.TargetPublicID = params.ReleasePublicID
	if err = insertAudit(ctx, tx, event); err != nil {
		return domain.Release{}, false, err
	}
	if err = tx.Commit(ctx); err != nil {
		return domain.Release{}, false, err
	}
	return item, false, nil
}

func (s *Store) ListReleases(ctx context.Context, workspaceID int64, projectPublicID, appPublicID string, beforeID int64, limit int) ([]domain.Release, string, error) {
	if beforeID == 0 {
		beforeID = math.MaxInt64
	}
	rows, err := s.Pool.Query(ctx, `SELECT `+releaseSelectColumns+` FROM releases r `+releaseJoins+`
		WHERE r.workspace_id=$1 AND project.public_id=$2 AND app.public_id=$3 AND r.id<$4
		ORDER BY r.id DESC LIMIT $5`, workspaceID, projectPublicID, appPublicID, beforeID, limit+1)
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()
	items := []domain.Release{}
	for rows.Next() {
		item, scanErr := scanRelease(rows)
		if scanErr != nil {
			return nil, "", scanErr
		}
		items = append(items, item)
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

func (s *Store) FindRelease(ctx context.Context, workspaceID int64, projectPublicID, appPublicID, releasePublicID string) (domain.Release, error) {
	return releaseByPublicID(ctx, s.Pool, workspaceID, projectPublicID, appPublicID, releasePublicID)
}

func releaseByPublicID(ctx context.Context, query rowQuerier, workspaceID int64, projectPublicID, appPublicID, releasePublicID string) (domain.Release, error) {
	item, err := scanRelease(query.QueryRow(ctx, `SELECT `+releaseSelectColumns+` FROM releases r `+releaseJoins+`
		WHERE r.workspace_id=$1 AND project.public_id=$2 AND app.public_id=$3 AND r.public_id=$4`, workspaceID, projectPublicID, appPublicID, releasePublicID))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Release{}, ErrNotFound
	}
	return item, err
}

func releaseByIdempotency(ctx context.Context, query rowQuerier, appID, principalID int64, idempotencyHash []byte) (domain.Release, []byte, bool, error) {
	var payloadHash []byte
	item, err := scanReleaseWithPayload(query.QueryRow(ctx, `SELECT `+releaseSelectColumns+`,r.payload_hash FROM releases r `+releaseJoins+`
		WHERE r.app_id=$1 AND r.created_by_principal_id=$2 AND r.idempotency_hash=$3`, appID, principalID, idempotencyHash), &payloadHash)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Release{}, nil, false, nil
	}
	return item, payloadHash, err == nil, err
}

func scanRelease(row pgx.Row) (domain.Release, error) {
	return scanReleaseWithPayload(row, nil)
}

func scanReleaseWithPayload(row pgx.Row, payloadHash *[]byte) (domain.Release, error) {
	var item domain.Release
	targets := []any{
		&item.ID, &item.PublicID, &item.WorkspaceID, &item.ProjectID, &item.AppID, &item.AppEnvironmentID,
		&item.BuildPublicID, &item.OriginKind, &item.SourceProvider, &item.SourceRepository, &item.SourceRevision,
		&item.SourceRef, &item.ProducerKind, &item.ProducerExternalID, &item.ProducerURL, &item.ProvenanceStatus,
		&item.CreatedBy.ID, &item.CreatedBy.Kind, &item.CreatedBy.DisplayName,
		&item.SourceBranch, &item.CommitSHA, &item.CommitTitle, &item.CommitAuthorName, &item.CommitAuthorLogin,
		&item.CommittedAt, &item.TriggerType, &item.Image, &item.Platform, &item.AvailabilityStatus, &item.ExpiredAt,
		&item.CreatedAt, &item.ProjectPublicID, &item.AppPublicID, &item.AppEnvironmentPublicID,
	}
	if payloadHash != nil {
		targets = append(targets, payloadHash)
	}
	err := row.Scan(targets...)
	return item, err
}
