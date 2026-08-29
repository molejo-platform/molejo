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
	target, err := storage.CreateAppEnvironment(ctx, workspaceID, actorID, newID(t, "aev"), project.PublicID, app.PublicID, environment.PublicID, "develop", integrationConfiguration("build-target"))
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

func TestBuildRetentionKeepsOnlyThreeFreshReleasesPerAppEnvironment(t *testing.T) {
	ctx := context.Background()
	storage, workspaceID, actorID := newIntegrationFixture(t)
	project, app, environment := createHierarchy(t, storage, workspaceID)
	target, err := storage.CreateAppEnvironment(ctx, workspaceID, actorID, newID(t, "aev"), project.PublicID, app.PublicID, environment.PublicID, "main", integrationConfiguration("retention-target"))
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
	for index, character := range []string{"a", "b", "c", "d"} {
		build, reused, createErr := storage.CreateBuild(ctx, workspaceID, actorID, newID(t, "bld"), project.PublicID, app.PublicID, target.PublicID, "main", strings.Repeat(character, 40), domain.SHA256([]byte("retention-key-"+character)), domain.SHA256([]byte("retention-payload-"+character)))
		if createErr != nil || reused {
			t.Fatalf("build %d=%+v reused=%v err=%v", index, build, reused, createErr)
		}
		claimed, ok, claimErr := storage.ClaimNextBuild(ctx, "retention-worker", time.Minute)
		if claimErr != nil || !ok || claimed.ID != build.ID {
			t.Fatalf("claim %d=%+v ok=%v err=%v", index, claimed, ok, claimErr)
		}
		image := "registry.example/molejo/apps/" + app.PublicID + "@sha256:" + strings.Repeat(character, 64)
		if _, completeErr := storage.CompleteBuild(ctx, claimed, newID(t, "rel"), image); completeErr != nil {
			t.Fatalf("complete build %d: %v", index, completeErr)
		}
	}

	releases, _, err := storage.ListReleases(ctx, workspaceID, project.PublicID, app.PublicID, 1<<62, 20)
	if err != nil {
		t.Fatal(err)
	}
	available, expired := 0, 0
	for _, release := range releases {
		if release.AppEnvironmentPublicID != target.PublicID {
			t.Fatalf("release crossed App Environment boundary: %+v", release)
		}
		switch release.AvailabilityStatus {
		case domain.ReleaseAvailable:
			available++
		case domain.ReleaseExpired:
			expired++
		}
	}
	if available != 3 || expired != 1 {
		t.Fatalf("available=%d expired=%d, want three fresh releases and one expired", available, expired)
	}
	var blocked int
	if err = storage.Pool.QueryRow(ctx, `SELECT count(*) FROM release_gc_candidates WHERE status='Blocked'`).Scan(&blocked); err != nil || blocked != 1 {
		t.Fatalf("blocked GC candidates=%d err=%v, want one until registry deletion is enabled", blocked, err)
	}
}
