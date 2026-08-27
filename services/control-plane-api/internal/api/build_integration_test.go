package api

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/fruto-platform/fruto/services/control-plane-api/internal/domain"
)

func TestBuildAPIResolvesExactCommitAndCreatesDeploymentFromPromotedRelease(t *testing.T) {
	ctx := context.Background()
	storage, workspaceID, ownerID, _ := newExecutorIntegrationFixture(t)
	workspace, err := storage.Workspace(ctx, workspaceID)
	if err != nil {
		t.Fatal(err)
	}
	project, err := storage.CreateProject(ctx, workspaceID, mustAPIID(t, "prj"), "Platform", "platform")
	if err != nil {
		t.Fatal(err)
	}
	environment, err := storage.CreateEnvironment(ctx, workspaceID, project.PublicID, mustAPIID(t, "env"), "Production", "production")
	if err != nil {
		t.Fatal(err)
	}
	app, err := storage.CreateApp(ctx, workspaceID, project.PublicID, mustAPIID(t, "app"), "API", "api")
	if err != nil {
		t.Fatal(err)
	}
	installation, err := storage.ConnectGitHubInstallation(ctx, mustAPIID(t, "ghi"), workspaceID, ownerID, 4242, 7, "molejo", "Organization", "selected")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = storage.SetAppGitHubSource(ctx, workspaceID, project.PublicID, app.PublicID, installation.PublicID, domain.GitHubRepository{ID: "99", Name: "platform", FullName: "molejo/platform", DefaultBranch: "main"}); err != nil {
		t.Fatal(err)
	}

	config := DefaultConfig()
	config.PublicURL = "https://console.example"
	config.AllowedOrigin = "https://console.example"
	config.AllowedHosts = []string{"console.example"}
	config.AllowedRegistries = []string{"registry.example"}
	server := NewServer(storage, nil, config, nil)
	const commitSHA = "0123456789abcdef0123456789abcdef01234567"
	server.GitHub = &fakeGitHubService{commitSHA: commitSHA}
	owner := createAPISession(t, storage, ownerID, "build-owner-session", "build-owner-csrf")
	base := "/api/v1/workspaces/" + workspace.PublicID + "/projects/" + project.PublicID + "/apps/" + app.PublicID

	response := hierarchyRequest(t, server, owner, http.MethodPost, base+"/builds", "", map[string]string{"Idempotency-Key": "build-main"})
	if response.Code != http.StatusAccepted {
		t.Fatalf("create build status=%d body=%s", response.Code, response.Body.String())
	}
	var build domain.Build
	decodeResponse(t, response, &build)
	if build.CommitSHA != commitSHA || build.RepositoryFullName != "molejo/platform" || build.Platform != domain.BuildPlatform {
		t.Fatalf("build=%+v", build)
	}
	response = hierarchyRequest(t, server, owner, http.MethodPost, base+"/builds", "", map[string]string{"Idempotency-Key": "build-main"})
	var retried domain.Build
	decodeResponse(t, response, &retried)
	if response.Code != http.StatusAccepted || retried.PublicID != build.PublicID {
		t.Fatalf("retry status=%d build=%+v", response.Code, retried)
	}

	claimed, ok, err := storage.ClaimNextBuild(ctx, "integration-builder", time.Minute)
	if err != nil || !ok {
		t.Fatalf("claim ok=%v err=%v", ok, err)
	}
	image := "registry.example/molejo/apps/" + app.PublicID + "@sha256:" + strings.Repeat("a", 64)
	release, err := storage.CompleteBuild(ctx, claimed, mustAPIID(t, "rel"), image)
	if err != nil {
		t.Fatal(err)
	}

	intent := `{"name":"api","environmentId":"` + environment.PublicID + `","replicas":1,"port":8080,"resources":{"requests":{"cpuMillis":50,"memoryMiB":64},"limits":{"cpuMillis":250,"memoryMiB":128}},"probes":{"liveness":{"path":"/healthz"},"readiness":{"path":"/readyz"}},"exposure":"Private"}`
	response = hierarchyRequest(t, server, owner, http.MethodPost, base+"/releases/"+release.PublicID+"/deployments", intent, map[string]string{"Idempotency-Key": "deploy-release"})
	if response.Code != http.StatusAccepted {
		t.Fatalf("release deployment status=%d body=%s", response.Code, response.Body.String())
	}
	var accepted struct {
		Deployment domain.Deployment `json:"deployment"`
	}
	decodeResponse(t, response, &accepted)
	if accepted.Deployment.ReleasePublicID != release.PublicID || accepted.Deployment.Intent.Image != image || accepted.Deployment.AppPublicID != app.PublicID {
		t.Fatalf("deployment=%+v", accepted.Deployment)
	}

	mutated := accepted.Deployment.Intent
	mutated.Image = "registry.example/molejo/apps/other@sha256:" + strings.Repeat("b", 64)
	payload, err := domain.CanonicalJSON(mutated)
	if err != nil {
		t.Fatal(err)
	}
	response = hierarchyRequest(t, server, owner, http.MethodPut, "/api/v1/workspaces/"+workspace.PublicID+"/deployments/"+accepted.Deployment.PublicID, string(payload), map[string]string{"Idempotency-Key": "mutate-release", "If-Match": "1"})
	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), "deployment_release_immutable") {
		t.Fatalf("release mutation status=%d body=%s", response.Code, response.Body.String())
	}
}

func mustAPIID(t *testing.T, prefix string) string {
	t.Helper()
	value, err := domain.NewPublicID(prefix)
	if err != nil {
		t.Fatal(err)
	}
	return value
}
