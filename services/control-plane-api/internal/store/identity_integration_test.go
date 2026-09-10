package store

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/molejo-platform/molejo/services/control-plane-api/internal/audit"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/auth"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/authorization"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/identity"
)

func TestInvitationCreatesCredentialOnlyWhenAccepted(t *testing.T) {
	ctx := context.Background()
	storage, workspaceID, administratorID := newIntegrationFixture(t)
	invitationToken := "a-high-entropy-invitation-token"
	invitation := UserInvitation{PublicID: newID(t, "uin"), TokenHash: auth.HashToken(invitationToken), ExpiresAt: time.Now().Add(time.Hour)}
	created, err := storage.CreateInvitedUser(ctx, identity.User{PublicID: newID(t, "usr"), Username: "invited.user", DisplayName: "Invited User"}, false, invitation, administratorID, identityAudit(t, administratorID, workspaceID, "identity.user.create"))
	if err != nil {
		t.Fatal(err)
	}
	if created.Status != identity.StatusInvited || created.InstallationAdministrator {
		t.Fatalf("unexpected invited user: %+v", created)
	}
	if _, _, err = storage.AuthenticateUser(ctx, created.Username); err == nil {
		t.Fatal("invited user already had a password credential")
	}
	passwordHash, err := auth.HashPassword("a durable invitation password")
	if err != nil {
		t.Fatal(err)
	}
	activated, err := storage.AcceptUserInvitation(ctx, invitation.TokenHash, passwordHash, identityAudit(t, administratorID, workspaceID, "identity.user.invitation.accept"))
	if err != nil || activated.Status != identity.StatusActive {
		t.Fatalf("activated=%+v err=%v", activated, err)
	}
	if _, hash, authenticateErr := storage.AuthenticateUser(ctx, created.Username); authenticateErr != nil || !auth.VerifyPassword("a durable invitation password", hash) {
		t.Fatalf("accepted credential is invalid: %v", authenticateErr)
	}
	if _, err = storage.AcceptUserInvitation(ctx, invitation.TokenHash, passwordHash, identityAudit(t, administratorID, workspaceID, "identity.user.invitation.accept")); !errors.Is(err, ErrInvitationInvalid) {
		t.Fatalf("accepted invitation was reusable: %v", err)
	}
}

func TestConcurrentUserDeactivationPreservesActiveGovernance(t *testing.T) {
	ctx := context.Background()
	storage, workspaceID, firstID := newIntegrationFixture(t)
	first, err := storage.User(ctx, firstID)
	if err != nil {
		t.Fatal(err)
	}
	passwordHash, err := auth.HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	second, err := storage.CreateUser(ctx, identity.User{PublicID: newID(t, "usr"), Username: "second.owner", DisplayName: "Second Owner"}, passwordHash, true, identityAudit(t, firstID, workspaceID, "identity.user.create"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = storage.PutWorkspaceMembership(ctx, workspaceID, second.PublicID, authorization.RoleOwner, "Active", nil, identityAudit(t, firstID, workspaceID, "authorization.membership.put")); err != nil {
		t.Fatal(err)
	}

	start := make(chan struct{})
	errorsByUser := make(chan error, 2)
	events := map[int64]audit.Event{
		first.ID:  identityAudit(t, first.ID, workspaceID, "identity.user.status.update"),
		second.ID: identityAudit(t, second.ID, workspaceID, "identity.user.status.update"),
	}
	var wait sync.WaitGroup
	for _, user := range []identity.User{first, second} {
		user := user
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			errorsByUser <- func() error {
				_, updateErr := storage.SetUserStatus(ctx, user.PublicID, identity.StatusDisabled, user.Version, events[user.ID])
				return updateErr
			}()
		}()
	}
	close(start)
	wait.Wait()
	close(errorsByUser)
	succeeded, protected := 0, 0
	for updateErr := range errorsByUser {
		switch {
		case updateErr == nil:
			succeeded++
		case errors.Is(updateErr, ErrGovernanceInvariant):
			protected++
		default:
			t.Fatalf("unexpected concurrent update error: %v", updateErr)
		}
	}
	if succeeded != 1 || protected != 1 {
		t.Fatalf("succeeded=%d protected=%d", succeeded, protected)
	}
	var administrators, owners int
	if err = storage.Pool.QueryRow(ctx, `SELECT count(*) FROM installation_role_assignments r JOIN users u ON u.id=r.user_id WHERE r.role='Administrator' AND u.status='Active'`).Scan(&administrators); err != nil {
		t.Fatal(err)
	}
	if err = storage.Pool.QueryRow(ctx, `SELECT count(*) FROM workspace_memberships wm JOIN users u ON u.id=wm.user_id WHERE wm.workspace_id=$1 AND wm.role='Owner' AND wm.status='Active' AND u.status='Active'`, workspaceID).Scan(&owners); err != nil {
		t.Fatal(err)
	}
	if administrators != 1 || owners != 1 {
		t.Fatalf("administrators=%d owners=%d", administrators, owners)
	}
}

func TestFinalAdministratorAndWorkspaceOwnerCannotBeRemoved(t *testing.T) {
	ctx := context.Background()
	storage, workspaceID, administratorID := newIntegrationFixture(t)
	administrator, err := storage.User(ctx, administratorID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = storage.SetInstallationAdministrator(ctx, administrator.PublicID, false, administrator.Version, identityAudit(t, administratorID, workspaceID, "authorization.installation_role.update")); !errors.Is(err, ErrGovernanceInvariant) {
		t.Fatalf("final installation administrator was removable: %v", err)
	}

	var membershipVersion int64
	if err = storage.Pool.QueryRow(ctx, `SELECT version FROM workspace_memberships WHERE workspace_id=$1 AND user_id=$2`, workspaceID, administratorID).Scan(&membershipVersion); err != nil {
		t.Fatal(err)
	}
	if _, err = storage.PutWorkspaceMembership(ctx, workspaceID, administrator.PublicID, authorization.RoleMember, "Active", &membershipVersion, identityAudit(t, administratorID, workspaceID, "authorization.membership.put")); !errors.Is(err, ErrGovernanceInvariant) {
		t.Fatalf("final workspace Owner was demotable: %v", err)
	}
	if err = storage.DeleteWorkspaceMembership(ctx, workspaceID, administrator.PublicID, identityAudit(t, administratorID, workspaceID, "authorization.membership.delete")); !errors.Is(err, ErrGovernanceInvariant) {
		t.Fatalf("final workspace Owner was removable: %v", err)
	}
}

func TestBootstrapCreatesInstallationAdministratorAndWorkspaceOwner(t *testing.T) {
	ctx := context.Background()
	storage, workspaceID, administratorID := newIntegrationFixture(t)
	var workspacePublicID string
	if err := storage.Pool.QueryRow(ctx, `SELECT public_id FROM workspaces WHERE id=$1`, workspaceID).Scan(&workspacePublicID); err != nil {
		t.Fatal(err)
	}
	roles, err := storage.UserWorkspaceRoles(ctx, administratorID)
	if err != nil {
		t.Fatal(err)
	}
	if roles[workspacePublicID] != authorization.RoleOwner {
		t.Fatalf("bootstrap role=%q, want %q", roles[workspacePublicID], authorization.RoleOwner)
	}
	administrator, err := storage.IsInstallationAdministrator(ctx, administratorID)
	if err != nil || !administrator {
		t.Fatalf("installation administrator=%t err=%v", administrator, err)
	}
}

func TestIdentityLifecycleRevokesSessionsAndKeepsAuditImmutable(t *testing.T) {
	ctx := context.Background()
	storage, workspaceID, administratorID := newIntegrationFixture(t)
	passwordHash, err := auth.HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	created, err := storage.CreateUser(ctx, identity.User{PublicID: newID(t, "usr"), Username: "integration.user", DisplayName: "Integration User"}, passwordHash, false, identityAudit(t, administratorID, workspaceID, "identity.user.create"))
	if err != nil {
		t.Fatal(err)
	}
	membership, err := storage.PutWorkspaceMembership(ctx, workspaceID, created.PublicID, authorization.RoleMember, "Active", nil, identityAudit(t, administratorID, workspaceID, "authorization.membership.put"))
	if err != nil || membership.Role != authorization.RoleMember {
		t.Fatalf("membership=%+v err=%v", membership, err)
	}

	tokenHash := auth.HashToken("identity-session")
	if err = storage.CreateUserSession(ctx, newID(t, "ses"), created, tokenHash, auth.HashToken("csrf"), "AAL1", time.Now().Add(time.Hour), time.Now().Add(12*time.Hour), identityAudit(t, created.ID, workspaceID, "authentication.login")); err != nil {
		t.Fatal(err)
	}
	if _, err = storage.UserSession(ctx, tokenHash, time.Hour); err != nil {
		t.Fatalf("new session is invalid: %v", err)
	}
	newHash, err := auth.HashPassword("a different durable passphrase")
	if err != nil {
		t.Fatal(err)
	}
	if err = storage.ChangePassword(ctx, created.ID, newHash, identityAudit(t, created.ID, workspaceID, "identity.password.change")); err != nil {
		t.Fatal(err)
	}
	if _, err = storage.UserSession(ctx, tokenHash, time.Hour); !errors.Is(err, ErrSessionInvalid) {
		t.Fatalf("session survived password change: %v", err)
	}

	items, _, err := storage.ListAuditEvents(ctx, workspaceID, 0, 20)
	if err != nil || len(items) < 4 {
		t.Fatalf("audit events=%+v err=%v", items, err)
	}
	if _, err = storage.Pool.Exec(ctx, `UPDATE audit_events SET reason='tampered' WHERE workspace_id=$1`, workspaceID); err == nil {
		t.Fatal("append-only audit event was updated")
	}
}

func TestWorkspaceGroupsGrantRelationsOnlyToWorkspaceMembers(t *testing.T) {
	ctx := context.Background()
	storage, workspaceID, administratorID := newIntegrationFixture(t)
	passwordHash, err := auth.HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	created, err := storage.CreateUser(ctx, identity.User{PublicID: newID(t, "usr"), Username: "group.member", DisplayName: "Group Member"}, passwordHash, false, identityAudit(t, administratorID, workspaceID, "identity.user.create"))
	if err != nil {
		t.Fatal(err)
	}
	group, err := storage.CreateWorkspaceGroup(ctx, Group{PublicID: newID(t, "grp"), WorkspaceID: workspaceID, Name: "Deployers"}, "deployers", identityAudit(t, administratorID, workspaceID, "authorization.group.create"))
	if err != nil {
		t.Fatal(err)
	}
	if err = storage.AddWorkspaceGroupMember(ctx, workspaceID, group.PublicID, created.PublicID, identityAudit(t, administratorID, workspaceID, "authorization.group.member.add")); err == nil {
		t.Fatal("non-member user was added to a workspace group")
	}
	if _, err = storage.PutWorkspaceMembership(ctx, workspaceID, created.PublicID, authorization.RoleViewer, "Active", nil, identityAudit(t, administratorID, workspaceID, "authorization.membership.put")); err != nil {
		t.Fatal(err)
	}
	if err = storage.AddWorkspaceGroupMember(ctx, workspaceID, group.PublicID, created.PublicID, identityAudit(t, administratorID, workspaceID, "authorization.group.member.add")); err != nil {
		t.Fatal(err)
	}
	groupMembers, err := storage.ListWorkspaceGroupMembers(ctx, workspaceID, group.PublicID)
	if err != nil || len(groupMembers) != 1 || groupMembers[0].UserPublicID != created.PublicID {
		t.Fatalf("group members=%+v err=%v", groupMembers, err)
	}
	var appEnvironmentID string
	if err = storage.Pool.QueryRow(ctx, `SELECT public_id FROM app_environments WHERE workspace_id=$1 ORDER BY id LIMIT 1`, workspaceID).Scan(&appEnvironmentID); err != nil {
		project, app, environment := createHierarchy(t, storage, workspaceID)
		target, createErr := storage.CreateAppEnvironment(ctx, workspaceID, administratorID, newID(t, "aev"), project.PublicID, app.PublicID, environment.PublicID, "main", integrationConfiguration("rebac-target"))
		if createErr != nil {
			t.Fatal(createErr)
		}
		appEnvironmentID = target.PublicID
	}
	grant, err := storage.CreateWorkspaceAccessGrant(ctx, AccessGrant{PublicID: newID(t, "agr"), WorkspaceID: workspaceID, SubjectType: "Group", SubjectPublicID: group.PublicID, ResourceType: "AppEnvironment", ResourcePublicID: appEnvironmentID, Relation: string(authorization.RelationDeployer)}, administratorID, identityAudit(t, administratorID, workspaceID, "authorization.access_grant.create"))
	if err != nil {
		t.Fatal(err)
	}
	grants, err := storage.ListWorkspaceAccessGrants(ctx, workspaceID)
	if err != nil || len(grants) != 1 || grants[0].PublicID != grant.PublicID {
		t.Fatalf("access grants=%+v err=%v", grants, err)
	}
	context, err := storage.AuthorizationContext(ctx, created.ID, workspaceID, "AppEnvironment", appEnvironmentID)
	if err != nil || !authorization.Allowed(context, authorization.Deploy) || authorization.Allowed(context, authorization.ManageMembers) {
		t.Fatalf("authorization context=%+v err=%v", context, err)
	}
}

func TestPasswordResetGrantIsSingleUseAndRevokesExistingSessions(t *testing.T) {
	ctx := context.Background()
	storage, workspaceID, administratorID := newIntegrationFixture(t)
	passwordHash, err := auth.HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	created, err := storage.CreateUser(ctx, identity.User{PublicID: newID(t, "usr"), Username: "reset.user", DisplayName: "Reset User"}, passwordHash, false, identityAudit(t, administratorID, workspaceID, "identity.user.create"))
	if err != nil {
		t.Fatal(err)
	}
	tokenHash := auth.HashToken("reset-session")
	if err = storage.CreateUserSession(ctx, newID(t, "ses"), created, tokenHash, auth.HashToken("csrf"), "AAL1", time.Now().Add(time.Hour), time.Now().Add(time.Hour), identityAudit(t, created.ID, workspaceID, "authentication.login")); err != nil {
		t.Fatal(err)
	}
	key := []byte("01234567890123456789012345678901")
	code := "ABCD-EFGH-JK23"
	grantID := newID(t, "prg")
	grant := PasswordResetGrant{PublicID: grantID, UserID: created.ID, CodeHash: auth.HashResetCode(key, code), ExpiresAt: time.Now().Add(time.Minute)}
	if err = storage.CreatePasswordResetGrant(ctx, grant, administratorID, identityAudit(t, administratorID, workspaceID, "identity.password_reset.grant.create")); err != nil {
		t.Fatal(err)
	}
	if _, _, err = storage.VerifyPasswordResetGrant(ctx, created.Username, auth.HashResetCode(key, "WRNG-CODE-0000")); !errors.Is(err, ErrResetCodeInvalid) {
		t.Fatalf("wrong reset code error=%v", err)
	}
	_, ticket, err := storage.VerifyPasswordResetGrant(ctx, created.Username, auth.HashResetCode(key, code))
	if err != nil || ticket != grantID {
		t.Fatalf("verified ticket=%q err=%v", ticket, err)
	}
	if _, _, err = storage.VerifyPasswordResetGrant(ctx, created.Username, auth.HashResetCode(key, code)); !errors.Is(err, ErrNotFound) {
		t.Fatalf("verified reset code was reusable: %v", err)
	}
	newHash, err := auth.HashPassword("a replacement durable passphrase")
	if err != nil {
		t.Fatal(err)
	}
	if err = storage.CompletePasswordReset(ctx, created.Username, ticket, newHash, identityAudit(t, created.ID, workspaceID, "identity.password_reset.complete")); err != nil {
		t.Fatal(err)
	}
	if err = storage.CompletePasswordReset(ctx, created.Username, ticket, newHash, identityAudit(t, created.ID, workspaceID, "identity.password_reset.complete")); !errors.Is(err, ErrResetCodeInvalid) {
		t.Fatalf("reset ticket was reusable: %v", err)
	}
	if _, err = storage.UserSession(ctx, tokenHash, time.Hour); !errors.Is(err, ErrSessionInvalid) {
		t.Fatalf("session survived password reset: %v", err)
	}
}

func TestMFAChallengesPreventTOTPReplayAndConsumeRecoveryCodesOnce(t *testing.T) {
	ctx := context.Background()
	storage, workspaceID, administratorID := newIntegrationFixture(t)
	passwordHash, err := auth.HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	created, err := storage.CreateUser(ctx, identity.User{PublicID: newID(t, "usr"), Username: "mfa.user", DisplayName: "MFA User"}, passwordHash, false, identityAudit(t, administratorID, workspaceID, "identity.user.create"))
	if err != nil {
		t.Fatal(err)
	}
	enrollmentToken := "enrollment-token"
	if err = storage.CreateAuthenticationChallenge(ctx, auth.HashToken(enrollmentToken), created.ID, "TOTPEnrollment", map[string]any{"secretReference": "identity/totp/test", "secretVersion": int64(1)}, time.Now().Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	enrollment, err := storage.AuthenticationChallenge(ctx, auth.HashToken(enrollmentToken), "TOTPEnrollment")
	if err != nil {
		t.Fatal(err)
	}
	recoveryKey := []byte("01234567890123456789012345678901")
	recoveryCode := "ABCD-EFGH-JK23"
	if err = storage.EnableTOTP(ctx, enrollment.ID, created.ID, "identity/totp/test", 1, [][]byte{auth.HashResetCode(recoveryKey, recoveryCode)}, identityAudit(t, created.ID, workspaceID, "authentication.totp.enable")); err != nil {
		t.Fatal(err)
	}

	loginToken := "login-token"
	if err = storage.CreateAuthenticationChallenge(ctx, auth.HashToken(loginToken), created.ID, "Login", nil, time.Now().Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	login, err := storage.AuthenticationChallenge(ctx, auth.HashToken(loginToken), "Login")
	if err != nil {
		t.Fatal(err)
	}
	step := time.Now().Unix() / 30
	if err = storage.CompleteTOTPChallenge(ctx, login.ID, created.ID, step, identityAudit(t, created.ID, workspaceID, "authentication.mfa.complete")); err != nil {
		t.Fatal(err)
	}
	if _, err = storage.AuthenticationChallenge(ctx, auth.HashToken(loginToken), "Login"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("consumed login challenge remained valid: %v", err)
	}

	replayToken := "replay-token"
	if err = storage.CreateAuthenticationChallenge(ctx, auth.HashToken(replayToken), created.ID, "Login", nil, time.Now().Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	replay, err := storage.AuthenticationChallenge(ctx, auth.HashToken(replayToken), "Login")
	if err != nil {
		t.Fatal(err)
	}
	if err = storage.CompleteTOTPChallenge(ctx, replay.ID, created.ID, step, identityAudit(t, created.ID, workspaceID, "authentication.mfa.complete")); !errors.Is(err, ErrConflict) {
		t.Fatalf("same TOTP step was accepted twice: %v", err)
	}

	recoveryToken := "recovery-token"
	if err = storage.CreateAuthenticationChallenge(ctx, auth.HashToken(recoveryToken), created.ID, "Login", nil, time.Now().Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	recovery, err := storage.AuthenticationChallenge(ctx, auth.HashToken(recoveryToken), "Login")
	if err != nil {
		t.Fatal(err)
	}
	if err = storage.CompleteRecoveryChallenge(ctx, recovery.ID, created.ID, auth.HashResetCode(recoveryKey, recoveryCode), identityAudit(t, created.ID, workspaceID, "authentication.mfa.complete")); err != nil {
		t.Fatal(err)
	}
	secondRecoveryToken := "second-recovery-token"
	if err = storage.CreateAuthenticationChallenge(ctx, auth.HashToken(secondRecoveryToken), created.ID, "Login", nil, time.Now().Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	secondRecovery, err := storage.AuthenticationChallenge(ctx, auth.HashToken(secondRecoveryToken), "Login")
	if err != nil {
		t.Fatal(err)
	}
	if err = storage.CompleteRecoveryChallenge(ctx, secondRecovery.ID, created.ID, auth.HashResetCode(recoveryKey, recoveryCode), identityAudit(t, created.ID, workspaceID, "authentication.mfa.complete")); !errors.Is(err, ErrNotFound) {
		t.Fatalf("recovery code was accepted twice: %v", err)
	}
}

func identityAudit(t *testing.T, actorUserID, workspaceID int64, action string) audit.Event {
	t.Helper()
	return audit.Event{
		PublicID:    newID(t, "aud"),
		ActorUserID: &actorUserID,
		WorkspaceID: &workspaceID,
		Action:      action,
		TargetType:  "Test",
		Outcome:     audit.Succeeded,
		Metadata:    map[string]any{"test": true},
	}
}
