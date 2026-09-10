package store

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/molejo-platform/molejo/packages/kubernetesbinding"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/audit"
)

const storageBindingSelect = `SELECT i.public_id,i.cluster_uid,b.storage_profile_id,b.storage_class_name,b.provisioner,b.access_modes_json,
	b.allow_expansion,b.volume_binding_mode,b.health,b.reason_code,b.observed_at,b.expires_at,b.version,b.created_at,b.updated_at
	FROM cluster_storage_bindings b JOIN agent_installations i ON i.id=b.cluster_id`

func (s *Store) ClusterStorageBindings(ctx context.Context, clusterPublicID string) ([]ClusterStorageBinding, error) {
	rows, err := s.Pool.Query(ctx, storageBindingSelect+` WHERE i.public_id=$1 ORDER BY b.storage_profile_id`, clusterPublicID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]ClusterStorageBinding, 0)
	for rows.Next() {
		item, scanErr := scanStorageBinding(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) ClusterStorageBinding(ctx context.Context, clusterPublicID, profileID string) (ClusterStorageBinding, error) {
	return scanStorageBinding(s.Pool.QueryRow(ctx, storageBindingSelect+` WHERE i.public_id=$1 AND b.storage_profile_id=$2`, clusterPublicID, profileID))
}

func (s *Store) PutClusterStorageBinding(ctx context.Context, clusterPublicID, profileID, storageClassName string, actorID int64, expectedVersion *int64, event audit.Event) (ClusterStorageBinding, error) {
	target := kubernetesbinding.Target{ID: storageBindingTargetID(profileID), Kind: kubernetesbinding.KindStorage, Version: 1, Storage: &kubernetesbinding.StorageTarget{StorageClassName: storageClassName}}
	if err := kubernetesbinding.ValidateTarget(target); err != nil {
		return ClusterStorageBinding{}, err
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return ClusterStorageBinding{}, err
	}
	defer tx.Rollback(ctx)
	var clusterID int64
	var profileExists bool
	if err = tx.QueryRow(ctx, `SELECT id FROM agent_installations WHERE public_id=$1 AND status='Active' FOR UPDATE`, clusterPublicID).Scan(&clusterID); errors.Is(err, pgx.ErrNoRows) {
		return ClusterStorageBinding{}, ErrClusterNotFound
	}
	if err != nil {
		return ClusterStorageBinding{}, err
	}
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM storage_profiles WHERE id=$1 AND enabled)`, profileID).Scan(&profileExists); err != nil {
		return ClusterStorageBinding{}, err
	}
	if !profileExists {
		return ClusterStorageBinding{}, ErrStorageProfileUnavailable
	}
	if expectedVersion == nil {
		result, execErr := tx.Exec(ctx, `INSERT INTO cluster_storage_bindings(cluster_id,storage_profile_id,storage_class_name,created_by,updated_by)
			VALUES($1,$2,$3,$4,$4) ON CONFLICT DO NOTHING`, clusterID, profileID, storageClassName, actorID)
		if execErr != nil {
			return ClusterStorageBinding{}, execErr
		}
		if result.RowsAffected() != 1 {
			return ClusterStorageBinding{}, ErrVersionConflict
		}
	} else {
		result, execErr := tx.Exec(ctx, `UPDATE cluster_storage_bindings SET storage_class_name=$3,provisioner='',access_modes_json='[]'::jsonb,
			allow_expansion=false,volume_binding_mode='',health='Unknown',reason_code='binding_observation_pending',observed_at=NULL,expires_at=NULL,
			observed_session_id='',observed_sequence=0,version=version+1,updated_by=$4,updated_at=now()
			WHERE cluster_id=$1 AND storage_profile_id=$2 AND version=$5`, clusterID, profileID, storageClassName, actorID, *expectedVersion)
		if execErr != nil {
			return ClusterStorageBinding{}, execErr
		}
		if result.RowsAffected() != 1 {
			return ClusterStorageBinding{}, ErrVersionConflict
		}
	}
	event.ActorUserID = &actorID
	event.TargetPublicID = clusterPublicID + "/" + profileID
	if err = insertAudit(ctx, tx, event); err != nil {
		return ClusterStorageBinding{}, err
	}
	binding, err := scanStorageBinding(tx.QueryRow(ctx, storageBindingSelect+` WHERE i.public_id=$1 AND b.storage_profile_id=$2`, clusterPublicID, profileID))
	if err != nil {
		return ClusterStorageBinding{}, err
	}
	return binding, tx.Commit(ctx)
}

func (s *Store) DeleteClusterStorageBinding(ctx context.Context, clusterPublicID, profileID string, actorID, expectedVersion int64, event audit.Event) error {
	return s.deleteKubernetesBinding(ctx, `DELETE FROM cluster_storage_bindings b USING agent_installations i WHERE b.cluster_id=i.id AND i.public_id=$1 AND b.storage_profile_id=$2 AND b.version=$3`, []any{clusterPublicID, profileID, expectedVersion}, actorID, clusterPublicID+"/"+profileID, event)
}

type storageBindingScanner interface{ Scan(...any) error }

func scanStorageBinding(row storageBindingScanner) (ClusterStorageBinding, error) {
	var item ClusterStorageBinding
	var modes []byte
	err := row.Scan(&item.ClusterID, &item.ClusterUID, &item.StorageProfileID, &item.StorageClassName, &item.Provisioner, &modes,
		&item.AllowExpansion, &item.VolumeBindingMode, &item.Health, &item.ReasonCode, &item.ObservedAt, &item.ExpiresAt,
		&item.Version, &item.CreatedAt, &item.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return item, ErrBindingNotFound
	}
	if err == nil {
		err = json.Unmarshal(modes, &item.AccessModes)
		if item.AccessModes == nil {
			item.AccessModes = []string{}
		}
	}
	return item, err
}
