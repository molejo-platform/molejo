package api

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/fruto-platform/fruto/services/control-plane-api/internal/domain"
)

func TestBuildAPIUsesTheAppEnvironmentBranchAndDeploysAReleaseSnapshot(t *testing.T) {
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
	target, err := storage.CreateAppEnvironment(ctx, workspaceID, ownerID, mustAPIID(t, "aev"), project.PublicID, app.PublicID, environment.PublicID, "develop", apiRuntimeConfiguration("api-production"))
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
	body := `{"appEnvironmentId":"` + target.PublicID + `"}`

	response := hierarchyRequest(t, server, owner, http.MethodPost, base+"/builds", body, map[string]string{"Idempotency-Key": "build-target"})
	if response.Code != http.StatusAccepted {
		t.Fatalf("create build status=%d body=%s", response.Code, response.Body.String())
	}
	var build domain.Build
	decodeResponse(t, response, &build)
	if build.AppEnvironmentPublicID != target.PublicID || build.SourceBranch != "develop" || build.CommitSHA != commitSHA || build.RepositoryFullName != "molejo/platform" {
		t.Fatalf("build=%+v", build)
	}
	if refs := server.GitHub.(*fakeGitHubService).resolvedRefs; len(refs) != 1 || refs[0] != "develop" {
		t.Fatalf("resolved refs=%v", refs)
	}
	response = hierarchyRequest(t, server, owner, http.MethodPost, base+"/builds", body, map[string]string{"Idempotency-Key": "build-target"})
	var retried domain.Build
	decodeResponse(t, response, &retried)
	if response.Code != http.StatusAccepted || retried.PublicID != build.PublicID || len(server.GitHub.(*fakeGitHubService).resolvedRefs) != 1 {
		t.Fatalf("idempotent retry status=%d build=%+v refs=%v", response.Code, retried, server.GitHub.(*fakeGitHubService).resolvedRefs)
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
	response = hierarchyRequest(t, server, owner, http.MethodGet, base+"/environments/"+target.PublicID+"/configuration-versions", "", nil)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"version":1`) {
		t.Fatalf("configuration versions status=%d body=%s", response.Code, response.Body.String())
	}
	response = hierarchyRequest(t, server, owner, http.MethodPost, base+"/environments/"+target.PublicID+"/deployment-preview", `{"releaseId":"`+release.PublicID+`","configurationVersion":1}`, nil)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"InitialDeployment"`) {
		t.Fatalf("deployment preview status=%d body=%s", response.Code, response.Body.String())
	}
	response = hierarchyRequest(t, server, owner, http.MethodPost, base+"/environments/"+target.PublicID+"/deployments", `{"releaseId":"`+release.PublicID+`","configurationVersion":1,"currentDeploymentId":null}`, map[string]string{"Idempotency-Key": "deploy-release", "If-Match": "1"})
	if response.Code != http.StatusAccepted {
		t.Fatalf("release deployment status=%d body=%s", response.Code, response.Body.String())
	}
	var accepted struct {
		Deployment domain.Deployment `json:"deployment"`
	}
	decodeResponse(t, response, &accepted)
	if accepted.Deployment.ReleasePublicID != release.PublicID || accepted.Deployment.AppEnvironmentPublicID != target.PublicID || accepted.Deployment.Configuration.Slug != "api-production" {
		t.Fatalf("deployment=%+v", accepted.Deployment)
	}
}

func apiRuntimeConfiguration(slug string) domain.RuntimeConfig {
	return domain.RuntimeConfig{
		Replicas: 1,
		Port:     8080,
		Resources: domain.Resources{
			Requests: domain.ResourceValues{CPUMillis: 50, MemoryMiB: 64},
			Limits:   domain.ResourceValues{CPUMillis: 250, MemoryMiB: 128},
		},
		Probes:    domain.Probes{Liveness: domain.Probe{Path: "/healthz"}, Readiness: domain.Probe{Path: "/readyz"}},
		Exposure:  domain.ExposurePrivate,
		Slug:      slug,
		Variables: []domain.Variable{{Name: "APP_MODE", Value: "production"}},
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
