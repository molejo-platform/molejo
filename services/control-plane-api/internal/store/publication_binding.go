package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/molejo-platform/molejo/packages/kubernetesbinding"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/audit"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/domain"
)

const publicationBindingSelect = `SELECT i.public_id,COALESCE(i.cluster_uid,''),b.id,b.version,b.configuration,b.observation,b.observed_at,b.expires_at,b.created_at,b.updated_at FROM cluster_publication_bindings b JOIN agent_installations i ON i.id=b.cluster_id`

func scanPublicationBinding(row pgx.Row) (ClusterPublicationBinding, error) {
	var b ClusterPublicationBinding
	var config, observed []byte
	err := row.Scan(&b.ClusterID, &b.ClusterUID, &b.ID, &b.Revision, &config, &observed, &b.ObservedAt, &b.ExpiresAt, &b.CreatedAt, &b.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return b, ErrBindingNotFound
	}
	if err != nil {
		return b, err
	}
	var target kubernetesbinding.HTTPBinding
	if err = json.Unmarshal(config, &target); err != nil {
		return b, err
	}
	target.ID, target.Revision = b.ID, b.Revision
	b.HTTPBinding = target
	b.Health, b.ReasonCode = publicationBindingHealth(observed, b.Listeners, time.Now())
	return b, b.Validate()
}

func (s *Store) ClusterPublicationBinding(ctx context.Context, cluster string) (ClusterPublicationBinding, error) {
	return scanPublicationBinding(s.Pool.QueryRow(ctx, publicationBindingSelect+` WHERE i.public_id=$1`, cluster))
}

func (s *Store) PutClusterPublicationBinding(ctx context.Context, cluster string, input kubernetesbinding.HTTPBinding, actor int64, expected *int64, event audit.Event) (ClusterPublicationBinding, error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return ClusterPublicationBinding{}, err
	}
	defer tx.Rollback(ctx)
	if err = lockPublication(ctx, tx); err != nil {
		return ClusterPublicationBinding{}, err
	}
	var cid int64
	if err = tx.QueryRow(ctx, `SELECT id FROM agent_installations WHERE public_id=$1 AND status='Active'`, cluster).Scan(&cid); err != nil {
		return ClusterPublicationBinding{}, ErrClusterNotFound
	}
	if expected == nil {
		input.ID, err = domain.NewPublicID("pbd")
		input.Revision = 1
		if err != nil {
			return ClusterPublicationBinding{}, err
		}
	} else {
		old, e := scanPublicationBinding(tx.QueryRow(ctx, publicationBindingSelect+` WHERE i.public_id=$1`, cluster))
		if e != nil {
			return old, e
		}
		if old.Revision != *expected {
			return old, ErrVersionConflict
		}
		input.ID = old.ID
		input.Revision = old.Revision + 1
		if err = input.Validate(); err != nil {
			return old, err
		}
		// Dependents are protected by target compatibility, not the global revision.
		rows, e := tx.Query(ctx, `SELECT d.publication_snapshot FROM deployments d WHERE EXISTS(SELECT 1 FROM publication_execution_claims ec WHERE ec.deployment_id=d.id AND ec.binding_id=$1)`, old.ID)
		if e != nil {
			return old, e
		}
		for rows.Next() {
			var raw []byte
			if e = rows.Scan(&raw); e != nil {
				rows.Close()
				return old, e
			}
			var snap PublicationSnapshot
			if e = json.Unmarshal(raw, &snap); e != nil {
				rows.Close()
				return old, e
			}
			for _, a := range snap.Addresses {
				if a.Destination.BindingID == old.ID && !input.SupportsSnapshot(a.Destination, a.Hostname) {
					rows.Close()
					return old, ErrPublicationDependency
				}
			}
		}
		if e = rows.Err(); e != nil {
			rows.Close()
			return old, e
		}
		rows.Close()

		rows, e = tx.Query(ctx, `SELECT ae.configuration_json FROM app_environments ae WHERE EXISTS(SELECT 1 FROM publication_claims c WHERE c.app_environment_id=ae.id AND c.binding_id=$1 AND c.desired_configuration_version IS NOT NULL)`, old.ID)
		if e != nil {
			return old, e
		}
		for rows.Next() {
			var raw []byte
			if e = rows.Scan(&raw); e != nil {
				rows.Close()
				return old, e
			}
			var config domain.RuntimeConfig
			if e = json.Unmarshal(raw, &config); e != nil {
				rows.Close()
				return old, e
			}
			for _, endpoint := range config.PublicEndpoints {
				for _, a := range endpoint.Addresses {
					if a.BindingID != old.ID {
						continue
					}
					before, e := old.Resolve(a.Hostname, a.ListenerName)
					if e != nil || !input.SupportsSnapshot(before, a.Hostname) {
						rows.Close()
						return old, ErrPublicationDependency
					}
				}
			}
		}
		if e = rows.Err(); e != nil {
			rows.Close()
			return old, e
		}
		rows.Close()
	}
	if err = input.Validate(); err != nil {
		return ClusterPublicationBinding{}, err
	}
	// Identity and revision live in columns; only the concrete atomic value is JSON.
	raw, err := json.Marshal(struct {
		SchemaVersion    string                           `json:"schemaVersion"`
		GatewayNamespace string                           `json:"gatewayNamespace"`
		GatewayName      string                           `json:"gatewayName"`
		Listeners        []kubernetesbinding.HTTPListener `json:"listeners"`
	}{input.SchemaVersion, input.GatewayNamespace, input.GatewayName, input.Listeners})
	if err != nil {
		return ClusterPublicationBinding{}, err
	}
	if expected == nil {
		_, err = tx.Exec(ctx, `INSERT INTO cluster_publication_bindings(id,cluster_id,configuration,created_by,updated_by) VALUES($1,$2,$3,$4,$4)`, input.ID, cid, raw, actor)
	} else {
		_, err = tx.Exec(ctx, `UPDATE cluster_publication_bindings SET configuration=$2,version=$3,updated_by=$4,updated_at=now(),observation='{}',observed_at=NULL,expires_at=NULL WHERE id=$1`, input.ID, raw, input.Revision, actor)
	}
	if err != nil {
		return ClusterPublicationBinding{}, translateDBError(err)
	}
	event.ActorUserID = &actor
	event.TargetPublicID = input.ID
	if err = insertAudit(ctx, tx, event); err != nil {
		return ClusterPublicationBinding{}, err
	}
	b, err := scanPublicationBinding(tx.QueryRow(ctx, publicationBindingSelect+` WHERE i.public_id=$1`, cluster))
	if err != nil {
		return b, err
	}
	return b, tx.Commit(ctx)
}

func (s *Store) DeleteClusterPublicationBinding(ctx context.Context, cluster string, actor, version int64, event audit.Event) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err = lockPublication(ctx, tx); err != nil {
		return err
	}
	b, err := scanPublicationBinding(tx.QueryRow(ctx, publicationBindingSelect+` WHERE i.public_id=$1`, cluster))
	if err != nil {
		return err
	}
	if b.Revision != version {
		return ErrVersionConflict
	}
	var used bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM publication_grants WHERE binding_id=$1)`, b.ID).Scan(&used); err != nil {
		return err
	}
	if used {
		return ErrPublicationDependency
	}
	_, err = tx.Exec(ctx, `DELETE FROM cluster_publication_bindings WHERE id=$1`, b.ID)
	if err != nil {
		return err
	}
	event.ActorUserID = &actor
	event.TargetPublicID = b.ID
	if err = insertAudit(ctx, tx, event); err != nil {
		return err
	}
	return tx.Commit(ctx)
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

func publicationBindingHealth(raw []byte, listeners []kubernetesbinding.HTTPListener, now time.Time) (kubernetesbinding.Health, string) {
	var facts map[string]struct {
		Health    kubernetesbinding.Health `json:"health"`
		Reason    string                   `json:"reasonCode"`
		SampledAt time.Time                `json:"sampledAt"`
	}
	if json.Unmarshal(raw, &facts) != nil {
		return kubernetesbinding.HealthUnknown, "binding_observation_invalid"
	}
	health, reason := kubernetesbinding.HealthHealthy, ""
	for _, listener := range listeners {
		f, ok := facts[listener.Name]
		if !ok {
			return kubernetesbinding.HealthUnknown, "binding_observation_pending"
		}
		if !f.SampledAt.Add(kubernetesbinding.ObservationTTL).After(now) {
			return kubernetesbinding.HealthUnknown, "binding_observation_stale"
		}
		if f.Health != kubernetesbinding.HealthHealthy {
			health, reason = f.Health, f.Reason
		}
	}
	return health, reason
}
