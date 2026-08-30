package api

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/fruto-platform/fruto/services/control-plane-api/internal/domain"
	controlruntime "github.com/fruto-platform/fruto/services/control-plane-api/internal/runtime"
	"github.com/fruto-platform/fruto/services/control-plane-api/internal/store"
	"github.com/fruto-platform/fruto/services/control-plane-api/internal/testsupport"
)

func TestWorkerAppliesAnImmutableDeploymentAndCorrelatesItsLogs(t *testing.T) {
	ctx := context.Background()
	s, workspaceID, actorID, workspaceNamespace := newExecutorIntegrationFixture(t)
	target, releaseID, image := createExecutorTargetAndRelease(t, s, workspaceID, actorID)
	plainValue := "https://internal.example"
	plain, err := s.CreateParameter(ctx, workspaceID, actorID, mustAPIID(t, "par"), "/test/internal-url", domain.ParameterPlainText, "", domain.ParameterValue{PlainTextValue: &plainValue})
	if err != nil {
		t.Fatal(err)
	}
	secretReference := "workspaces/test/parameters/runtime-token"
	secret, err := s.CreateParameter(ctx, workspaceID, actorID, mustAPIID(t, "par"), "/test/runtime-token", domain.ParameterSecret, "", domain.ParameterValue{SecretReference: secretReference, SecretBackendVersion: 1, Fingerprint: bytes.Repeat([]byte{1}, 32)})
	if err != nil {
		t.Fatal(err)
	}
	configuration := target.Configuration
	configuration.Parameters = []domain.ParameterBinding{{Name: "INTERNAL_URL", ParameterPublicID: plain.PublicID, ParameterVersion: 1}, {Name: "RUNTIME_TOKEN", ParameterPublicID: secret.PublicID, ParameterVersion: 1}}
	target, err = s.UpdateAppEnvironment(ctx, workspaceID, actorID, target.PublicID, target.SourceBranch, configuration, target.Version)
	if err != nil {
		t.Fatal(err)
	}
	deployment, operation, _, err := s.CreateDeployment(ctx, workspaceID, actorID, target.PublicID, mustAPIID(t, "dpl"), releaseID, target.ConfigurationVersion, target.Version, "", domain.SHA256([]byte("worker-apply")), domain.SHA256([]byte("worker-apply-payload")))
	if err != nil {
		t.Fatal(err)
	}
	runtimeClient := &recordingRuntime{}
	var output bytes.Buffer
	server := NewServer(s, runtimeClient, Config{OperationLease: time.Minute, WorkspaceNamespace: workspaceNamespace}, slog.New(slog.NewJSONHandler(&output, nil)))
	server.ParameterSecrets = &recordingSecretStore{values: map[string]string{secretReference: "secret-runtime-value"}, versions: map[string]int64{secretReference: 1}}

	if processed, runErr := server.RunOnce(ctx, "worker-correlation"); runErr != nil || !processed {
		t.Fatalf("run operation: processed=%v err=%v", processed, runErr)
	}
	current, err := s.FindAppEnvironment(ctx, workspaceID, target.PublicID)
	if err != nil {
		t.Fatal(err)
	}
	if runtimeClient.name != target.RuntimeName || runtimeClient.intent.Image != image || current.CurrentDeploymentPublicID != deployment.PublicID || current.CurrentReleasePublicID != releaseID || current.State != domain.Ready {
		t.Fatalf("runtime=%+v App Environment=%+v", runtimeClient, current)
	}
	if runtimeClient.intent.ConfigurationVersion != target.ConfigurationVersion || len(runtimeClient.intent.SecretVariables) != 1 || runtimeClient.intent.SecretVariables[0].Value != "secret-runtime-value" || !containsVariable(runtimeClient.intent.Variables, "INTERNAL_URL", plainValue) {
		t.Fatalf("materialized intent=%+v", runtimeClient.intent)
	}
	if runtimeClient.garbageCollections != 1 {
		t.Fatalf("configuration garbage collections = %d, want 1", runtimeClient.garbageCollections)
	}
	for _, expected := range []string{`"operation_id":"` + operation.PublicID + `"`, `"app_environment_id":"` + target.PublicID + `"`, `"deployment_id":"` + deployment.PublicID + `"`, `"worker_id":"worker-correlation"`} {
		if !strings.Contains(output.String(), expected) {
			t.Errorf("worker log is missing %s: %s", expected, output.String())
		}
	}
}

func containsVariable(items []domain.Variable, name, value string) bool {
	for _, item := range items {
		if item.Name == name && item.Value == value {
			return true
		}
	}
	return false
}

func TestWorkerRetriesAppEnvironmentDeletionUntilRuntimeAbsenceIsObserved(t *testing.T) {
	ctx := context.Background()
	s, workspaceID, actorID, workspaceNamespace := newExecutorIntegrationFixture(t)
	target, _, _ := createExecutorTargetAndRelease(t, s, workspaceID, actorID)
	operation, err := s.DeleteAppEnvironment(ctx, workspaceID, actorID, target.PublicID, target.Version, domain.SHA256([]byte("delete-target")), domain.SHA256([]byte("delete-target-payload")))
	if err != nil {
		t.Fatal(err)
	}
	runtimeClient := &recordingRuntime{exists: true}
	server := NewServer(s, runtimeClient, Config{OperationLease: time.Minute, WorkspaceNamespace: workspaceNamespace}, nil)

	if processed, runErr := server.RunOnce(ctx, "delete-worker"); runErr != nil || !processed {
		t.Fatalf("first delete: processed=%v err=%v", processed, runErr)
	}
	currentOperation, err := s.GetOperationForUser(ctx, actorID, operation.PublicID)
	if err != nil || currentOperation.Status != domain.OperationPending {
		t.Fatalf("operation after observed runtime=%+v err=%v", currentOperation, err)
	}
	if _, err = s.FindAppEnvironment(ctx, workspaceID, target.PublicID); err != nil {
		t.Fatalf("target was archived before runtime absence: %v", err)
	}
	runtimeClient.exists = false
	if _, err = s.Pool.Exec(ctx, `UPDATE operations SET next_attempt_at=now() WHERE id=$1`, currentOperation.ID); err != nil {
		t.Fatal(err)
	}
	if processed, runErr := server.RunOnce(ctx, "delete-worker"); runErr != nil || !processed {
		t.Fatalf("second delete: processed=%v err=%v", processed, runErr)
	}
	if _, err = s.FindAppEnvironment(ctx, workspaceID, target.PublicID); err != store.ErrNotFound {
		t.Fatalf("deleted target error=%v, want not found", err)
	}
	if runtimeClient.garbageCollections != 1 {
		t.Fatalf("configuration garbage collections = %d, want 1", runtimeClient.garbageCollections)
	}
}

func TestWorkerRetriesVolumeProvisioningAfterATemporaryRuntimeFailure(t *testing.T) {
	ctx := context.Background()
	s, workspaceID, actorID, workspaceNamespace := newExecutorIntegrationFixture(t)
	if err := s.ConfigureStorageProfile(ctx, store.StorageProfileInstallation{
		ID: "persistent-standard", Name: "Persistent storage", MinimumSizeGiB: 1,
		MaximumSizeGiB: 10, TotalCapacityGiB: 10, WorkspaceQuotaGiB: 10,
		Expandable: true, Durability: "NodeLocal", RuntimeBinding: "test-storage", Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}
	project, err := s.CreateProject(ctx, workspaceID, mustAPIID(t, "prj"), "Platform", "platform")
	if err != nil {
		t.Fatal(err)
	}
	app, err := s.CreateApp(ctx, workspaceID, project.PublicID, mustAPIID(t, "app"), "API", "api")
	if err != nil {
		t.Fatal(err)
	}
	environment, err := s.CreateEnvironment(ctx, workspaceID, project.PublicID, mustAPIID(t, "env"), "Production", "production")
	if err != nil {
		t.Fatal(err)
	}
	target, volume, err := s.CreateAppEnvironmentWithWorkload(ctx, workspaceID, actorID, mustAPIID(t, "aev"), project.PublicID, app.PublicID, environment.PublicID, "main", domain.WorkloadStateful, apiRuntimeConfiguration("stateful-api"), &domain.VolumeRequest{StorageProfileID: "persistent-standard", SizeGiB: 1, MountPath: "/data"})
	if err != nil {
		t.Fatal(err)
	}
	runtimeClient := &recordingRuntime{volumeApplyErr: errors.New("temporary runtime failure")}
	server := NewServer(s, runtimeClient, Config{OperationLease: time.Minute, WorkspaceNamespace: workspaceNamespace}, nil)
	if processed, runErr := server.RunOnce(ctx, "volume-worker"); runErr != nil || !processed {
		t.Fatalf("failed volume attempt: processed=%v err=%v", processed, runErr)
	}
	var operationID int64
	var status string
	if err = s.Pool.QueryRow(ctx, `SELECT id,status FROM operations WHERE app_volume_id=$1 AND kind=$2`, volume.ID, domain.OperationEnsureVolume).Scan(&operationID, &status); err != nil {
		t.Fatal(err)
	}
	if status != domain.OperationPending {
		t.Fatalf("operation status after temporary failure=%s", status)
	}
	runtimeClient.volumeApplyErr = nil
	if _, err = s.Pool.Exec(ctx, `UPDATE operations SET next_attempt_at=now() WHERE id=$1`, operationID); err != nil {
		t.Fatal(err)
	}
	if processed, runErr := server.RunOnce(ctx, "volume-worker"); runErr != nil || !processed {
		t.Fatalf("retried volume attempt: processed=%v err=%v", processed, runErr)
	}
	current, err := s.FindAppVolume(ctx, workspaceID, target.PublicID)
	if err != nil || current.State != domain.VolumeStateReady {
		t.Fatalf("volume after retry=%+v err=%v", current, err)
	}
}

func TestWorkerAllowsFirstStatefulDeploymentToBindAWaitingVolume(t *testing.T) {
	ctx := context.Background()
	s, workspaceID, actorID, workspaceNamespace := newExecutorIntegrationFixture(t)
	if err := s.ConfigureStorageProfile(ctx, store.StorageProfileInstallation{
		ID: "persistent-standard", Name: "Persistent storage", MinimumSizeGiB: 1,
		MaximumSizeGiB: 10, TotalCapacityGiB: 10, WorkspaceQuotaGiB: 10,
		Expandable: true, Durability: "NodeLocal", RuntimeBinding: "test-storage", Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}
	project, err := s.CreateProject(ctx, workspaceID, mustAPIID(t, "prj"), "Platform", "platform")
	if err != nil {
		t.Fatal(err)
	}
	app, err := s.CreateApp(ctx, workspaceID, project.PublicID, mustAPIID(t, "app"), "API", "api")
	if err != nil {
		t.Fatal(err)
	}
	environment, err := s.CreateEnvironment(ctx, workspaceID, project.PublicID, mustAPIID(t, "env"), "Production", "production")
	if err != nil {
		t.Fatal(err)
	}
	target, _, err := s.CreateAppEnvironmentWithWorkload(ctx, workspaceID, actorID, mustAPIID(t, "aev"), project.PublicID, app.PublicID, environment.PublicID, "main", domain.WorkloadStateful, apiRuntimeConfiguration("stateful-api"), &domain.VolumeRequest{StorageProfileID: "persistent-standard", SizeGiB: 1, MountPath: "/data"})
	if err != nil {
		t.Fatal(err)
	}
	runtimeClient := &recordingRuntime{volumeObservation: controlruntime.VolumeObservation{
		Exists: true, State: domain.VolumeStateProvisioning, Message: "waiting for first consumer",
	}}
	server := NewServer(s, runtimeClient, Config{OperationLease: time.Minute, WorkspaceNamespace: workspaceNamespace}, nil)
	if processed, runErr := server.RunOnce(ctx, "stateful-worker"); runErr != nil || !processed {
		t.Fatalf("prepare volume: processed=%v err=%v", processed, runErr)
	}
	currentVolume, err := s.FindAppVolume(ctx, workspaceID, target.PublicID)
	if err != nil || currentVolume.State != domain.VolumeStateProvisioning {
		t.Fatalf("prepared volume=%+v err=%v", currentVolume, err)
	}
	target, err = s.FindAppEnvironment(ctx, workspaceID, target.PublicID)
	if err != nil {
		t.Fatal(err)
	}
	releaseID, image := createExecutorRelease(t, s, workspaceID, actorID, project, app, target)
	deployment, _, _, err := s.CreateDeployment(ctx, workspaceID, actorID, target.PublicID, mustAPIID(t, "dpl"), releaseID, target.ConfigurationVersion, target.Version, "", domain.SHA256([]byte("first-stateful-deploy")), domain.SHA256([]byte("first-stateful-deploy-payload")))
	if err != nil {
		t.Fatalf("create first stateful deployment: %v", err)
	}
	runtimeClient.volumeObservation = controlruntime.VolumeObservation{
		Exists: true, State: domain.VolumeStateReady, Message: "persistent storage is ready", ObservedSizeGiB: 1,
	}
	if processed, runErr := server.RunOnce(ctx, "stateful-worker"); runErr != nil || !processed {
		t.Fatalf("deploy stateful workload: processed=%v err=%v", processed, runErr)
	}
	currentVolume, err = s.FindAppVolume(ctx, workspaceID, target.PublicID)
	if err != nil || currentVolume.State != domain.VolumeStateReady || !currentVolume.Attached {
		t.Fatalf("bound volume=%+v err=%v", currentVolume, err)
	}
	currentTarget, err := s.FindAppEnvironment(ctx, workspaceID, target.PublicID)
	if err != nil || currentTarget.CurrentDeploymentPublicID != deployment.PublicID || currentTarget.State != domain.Ready {
		t.Fatalf("stateful App Environment=%+v err=%v", currentTarget, err)
	}
	if runtimeClient.intent.Volume == nil || runtimeClient.intent.Volume.PublicID != currentVolume.PublicID || runtimeClient.intent.Image != image {
		t.Fatalf("stateful runtime intent=%+v", runtimeClient.intent)
	}
}

func TestSessionReadKeepsTheExistingSessionStable(t *testing.T) {
	ctx := context.Background()
	s, _, actorID, _ := newExecutorIntegrationFixture(t)
	oldToken := "old-session-token"
	csrfToken := "csrf-token"
	csrf := domain.SHA256([]byte(csrfToken))
	if err := s.CreateSession(ctx, actorID, domain.SHA256([]byte(oldToken)), csrf, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	server := NewServer(s, nil, Config{CookieName: "fruto_session", SessionTTL: time.Hour, SessionIdleTTL: time.Hour}, nil)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/session", nil)
	request.AddCookie(&http.Cookie{Name: "fruto_session", Value: oldToken})
	request.AddCookie(&http.Cookie{Name: "fruto_session_csrf", Value: csrfToken})
	recorder := httptest.NewRecorder()

	server.sessionInfo(recorder, request, actorID, "AAL1")

	if recorder.Code != http.StatusOK || recorder.Header().Get("Set-Cookie") != "" {
		t.Fatalf("session status=%d Set-Cookie=%q", recorder.Code, recorder.Header().Get("Set-Cookie"))
	}
	if _, _, err := s.Session(ctx, domain.SHA256([]byte(oldToken))); err != nil {
		t.Fatalf("session read revoked the existing session: %v", err)
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

	if recorder.Code != http.StatusInternalServerError || recorder.Header().Get("Set-Cookie") != "" {
		t.Fatalf("logout status=%d Set-Cookie=%q", recorder.Code, recorder.Header().Get("Set-Cookie"))
	}
}

type recordingRuntime struct {
	name               string
	intent             domain.Intent
	exists             bool
	garbageCollections int
	volumeApplyErr     error
	volumeObservation  controlruntime.VolumeObservation
}

func (r *recordingRuntime) EnsureWorkspace(context.Context, string) error { return nil }

func (r *recordingRuntime) ApplyVolume(context.Context, string, string, controlruntime.VolumeIntent) error {
	return r.volumeApplyErr
}

func (r *recordingRuntime) ObserveVolume(context.Context, string, string) (controlruntime.VolumeObservation, error) {
	if r.volumeObservation.State != "" {
		return r.volumeObservation, nil
	}
	return controlruntime.VolumeObservation{Exists: true, State: domain.VolumeStateReady, ObservedSizeGiB: 1}, nil
}

func (r *recordingRuntime) ApplyDeployment(_ context.Context, _, name string, intent domain.Intent) error {
	r.name = name
	r.intent = intent
	r.exists = true
	return nil
}

func (r *recordingRuntime) ObserveDeployment(context.Context, string, string) (controlruntime.Observation, error) {
	if !r.exists {
		return controlruntime.Observation{Exists: false, State: domain.Unknown, Message: "runtime absent"}, nil
	}
	return controlruntime.Observation{Exists: true, State: domain.Ready, Message: "runtime ready", ObservedRelease: r.intent.Image}, nil
}

func (r *recordingRuntime) DeleteDeployment(context.Context, string, string) error { return nil }

func (r *recordingRuntime) GarbageCollectConfiguration(context.Context, string, string) error {
	r.garbageCollections++
	return nil
}

func createExecutorTargetAndRelease(t *testing.T, s *store.Store, workspaceID, actorID int64) (domain.AppEnvironment, string, string) {
	t.Helper()
	ctx := context.Background()
	project, err := s.CreateProject(ctx, workspaceID, mustAPIID(t, "prj"), "Platform", "platform")
	if err != nil {
		t.Fatal(err)
	}
	app, err := s.CreateApp(ctx, workspaceID, project.PublicID, mustAPIID(t, "app"), "API", "api")
	if err != nil {
		t.Fatal(err)
	}
	environment, err := s.CreateEnvironment(ctx, workspaceID, project.PublicID, mustAPIID(t, "env"), "Production", "production")
	if err != nil {
		t.Fatal(err)
	}
	target, err := s.CreateAppEnvironment(ctx, workspaceID, actorID, mustAPIID(t, "aev"), project.PublicID, app.PublicID, environment.PublicID, "main", apiRuntimeConfiguration("executor-api"))
	if err != nil {
		t.Fatal(err)
	}
	releaseID, image := createExecutorRelease(t, s, workspaceID, actorID, project, app, target)
	return target, releaseID, image
}

func createExecutorRelease(t *testing.T, s *store.Store, workspaceID, actorID int64, project domain.Project, app domain.App, target domain.AppEnvironment) (string, string) {
	t.Helper()
	ctx := context.Background()
	buildID := mustAPIID(t, "bld")
	var internalBuildID int64
	err := s.Pool.QueryRow(ctx, `INSERT INTO builds(public_id,workspace_id,project_id,app_id,app_environment_id,requested_by_user_id,github_installation_external_id,repository_id,repository_full_name,source_branch,commit_sha,platform,status,idempotency_hash,payload_hash)
		VALUES($1,$2,$3,$4,$5,$6,1,1,'molejo/platform',$7,$8,'linux/amd64','Succeeded',$9,$10) RETURNING id`, buildID, workspaceID, project.ID, app.ID, target.ID, actorID, target.SourceBranch, strings.Repeat("a", 40), domain.SHA256([]byte(buildID)), domain.SHA256([]byte("payload:"+buildID))).Scan(&internalBuildID)
	if err != nil {
		t.Fatal(err)
	}
	releaseID := mustAPIID(t, "rel")
	image := "registry.example/molejo/apps/api@sha256:" + strings.Repeat("b", 64)
	if _, err = s.Pool.Exec(ctx, `INSERT INTO releases(public_id,workspace_id,project_id,app_id,app_environment_id,build_id,commit_sha,image,platform) VALUES($1,$2,$3,$4,$5,$6,$7,$8,'linux/amd64')`, releaseID, workspaceID, project.ID, app.ID, target.ID, internalBuildID, strings.Repeat("a", 40), image); err != nil {
		t.Fatal(err)
	}
	return releaseID, image
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
	t.Cleanup(func() { _ = cleanup(context.Background()) })
	s, err := store.New(ctx, isolatedDSN)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	if err = s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	actorKey := "executor-owner-" + suffix
	workspace := domain.Workspace{PublicID: mustAPIID(t, "ws"), Name: "Executor Integration", Namespace: "executor-" + suffix}
	if err = s.Bootstrap(ctx, workspace, map[string]struct{ Role, PasswordHash string }{actorKey: {Role: "owner", PasswordHash: "integration-only"}}); err != nil {
		t.Fatal(err)
	}
	var workspaceID, actorID int64
	if err = s.Pool.QueryRow(ctx, `SELECT id FROM workspaces WHERE public_id=$1`, workspace.PublicID).Scan(&workspaceID); err != nil {
		t.Fatal(err)
	}
	if err = s.Pool.QueryRow(ctx, `SELECT id FROM users WHERE username=$1`, actorKey).Scan(&actorID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Pool.Exec(ctx, `UPDATE workspaces SET bootstrap_state='Ready',updated_at=now() WHERE id=$1`, workspaceID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Pool.Exec(ctx, `UPDATE operations SET status='Succeeded',started_at=COALESCE(started_at,now()),completed_at=now(),updated_at=now() WHERE workspace_id=$1 AND kind='EnsureWorkspace'`, workspaceID); err != nil {
		t.Fatal(err)
	}
	return s, workspaceID, actorID, workspace.Namespace
}
