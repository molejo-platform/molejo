package store

import (
	"context"
	"errors"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fruto-platform/fruto/services/control-plane-api/internal/domain"
)

func TestCompleteRejectsAStaleWorkerAfterLeaseHandoff(t *testing.T) {
	ctx := context.Background()
	s, workspaceID, actorID := newIntegrationFixture(t)

	publicID, err := domain.NewPublicID("dep")
	if err != nil {
		t.Fatal(err)
	}
	intent := domain.Intent{
		Name:     "fencing-test",
		Image:    "ghcr.io/fruto-platform/testkit@sha256:" + strings.Repeat("a", 64),
		Replicas: 1,
		Port:     8080,
		Resources: domain.Resources{
			Requests: domain.ResourceValues{CPUMillis: 50, MemoryMiB: 64},
			Limits:   domain.ResourceValues{CPUMillis: 250, MemoryMiB: 128},
		},
		Probes: domain.Probes{
			Liveness:  domain.Probe{Path: "/healthz"},
			Readiness: domain.Probe{Path: "/readyz"},
		},
		Exposure: domain.ExposurePrivate,
	}
	_, _, _, err = s.CreateDeployment(ctx, workspaceID, actorID, publicID, intent, domain.SHA256([]byte("create-idem")), domain.SHA256([]byte("create-payload")))
	if err != nil {
		t.Fatal(err)
	}

	first, _, ok, err := s.ClaimNext(ctx, "worker-a", time.Millisecond)
	if err != nil || !ok {
		t.Fatalf("first worker claim: ok=%v err=%v", ok, err)
	}
	if _, err := s.Pool.Exec(ctx, `UPDATE operations SET lease_until=now()-interval '1 second' WHERE id=$1`, first.ID); err != nil {
		t.Fatal(err)
	}
	second, _, ok, err := s.ClaimNext(ctx, "worker-b", time.Second)
	if err != nil || !ok {
		t.Fatalf("lease handoff claim: ok=%v err=%v", ok, err)
	}
	if first.ID != second.ID {
		t.Fatalf("expected the same operation to be reclaimed, got %d and %d", first.ID, second.ID)
	}

	if err := s.Complete(ctx, first, domain.Ready, "stale worker", first.DesiredVersion, "release-a", false); !errors.Is(err, ErrLeaseLost) {
		t.Fatalf("expected stale worker completion to fail with ErrLeaseLost, got %v", err)
	}

	var status string
	if err := s.Pool.QueryRow(ctx, `SELECT status FROM operations WHERE id=$1`, first.ID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status == domain.OperationSucceeded {
		t.Fatalf("stale worker completed an operation after fencing handoff: status=%s", status)
	}
}

func TestClaimNextReturnsThePersistedOperationSnapshot(t *testing.T) {
	ctx := context.Background()
	s, workspaceID, actorID := newIntegrationFixture(t)

	publicID, err := domain.NewPublicID("dep")
	if err != nil {
		t.Fatal(err)
	}
	intent := integrationIntent("snapshot-original")
	deployment, _, _, err := s.CreateDeployment(ctx, workspaceID, actorID, publicID, intent, domain.SHA256([]byte("snapshot-create-idem")), domain.SHA256([]byte("snapshot-create-payload")))
	if err != nil {
		t.Fatal(err)
	}

	op, _, ok, err := s.ClaimNext(ctx, "snapshot-worker", time.Second)
	if err != nil || !ok {
		t.Fatalf("claim operation: ok=%v err=%v", ok, err)
	}
	newIntent := integrationIntent("snapshot-current")
	newJSON, err := domain.CanonicalJSON(newIntent)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Pool.Exec(ctx, `UPDATE deployments SET intent_json=$1 WHERE id=$2`, newJSON, deployment.ID); err != nil {
		t.Fatal(err)
	}

	if op.Intent.Name != intent.Name {
		t.Fatalf("operation snapshot name=%q, want %q", op.Intent.Name, intent.Name)
	}
}

func TestOperationExposesThePublicDeploymentID(t *testing.T) {
	ctx := context.Background()
	s, workspaceID, actorID := newIntegrationFixture(t)
	publicID, err := domain.NewPublicID("dep")
	if err != nil {
		t.Fatal(err)
	}
	deployment, operation, _, err := s.CreateDeployment(ctx, workspaceID, actorID, publicID, integrationIntent("operation-public-id"), domain.SHA256([]byte("operation-idem")), domain.SHA256([]byte("operation-payload")))
	if err != nil {
		t.Fatal(err)
	}

	got, err := s.GetOperation(ctx, workspaceID, operation.PublicID)
	if err != nil {
		t.Fatal(err)
	}
	if got.DeploymentPublicID != deployment.PublicID {
		t.Fatalf("operation deploymentId=%q, want %q", got.DeploymentPublicID, deployment.PublicID)
	}
}

func TestUpdateRejectsChangingThePublicDeploymentName(t *testing.T) {
	ctx := context.Background()
	s, workspaceID, actorID := newIntegrationFixture(t)
	publicID, err := domain.NewPublicID("dep")
	if err != nil {
		t.Fatal(err)
	}
	intent := integrationIntent("immutable-name")
	deployment, _, _, err := s.CreateDeployment(ctx, workspaceID, actorID, publicID, intent, domain.SHA256([]byte("rename-create-idem")), domain.SHA256([]byte("rename-create-payload")))
	if err != nil {
		t.Fatal(err)
	}
	intent.Name = "renamed"
	if _, _, err = s.UpdateDeployment(ctx, workspaceID, actorID, deployment.ID, intent, deployment.DesiredVersion, domain.SHA256([]byte("rename-update-idem")), domain.SHA256([]byte("rename-update-payload"))); !errors.Is(err, ErrImmutableName) {
		t.Fatalf("expected ErrImmutableName, got %v", err)
	}
}

func TestDeploymentLookupIsScopedToTheWorkspace(t *testing.T) {
	ctx := context.Background()
	s, workspaceID, actorID := newIntegrationFixture(t)
	otherPublicID, err := domain.NewPublicID("ws")
	if err != nil {
		t.Fatal(err)
	}
	var otherWorkspaceID int64
	if err := s.Pool.QueryRow(ctx, `INSERT INTO workspaces(public_id,name,namespace_name) VALUES ($1,$2,$3) RETURNING id`, otherPublicID, "Other Workspace", "other-"+strings.TrimPrefix(otherPublicID, "ws-")).Scan(&otherWorkspaceID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = s.Pool.Exec(ctx, `DELETE FROM operations WHERE workspace_id=$1`, otherWorkspaceID)
		_, _ = s.Pool.Exec(ctx, `DELETE FROM deployments WHERE workspace_id=$1`, otherWorkspaceID)
		_, _ = s.Pool.Exec(ctx, `DELETE FROM workspaces WHERE id=$1`, otherWorkspaceID)
	})

	publicID, err := domain.NewPublicID("dep")
	if err != nil {
		t.Fatal(err)
	}
	deployment, _, _, err := s.CreateDeployment(ctx, otherWorkspaceID, actorID, publicID, integrationIntent("other-workspace"), domain.SHA256([]byte("other-workspace-idem")), domain.SHA256([]byte("other-workspace-payload")))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.FindDeployment(ctx, workspaceID, deployment.PublicID); !errors.Is(err, ErrNoRows) {
		t.Fatalf("expected cross-workspace lookup to be hidden, got %v", err)
	}
}

func TestConcurrentCreateWithTheSameIdempotencyKeyReusesTheCommittedOperation(t *testing.T) {
	ctx := context.Background()
	s, workspaceID, actorID := newIntegrationFixture(t)
	intent := integrationIntent("concurrent-create")
	idempotencyHash := domain.SHA256([]byte("concurrent-create-idempotency"))
	payloadHash := domain.SHA256([]byte("concurrent-create-payload"))

	type result struct {
		deployment domain.Deployment
		operation  domain.Operation
		reused     bool
		err        error
	}
	results := make(chan result, 2)
	var group sync.WaitGroup
	for i := 0; i < 2; i++ {
		group.Add(1)
		go func() {
			defer group.Done()
			publicID, err := domain.NewPublicID("dep")
			if err != nil {
				results <- result{err: err}
				return
			}
			deployment, operation, reused, err := s.CreateDeployment(ctx, workspaceID, actorID, publicID, intent, idempotencyHash, payloadHash)
			results <- result{deployment: deployment, operation: operation, reused: reused, err: err}
		}()
	}
	group.Wait()
	close(results)

	var successful []result
	for item := range results {
		if item.err != nil {
			t.Fatalf("concurrent create failed: %v", item.err)
		}
		successful = append(successful, item)
	}
	if len(successful) != 2 || successful[0].operation.PublicID != successful[1].operation.PublicID {
		t.Fatalf("expected both requests to reuse one operation, got %+v", successful)
	}
	var deployments, operations int
	if err := s.Pool.QueryRow(ctx, `SELECT count(*) FROM deployments WHERE workspace_id=$1 AND name=$2`, workspaceID, intent.Name).Scan(&deployments); err != nil {
		t.Fatal(err)
	}
	if err := s.Pool.QueryRow(ctx, `SELECT count(*) FROM operations WHERE workspace_id=$1 AND idempotency_hash=$2`, workspaceID, idempotencyHash).Scan(&operations); err != nil {
		t.Fatal(err)
	}
	if deployments != 1 || operations != 1 {
		t.Fatalf("expected one deployment and operation, got deployments=%d operations=%d", deployments, operations)
	}
}

func integrationIntent(name string) domain.Intent {
	return domain.Intent{
		Name:     name,
		Image:    "ghcr.io/fruto-platform/testkit@sha256:" + strings.Repeat("a", 64),
		Replicas: 1,
		Port:     8080,
		Resources: domain.Resources{
			Requests: domain.ResourceValues{CPUMillis: 50, MemoryMiB: 64},
			Limits:   domain.ResourceValues{CPUMillis: 250, MemoryMiB: 128},
		},
		Probes: domain.Probes{
			Liveness:  domain.Probe{Path: "/healthz"},
			Readiness: domain.Probe{Path: "/readyz"},
		},
		Exposure: domain.ExposurePrivate,
	}
}

func newIntegrationFixture(t *testing.T) (*Store, int64, int64) {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv("FRUTO_TEST_DATABASE_URL"))
	if dsn == "" {
		t.Skip("set FRUTO_TEST_DATABASE_URL to run PostgreSQL integration tests")
	}

	ctx := context.Background()
	s, err := New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	if err := s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	actorKey := "integration-owner-" + suffix
	workspace := domain.Workspace{
		PublicID:  "ws-integration-" + suffix,
		Name:      "Control Plane Integration",
		Namespace: "integration-" + suffix,
	}
	if err := s.Bootstrap(ctx, workspace, map[string]struct{ Role, PasswordHash string }{
		actorKey: {Role: "owner", PasswordHash: "integration-only"},
	}); err != nil {
		t.Fatal(err)
	}

	var workspaceID, actorID int64
	if err := s.Pool.QueryRow(ctx, `SELECT id FROM workspaces WHERE public_id=$1`, workspace.PublicID).Scan(&workspaceID); err != nil {
		t.Fatal(err)
	}
	if err := s.Pool.QueryRow(ctx, `SELECT id FROM actors WHERE actor_key=$1`, actorKey).Scan(&actorID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanupCtx := context.Background()
		if _, err := s.Pool.Exec(cleanupCtx, `DELETE FROM operations WHERE workspace_id=$1`, workspaceID); err != nil {
			t.Errorf("delete integration operations: %v", err)
		}
		if _, err := s.Pool.Exec(cleanupCtx, `DELETE FROM deployments WHERE workspace_id=$1`, workspaceID); err != nil {
			t.Errorf("delete integration deployments: %v", err)
		}
		if _, err := s.Pool.Exec(cleanupCtx, `DELETE FROM workspace_actors WHERE workspace_id=$1`, workspaceID); err != nil {
			t.Errorf("delete integration memberships: %v", err)
		}
		if _, err := s.Pool.Exec(cleanupCtx, `DELETE FROM workspaces WHERE id=$1`, workspaceID); err != nil {
			t.Errorf("delete integration workspace: %v", err)
		}
		if _, err := s.Pool.Exec(cleanupCtx, `DELETE FROM actors WHERE id=$1`, actorID); err != nil {
			t.Errorf("delete integration actor: %v", err)
		}
	})
	return s, workspaceID, actorID
}
