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

	"github.com/molejo-platform/molejo/packages/capabilitycontract"
)

const capabilityObservationTTL = 90 * time.Second

func (s *Store) ReconcileCapabilityObservations(ctx context.Context, clusterPublicID, sessionID string, sequence uint64, observations []capabilitycontract.Observation, complete bool, receivedAt time.Time) error {
	if strings.TrimSpace(sessionID) == "" || sequence == 0 || sequence > math.MaxInt64 || len(observations) > capabilitycontract.MaxObservations {
		return ErrAgentIdentityMismatch
	}
	normalized, err := normalizeCapabilityObservations(observations, receivedAt)
	if err != nil {
		return err
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var clusterID int64
	var activeSessionID *string
	var activeSequence int64
	err = tx.QueryRow(ctx, `SELECT id,control_session_id,control_session_sequence FROM agent_installations WHERE public_id=$1 AND status='Active' FOR UPDATE`, clusterPublicID).Scan(&clusterID, &activeSessionID, &activeSequence)
	if errors.Is(err, pgx.ErrNoRows) || activeSessionID == nil || *activeSessionID != sessionID || activeSequence != int64(sequence) {
		return ErrAgentIdentityMismatch
	}
	if err != nil {
		return err
	}
	if complete {
		if _, err = tx.Exec(ctx, `DELETE FROM cluster_capability_observations WHERE cluster_id=$1`, clusterID); err != nil {
			return err
		}
	}
	for _, observation := range normalized {
		limitations, marshalErr := json.Marshal(observation.Limitations)
		if marshalErr != nil {
			return marshalErr
		}
		_, err = tx.Exec(ctx, `INSERT INTO cluster_capability_observations(
			cluster_id,capability_id,contract_version,support,health,provider_kind,reason_code,sanitized_message,limitations_json,
			sampled_at,received_at,expires_at,observed_session_id,snapshot_sequence)
			VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9::text::jsonb,$10,$11,$12,$13,$14)
			ON CONFLICT(cluster_id,capability_id,contract_version) DO UPDATE SET
			support=EXCLUDED.support,health=EXCLUDED.health,provider_kind=EXCLUDED.provider_kind,reason_code=EXCLUDED.reason_code,
			sanitized_message=EXCLUDED.sanitized_message,limitations_json=EXCLUDED.limitations_json,sampled_at=EXCLUDED.sampled_at,
			received_at=EXCLUDED.received_at,expires_at=EXCLUDED.expires_at,observed_session_id=EXCLUDED.observed_session_id,
			snapshot_sequence=EXCLUDED.snapshot_sequence`, clusterID, observation.ID, observation.ContractVersion, observation.Support,
			observation.Health, observation.ProviderKind, observation.ReasonCode, observation.Message, string(limitations), observation.SampledAt,
			receivedAt, receivedAt.Add(capabilityObservationTTL), sessionID, int64(sequence))
		if err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (s *Store) CapabilityObservations(ctx context.Context, clusterPublicID string) ([]capabilitycontract.Observation, error) {
	rows, err := s.Pool.Query(ctx, `SELECT o.capability_id,o.contract_version,o.support,o.health,o.provider_kind,o.reason_code,
		o.sanitized_message,o.limitations_json,o.sampled_at,o.received_at,o.expires_at
		FROM cluster_capability_observations o JOIN agent_installations i ON i.id=o.cluster_id
		WHERE i.public_id=$1 ORDER BY o.capability_id,o.contract_version`, clusterPublicID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]capabilitycontract.Observation, 0)
	for rows.Next() {
		var item capabilitycontract.Observation
		var limitations []byte
		if err = rows.Scan(&item.ID, &item.ContractVersion, &item.Support, &item.Health, &item.ProviderKind, &item.ReasonCode, &item.Message, &limitations, &item.SampledAt, &item.ReceivedAt, &item.ExpiresAt); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(limitations, &item.Limitations); err != nil {
			return nil, err
		}
		if item.Limitations == nil {
			item.Limitations = []string{}
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func normalizeCapabilityObservations(items []capabilitycontract.Observation, now time.Time) ([]capabilitycontract.Observation, error) {
	if len(items) > capabilitycontract.MaxObservations {
		return nil, fmt.Errorf("capability snapshot exceeds %d observations", capabilitycontract.MaxObservations)
	}
	seen := make(map[string]struct{}, len(items))
	result := make([]capabilitycontract.Observation, 0, len(items))
	for _, item := range items {
		if err := capabilitycontract.ValidateObservation(item, now); err != nil {
			return nil, err
		}
		key := string(item.ID) + "\x00" + item.ContractVersion
		if _, duplicate := seen[key]; duplicate {
			return nil, fmt.Errorf("capability snapshot contains duplicate %q", item.ID)
		}
		seen[key] = struct{}{}
		item.Limitations = append([]string{}, item.Limitations...)
		result = append(result, item)
	}
	return result, nil
}
