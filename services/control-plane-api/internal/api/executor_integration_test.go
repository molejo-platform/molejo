package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	clusteragentv1alpha1 "github.com/molejo-platform/molejo/contracts/molejo/clusteragent/v1alpha1"
	"github.com/molejo-platform/molejo/packages/runtimecontract"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/audit"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/domain"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/operationworker"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/store"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/testsupport"
)

const testAgentInstallationID = "agi-aaaaaaaaaaaaaaaaaaaa"

func TestAgentCommandCompletesADeploymentWithoutControlPlaneKubernetesAccess(t *testing.T) {
	ctx := context.Background()
	s, workspaceID, actorID, _ := newExecutorIntegrationFixture(t)
	target, releaseID, image := createExecutorTargetAndRelease(t, s, workspaceID, actorID)
	deployment, operation, _, err := s.CreateDeployment(ctx, workspaceID, actorID, target.PublicID, mustAPIID(t, "dpl"), releaseID, target.ConfigurationVersion, target.Version, "", domain.SHA256([]byte("agent-apply")), domain.SHA256([]byte("agent-apply-payload")), audit.Event{PublicID: mustAPIID(t, "aud"), Action: "deployment.create", TargetType: "Deployment", Outcome: audit.Succeeded})
	if err != nil {
		t.Fatal(err)
	}
	worker := operationworker.Worker{Store: s, Publication: s.PublicationPolicy(), ParameterSecrets: &recordingSecretStore{}, OperationLease: time.Minute}
	command, ok, err := worker.NextCommand(ctx, testAgentInstallationID)
	if err != nil || !ok {
		t.Fatalf("next command: ok=%v err=%v", ok, err)
	}
	if command.GetOperationId() != operation.PublicID || command.GetKind() != runtimecontract.OperationApplyDeployment {
		t.Fatalf("command=%+v", command)
	}
	var payload runtimecontract.Payload
	if err = json.Unmarshal(command.GetPayloadJson(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Name != target.RuntimeName || payload.Deployment == nil || payload.Deployment.Image != image {
		t.Fatalf("payload=%+v", payload)
	}
	result := &clusteragentv1alpha1.RuntimeResult{
		CommandId:       command.GetCommandId(),
		FencingToken:    command.GetFencingToken(),
		State:           runtimecontract.StateReady,
		Message:         "runtime ready",
		ObservedRelease: image,
		DesiredVersion:  command.GetDesiredVersion(),
		SpecHash:        strings.Repeat("a", 64),
	}
	if err = worker.HandleResult(ctx, testAgentInstallationID, result); err != nil {
		t.Fatal(err)
	}
	current, err := s.FindAppEnvironment(ctx, workspaceID, target.PublicID)
	if err != nil {
		t.Fatal(err)
	}
	if current.CurrentDeploymentPublicID != deployment.PublicID || current.CurrentReleasePublicID != releaseID || current.State != domain.Ready {
		t.Fatalf("App Environment=%+v", current)
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
	server := NewServer(Config{CookieName: "molejo_session", SessionTTL: time.Hour, SessionIdleTTL: time.Hour}, Dependencies{Store: s})
	request := httptest.NewRequest(http.MethodGet, "/api/v1/session", nil)
	request.AddCookie(&http.Cookie{Name: "molejo_session", Value: oldToken})
	request.AddCookie(&http.Cookie{Name: "molejo_session_csrf", Value: csrfToken})
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
	dsn := strings.TrimSpace(os.Getenv("MOLEJO_TEST_DATABASE_URL"))
	if dsn == "" {
		t.Skip("set MOLEJO_TEST_DATABASE_URL to run PostgreSQL integration tests")
	}
	brokenStore, err := store.New(context.Background(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	brokenStore.Close()
	server := NewServer(Config{CookieName: "molejo_session"}, Dependencies{Store: brokenStore})
	request := httptest.NewRequest(http.MethodDelete, "/api/v1/session", nil)
	request.AddCookie(&http.Cookie{Name: "molejo_session", Value: "session-token"})
	recorder := httptest.NewRecorder()

	server.logout(recorder, request)

	if recorder.Code != http.StatusInternalServerError || recorder.Header().Get("Set-Cookie") != "" {
		t.Fatalf("logout status=%d Set-Cookie=%q", recorder.Code, recorder.Header().Get("Set-Cookie"))
	}
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
	if _, err = s.Pool.Exec(ctx, `INSERT INTO releases(public_id,workspace_id,project_id,app_id,app_environment_id,build_id,commit_sha,image,platform,source_provider,source_repository,source_revision,producer_kind,created_by_principal_id)
		SELECT $1,$2,$3,$4,$5,$6,$7,$8,'linux/amd64','GitHub','molejo/platform',$7,'buildkit',u.principal_id FROM users u WHERE u.id=$9`, releaseID, workspaceID, project.ID, app.ID, target.ID, internalBuildID, strings.Repeat("a", 40), image, actorID); err != nil {
		t.Fatal(err)
	}
	return releaseID, image
}

func newExecutorIntegrationFixture(t *testing.T) (*store.Store, int64, int64, string) {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv("MOLEJO_TEST_DATABASE_URL"))
	if dsn == "" {
		t.Skip("set MOLEJO_TEST_DATABASE_URL to run PostgreSQL integration tests")
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
	var clusterID int64
	if err = s.Pool.QueryRow(ctx, `INSERT INTO agent_installations(public_id,name,status,cluster_uid,agent_version,kubernetes_version,capabilities_json,created_by)
		VALUES($1,'test-agent','Active','cluster-test-uid','test','v1.36.3','["runtime.v1alpha1"]',$2) RETURNING id`, testAgentInstallationID, actorID).Scan(&clusterID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Pool.Exec(ctx, `INSERT INTO workspace_clusters(workspace_id,installation_id,namespace_name,state,observed_generation)
		VALUES($1,$2,$3,'Ready',1)`, workspaceID, clusterID, workspace.Namespace); err != nil {
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
