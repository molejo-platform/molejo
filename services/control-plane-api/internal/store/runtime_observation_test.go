package store

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/molejo-platform/molejo/services/control-plane-api/internal/domain"
)

func TestNormalizeRuntimeObservationsRejectsDuplicatesAndSanitizesMessages(t *testing.T) {
	items, err := normalizeRuntimeObservations([]RuntimeObservation{{
		Kind: "AppDeployment", Namespace: "workspace-one", Name: "ap-test", State: "Ready", Message: "ready\nnow",
	}})
	if err != nil || len(items) != 1 || items[0].Message != "readynow" {
		t.Fatalf("normalized=%+v err=%v", items, err)
	}
	_, err = normalizeRuntimeObservations([]RuntimeObservation{
		{Kind: "AppVolume", Namespace: "workspace-one", Name: "vol-test", State: "Ready"},
		{Kind: "AppVolume", Namespace: "workspace-one", Name: "vol-test", State: "Ready"},
	})
	if err == nil {
		t.Fatal("expected duplicate runtime observation to be rejected")
	}
}

func TestCompleteRuntimeSnapshotQueuesOneDurableRepairForMissingDeployment(t *testing.T) {
	storage, workspaceID, actorID := newIntegrationFixture(t)
	ctx := context.Background()
	project, app, environment := createHierarchy(t, storage, workspaceID)
	target, err := storage.CreateAppEnvironment(ctx, workspaceID, actorID, newID(t, "aev"), project.PublicID, app.PublicID, environment.PublicID, "main", integrationConfiguration("runtime-repair"))
	if err != nil {
		t.Fatal(err)
	}
	releaseID, image := createRelease(t, storage, workspaceID, actorID, project, app, target)
	deployment, _, _, err := storage.CreateDeployment(ctx, workspaceID, actorID, target.PublicID, newID(t, "dpl"), releaseID, target.ConfigurationVersion, target.Version, "", domain.SHA256([]byte("initial-deploy")), domain.SHA256([]byte("initial-payload")), deploymentAudit(t))
	if err != nil {
		t.Fatal(err)
	}
	operation, _, _, ok, err := storage.ClaimNextForAgent(ctx, "agent:"+target.ClusterPublicID, target.ClusterPublicID, time.Minute)
	if err != nil || !ok {
		t.Fatalf("claim initial deployment ok=%v err=%v", ok, err)
	}
	if err = storage.CompleteDeployment(ctx, operation, "ready", image, strings.Repeat("a", 64)); err != nil {
		t.Fatal(err)
	}
	const sessionID = "ags-runtime-observation"
	if _, err = storage.Pool.Exec(ctx, `UPDATE agent_installations SET control_session_id=$2,control_session_sequence=1 WHERE public_id=$1`, target.ClusterPublicID, sessionID); err != nil {
		t.Fatal(err)
	}

	if err = storage.ReconcileAgentObservations(ctx, target.ClusterPublicID, sessionID, 1, nil, true); err != nil {
		t.Fatal(err)
	}
	if err = storage.ReconcileAgentObservations(ctx, target.ClusterPublicID, sessionID, 1, nil, true); err != nil {
		t.Fatal(err)
	}
	var pending int
	if err = storage.Pool.QueryRow(ctx, `SELECT count(*) FROM operations WHERE app_environment_id=$1 AND deployment_id=$2 AND kind='ApplyDeployment' AND status='Pending'`, target.ID, deployment.ID).Scan(&pending); err != nil {
		t.Fatal(err)
	}
	if pending != 1 {
		t.Fatalf("pending runtime repairs=%d, want 1", pending)
	}
	var auditEvents int
	if err = storage.Pool.QueryRow(ctx, `SELECT count(*) FROM audit_events WHERE workspace_id=$1 AND action='runtime.reconcile.queue' AND target_public_id=$2`, workspaceID, target.PublicID).Scan(&auditEvents); err != nil {
		t.Fatal(err)
	}
	if auditEvents != 1 {
		t.Fatalf("runtime repair audit events=%d, want 1", auditEvents)
	}
	if _, err = storage.Pool.Exec(ctx, `UPDATE agent_installations SET control_session_id='ags-replacement',control_session_sequence=1 WHERE public_id=$1`, target.ClusterPublicID); err != nil {
		t.Fatal(err)
	}
	if err = storage.ReconcileAgentObservations(ctx, target.ClusterPublicID, sessionID, 1, nil, true); !errors.Is(err, ErrAgentIdentityMismatch) {
		t.Fatalf("replaced session observation error=%v", err)
	}
}
