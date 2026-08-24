package api

import (
	"context"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fruto-platform/fruto/services/control-plane-api/internal/domain"
	controlruntime "github.com/fruto-platform/fruto/services/control-plane-api/internal/runtime"
	"github.com/fruto-platform/fruto/services/control-plane-api/internal/store"
)

func TestWorkerDoesNotHideADeploymentBeforeRuntimeRemovalIsObserved(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	s, workspaceID, actorID, workspaceNamespace := newExecutorIntegrationFixture(t)
	publicID, err := domain.NewPublicID("dep")
	if err != nil {
		t.Fatal(err)
	}
	intent := domain.Intent{
		Name:     "delete-observation-test",
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
	deployment, _, _, err := s.CreateDeployment(ctx, workspaceID, actorID, publicID, intent, domain.SHA256([]byte("delete-create-idem")), domain.SHA256([]byte("delete-create-payload")))
	if err != nil {
		t.Fatal(err)
	}
	operation, err := s.DeleteDeployment(ctx, workspaceID, actorID, deployment.ID, deployment.DesiredVersion, domain.SHA256([]byte("delete-idem")), domain.SHA256([]byte("delete-payload")))
	if err != nil {
		t.Fatal(err)
	}

	runtimeClient := &deleteObservationRuntime{}
	server := NewServer(s, runtimeClient, Config{WorkspaceNamespace: workspaceNamespace}, nil)
	go server.RunWorker(ctx, "delete-observation-worker")

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		current, getErr := s.GetOperation(ctx, workspaceID, operation.PublicID)
		if getErr != nil {
			t.Fatal(getErr)
		}
		if current.Status == domain.OperationSucceeded || current.Status == domain.OperationFailed {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if got := atomic.LoadInt32(&runtimeClient.observeCalls); got == 0 {
		t.Fatal("delete worker completed without observing whether the AppDeployment disappeared")
	}

	var deletedAt *time.Time
	if err := s.Pool.QueryRow(ctx, `SELECT deleted_at FROM deployments WHERE id=$1`, deployment.ID).Scan(&deletedAt); err != nil {
		t.Fatal(err)
	}
	if deletedAt != nil {
		t.Fatalf("deployment was hidden before runtime removal was observed at %s", deletedAt.Format(time.RFC3339Nano))
	}
}

type deleteObservationRuntime struct {
	deleteCalls  int32
	observeCalls int32
}

func (r *deleteObservationRuntime) EnsureWorkspace(context.Context, string) error {
	return nil
}

func (r *deleteObservationRuntime) ApplyDeployment(context.Context, string, string, domain.Intent) error {
	return nil
}

func (r *deleteObservationRuntime) ObserveDeployment(context.Context, string, string) (controlruntime.Observation, error) {
	atomic.AddInt32(&r.observeCalls, 1)
	return controlruntime.Observation{Exists: true, State: domain.Unknown, Message: "still present"}, nil
}

func (r *deleteObservationRuntime) DeleteDeployment(context.Context, string, string) error {
	atomic.AddInt32(&r.deleteCalls, 1)
	return nil
}

func newExecutorIntegrationFixture(t *testing.T) (*store.Store, int64, int64, string) {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv("FRUTO_TEST_DATABASE_URL"))
	if dsn == "" {
		t.Skip("set FRUTO_TEST_DATABASE_URL to run PostgreSQL integration tests")
	}

	ctx := context.Background()
	s, err := store.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	if err := s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	actorKey := "executor-owner-" + suffix
	workspace := domain.Workspace{
		PublicID:  "ws-executor-" + suffix,
		Name:      "Executor Integration",
		Namespace: "executor-" + suffix,
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
			t.Errorf("delete executor operations: %v", err)
		}
		if _, err := s.Pool.Exec(cleanupCtx, `DELETE FROM deployments WHERE workspace_id=$1`, workspaceID); err != nil {
			t.Errorf("delete executor deployments: %v", err)
		}
		if _, err := s.Pool.Exec(cleanupCtx, `DELETE FROM workspace_actors WHERE workspace_id=$1`, workspaceID); err != nil {
			t.Errorf("delete executor memberships: %v", err)
		}
		if _, err := s.Pool.Exec(cleanupCtx, `DELETE FROM workspaces WHERE id=$1`, workspaceID); err != nil {
			t.Errorf("delete executor workspace: %v", err)
		}
		if _, err := s.Pool.Exec(cleanupCtx, `DELETE FROM actors WHERE id=$1`, actorID); err != nil {
			t.Errorf("delete executor actor: %v", err)
		}
	})
	return s, workspaceID, actorID, workspace.Namespace
}
