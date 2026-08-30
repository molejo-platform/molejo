package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/fruto-platform/fruto/services/control-plane-api/internal/domain"
	"github.com/jackc/pgx/v5"
)

var ErrStorageProfileUnavailable = errors.New("storage profile unavailable")
var ErrStorageQuotaExceeded = errors.New("storage quota exceeded")
var ErrVolumeAttached = errors.New("volume is attached")

type StorageProfileInstallation struct {
	ID                string
	Name              string
	MinimumSizeGiB    int64
	MaximumSizeGiB    int64
	TotalCapacityGiB  int64
	WorkspaceQuotaGiB int64
	Expandable        bool
	Snapshots         bool
	AutomaticBackup   bool
	Durability        string
	RuntimeBinding    string
	Enabled           bool
}

type VolumeRuntime struct {
	Volume         domain.AppVolume
	RuntimeBinding string
}

func (s *Store) ConfigureStorageProfile(ctx context.Context, profile StorageProfileInstallation) error {
	if profile.ID == "" || profile.MinimumSizeGiB < 1 || profile.MaximumSizeGiB < profile.MinimumSizeGiB || profile.TotalCapacityGiB < profile.MaximumSizeGiB || profile.WorkspaceQuotaGiB < profile.MinimumSizeGiB {
		return errors.New("invalid storage profile installation")
	}
	if profile.Enabled && profile.RuntimeBinding == "" {
		return errors.New("enabled storage profile requires an internal runtime binding")
	}
	_, err := s.Pool.Exec(ctx, `INSERT INTO storage_profiles(id,display_name,minimum_size_gib,maximum_size_gib,total_capacity_gib,workspace_quota_gib,expandable,snapshots,automatic_backup,durability,runtime_binding,enabled)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
		ON CONFLICT(id) DO UPDATE SET display_name=EXCLUDED.display_name,minimum_size_gib=EXCLUDED.minimum_size_gib,maximum_size_gib=EXCLUDED.maximum_size_gib,total_capacity_gib=EXCLUDED.total_capacity_gib,workspace_quota_gib=EXCLUDED.workspace_quota_gib,expandable=EXCLUDED.expandable,snapshots=EXCLUDED.snapshots,automatic_backup=EXCLUDED.automatic_backup,durability=EXCLUDED.durability,runtime_binding=EXCLUDED.runtime_binding,enabled=EXCLUDED.enabled,version=storage_profiles.version+1,updated_at=now()
		WHERE (storage_profiles.display_name,storage_profiles.minimum_size_gib,storage_profiles.maximum_size_gib,storage_profiles.total_capacity_gib,storage_profiles.workspace_quota_gib,storage_profiles.expandable,storage_profiles.snapshots,storage_profiles.automatic_backup,storage_profiles.durability,storage_profiles.runtime_binding,storage_profiles.enabled)
		IS DISTINCT FROM (EXCLUDED.display_name,EXCLUDED.minimum_size_gib,EXCLUDED.maximum_size_gib,EXCLUDED.total_capacity_gib,EXCLUDED.workspace_quota_gib,EXCLUDED.expandable,EXCLUDED.snapshots,EXCLUDED.automatic_backup,EXCLUDED.durability,EXCLUDED.runtime_binding,EXCLUDED.enabled)`,
		profile.ID, profile.Name, profile.MinimumSizeGiB, profile.MaximumSizeGiB, profile.TotalCapacityGiB, profile.WorkspaceQuotaGiB, profile.Expandable, profile.Snapshots, profile.AutomaticBackup, profile.Durability, profile.RuntimeBinding, profile.Enabled)
	return err
}

func (s *Store) ListStorageProfiles(ctx context.Context, workspaceID int64) ([]domain.StorageProfile, error) {
	rows, err := s.Pool.Query(ctx, `SELECT sp.id,sp.display_name,sp.minimum_size_gib,sp.maximum_size_gib,sp.expandable,sp.snapshots,sp.automatic_backup,sp.durability,
		GREATEST(0,LEAST(sp.total_capacity_gib-COALESCE(all_usage.used,0),sp.workspace_quota_gib-COALESCE(workspace_usage.used,0)))
		FROM storage_profiles sp
		LEFT JOIN (SELECT storage_profile_id,SUM(requested_size_gib) used FROM app_volumes GROUP BY storage_profile_id) all_usage ON all_usage.storage_profile_id=sp.id
		LEFT JOIN (SELECT storage_profile_id,SUM(requested_size_gib) used FROM app_volumes WHERE workspace_id=$1 GROUP BY storage_profile_id) workspace_usage ON workspace_usage.storage_profile_id=sp.id
		WHERE sp.enabled ORDER BY sp.id`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]domain.StorageProfile, 0)
	for rows.Next() {
		var item domain.StorageProfile
		if err = rows.Scan(&item.ID, &item.Name, &item.MinimumSizeGiB, &item.MaximumSizeGiB, &item.Expandable, &item.Snapshots, &item.AutomaticBackup, &item.Durability, &item.AvailableGiB); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func createAppVolume(ctx context.Context, tx pgx.Tx, workspaceID, actorID int64, appEnvironment domain.AppEnvironment, request domain.VolumeRequest) (domain.AppVolume, error) {
	var profile StorageProfileInstallation
	if err := tx.QueryRow(ctx, `SELECT id,display_name,minimum_size_gib,maximum_size_gib,total_capacity_gib,workspace_quota_gib,expandable,snapshots,automatic_backup,durability,runtime_binding,enabled FROM storage_profiles WHERE id=$1 FOR UPDATE`, request.StorageProfileID).
		Scan(&profile.ID, &profile.Name, &profile.MinimumSizeGiB, &profile.MaximumSizeGiB, &profile.TotalCapacityGiB, &profile.WorkspaceQuotaGiB, &profile.Expandable, &profile.Snapshots, &profile.AutomaticBackup, &profile.Durability, &profile.RuntimeBinding, &profile.Enabled); errors.Is(err, pgx.ErrNoRows) {
		return domain.AppVolume{}, ErrStorageProfileUnavailable
	} else if err != nil {
		return domain.AppVolume{}, err
	}
	if !profile.Enabled || request.SizeGiB < profile.MinimumSizeGiB || request.SizeGiB > profile.MaximumSizeGiB {
		return domain.AppVolume{}, ErrStorageProfileUnavailable
	}
	var globalUsed, workspaceUsed int64
	if err := tx.QueryRow(ctx, `SELECT COALESCE(SUM(requested_size_gib),0),COALESCE(SUM(requested_size_gib) FILTER (WHERE workspace_id=$1),0) FROM app_volumes WHERE storage_profile_id=$2`, workspaceID, profile.ID).Scan(&globalUsed, &workspaceUsed); err != nil {
		return domain.AppVolume{}, err
	}
	if globalUsed+request.SizeGiB > profile.TotalCapacityGiB || workspaceUsed+request.SizeGiB > profile.WorkspaceQuotaGiB {
		return domain.AppVolume{}, ErrStorageQuotaExceeded
	}
	publicID, err := domain.NewPublicID("vol")
	if err != nil {
		return domain.AppVolume{}, err
	}
	var volume domain.AppVolume
	err = tx.QueryRow(ctx, `INSERT INTO app_volumes(public_id,workspace_id,app_environment_id,storage_profile_id,requested_size_gib,mount_path)
		VALUES($1,$2,$3,$4,$5,$6)
		RETURNING id,public_id,workspace_id,app_environment_id,storage_profile_id,requested_size_gib,mount_path,retention_policy,desired_state,observed_state,message,false,version,created_at,updated_at,deletion_requested_at`,
		publicID, workspaceID, appEnvironment.ID, request.StorageProfileID, request.SizeGiB, request.MountPath).
		Scan(&volume.ID, &volume.PublicID, &volume.WorkspaceID, &volume.AppEnvironmentID, &volume.StorageProfileID, &volume.SizeGiB, &volume.MountPath, &volume.RetentionPolicy, &volume.DesiredState, &volume.State, &volume.Message, &volume.Attached, &volume.Version, &volume.CreatedAt, &volume.UpdatedAt, &volume.DeletionRequestedAt)
	if err != nil {
		return domain.AppVolume{}, translateDBError(err)
	}
	volume.AppEnvironmentPublicID = appEnvironment.PublicID
	payload, _ := domain.CanonicalJSON(request)
	operation, err := insertVolumeOperation(ctx, tx, workspaceID, appEnvironment.ID, volume.ID, actorID, domain.OperationEnsureVolume, domain.SHA256([]byte("ensure-volume:"+publicID)), domain.SHA256(payload), volume.Version)
	if err != nil {
		return domain.AppVolume{}, err
	}
	_ = operation
	if _, err = tx.Exec(ctx, `UPDATE app_environments SET last_state='Progressing',last_message='persistent storage is being prepared',updated_at=now() WHERE id=$1`, appEnvironment.ID); err != nil {
		return domain.AppVolume{}, err
	}
	return volume, nil
}

func (s *Store) FindAppVolume(ctx context.Context, workspaceID int64, appEnvironmentPublicID string) (domain.AppVolume, error) {
	volume, err := scanAppVolume(s.Pool.QueryRow(ctx, appVolumeSelect+` WHERE av.workspace_id=$1 AND ae.public_id=$2`, workspaceID, appEnvironmentPublicID))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.AppVolume{}, ErrNotFound
	}
	return volume, err
}

func (s *Store) VolumeRuntime(ctx context.Context, workspaceID, volumeID int64) (VolumeRuntime, error) {
	var item VolumeRuntime
	var attached bool
	err := s.Pool.QueryRow(ctx, `SELECT av.id,av.public_id,av.workspace_id,av.app_environment_id,ae.public_id,av.storage_profile_id,av.requested_size_gib,av.mount_path,av.retention_policy,av.desired_state,av.observed_state,av.message,
		(ae.archived_at IS NULL AND EXISTS(SELECT 1 FROM deployments d WHERE d.app_volume_id=av.id AND d.status IN ('Pending','Progressing','Ready'))),av.version,av.created_at,av.updated_at,av.deletion_requested_at,sp.runtime_binding
		FROM app_volumes av JOIN app_environments ae ON ae.id=av.app_environment_id JOIN storage_profiles sp ON sp.id=av.storage_profile_id WHERE av.workspace_id=$1 AND av.id=$2`, workspaceID, volumeID).
		Scan(&item.Volume.ID, &item.Volume.PublicID, &item.Volume.WorkspaceID, &item.Volume.AppEnvironmentID, &item.Volume.AppEnvironmentPublicID, &item.Volume.StorageProfileID, &item.Volume.SizeGiB, &item.Volume.MountPath, &item.Volume.RetentionPolicy, &item.Volume.DesiredState, &item.Volume.State, &item.Volume.Message, &attached, &item.Volume.Version, &item.Volume.CreatedAt, &item.Volume.UpdatedAt, &item.Volume.DeletionRequestedAt, &item.RuntimeBinding)
	item.Volume.Attached = attached
	if errors.Is(err, pgx.ErrNoRows) {
		return VolumeRuntime{}, ErrNotFound
	}
	return item, err
}

const appVolumeSelect = `SELECT av.id,av.public_id,av.workspace_id,av.app_environment_id,ae.public_id,av.storage_profile_id,av.requested_size_gib,av.mount_path,av.retention_policy,av.desired_state,av.observed_state,av.message,
	(ae.archived_at IS NULL AND EXISTS(SELECT 1 FROM deployments d WHERE d.app_volume_id=av.id AND d.status IN ('Pending','Progressing','Ready'))),av.version,av.created_at,av.updated_at,av.deletion_requested_at
	FROM app_volumes av JOIN app_environments ae ON ae.id=av.app_environment_id`

func scanAppVolume(row pgx.Row) (domain.AppVolume, error) {
	var item domain.AppVolume
	err := row.Scan(&item.ID, &item.PublicID, &item.WorkspaceID, &item.AppEnvironmentID, &item.AppEnvironmentPublicID, &item.StorageProfileID, &item.SizeGiB, &item.MountPath, &item.RetentionPolicy, &item.DesiredState, &item.State, &item.Message, &item.Attached, &item.Version, &item.CreatedAt, &item.UpdatedAt, &item.DeletionRequestedAt)
	return item, err
}

func (s *Store) ExpandAppVolume(ctx context.Context, workspaceID, actorID int64, appEnvironmentPublicID string, requestedSizeGiB, expectedVersion int64, idempotencyHash, payloadHash []byte) (domain.AppVolume, domain.Operation, bool, error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return domain.AppVolume{}, domain.Operation{}, false, err
	}
	defer tx.Rollback(ctx)
	if existing, found, findErr := operationByIdempotency(ctx, tx, workspaceID, actorID, idempotencyHash, payloadHash); findErr != nil {
		return domain.AppVolume{}, domain.Operation{}, false, findErr
	} else if found {
		volume, volumeErr := appVolumeByID(ctx, tx, existing.AppVolumeID)
		if volumeErr != nil {
			return domain.AppVolume{}, domain.Operation{}, false, volumeErr
		}
		return volume, existing, true, tx.Commit(ctx)
	}
	volume, err := scanAppVolume(tx.QueryRow(ctx, appVolumeSelect+` WHERE av.workspace_id=$1 AND ae.public_id=$2 FOR UPDATE OF av`, workspaceID, appEnvironmentPublicID))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.AppVolume{}, domain.Operation{}, false, ErrNotFound
	}
	if err != nil {
		return domain.AppVolume{}, domain.Operation{}, false, err
	}
	if volume.Version != expectedVersion || volume.DeletionRequestedAt != nil {
		return domain.AppVolume{}, domain.Operation{}, false, ErrVersionConflict
	}
	var maximumSizeGiB int64
	var expandable bool
	var totalCapacityGiB, workspaceQuotaGiB int64
	if err = tx.QueryRow(ctx, `SELECT maximum_size_gib,expandable,total_capacity_gib,workspace_quota_gib FROM storage_profiles WHERE id=$1 AND enabled FOR UPDATE`, volume.StorageProfileID).Scan(&maximumSizeGiB, &expandable, &totalCapacityGiB, &workspaceQuotaGiB); errors.Is(err, pgx.ErrNoRows) {
		return domain.AppVolume{}, domain.Operation{}, false, ErrStorageProfileUnavailable
	} else if err != nil {
		return domain.AppVolume{}, domain.Operation{}, false, err
	}
	if !expandable || domain.ValidateVolumeExpansion(volume.SizeGiB, requestedSizeGiB, maximumSizeGiB) != nil {
		return domain.AppVolume{}, domain.Operation{}, false, ErrConflict
	}
	var globalUsed, workspaceUsed int64
	if err = tx.QueryRow(ctx, `SELECT COALESCE(SUM(requested_size_gib),0),COALESCE(SUM(requested_size_gib) FILTER (WHERE workspace_id=$1),0) FROM app_volumes WHERE storage_profile_id=$2`, workspaceID, volume.StorageProfileID).Scan(&globalUsed, &workspaceUsed); err != nil {
		return domain.AppVolume{}, domain.Operation{}, false, err
	}
	delta := requestedSizeGiB - volume.SizeGiB
	if globalUsed+delta > totalCapacityGiB || workspaceUsed+delta > workspaceQuotaGiB {
		return domain.AppVolume{}, domain.Operation{}, false, ErrStorageQuotaExceeded
	}
	if err = tx.QueryRow(ctx, `UPDATE app_volumes SET requested_size_gib=$1,observed_state='Expanding',message='',version=version+1,updated_at=now() WHERE id=$2 RETURNING requested_size_gib,observed_state,version,updated_at`, requestedSizeGiB, volume.ID).Scan(&volume.SizeGiB, &volume.State, &volume.Version, &volume.UpdatedAt); err != nil {
		return domain.AppVolume{}, domain.Operation{}, false, err
	}
	operation, err := insertVolumeOperation(ctx, tx, workspaceID, volume.AppEnvironmentID, volume.ID, actorID, domain.OperationExpandVolume, idempotencyHash, payloadHash, volume.Version)
	if err != nil {
		return domain.AppVolume{}, domain.Operation{}, false, translateDBError(err)
	}
	operation.AppVolumePublicID = volume.PublicID
	if _, err = tx.Exec(ctx, `UPDATE app_environments SET last_state='Progressing',last_message='persistent storage is being expanded',updated_at=now() WHERE id=$1`, volume.AppEnvironmentID); err != nil {
		return domain.AppVolume{}, domain.Operation{}, false, err
	}
	return volume, operation, false, tx.Commit(ctx)
}

func (s *Store) DeleteAppVolume(ctx context.Context, workspaceID, actorID int64, appEnvironmentPublicID string, expectedVersion int64, idempotencyHash, payloadHash []byte) (domain.AppVolume, domain.Operation, bool, error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return domain.AppVolume{}, domain.Operation{}, false, err
	}
	defer tx.Rollback(ctx)
	if existing, found, findErr := operationByIdempotency(ctx, tx, workspaceID, actorID, idempotencyHash, payloadHash); findErr != nil {
		return domain.AppVolume{}, domain.Operation{}, false, findErr
	} else if found {
		volume, volumeErr := appVolumeByID(ctx, tx, existing.AppVolumeID)
		if volumeErr != nil {
			return domain.AppVolume{}, domain.Operation{}, false, volumeErr
		}
		return volume, existing, true, tx.Commit(ctx)
	}
	volume, err := scanAppVolume(tx.QueryRow(ctx, appVolumeSelect+` WHERE av.workspace_id=$1 AND ae.public_id=$2 FOR UPDATE OF av`, workspaceID, appEnvironmentPublicID))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.AppVolume{}, domain.Operation{}, false, ErrNotFound
	}
	if err != nil {
		return domain.AppVolume{}, domain.Operation{}, false, err
	}
	if volume.Version != expectedVersion || volume.DeletionRequestedAt != nil {
		return domain.AppVolume{}, domain.Operation{}, false, ErrVersionConflict
	}
	if volume.Attached {
		return domain.AppVolume{}, domain.Operation{}, false, ErrVolumeAttached
	}
	if err = tx.QueryRow(ctx, `UPDATE app_volumes SET desired_state='Deleted',deletion_requested_at=now(),version=version+1,updated_at=now() WHERE id=$1 RETURNING desired_state,deletion_requested_at,version,updated_at`, volume.ID).Scan(&volume.DesiredState, &volume.DeletionRequestedAt, &volume.Version, &volume.UpdatedAt); err != nil {
		return domain.AppVolume{}, domain.Operation{}, false, err
	}
	operation, err := insertVolumeOperation(ctx, tx, workspaceID, volume.AppEnvironmentID, volume.ID, actorID, domain.OperationDeleteVolume, idempotencyHash, payloadHash, volume.Version)
	if err != nil {
		return domain.AppVolume{}, domain.Operation{}, false, translateDBError(err)
	}
	operation.AppVolumePublicID = volume.PublicID
	if _, err = tx.Exec(ctx, `UPDATE app_environments SET last_state='Progressing',last_message='persistent storage removal is in progress',updated_at=now() WHERE id=$1 AND archived_at IS NULL`, volume.AppEnvironmentID); err != nil {
		return domain.AppVolume{}, domain.Operation{}, false, err
	}
	return volume, operation, false, tx.Commit(ctx)
}

func appVolumeByID(ctx context.Context, query rowQuerier, id int64) (domain.AppVolume, error) {
	return scanAppVolume(query.QueryRow(ctx, appVolumeSelect+` WHERE av.id=$1`, id))
}

func insertVolumeOperation(ctx context.Context, tx pgx.Tx, workspaceID, appEnvironmentID, volumeID, actorID int64, kind string, idempotencyHash, payloadHash []byte, desiredVersion int64) (domain.Operation, error) {
	for range 3 {
		if _, err := tx.Exec(ctx, `SAVEPOINT volume_operation_public_id`); err != nil {
			return domain.Operation{}, err
		}
		publicID, err := domain.NewPublicID("op")
		if err != nil {
			return domain.Operation{}, err
		}
		var item domain.Operation
		err = tx.QueryRow(ctx, `INSERT INTO operations(public_id,workspace_id,app_environment_id,app_volume_id,requested_by_user_id,kind,status,idempotency_hash,payload_hash,desired_version)
			VALUES($1,$2,$3,$4,$5,$6,'Pending',$7,$8,$9)
			RETURNING id,public_id,workspace_id,app_environment_id,app_volume_id,requested_by_user_id,kind,status,desired_version,attempts,created_at,updated_at`,
			publicID, workspaceID, appEnvironmentID, volumeID, actorID, kind, idempotencyHash, payloadHash, desiredVersion).
			Scan(&item.ID, &item.PublicID, &item.WorkspaceID, &item.AppEnvironmentID, &item.AppVolumeID, &item.ActorID, &item.Kind, &item.Status, &item.DesiredVersion, &item.Attempts, &item.CreatedAt, &item.UpdatedAt)
		if uniqueConstraint(err) == "operations_public_id_key" {
			if _, rollbackErr := tx.Exec(ctx, `ROLLBACK TO SAVEPOINT volume_operation_public_id`); rollbackErr != nil {
				return domain.Operation{}, rollbackErr
			}
			if _, releaseErr := tx.Exec(ctx, `RELEASE SAVEPOINT volume_operation_public_id`); releaseErr != nil {
				return domain.Operation{}, releaseErr
			}
			continue
		}
		if err == nil {
			_, err = tx.Exec(ctx, `RELEASE SAVEPOINT volume_operation_public_id`)
		}
		item.AppVolumeID = volumeID
		return item, err
	}
	return domain.Operation{}, fmt.Errorf("allocate volume operation: %w", ErrPublicIDCollision)
}

func (s *Store) CompleteVolume(ctx context.Context, operation domain.Operation, state, message string, observedSizeGiB int64) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err = completeOperationLease(ctx, tx, operation); err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, `UPDATE app_volumes SET observed_state=$1,message=$2,observed_size_gib=$3,updated_at=now() WHERE id=$4 AND version=$5`, state, message, observedSizeGiB, operation.AppVolumeID, operation.DesiredVersion)
	if err != nil || tag.RowsAffected() != 1 {
		return ErrLeaseLost
	}
	if state == domain.VolumeStateReady {
		if _, err = tx.Exec(ctx, `UPDATE app_environments SET last_state=CASE WHEN current_deployment_id IS NULL THEN 'Pending' ELSE 'Ready' END,last_message=CASE WHEN current_deployment_id IS NULL THEN 'persistent storage is ready; no release is deployed' ELSE '' END,updated_at=now() WHERE id=$1 AND archived_at IS NULL`, operation.AppEnvironmentID); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func storageAvailable(globalCapacity, globalUsed, workspaceQuota, workspaceUsed int64) int64 {
	return max(0, min(globalCapacity-globalUsed, workspaceQuota-workspaceUsed))
}
