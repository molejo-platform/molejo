package api

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fruto-platform/fruto/services/control-plane-api/internal/domain"
	controlruntime "github.com/fruto-platform/fruto/services/control-plane-api/internal/runtime"
	"github.com/fruto-platform/fruto/services/control-plane-api/internal/store"
	"github.com/fruto-platform/fruto/services/control-plane-api/internal/testsupport"
)

func TestWorkerResumesAcrossRuntimeCrashWindows(t *testing.T) {
	tests := []struct {
		kind       string
		crashAfter bool
	}{
		{kind: "create", crashAfter: false},
		{kind: "create", crashAfter: true},
		{kind: "update", crashAfter: false},
		{kind: "update", crashAfter: true},
		{kind: "delete", crashAfter: false},
		{kind: "delete", crashAfter: true},
	}
	for _, tt := range tests {
		window := "before-effect"
		if tt.crashAfter {
			window = "after-effect-before-complete"
		}
		t.Run(tt.kind+"/"+window, func(t *testing.T) {
			ctx := context.Background()
			s, workspaceID, actorID, workspaceNamespace := newExecutorIntegrationFixture(t)
			publicID, err := domain.NewPublicID("ap")
			if err != nil {
				t.Fatal(err)
			}
			intent := executorIntent("crash-window-" + strconv.FormatInt(time.Now().UnixNano(), 10))
			deployment, operation, _, err := s.CreateDeployment(ctx, workspaceID, actorID, publicID, intent, domain.SHA256([]byte("crash-window-idem")), domain.SHA256([]byte("crash-window-payload")))
			if err != nil {
				t.Fatal(err)
			}
			runtimeClient := &crashBarrierRuntime{}
			if tt.kind != "create" {
				server := NewServer(s, runtimeClient, Config{OperationLease: time.Second}, nil)
				if processed, err := server.RunOnce(ctx, "initial-worker"); err != nil || !processed {
					t.Fatalf("complete initial create: processed=%v err=%v", processed, err)
				}
				switch tt.kind {
				case "update":
					updatedIntent := intent
					updatedIntent.Image = "ghcr.io/fruto-platform/testkit@sha256:" + strings.Repeat("b", 64)
					deployment, operation, err = s.UpdateDeployment(ctx, workspaceID, actorID, deployment.ID, updatedIntent, deployment.DesiredVersion, domain.SHA256([]byte("update-idem")), domain.SHA256([]byte("update-payload")))
					if err != nil {
						t.Fatal(err)
					}
					intent = updatedIntent
				case "delete":
					operation, err = s.DeleteDeployment(ctx, workspaceID, actorID, deployment.ID, deployment.DesiredVersion, domain.SHA256([]byte("delete-idem")), domain.SHA256([]byte("delete-payload")))
					if err != nil {
						t.Fatal(err)
					}
				}
			}

			runtimeClient.crashAfterEffect = tt.crashAfter
			runtimeClient.crashBeforeEffect = !tt.crashAfter
			runtimeClient.crashOperation = tt.kind
			crashedServer := NewServer(s, runtimeClient, Config{OperationLease: time.Minute}, nil)
			func() {
				defer func() {
					if recover() == nil {
						t.Fatal("worker did not stop at the configured crash barrier")
					}
				}()
				_, _ = crashedServer.RunOnce(ctx, "crashed-worker")
			}()
			currentOperation, err := s.GetOperation(ctx, workspaceID, operation.PublicID)
			if err != nil {
				t.Fatal(err)
			}
			if currentOperation.Status != domain.OperationRunning || currentOperation.Attempts != 1 {
				t.Fatalf("crashed operation status=%q attempts=%d", currentOperation.Status, currentOperation.Attempts)
			}
			if _, err := s.Pool.Exec(ctx, `UPDATE operations SET lease_until=now()-interval '1 second' WHERE id=$1`, currentOperation.ID); err != nil {
				t.Fatal(err)
			}

			replacementStore, err := store.New(ctx, s.Pool.Config().ConnString())
			if err != nil {
				t.Fatal(err)
			}
			defer replacementStore.Close()
			runtimeClient.crashBeforeEffect = false
			runtimeClient.crashAfterEffect = false
			server := NewServer(replacementStore, runtimeClient, Config{OperationLease: time.Second, WorkspaceNamespace: workspaceNamespace}, nil)
			processed, err := server.RunOnce(ctx, "replacement-worker")
			if err != nil || !processed {
				t.Fatalf("resume operation: processed=%v err=%v", processed, err)
			}
			currentOperation, err = s.GetOperation(ctx, workspaceID, operation.PublicID)
			if err != nil {
				t.Fatal(err)
			}
			if currentOperation.Status != domain.OperationSucceeded || currentOperation.Attempts != 2 {
				t.Fatalf("resumed operation status=%q attempts=%d", currentOperation.Status, currentOperation.Attempts)
			}
			currentDeployment, err := s.FindDeployment(ctx, workspaceID, deployment.PublicID)
			if tt.kind == "delete" {
				if err == nil {
					t.Fatalf("deleted deployment remained visible: %+v", currentDeployment)
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				if currentDeployment.ObservedVersion != currentDeployment.DesiredVersion || currentDeployment.ObservedRelease != intent.Image {
					t.Fatalf("deployment was not finalized after resume: %+v", currentDeployment)
				}
			}
		})
	}
}

func TestWorkerDoesNotHideADeploymentBeforeRuntimeRemovalIsObserved(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	s, workspaceID, actorID, workspaceNamespace := newExecutorIntegrationFixture(t)
	publicID, err := domain.NewPublicID("ap")
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

func TestWorkerLogsOperationAndWorkerCorrelation(t *testing.T) {
	ctx := context.Background()
	s, workspaceID, actorID, _ := newExecutorIntegrationFixture(t)
	publicID, err := domain.NewPublicID("ap")
	if err != nil {
		t.Fatal(err)
	}
	_, operation, _, err := s.CreateDeployment(ctx, workspaceID, actorID, publicID, executorIntent("log-correlation"), domain.SHA256([]byte("log-idem")), domain.SHA256([]byte("log-payload")))
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	server := NewServer(s, &crashBarrierRuntime{}, Config{OperationLease: time.Second}, slog.New(slog.NewJSONHandler(&output, nil)))
	if processed, err := server.RunOnce(ctx, "worker-correlation"); err != nil || !processed {
		t.Fatalf("run operation: processed=%v err=%v", processed, err)
	}
	log := output.String()
	for _, expected := range []string{`"operation_id":"` + operation.PublicID + `"`, `"worker_id":"worker-correlation"`, `"msg":"operation claimed"`, `"msg":"operation completed"`} {
		if !strings.Contains(log, expected) {
			t.Errorf("worker log is missing %s: %s", expected, log)
		}
	}
}

func TestSessionRefreshFailureKeepsTheOldSessionValid(t *testing.T) {
	ctx := context.Background()
	s, _, actorID, _ := newExecutorIntegrationFixture(t)
	oldToken := "old-session-token"
	collidingToken := "already-issued-token"
	csrf := domain.SHA256([]byte("csrf"))
	if err := s.CreateSession(ctx, actorID, domain.SHA256([]byte(oldToken)), csrf, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateSession(ctx, actorID, domain.SHA256([]byte(collidingToken)), csrf, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	server := NewServer(s, nil, Config{CookieName: "fruto_session", SessionTTL: time.Hour}, nil)
	issued := []string{collidingToken, "new-csrf-token"}
	server.token = func(int) (string, error) {
		value := issued[0]
		issued = issued[1:]
		return value, nil
	}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/session", nil)
	request.AddCookie(&http.Cookie{Name: "fruto_session", Value: oldToken})
	recorder := httptest.NewRecorder()

	server.sessionInfo(recorder, request, actorID)

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("refresh status=%d, want %d", recorder.Code, http.StatusInternalServerError)
	}
	if recorder.Header().Get("Set-Cookie") != "" {
		t.Fatal("refresh failure emitted a replacement cookie")
	}
	if _, _, err := s.Session(ctx, domain.SHA256([]byte(oldToken))); err != nil {
		t.Fatalf("refresh failure revoked the old session: %v", err)
	}
}

func TestLogoutDoesNotClaimSuccessWhenRevocationFails(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("FRUTO_TEST_DATABASE_URL"))
	if dsn == "" {
		t.Skip("set FRUTO_TEST_DATABASE_URL to run PostgreSQL integration tests")
	}
	brokenStore, err := store.New(context.Background(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	brokenStore.Close()
	server := NewServer(brokenStore, nil, Config{CookieName: "fruto_session"}, nil)
	request := httptest.NewRequest(http.MethodDelete, "/api/v1/session", nil)
	request.AddCookie(&http.Cookie{Name: "fruto_session", Value: "session-token"})
	recorder := httptest.NewRecorder()

	server.logout(recorder, request)

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("logout status=%d, want %d", recorder.Code, http.StatusInternalServerError)
	}
	if recorder.Header().Get("Set-Cookie") != "" {
		t.Fatal("failed logout cleared the client cookie despite failed server-side revocation")
	}
}

type deleteObservationRuntime struct {
	deleteCalls  int32
	observeCalls int32
}

type crashBarrierRuntime struct {
	applyCalls        int32
	deleteCalls       int32
	release           string
	exists            bool
	crashOperation    string
	crashBeforeEffect bool
	crashAfterEffect  bool
}

func (r *crashBarrierRuntime) EnsureWorkspace(context.Context, string) error { return nil }

func (r *crashBarrierRuntime) ApplyDeployment(_ context.Context, _, _ string, intent domain.Intent) error {
	if r.crashBeforeEffect && (r.crashOperation == "create" || r.crashOperation == "update") {
		panic("worker crashed before runtime apply")
	}
	atomic.AddInt32(&r.applyCalls, 1)
	r.release = intent.Image
	r.exists = true
	if r.crashAfterEffect && (r.crashOperation == "create" || r.crashOperation == "update") {
		panic("worker crashed after runtime apply")
	}
	return nil
}

func (r *crashBarrierRuntime) ObserveDeployment(context.Context, string, string) (controlruntime.Observation, error) {
	if !r.exists {
		return controlruntime.Observation{Exists: false, State: domain.Unknown, Message: "runtime absent"}, nil
	}
	return controlruntime.Observation{Exists: true, State: domain.Ready, Message: "runtime ready", ObservedRelease: r.release}, nil
}

func (r *crashBarrierRuntime) DeleteDeployment(context.Context, string, string) error {
	if r.crashBeforeEffect && r.crashOperation == "delete" {
		panic("worker crashed before runtime delete")
	}
	atomic.AddInt32(&r.deleteCalls, 1)
	r.exists = false
	if r.crashAfterEffect && r.crashOperation == "delete" {
		panic("worker crashed after runtime delete")
	}
	return nil
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
	isolatedDSN, cleanup, err := testsupport.IsolatedPostgres(ctx, dsn, "control_plane_api")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := cleanup(context.Background()); err != nil {
			t.Errorf("drop executor integration schema: %v", err)
		}
	})
	s, err := store.New(ctx, isolatedDSN)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	if err := s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	actorKey := "executor-owner-" + suffix
	workspacePublicID, err := domain.NewPublicID("ws")
	if err != nil {
		t.Fatal(err)
	}
	workspace := domain.Workspace{
		PublicID:  workspacePublicID,
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
	if _, err := s.Pool.Exec(ctx, `UPDATE workspaces SET bootstrap_state='Ready',updated_at=now() WHERE id=$1`, workspaceID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Pool.Exec(ctx, `UPDATE operations SET status='Succeeded',started_at=COALESCE(started_at,now()),completed_at=now(),updated_at=now() WHERE workspace_id=$1 AND kind='EnsureWorkspace'`, workspaceID); err != nil {
		t.Fatal(err)
	}
	return s, workspaceID, actorID, workspace.Namespace
}
