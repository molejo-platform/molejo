package store

import (
	"errors"
	"testing"
	"time"

	"github.com/molejo-platform/molejo/packages/capabilitycontract"
)

func TestCapabilitySnapshotReconciliationIsAtomicAndPartialSafe(t *testing.T) {
	storage, _, _ := newIntegrationFixture(t)
	ctx := t.Context()
	const sessionID = "ags-capability-test"
	var clusterID int64
	var clusterPublicID string
	if err := storage.Pool.QueryRow(ctx, `UPDATE agent_installations SET control_session_id=$1,control_session_sequence=1 WHERE status='Active' RETURNING id,public_id`, sessionID).Scan(&clusterID, &clusterPublicID); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	observation := func(id capabilitycontract.ID) capabilitycontract.Observation {
		return capabilitycontract.Observation{ID: id, ContractVersion: capabilitycontract.ContractVersion, Support: capabilitycontract.SupportSupported, Health: capabilitycontract.HealthHealthy, ProviderKind: "kubernetes", Limitations: []string{}, SampledAt: now}
	}
	if err := storage.ReconcileCapabilityObservations(ctx, clusterPublicID, sessionID, 1, []capabilitycontract.Observation{observation(capabilitycontract.StorageRWO), observation(capabilitycontract.RuntimeEventsCurrent)}, true, now); err != nil {
		t.Fatal(err)
	}
	if _, err := storage.Pool.Exec(ctx, `UPDATE agent_installations SET control_session_sequence=2 WHERE id=$1`, clusterID); err != nil {
		t.Fatal(err)
	}
	partial := observation(capabilitycontract.StorageRWO)
	partial.Health = capabilitycontract.HealthDegraded
	if err := storage.ReconcileCapabilityObservations(ctx, clusterPublicID, sessionID, 2, []capabilitycontract.Observation{partial}, false, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	items, err := storage.CapabilityObservations(ctx, clusterPublicID)
	if err != nil || len(items) != 2 {
		t.Fatalf("partial snapshot items=%d err=%v", len(items), err)
	}
	if _, err = storage.Pool.Exec(ctx, `UPDATE agent_installations SET control_session_sequence=3 WHERE id=$1`, clusterID); err != nil {
		t.Fatal(err)
	}
	if err = storage.ReconcileCapabilityObservations(ctx, clusterPublicID, sessionID, 3, []capabilitycontract.Observation{observation(capabilitycontract.StorageRWO)}, true, now.Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	items, err = storage.CapabilityObservations(ctx, clusterPublicID)
	if err != nil || len(items) != 1 || items[0].ID != capabilitycontract.StorageRWO || !items[0].ExpiresAt.After(items[0].ReceivedAt) {
		t.Fatalf("complete snapshot=%+v err=%v", items, err)
	}
	if err = storage.ReconcileCapabilityObservations(ctx, clusterPublicID, "ags-foreign", 3, nil, true, now); !errors.Is(err, ErrAgentIdentityMismatch) {
		t.Fatalf("foreign session error=%v", err)
	}
}

func TestCapabilitySnapshotRejectsDuplicatesWithoutChangingState(t *testing.T) {
	storage, _, _ := newIntegrationFixture(t)
	ctx := t.Context()
	const sessionID = "ags-duplicate-test"
	var clusterPublicID string
	if err := storage.Pool.QueryRow(ctx, `UPDATE agent_installations SET control_session_id=$1,control_session_sequence=1 WHERE status='Active' RETURNING public_id`, sessionID).Scan(&clusterPublicID); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	item := capabilitycontract.Observation{ID: capabilitycontract.StorageRWO, ContractVersion: capabilitycontract.ContractVersion, Support: capabilitycontract.SupportSupported, Health: capabilitycontract.HealthHealthy, SampledAt: now}
	if err := storage.ReconcileCapabilityObservations(ctx, clusterPublicID, sessionID, 1, []capabilitycontract.Observation{item, item}, true, now); err == nil {
		t.Fatal("duplicate snapshot was accepted")
	}
	items, err := storage.CapabilityObservations(ctx, clusterPublicID)
	if err != nil || len(items) != 0 {
		t.Fatalf("rejected snapshot changed state: %+v err=%v", items, err)
	}
}
