package store

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/fruto-platform/fruto/services/control-plane-api/internal/domain"
)

func TestBuildFailureNeverPromotesAReleaseAndRetryCompletesImmutably(t *testing.T) {
	ctx := context.Background()
	storage, workspaceID, actorID := newIntegrationFixture(t)
	project, app, environment := createHierarchy(t, storage, workspaceID)
	target, err := storage.CreateAppEnvironment(ctx, workspaceID, newID(t, "aev"), project.PublicID, app.PublicID, environment.PublicID, "develop", integrationConfiguration("build-target"))
	if err != nil {
		t.Fatal(err)
	}
	installation, err := storage.ConnectGitHubInstallation(ctx, newID(t, "ghi"), workspaceID, actorID, 4242, 7, "molejo", "Organization", "selected")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = storage.SetAppGitHubSource(ctx, workspaceID, project.PublicID, app.PublicID, installation.PublicID, domain.GitHubRepository{ID: "99", Name: "platform", FullName: "molejo/platform", DefaultBranch: "main"}); err != nil {
		t.Fatal(err)
	}
	sha := "0123456789abcdef0123456789abcdef01234567"
	build, reused, err := storage.CreateBuild(ctx, workspaceID, actorID, newID(t, "bld"), project.PublicID, app.PublicID, target.PublicID, target.SourceBranch, sha, domain.SHA256([]byte("build-key")), domain.SHA256([]byte("{}")))
	if err != nil || reused || build.AppEnvironmentPublicID != target.PublicID || build.SourceBranch != "develop" || build.CommitSHA != sha || build.Status != domain.BuildPending {
		t.Fatalf("build=%+v reused=%v err=%v", build, reused, err)
	}
	claimed, ok, err := storage.ClaimNextBuild(ctx, "worker-1", time.Minute)
	if err != nil || !ok || claimed.PublicID != build.PublicID || claimed.Attempts != 1 {
		t.Fatalf("claimed=%+v ok=%v err=%v", claimed, ok, err)
	}
	if err = storage.AppendBuildLog(ctx, claimed, "Dockerfile parse failed"); err != nil {
		t.Fatal(err)
	}
	if err = storage.FailBuild(ctx, claimed, "dockerfile_invalid", "Dockerfile could not be built", true); err != nil {
		t.Fatal(err)
	}
	if releases, _, listErr := storage.ListReleases(ctx, workspaceID, project.PublicID, app.PublicID, 1<<62, 20); listErr != nil || len(releases) != 0 {
		t.Fatalf("failed build releases=%+v err=%v", releases, listErr)
	}
	claimed, ok, err = storage.ClaimNextBuild(ctx, "worker-2", time.Minute)
	if err != nil || !ok || claimed.Attempts != 2 {
		t.Fatalf("retried=%+v ok=%v err=%v", claimed, ok, err)
	}
	image := "registry.example/molejo/apps/" + app.PublicID + "@sha256:" + strings.Repeat("a", 64)
	release, err := storage.CompleteBuild(ctx, claimed, newID(t, "rel"), image)
	if err != nil || release.BuildPublicID != build.PublicID || release.SourceBranch != "develop" || release.Image != image || release.CommitSHA != sha {
		t.Fatalf("release=%+v err=%v", release, err)
	}
	if _, err = storage.CompleteBuild(ctx, claimed, newID(t, "rel"), strings.Replace(image, strings.Repeat("a", 64), strings.Repeat("b", 64), 1)); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale completion err=%v", err)
	}
	logs, err := storage.ListBuildLogs(ctx, workspaceID, build.PublicID)
	if err != nil || len(logs) != 1 || logs[0].Message != "Dockerfile parse failed" {
		t.Fatalf("logs=%+v err=%v", logs, err)
	}
}
