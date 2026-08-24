package api

import (
	"context"
	"fmt"
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

func TestWorkerResumesAcrossRuntimeCrashWindows(t *testing.T) {
	tests := []struct {
		name       string
		preApplied bool
		wantCalls  int32
	}{
		{name: "before runtime effect", preApplied: false, wantCalls: 1},
		{name: "after runtime effect before completion", preApplied: true, wantCalls: 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			s, workspaceID, actorID, workspaceNamespace := newExecutorIntegrationFixture(t)
			publicID, err := domain.NewPublicID("dep")
			if err != nil {
				t.Fatal(err)
			}
			intent := executorIntent("crash-window-" + strconv.FormatInt(time.Now().UnixNano(), 10))
			deployment, operation, _, err := s.CreateDeployment(ctx, workspaceID, actorID, publicID, intent, domain.SHA256([]byte("crash-window-idem")), domain.SHA256([]byte("crash-window-payload")))
			if err != nil {
				t.Fatal(err)
			}

			claimed, claimedDeployment, ok, err := s.ClaimNext(ctx, "crashed-worker", time.Minute)
			if err != nil || !ok {
				t.Fatalf("claim before simulated crash: ok=%v err=%v", ok, err)
			}
			runtimeClient := &resumableRuntime{}
			if tt.preApplied {
				if err := runtimeClient.ApplyDeployment(ctx, workspaceNamespace, claimedDeployment.RuntimeName, claimed.Intent); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := s.Pool.Exec(ctx, `UPDATE operations SET lease_until=now()-interval '1 second' WHERE id=$1`, claimed.ID); err != nil {
				t.Fatal(err)
			}

			server := NewServer(s, runtimeClient, Config{OperationLease: time.Second}, nil)
			processed, err := server.RunOnce(ctx, "replacement-worker")
			if err != nil || !processed {
				t.Fatalf("resume operation: processed=%v err=%v", processed, err)
			}
			if got := atomic.LoadInt32(&runtimeClient.applyCalls); got != tt.wantCalls {
				t.Fatalf("runtime apply calls=%d, want %d", got, tt.wantCalls)
			}
			currentOperation, err := s.GetOperation(ctx, workspaceID, operation.PublicID)
			if err != nil {
				t.Fatal(err)
			}
			if currentOperation.Status != domain.OperationSucceeded || currentOperation.Attempts != 2 {
				t.Fatalf("resumed operation status=%q attempts=%d", currentOperation.Status, currentOperation.Attempts)
			}
			currentDeployment, err := s.FindDeployment(ctx, workspaceID, deployment.PublicID)
			if err != nil {
				t.Fatal(err)
			}
			if currentDeployment.ObservedVersion != currentDeployment.DesiredVersion || currentDeployment.ObservedRelease != intent.Image {
				t.Fatalf("deployment was not finalized after resume: %+v", currentDeployment)
			}
		})
	}
}

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

type resumableRuntime struct {
	applyCalls int32
	release    string
}

func (r *resumableRuntime) EnsureWorkspace(context.Context, string) error { return nil }

func (r *resumableRuntime) ApplyDeployment(_ context.Context, _, _ string, intent domain.Intent) error {
	atomic.AddInt32(&r.applyCalls, 1)
	r.release = intent.Image
	return nil
}

func (r *resumableRuntime) ObserveDeployment(context.Context, string, string) (controlruntime.Observation, error) {
	if r.release == "" {
		return controlruntime.Observation{}, fmt.Errorf("runtime release was not applied")
	}
	return controlruntime.Observation{Exists: true, State: domain.Ready, Message: "runtime ready", ObservedRelease: r.release}, nil
}

func (r *resumableRuntime) DeleteDeployment(context.Context, string, string) error { return nil }

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

func executorIntent(name string) domain.Intent {
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
