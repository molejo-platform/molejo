package store

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/molejo-platform/molejo/packages/kubernetesbinding"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/audit"
)

const publicationBindingSelect = `SELECT i.public_id,i.cluster_uid,b.gateway_namespace,b.gateway_name,b.section_name,b.gateway_class_name,
	b.gateway_class_accepted,b.gateway_programmed,b.listener_ready,b.supported_route_kinds_json,b.health,b.reason_code,b.observed_at,b.expires_at,
	b.version,b.created_at,b.updated_at FROM cluster_publication_bindings b JOIN agent_installations i ON i.id=b.cluster_id`

func (s *Store) ClusterPublicationBinding(ctx context.Context, clusterPublicID string) (ClusterPublicationBinding, error) {
	return scanPublicationBinding(s.Pool.QueryRow(ctx, publicationBindingSelect+` WHERE i.public_id=$1`, clusterPublicID))
}

func (s *Store) PutClusterPublicationBinding(ctx context.Context, clusterPublicID, namespace, name, section string, actorID int64, expectedVersion *int64, event audit.Event) (ClusterPublicationBinding, error) {
	target := kubernetesbinding.Target{ID: publicationBindingTargetID, Kind: kubernetesbinding.KindPublicationHTTP, Version: 1, Publication: &kubernetesbinding.PublicationTarget{GatewayNamespace: namespace, GatewayName: name, SectionName: section}}
	if err := kubernetesbinding.ValidateTarget(target); err != nil {
		return ClusterPublicationBinding{}, err
	}
	if namespace != "molejo-system" || name != "molejo" || section != "https-molejo" {
		return ClusterPublicationBinding{}, ErrBindingUnsupported
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return ClusterPublicationBinding{}, err
	}
	defer tx.Rollback(ctx)
	var clusterID int64
	if err = tx.QueryRow(ctx, `SELECT id FROM agent_installations WHERE public_id=$1 AND status='Active' FOR UPDATE`, clusterPublicID).Scan(&clusterID); errors.Is(err, pgx.ErrNoRows) {
		return ClusterPublicationBinding{}, ErrClusterNotFound
	}
	if err != nil {
		return ClusterPublicationBinding{}, err
	}
	if expectedVersion == nil {
		result, execErr := tx.Exec(ctx, `INSERT INTO cluster_publication_bindings(cluster_id,gateway_namespace,gateway_name,section_name,created_by,updated_by)
			VALUES($1,$2,$3,$4,$5,$5) ON CONFLICT DO NOTHING`, clusterID, namespace, name, section, actorID)
		if execErr != nil {
			return ClusterPublicationBinding{}, execErr
		}
		if result.RowsAffected() != 1 {
			return ClusterPublicationBinding{}, ErrVersionConflict
		}
	} else {
		result, execErr := tx.Exec(ctx, `UPDATE cluster_publication_bindings SET gateway_namespace=$2,gateway_name=$3,section_name=$4,
			gateway_class_name='',gateway_class_accepted=false,gateway_programmed=false,listener_ready=false,supported_route_kinds_json='[]'::jsonb,
			health='Unknown',reason_code='binding_observation_pending',observed_at=NULL,expires_at=NULL,observed_session_id='',observed_sequence=0,
			version=version+1,updated_by=$5,updated_at=now() WHERE cluster_id=$1 AND version=$6`, clusterID, namespace, name, section, actorID, *expectedVersion)
		if execErr != nil {
			return ClusterPublicationBinding{}, execErr
		}
		if result.RowsAffected() != 1 {
			return ClusterPublicationBinding{}, ErrVersionConflict
		}
	}
	event.ActorUserID = &actorID
	event.TargetPublicID = clusterPublicID
	if err = insertAudit(ctx, tx, event); err != nil {
		return ClusterPublicationBinding{}, err
	}
	binding, err := scanPublicationBinding(tx.QueryRow(ctx, publicationBindingSelect+` WHERE i.public_id=$1`, clusterPublicID))
	if err != nil {
		return ClusterPublicationBinding{}, err
	}
	return binding, tx.Commit(ctx)
}

func (s *Store) DeleteClusterPublicationBinding(ctx context.Context, clusterPublicID string, actorID, expectedVersion int64, event audit.Event) error {
	return s.deleteKubernetesBinding(ctx, `DELETE FROM cluster_publication_bindings b USING agent_installations i WHERE b.cluster_id=i.id AND i.public_id=$1 AND b.version=$2`, []any{clusterPublicID, expectedVersion}, actorID, clusterPublicID, event)
}

type publicationBindingScanner interface{ Scan(...any) error }

func scanPublicationBinding(row publicationBindingScanner) (ClusterPublicationBinding, error) {
	var item ClusterPublicationBinding
	var kinds []byte
	err := row.Scan(&item.ClusterID, &item.ClusterUID, &item.GatewayNamespace, &item.GatewayName, &item.SectionName, &item.GatewayClassName,
		&item.GatewayClassAccepted, &item.GatewayProgrammed, &item.ListenerReady, &kinds, &item.Health, &item.ReasonCode,
		&item.ObservedAt, &item.ExpiresAt, &item.Version, &item.CreatedAt, &item.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return item, ErrBindingNotFound
	}
	if err == nil {
		err = json.Unmarshal(kinds, &item.SupportedRouteKinds)
		if item.SupportedRouteKinds == nil {
			item.SupportedRouteKinds = []string{}
		}
	}
	return item, err
}

func (s *Store) deleteKubernetesBinding(ctx context.Context, statement string, arguments []any, actorID int64, targetID string, event audit.Event) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	result, err := tx.Exec(ctx, statement, arguments...)
	if err != nil {
		return err
	}
	if result.RowsAffected() != 1 {
		return ErrVersionConflict
	}
	event.ActorUserID = &actorID
	event.TargetPublicID = targetID
	if err = insertAudit(ctx, tx, event); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
