package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/molejo-platform/molejo/services/control-plane-api/internal/audit"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/historicalmetrics"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/observability"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/providerbinding"
)

func (s *Store) HistoricalMetricCluster(ctx context.Context, publicID string) (historicalmetrics.Cluster, error) {
	var cluster historicalmetrics.Cluster
	err := s.Pool.QueryRow(ctx, `SELECT public_id,cluster_uid FROM agent_installations WHERE public_id=$1 AND status='Active'`, publicID).Scan(&cluster.ID, &cluster.UID)
	if errors.Is(err, pgx.ErrNoRows) {
		return cluster, ErrClusterNotFound
	}
	return cluster, err
}

const historicalMetricBindingSelect = `SELECT i.public_id,i.cluster_uid,b.provider_type,b.endpoint,b.health,b.conformant,b.reason_code,
	b.limitations_json,b.observed_at,b.version,b.created_at,b.updated_at
	FROM cluster_historical_metric_bindings b JOIN agent_installations i ON i.id=b.cluster_id`

func (s *Store) HistoricalMetricBinding(ctx context.Context, clusterID string) (historicalmetrics.Binding, error) {
	return scanHistoricalMetricBinding(s.Pool.QueryRow(ctx, historicalMetricBindingSelect+` WHERE i.public_id=$1`, clusterID))
}

func (s *Store) PutHistoricalMetricBinding(ctx context.Context, clusterID, endpoint string, evidence observability.MetricConformanceEvidence, actorID int64, expectedVersion *int64, event audit.Event) (historicalmetrics.Binding, error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return historicalmetrics.Binding{}, err
	}
	defer tx.Rollback(ctx)
	var installationID int64
	if err = tx.QueryRow(ctx, `SELECT id FROM agent_installations WHERE public_id=$1 AND status='Active' FOR UPDATE`, clusterID).Scan(&installationID); errors.Is(err, pgx.ErrNoRows) {
		return historicalmetrics.Binding{}, ErrClusterNotFound
	}
	if err != nil {
		return historicalmetrics.Binding{}, err
	}
	limitations, err := json.Marshal(evidence.Limitations)
	if err != nil {
		return historicalmetrics.Binding{}, err
	}
	health := providerbinding.HealthUnknown
	if evidence.Conformant {
		health = providerbinding.HealthHealthy
	} else if evidence.ReasonCode == "metrics_provider_unreachable" {
		health = providerbinding.HealthUnavailable
	}
	if expectedVersion == nil {
		command, execErr := tx.Exec(ctx, `INSERT INTO cluster_historical_metric_bindings(cluster_id,provider_type,endpoint,health,conformant,reason_code,limitations_json,observed_at,created_by,updated_by)
			VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$9) ON CONFLICT DO NOTHING`, installationID, historicalmetrics.ProviderPrometheusCompatible, endpoint, health, evidence.Conformant, evidence.ReasonCode, limitations, nullTime(evidence.ObservedAt), actorID)
		if execErr != nil {
			return historicalmetrics.Binding{}, execErr
		}
		if command.RowsAffected() != 1 {
			return historicalmetrics.Binding{}, ErrVersionConflict
		}
	} else {
		command, execErr := tx.Exec(ctx, `UPDATE cluster_historical_metric_bindings SET endpoint=$2,health=$3,conformant=$4,reason_code=$5,limitations_json=$6,observed_at=$7,version=version+1,updated_by=$8,updated_at=now() WHERE cluster_id=$1 AND version=$9`, installationID, endpoint, health, evidence.Conformant, evidence.ReasonCode, limitations, nullTime(evidence.ObservedAt), actorID, *expectedVersion)
		if execErr != nil {
			return historicalmetrics.Binding{}, execErr
		}
		if command.RowsAffected() != 1 {
			return historicalmetrics.Binding{}, ErrVersionConflict
		}
	}
	event.TargetPublicID = clusterID
	if err = insertAudit(ctx, tx, event); err != nil {
		return historicalmetrics.Binding{}, err
	}
	binding, err := scanHistoricalMetricBinding(tx.QueryRow(ctx, historicalMetricBindingSelect+` WHERE i.public_id=$1`, clusterID))
	if err != nil {
		return historicalmetrics.Binding{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return historicalmetrics.Binding{}, err
	}
	return binding, nil
}

func (s *Store) DeleteHistoricalMetricBinding(ctx context.Context, clusterID string, actorID, expectedVersion int64, event audit.Event) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	command, err := tx.Exec(ctx, `DELETE FROM cluster_historical_metric_bindings b USING agent_installations i WHERE b.cluster_id=i.id AND i.public_id=$1 AND b.version=$2`, clusterID, expectedVersion)
	if err != nil {
		return err
	}
	if command.RowsAffected() != 1 {
		var clusterExists bool
		if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agent_installations WHERE public_id=$1)`, clusterID).Scan(&clusterExists); err != nil {
			return err
		}
		if !clusterExists {
			return ErrClusterNotFound
		}
		return ErrVersionConflict
	}
	event.ActorUserID = &actorID
	event.TargetPublicID = clusterID
	if err = insertAudit(ctx, tx, event); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

type historicalMetricBindingScanner interface{ Scan(...any) error }

func scanHistoricalMetricBinding(row historicalMetricBindingScanner) (historicalmetrics.Binding, error) {
	var binding historicalmetrics.Binding
	var limitations []byte
	err := row.Scan(&binding.ClusterID, &binding.ClusterUID, &binding.Provider, &binding.Endpoint, &binding.Health, &binding.Conformant, &binding.ReasonCode, &limitations, &binding.ObservedAt, &binding.Version, &binding.CreatedAt, &binding.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return binding, historicalmetrics.ErrNotFound
	}
	if err == nil {
		err = json.Unmarshal(limitations, &binding.Limitations)
		if binding.Limitations == nil {
			binding.Limitations = []string{}
		}
	}
	return binding, err
}

func nullTime(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value
}
