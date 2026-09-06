package store

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/molejo-platform/molejo/services/control-plane-api/internal/audit"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/auth"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/automation"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/domain"
	releasecontract "github.com/molejo-platform/molejo/services/control-plane-api/internal/release"
)

func TestExternalReleaseAndDeploymentUseScopedServiceAccount(t *testing.T) {
	ctx := context.Background()
	storage, workspaceID, userID := newIntegrationFixture(t)
	project, app, environment := createHierarchy(t, storage, workspaceID)
	target, err := storage.CreateAppEnvironment(ctx, workspaceID, userID, newID(t, "aev"), project.PublicID, app.PublicID, environment.PublicID, "main", integrationConfiguration("external-release"))
	if err != nil {
		t.Fatal(err)
	}
	token := "automation-" + strings.Repeat("a", 64)
	accountID := newID(t, "svc")
	account, err := storage.CreateServiceAccount(ctx, workspaceID, userID, project.PublicID, app.PublicID, accountID, "GitHub Actions", []string{target.PublicID}, audit.Event{
		PublicID: newID(t, "aud"), Action: "service_account.create", TargetType: "ServiceAccount", Outcome: audit.Succeeded,
	})
	if err != nil || account.PublicID != accountID || len(account.DeploymentEnvironmentIDs) != 1 {
		t.Fatalf("account=%+v err=%v", account, err)
	}
	if _, err = storage.CreateServiceAccountToken(ctx, workspaceID, userID, project.PublicID, app.PublicID, accountID, newID(t, "sat"), auth.HashToken(token), time.Now().Add(time.Hour), audit.Event{
		PublicID: newID(t, "aud"), Action: "service_account_token.create", TargetType: "ServiceAccountToken", Outcome: audit.Succeeded,
	}); err != nil {
		t.Fatal(err)
	}
	actor, err := storage.AuthenticateServiceAccount(ctx, auth.HashToken(token))
	if err != nil {
		t.Fatal(err)
	}
	authenticationErrors := make(chan error, 20)
	for range 20 {
		go func() {
			_, authenticateErr := storage.AuthenticateServiceAccount(ctx, auth.HashToken(token))
			authenticationErrors <- authenticateErr
		}()
	}
	for range 20 {
		if authenticateErr := <-authenticationErrors; authenticateErr != nil {
			t.Fatalf("concurrent authentication: %v", authenticateErr)
		}
	}
	workspace, err := storage.AuthorizeServiceAccount(ctx, actor, account.WorkspacePublicID, project.PublicID, app.PublicID, "", automation.PermissionReleaseWrite)
	if err != nil || workspace.ID != workspaceID {
		t.Fatalf("workspace=%+v err=%v", workspace, err)
	}
	if _, err = storage.AuthorizeServiceAccount(ctx, actor, account.WorkspacePublicID, project.PublicID, app.PublicID, newID(t, "aev"), automation.PermissionDeploymentCreate); !errors.Is(err, ErrAutomationAuthorization) {
		t.Fatalf("unexpected deployment authorization error = %v", err)
	}

	command := releasecontract.RegisterCommand{
		Artifact:   releasecontract.Artifact{Kind: releasecontract.ArtifactOCIImage, Reference: "registry.example/molejo/testkit@sha256:" + strings.Repeat("b", 64)},
		Source:     releasecontract.Source{Provider: "GitHub", Repository: "molejo-platform/testkit", Revision: strings.Repeat("c", 40), Ref: "refs/heads/main"},
		Provenance: releasecontract.Provenance{Producer: "github-actions", ExternalRunID: "42", URL: "https://github.com/molejo-platform/testkit/actions/runs/42"},
	}
	idempotencyHash := domain.SHA256([]byte("release-key"))
	payloadHash := domain.SHA256([]byte("release-payload"))
	releaseID := newID(t, "rel")
	registered, replay, err := storage.RegisterExternalRelease(ctx, actor, workspaceID, project.PublicID, app.PublicID, releaseID, command, idempotencyHash, payloadHash, audit.Event{
		PublicID: newID(t, "aud"), Action: "release.register", TargetType: "Release", Outcome: audit.Succeeded,
	})
	if err != nil || replay || registered.Image != command.Artifact.Reference || registered.BuildPublicID != "" || registered.OriginKind != releasecontract.OriginExternal {
		t.Fatalf("release=%+v replay=%v err=%v", registered, replay, err)
	}
	replayed, replay, err := storage.RegisterExternalRelease(ctx, actor, workspaceID, project.PublicID, app.PublicID, newID(t, "rel"), command, idempotencyHash, payloadHash, audit.Event{})
	if err != nil || !replay || replayed.PublicID != registered.PublicID {
		t.Fatalf("replayed=%+v replay=%v err=%v", replayed, replay, err)
	}
	if _, _, err = storage.RegisterExternalRelease(ctx, actor, workspaceID, project.PublicID, app.PublicID, newID(t, "rel"), command, idempotencyHash, domain.SHA256([]byte("different")), audit.Event{}); !errors.Is(err, ErrConflict) {
		t.Fatalf("idempotency conflict error = %v", err)
	}

	deployment, operation, replay, err := storage.CreateDeploymentForPrincipal(ctx, workspaceID, actor, target.PublicID, newID(t, "dpl"), registered.PublicID, target.ConfigurationVersion, target.Version, "", domain.SHA256([]byte("deployment-key")), domain.SHA256([]byte("deployment-payload")), deploymentAudit(t))
	if err != nil || replay || deployment.RequestedBy.DisplayName != "GitHub Actions" || deployment.RequestedBy.ID != accountID || operation.Kind != domain.OperationApplyDeployment {
		t.Fatalf("deployment=%+v operation=%+v replay=%v err=%v", deployment, operation, replay, err)
	}
	claimed, _, claimedDeployment, ok, err := storage.ClaimNext(ctx, "external-worker", time.Minute)
	if err != nil || !ok || claimed.ID != operation.ID || claimedDeployment.ID != deployment.ID {
		t.Fatalf("claimed=%+v deployment=%+v ok=%v err=%v", claimed, claimedDeployment, ok, err)
	}
	if err = storage.RevokeServiceAccount(ctx, workspaceID, userID, project.PublicID, app.PublicID, accountID, audit.Event{
		PublicID: newID(t, "aud"), Action: "service_account.revoke", TargetType: "ServiceAccount", Outcome: audit.Succeeded,
	}); err != nil {
		t.Fatal(err)
	}
	events, _, err := storage.ListAuditEvents(ctx, workspaceID, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	actions := make(map[string]bool, len(events))
	for _, event := range events {
		actions[event.Action] = true
	}
	for _, action := range []string{"service_account.create", "service_account_token.create", "release.register", "deployment.create", "service_account.revoke"} {
		if !actions[action] {
			t.Errorf("workspace audit is missing %q: %+v", action, actions)
		}
	}
}
