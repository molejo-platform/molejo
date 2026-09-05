package store

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	"github.com/molejo-platform/molejo/services/control-plane-api/internal/audit"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/domain"
)

const maximumRuntimeObservations = 1000

var runtimeObjectNamePattern = regexp.MustCompile(`^[a-z0-9](?:[-a-z0-9]*[a-z0-9])?$`)

// RuntimeObservation contains only non-sensitive state reported by a cluster
// Agent. Desired state remains authoritative in the control plane.
type RuntimeObservation struct {
	Kind               string
	Namespace          string
	Name               string
	State              string
	Message            string
	Generation         int64
	ObservedGeneration int64
	ObservedRelease    string
	ObservedSizeGiB    int64
}

type observedRuntimeTarget struct {
	workspaceID         int64
	environmentID       int64
	environmentPublicID string
	deploymentID        int64
	actorID             int64
	desiredVersion      int64
	key                 string
	image               string
}

// ReconcileAgentObservations records the observed state for one durable
// cluster. A complete snapshot also repairs missing or release-drifted
// AppDeployments by enqueueing the existing idempotent ApplyDeployment flow.
func (s *Store) ReconcileAgentObservations(ctx context.Context, clusterPublicID string, observations []RuntimeObservation, complete bool) error {
	normalized, err := normalizeRuntimeObservations(observations)
	if err != nil {
		return err
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var clusterID int64
	if err = tx.QueryRow(ctx, `SELECT id FROM agent_installations WHERE public_id=$1 AND status='Active' FOR UPDATE`, clusterPublicID).Scan(&clusterID); errors.Is(err, pgx.ErrNoRows) {
		return ErrAgentIdentityMismatch
	} else if err != nil {
		return err
	}

	seenDeployments := make(map[string]RuntimeObservation, len(normalized))
	for _, observation := range normalized {
		switch observation.Kind {
		case "AppDeployment":
			seenDeployments[runtimeObservationKey(observation.Namespace, observation.Name)] = observation
			_, err = tx.Exec(ctx, `UPDATE app_environments ae
				SET runtime_observed_generation=$1,runtime_observed_at=now(),
				    last_state=CASE WHEN EXISTS(SELECT 1 FROM operations o WHERE o.app_environment_id=ae.id AND o.status IN ('Pending','Running')) THEN ae.last_state ELSE $2 END,
				    last_message=CASE WHEN EXISTS(SELECT 1 FROM operations o WHERE o.app_environment_id=ae.id AND o.status IN ('Pending','Running')) THEN ae.last_message ELSE $3 END,
				    updated_at=now()
				FROM workspaces w
				WHERE ae.workspace_id=w.id AND ae.cluster_id=$4 AND w.namespace_name=$5 AND ae.runtime_name=$6 AND ae.archived_at IS NULL`,
				observation.ObservedGeneration, observation.State, observation.Message, clusterID, observation.Namespace, observation.Name)
		case "AppVolume":
			_, err = tx.Exec(ctx, `UPDATE app_volumes av
				SET observed_state=$1,message=$2,observed_size_gib=LEAST(requested_size_gib,$3),updated_at=now()
				FROM app_environments ae JOIN workspaces w ON w.id=ae.workspace_id
				WHERE av.app_environment_id=ae.id AND ae.cluster_id=$4 AND w.namespace_name=$5 AND av.public_id=$6`,
				observation.State, observation.Message, observation.ObservedSizeGiB, clusterID, observation.Namespace, observation.Name)
		}
		if err != nil {
			return err
		}
	}

	if complete {
		targets, targetErr := driftCandidates(ctx, tx, clusterID)
		if targetErr != nil {
			return targetErr
		}
		for _, target := range targets {
			observation, found := seenDeployments[target.key]
			if found && observation.ObservedRelease == target.image {
				continue
			}
			if err = enqueueRuntimeReconciliation(ctx, tx, target, found); err != nil {
				return err
			}
		}
	}
	return tx.Commit(ctx)
}

func normalizeRuntimeObservations(items []RuntimeObservation) ([]RuntimeObservation, error) {
	if len(items) > maximumRuntimeObservations {
		return nil, fmt.Errorf("runtime observation snapshot exceeds %d objects", maximumRuntimeObservations)
	}
	seen := make(map[string]struct{}, len(items))
	result := make([]RuntimeObservation, 0, len(items))
	for _, item := range items {
		if item.Kind != "AppDeployment" && item.Kind != "AppVolume" {
			return nil, fmt.Errorf("unsupported runtime observation kind %q", item.Kind)
		}
		if !validRuntimeObjectName(item.Namespace) || !validRuntimeObjectName(item.Name) {
			return nil, errors.New("runtime observation contains an invalid object name")
		}
		if item.Generation < 0 || item.ObservedGeneration < 0 || item.ObservedSizeGiB < 0 {
			return nil, errors.New("runtime observation contains a negative value")
		}
		if !validRuntimeState(item.Kind, item.State) {
			return nil, fmt.Errorf("invalid %s state %q", item.Kind, item.State)
		}
		key := item.Kind + ":" + runtimeObservationKey(item.Namespace, item.Name)
		if _, duplicate := seen[key]; duplicate {
			return nil, errors.New("runtime observation snapshot contains a duplicate object")
		}
		seen[key] = struct{}{}
		item.Message = sanitizeRuntimeMessage(item.Message)
		if len(item.ObservedRelease) > 512 || strings.IndexFunc(item.ObservedRelease, unicode.IsControl) >= 0 {
			return nil, errors.New("runtime observation contains an invalid release")
		}
		result = append(result, item)
	}
	return result, nil
}

func validRuntimeObjectName(value string) bool {
	return len(value) <= 63 && runtimeObjectNamePattern.MatchString(value)
}

func validRuntimeState(kind, state string) bool {
	if kind == "AppDeployment" {
		return state == domain.StatePending || state == domain.StateProgressing || state == domain.StateReady || state == domain.StateDegraded || state == domain.StateUnknown
	}
	return state == domain.VolumeStatePending || state == domain.VolumeStateProvisioning || state == domain.VolumeStateReady || state == domain.VolumeStateExpanding || state == domain.VolumeStateRetained || state == domain.VolumeStateDegraded
}

func sanitizeRuntimeMessage(value string) string {
	value = strings.Map(func(character rune) rune {
		if unicode.IsControl(character) {
			return -1
		}
		return character
	}, value)
	if utf8.RuneCountInString(value) <= 500 {
		return value
	}
	runes := []rune(value)
	return string(runes[:500])
}

func runtimeObservationKey(namespace, name string) string { return namespace + "/" + name }

func driftCandidates(ctx context.Context, tx pgx.Tx, clusterID int64) ([]observedRuntimeTarget, error) {
	rows, err := tx.Query(ctx, `SELECT ae.workspace_id,ae.id,ae.public_id,d.id,d.requested_by_user_id,ae.version,w.namespace_name,ae.runtime_name,r.image
		FROM app_environments ae
		JOIN workspaces w ON w.id=ae.workspace_id
		JOIN deployments d ON d.id=ae.desired_deployment_id
		JOIN releases r ON r.id=d.release_id
		WHERE ae.cluster_id=$1 AND ae.archived_at IS NULL AND ae.deletion_requested_at IS NULL
		  AND NOT EXISTS(SELECT 1 FROM operations o WHERE o.app_environment_id=ae.id AND o.status IN ('Pending','Running'))
		ORDER BY ae.id FOR UPDATE OF ae`, clusterID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	targets := make([]observedRuntimeTarget, 0)
	for rows.Next() {
		var target observedRuntimeTarget
		var namespace, name string
		if err = rows.Scan(&target.workspaceID, &target.environmentID, &target.environmentPublicID, &target.deploymentID, &target.actorID, &target.desiredVersion, &namespace, &name, &target.image); err != nil {
			return nil, err
		}
		target.key = runtimeObservationKey(namespace, name)
		targets = append(targets, target)
	}
	return targets, rows.Err()
}

func enqueueRuntimeReconciliation(ctx context.Context, tx pgx.Tx, target observedRuntimeTarget, existed bool) error {
	var generation int64
	message := "runtime resource is missing; reconciliation queued"
	if existed {
		message = "runtime release drift detected; reconciliation queued"
	}
	if err := tx.QueryRow(ctx, `UPDATE app_environments SET reconciliation_generation=reconciliation_generation+1,last_state='Progressing',last_message=$2,updated_at=now()
		WHERE id=$1 RETURNING reconciliation_generation`, target.environmentID, message).Scan(&generation); err != nil {
		return err
	}
	operationPublicID, err := domain.NewPublicID("op")
	if err != nil {
		return err
	}
	key := []byte(fmt.Sprintf("runtime-reconcile:%d:%d:%d", target.environmentID, target.deploymentID, generation))
	hash := domain.SHA256(key)
	_, err = tx.Exec(ctx, `INSERT INTO operations(public_id,workspace_id,app_environment_id,deployment_id,requested_by_user_id,kind,status,idempotency_hash,payload_hash,desired_version,agent_installation_id)
		SELECT $1,$2,$3,$4,$5,'ApplyDeployment','Pending',$6,$6,$7,cluster_id FROM app_environments WHERE id=$3`,
		operationPublicID, target.workspaceID, target.environmentID, target.deploymentID, target.actorID, hash, target.desiredVersion)
	if err != nil {
		return err
	}
	auditID, err := domain.NewPublicID("aud")
	if err != nil {
		return err
	}
	workspaceID := target.workspaceID
	return insertAudit(ctx, tx, audit.Event{
		PublicID: auditID, WorkspaceID: &workspaceID, Action: "runtime.reconcile.queue", TargetType: "AppEnvironment",
		TargetPublicID: target.environmentPublicID, Outcome: audit.Succeeded, Reason: message,
		Metadata: map[string]any{"operationId": operationPublicID, "reconciliationGeneration": generation},
	})
}
