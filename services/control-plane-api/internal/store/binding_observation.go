package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/molejo-platform/molejo/packages/kubernetesbinding"
)

func (s *Store) BindingObservationTargets(ctx context.Context, clusterPublicID string) ([]kubernetesbinding.Target, error) {
	var active bool
	if err := s.Pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agent_installations WHERE public_id=$1 AND status='Active')`, clusterPublicID).Scan(&active); err != nil {
		return nil, err
	}
	if !active {
		return nil, ErrClusterNotFound
	}
	targets, err := storageBindingTargets(ctx, s.Pool, clusterPublicID)
	if err != nil {
		return nil, err
	}
	publication, err := publicationBindingTargets(ctx, s.Pool, clusterPublicID)
	if err != nil {
		return nil, err
	}
	targets = append(targets, publication...)
	if len(targets) > kubernetesbinding.MaxTargets {
		return nil, fmt.Errorf("binding targets exceed %d", kubernetesbinding.MaxTargets)
	}
	return targets, nil
}

func (s *Store) ReconcileBindingObservations(ctx context.Context, clusterPublicID, sessionID string, sequence uint64, observations []kubernetesbinding.Observation, complete bool, receivedAt time.Time) error {
	if sessionID == "" || sequence == 0 || sequence > math.MaxInt64 || len(observations) > kubernetesbinding.MaxTargets {
		return ErrAgentIdentityMismatch
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var clusterID int64
	var activeSessionID *string
	var activeSequence int64
	if err = tx.QueryRow(ctx, `SELECT id,control_session_id,control_session_sequence FROM agent_installations WHERE public_id=$1 AND status='Active' FOR UPDATE`, clusterPublicID).Scan(&clusterID, &activeSessionID, &activeSequence); errors.Is(err, pgx.ErrNoRows) || activeSessionID == nil || *activeSessionID != sessionID || activeSequence != int64(sequence) {
		return ErrAgentIdentityMismatch
	}
	if err != nil {
		return err
	}
	targets, err := storageBindingTargets(ctx, tx, clusterPublicID)
	if err != nil {
		return err
	}
	publication, err := publicationBindingTargets(ctx, tx, clusterPublicID)
	if err != nil {
		return err
	}
	targets = append(targets, publication...)
	targetByID := make(map[string]kubernetesbinding.Target, len(targets))
	for _, target := range targets {
		targetByID[target.ID] = target
	}
	seen := make(map[string]struct{}, len(observations))
	for _, observation := range observations {
		target, exists := targetByID[observation.ID]
		if !exists {
			continue
		}
		if _, duplicate := seen[observation.ID]; duplicate {
			return errors.New("binding observation is duplicated")
		}
		if observation.Version != target.Version {
			continue
		}
		if err = kubernetesbinding.ValidateObservation(target, observation, receivedAt); err != nil {
			return err
		}
		seen[observation.ID] = struct{}{}
	}
	if complete {
		if _, err = tx.Exec(ctx, `UPDATE cluster_storage_bindings SET health='Unknown',reason_code='binding_observation_missing',observed_at=NULL,expires_at=NULL,observed_session_id=$2,observed_sequence=$3 WHERE cluster_id=$1`, clusterID, sessionID, int64(sequence)); err != nil {
			return err
		}
	}
	for _, observation := range observations {
		target, ok := targetByID[observation.ID]
		if !ok || target.Version != observation.Version {
			continue
		}
		expiresAt := observation.SampledAt.Add(kubernetesbinding.ObservationTTL)
		switch observation.Kind {
		case kubernetesbinding.KindStorage:
			modes, marshalErr := json.Marshal(kubernetesbinding.NormalizeStrings(observation.Storage.AccessModes))
			if marshalErr != nil {
				return marshalErr
			}
			result, execErr := tx.Exec(ctx, `UPDATE cluster_storage_bindings SET provisioner=$4,access_modes_json=$5::text::jsonb,allow_expansion=$6,
				volume_binding_mode=$7,health=$8,reason_code=$9,observed_at=$10,expires_at=$11,observed_session_id=$12,observed_sequence=$13
				WHERE cluster_id=$1 AND storage_profile_id=$2 AND version=$3`, clusterID, observation.ID[len("storage:"):], observation.Version,
				observation.Storage.Provisioner, string(modes), observation.Storage.AllowExpansion, observation.Storage.VolumeBindingMode,
				observation.Health, observation.ReasonCode, observation.SampledAt, expiresAt, sessionID, int64(sequence))
			if execErr != nil {
				return execErr
			}
			if result.RowsAffected() != 1 {
				return ErrVersionConflict
			}
		case kubernetesbinding.KindPublicationHTTP:
			parts := strings.SplitN(observation.ID, ":", 2)
			if len(parts) != 2 {
				return errors.New("publication observation identity invalid")
			}
			value, marshalErr := json.Marshal(map[string]any{"health": observation.Health, "reasonCode": observation.ReasonCode, "sampledAt": observation.SampledAt, "facts": observation.Publication})
			if marshalErr != nil {
				return marshalErr
			}
			_, err = tx.Exec(ctx, `UPDATE cluster_publication_bindings SET observation=jsonb_set(observation,ARRAY[$3],$4::jsonb),observed_at=GREATEST(observed_at,$5),expires_at=GREATEST(expires_at,$6),observed_session_id=$7,observed_sequence=$8 WHERE cluster_id=$1 AND id=$2 AND version=$9 AND (observation->$3->>'sampledAt' IS NULL OR (observation->$3->>'sampledAt')::timestamptz<$5)`, clusterID, parts[0], parts[1], value, observation.SampledAt, expiresAt, sessionID, int64(sequence), observation.Version)
			if err != nil {
				return err
			}

		}
	}
	return tx.Commit(ctx)
}

type bindingTargetQuerier interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

func storageBindingTargets(ctx context.Context, query bindingTargetQuerier, clusterPublicID string) ([]kubernetesbinding.Target, error) {
	rows, err := query.Query(ctx, `SELECT b.storage_profile_id,b.storage_class_name,b.version FROM cluster_storage_bindings b JOIN agent_installations i ON i.id=b.cluster_id WHERE i.public_id=$1 ORDER BY b.storage_profile_id`, clusterPublicID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	targets := make([]kubernetesbinding.Target, 0)
	for rows.Next() {
		var profileID, className string
		var version int64
		if err = rows.Scan(&profileID, &className, &version); err != nil {
			return nil, err
		}
		targets = append(targets, kubernetesbinding.Target{ID: storageBindingTargetID(profileID), Kind: kubernetesbinding.KindStorage, Version: version, Storage: &kubernetesbinding.StorageTarget{StorageClassName: className}})
	}
	return targets, rows.Err()
}

func publicationBindingTargets(ctx context.Context, query bindingTargetQuerier, clusterPublicID string) ([]kubernetesbinding.Target, error) {
	rows, err := query.Query(ctx, publicationBindingSelect+` WHERE i.public_id=$1`, clusterPublicID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []kubernetesbinding.Target{}
	for rows.Next() {
		b, err := scanPublicationBinding(rows)
		if err != nil {
			return nil, err
		}
		for _, l := range b.Listeners {
			result = append(result, kubernetesbinding.Target{ID: b.ID + ":" + l.Name, Kind: kubernetesbinding.KindPublicationHTTP, Version: b.Revision, Publication: &kubernetesbinding.PublicationTarget{GatewayNamespace: b.GatewayNamespace, GatewayName: b.GatewayName, SectionName: l.Name}})
		}
	}
	return result, rows.Err()
}
