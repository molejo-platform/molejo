package store

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/molejo-platform/molejo/services/control-plane-api/internal/domain"
)

func TestGitHubDeliveryFansOutToEveryMatchingAppEnvironmentAndCoalescesPendingPushes(t *testing.T) {
	ctx := context.Background()
	storage, workspaceID, actorID := newIntegrationFixture(t)
	project, app, firstEnvironment := createHierarchy(t, storage, workspaceID)
	secondEnvironment, err := storage.CreateEnvironment(ctx, workspaceID, project.PublicID, newID(t, "env"), "Staging", "staging")
	if err != nil {
		t.Fatal(err)
	}
	first, err := storage.CreateAppEnvironment(ctx, workspaceID, actorID, newID(t, "aev"), project.PublicID, app.PublicID, firstEnvironment.PublicID, "main", integrationConfiguration("delivery-first"))
	if err != nil {
		t.Fatal(err)
	}
	second, err := storage.CreateAppEnvironment(ctx, workspaceID, actorID, newID(t, "aev"), project.PublicID, app.PublicID, secondEnvironment.PublicID, "main", integrationConfiguration("delivery-second"))
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
	for _, target := range []domain.AppEnvironment{first, second} {
		policy, putErr := storage.PutDeliveryPolicy(ctx, workspaceID, actorID, project.PublicID, app.PublicID, target.PublicID, 0, true, true)
		if putErr != nil || policy.Version != 1 {
			t.Fatalf("policy for %s=%+v err=%v", target.PublicID, policy, putErr)
		}
	}

	firstDelivery := acceptDelivery(t, storage, domain.GitHubDelivery{DeliveryID: "delivery-one", EventType: "push", InstallationExternalID: 4242, RepositoryID: 99, RepositoryFullName: "molejo/platform", SourceBranch: "main", SourceRef: "refs/heads/main", CommitSHA: strings.Repeat("a", 40), PayloadHash: domain.SHA256([]byte("delivery-one"))})
	candidates, err := storage.DeliveryCandidates(ctx, firstDelivery, domain.TriggerPush)
	if err != nil || len(candidates) != 2 {
		t.Fatalf("candidates=%+v err=%v, want both App Environments", candidates, err)
	}
	for _, candidate := range candidates {
		target, reused, createErr := storage.CreateDeliveryTargetBuild(ctx, firstDelivery, candidate, domain.CommitMetadata{SHA: firstDelivery.CommitSHA, Title: "First push", AuthorLogin: "octocat"}, domain.TriggerPush, newID(t, "dlt"), newID(t, "bld"))
		if createErr != nil || reused || target.AppEnvironmentPublicID != candidate.AppEnvironmentPublicID {
			t.Fatalf("target=%+v reused=%v err=%v", target, reused, createErr)
		}
	}

	secondDelivery := acceptDelivery(t, storage, domain.GitHubDelivery{DeliveryID: "delivery-two", EventType: "push", InstallationExternalID: 4242, RepositoryID: 99, RepositoryFullName: "molejo/platform", SourceBranch: "main", SourceRef: "refs/heads/main", CommitSHA: strings.Repeat("b", 40), PayloadHash: domain.SHA256([]byte("delivery-two"))})
	candidates, err = storage.DeliveryCandidates(ctx, secondDelivery, domain.TriggerPush)
	if err != nil || len(candidates) != 2 {
		t.Fatalf("second candidates=%+v err=%v", candidates, err)
	}
	for _, candidate := range candidates {
		if _, reused, createErr := storage.CreateDeliveryTargetBuild(ctx, secondDelivery, candidate, domain.CommitMetadata{SHA: secondDelivery.CommitSHA}, domain.TriggerPush, newID(t, "dlt"), newID(t, "bld")); createErr != nil || reused {
			t.Fatalf("second target reused=%v err=%v", reused, createErr)
		}
	}

	var pending, superseded int
	if err = storage.Pool.QueryRow(ctx, `SELECT count(*) FILTER (WHERE status='Pending'),count(*) FILTER (WHERE status='Superseded') FROM builds WHERE app_id=$1`, app.ID).Scan(&pending, &superseded); err != nil {
		t.Fatal(err)
	}
	if pending != 2 || superseded != 2 {
		t.Fatalf("pending=%d superseded=%d, want one fresh pending build per App Environment", pending, superseded)
	}
	releaseDelivery := acceptDelivery(t, storage, domain.GitHubDelivery{DeliveryID: "delivery-release", EventType: "release", Action: "published", InstallationExternalID: 4242, RepositoryID: 99, RepositoryFullName: "molejo/platform", SourceBranch: "main", TagName: "v1.0.0", PayloadHash: domain.SHA256([]byte("delivery-release"))})
	candidates, err = storage.DeliveryCandidates(ctx, releaseDelivery, domain.TriggerRelease)
	if err != nil || len(candidates) != 2 {
		t.Fatalf("release candidates=%+v err=%v", candidates, err)
	}
	for _, candidate := range candidates {
		if _, reused, createErr := storage.CreateDeliveryTargetBuild(ctx, releaseDelivery, candidate, domain.CommitMetadata{SHA: strings.Repeat("c", 40)}, domain.TriggerRelease, newID(t, "dlt"), newID(t, "bld")); createErr != nil || reused {
			t.Fatalf("release target reused=%v err=%v", reused, createErr)
		}
	}
	if err = storage.Pool.QueryRow(ctx, `SELECT count(*) FILTER (WHERE status='Pending'),count(*) FILTER (WHERE status='Superseded') FROM builds WHERE app_id=$1`, app.ID).Scan(&pending, &superseded); err != nil {
		t.Fatal(err)
	}
	if pending != 2 || superseded != 4 {
		t.Fatalf("after release pending=%d superseded=%d, want release to replace queued automatic pushes", pending, superseded)
	}

	wrongBranch := secondDelivery
	wrongBranch.SourceBranch = "develop"
	if candidates, err = storage.DeliveryCandidates(ctx, wrongBranch, domain.TriggerPush); err != nil || len(candidates) != 0 {
		t.Fatalf("wrong branch candidates=%+v err=%v", candidates, err)
	}
	wrongBranch.EventType = "release"
	wrongBranch.Action = "published"
	if candidates, err = storage.DeliveryCandidates(ctx, wrongBranch, domain.TriggerRelease); err != nil || len(candidates) != 0 {
		t.Fatalf("release on wrong branch candidates=%+v err=%v", candidates, err)
	}
}

func TestGitHubDeliveryAndPolicyOptimisticConcurrencyAreIdempotent(t *testing.T) {
	ctx := context.Background()
	storage, workspaceID, actorID := newIntegrationFixture(t)
	project, app, environment := createHierarchy(t, storage, workspaceID)
	target, err := storage.CreateAppEnvironment(ctx, workspaceID, actorID, newID(t, "aev"), project.PublicID, app.PublicID, environment.PublicID, "main", integrationConfiguration("delivery-policy"))
	if err != nil {
		t.Fatal(err)
	}
	policy, err := storage.PutDeliveryPolicy(ctx, workspaceID, actorID, project.PublicID, app.PublicID, target.PublicID, 0, true, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = storage.PutDeliveryPolicy(ctx, workspaceID, actorID, project.PublicID, app.PublicID, target.PublicID, 0, false, true); !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("duplicate initial write err=%v, want version conflict", err)
	}
	updated, err := storage.PutDeliveryPolicy(ctx, workspaceID, actorID, project.PublicID, app.PublicID, target.PublicID, policy.Version, false, true)
	if err != nil || updated.Version != policy.Version+1 {
		t.Fatalf("updated policy=%+v err=%v", updated, err)
	}

	delivery := domain.GitHubDelivery{DeliveryID: "same-delivery", EventType: "push", InstallationExternalID: 1, RepositoryID: 2, RepositoryFullName: "molejo/platform", SourceBranch: "main", CommitSHA: strings.Repeat("a", 40), PayloadHash: domain.SHA256([]byte("same"))}
	stored, duplicate, err := storage.AcceptGitHubDelivery(ctx, delivery)
	if err != nil || duplicate {
		t.Fatalf("first delivery=%+v duplicate=%v err=%v", stored, duplicate, err)
	}
	if _, duplicate, err = storage.AcceptGitHubDelivery(ctx, delivery); err != nil || !duplicate {
		t.Fatalf("replay duplicate=%v err=%v", duplicate, err)
	}
	delivery.PayloadHash = domain.SHA256([]byte("different"))
	if _, _, err = storage.AcceptGitHubDelivery(ctx, delivery); !errors.Is(err, ErrConflict) {
		t.Fatalf("delivery ID reuse err=%v, want conflict", err)
	}
}

func acceptDelivery(t *testing.T, storage *Store, delivery domain.GitHubDelivery) domain.GitHubDelivery {
	t.Helper()
	stored, duplicate, err := storage.AcceptGitHubDelivery(context.Background(), delivery)
	if err != nil || duplicate {
		t.Fatalf("accept delivery=%+v duplicate=%v err=%v", stored, duplicate, err)
	}
	return stored
}
