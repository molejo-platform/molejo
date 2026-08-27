package store

import (
	"context"
	"errors"
	"testing"

	"github.com/fruto-platform/fruto/services/control-plane-api/internal/domain"
)

func TestGitHubRepositoryCanBeSharedByAppsButInstallationCannotBeDisconnectedWhileInUse(t *testing.T) {
	ctx := context.Background()
	storage, workspaceID, actorID := newIntegrationFixture(t)
	project, err := storage.CreateProject(ctx, workspaceID, mustPublicID(t, "prj"), "Platform", "platform")
	if err != nil {
		t.Fatal(err)
	}
	first, err := storage.CreateApp(ctx, workspaceID, project.PublicID, mustPublicID(t, "app"), "API", "api")
	if err != nil {
		t.Fatal(err)
	}
	second, err := storage.CreateApp(ctx, workspaceID, project.PublicID, mustPublicID(t, "app"), "Worker", "worker")
	if err != nil {
		t.Fatal(err)
	}
	installation, err := storage.ConnectGitHubInstallation(ctx, mustPublicID(t, "ghi"), workspaceID, actorID, 4242, 7, "molejo", "Organization", "selected")
	if err != nil {
		t.Fatal(err)
	}
	repository := domain.GitHubRepository{ID: "99", Name: "platform", FullName: "molejo/platform", Private: true, DefaultBranch: "main"}
	for _, app := range []domain.App{first, second} {
		if _, err = storage.SetAppGitHubSource(ctx, workspaceID, project.PublicID, app.PublicID, installation.PublicID, repository, "develop"); err != nil {
			t.Fatalf("set source for %s: %v", app.PublicID, err)
		}
	}
	for _, app := range []domain.App{first, second} {
		source, getErr := storage.GetAppGitHubSource(ctx, workspaceID, project.PublicID, app.PublicID)
		if getErr != nil || source == nil || source.Repository.ID != repository.ID || source.PrimaryBranch != "develop" || source.Repository.DefaultBranch != "main" {
			t.Fatalf("source for %s=%+v err=%v", app.PublicID, source, getErr)
		}
	}
	if err = storage.DeleteGitHubInstallation(ctx, workspaceID, installation.PublicID); !errors.Is(err, ErrDependencyConflict) {
		t.Fatalf("disconnect in-use installation err=%v", err)
	}
	if err = storage.ClearAppGitHubSource(ctx, workspaceID, project.PublicID, first.PublicID); err != nil {
		t.Fatal(err)
	}
	if err = storage.ClearAppGitHubSource(ctx, workspaceID, project.PublicID, second.PublicID); err != nil {
		t.Fatal(err)
	}
	if err = storage.DeleteGitHubInstallation(ctx, workspaceID, installation.PublicID); err != nil {
		t.Fatal(err)
	}
}
