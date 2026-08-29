package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/fruto-platform/fruto/services/control-plane-api/internal/auth"
	"github.com/fruto-platform/fruto/services/control-plane-api/internal/identity"
	"github.com/fruto-platform/fruto/services/control-plane-api/internal/store"
	"github.com/pquerna/otp/totp"
)

func TestIdentityAPICreatesIndependentUserMembershipAndGroup(t *testing.T) {
	storage, workspace, server, owner := newHierarchyAPITestFixture(t)
	server.PasswordResetKey = []byte("01234567890123456789012345678901")

	response := hierarchyRequest(t, server, owner, http.MethodPost, "/api/v1/admin/users", `{"username":"new.user","displayName":"New User","password":"correct horse battery staple","installationAdministrator":false}`, nil)
	if response.Code != http.StatusCreated {
		t.Fatalf("create user status=%d body=%s", response.Code, response.Body.String())
	}
	var user identity.User
	decodeResponse(t, response, &user)

	response = hierarchyRequest(t, server, owner, http.MethodPost, "/api/v1/workspaces/"+workspace.PublicID+"/members", `{"username":"new.user","role":"Viewer","status":"Active"}`, nil)
	if response.Code != http.StatusCreated {
		t.Fatalf("create membership status=%d body=%s", response.Code, response.Body.String())
	}
	response = hierarchyRequest(t, server, owner, http.MethodPost, "/api/v1/workspaces/"+workspace.PublicID+"/groups", `{"name":"Deployers"}`, nil)
	if response.Code != http.StatusCreated {
		t.Fatalf("create group status=%d body=%s", response.Code, response.Body.String())
	}
	var group store.Group
	decodeResponse(t, response, &group)
	response = hierarchyRequest(t, server, owner, http.MethodPut, "/api/v1/workspaces/"+workspace.PublicID+"/groups/"+group.PublicID+"/members/"+user.PublicID, "", nil)
	if response.Code != http.StatusNoContent {
		t.Fatalf("add group member status=%d body=%s", response.Code, response.Body.String())
	}
	response = hierarchyRequest(t, server, owner, http.MethodGet, "/api/v1/workspaces/"+workspace.PublicID+"/groups/"+group.PublicID+"/members", "", nil)
	if response.Code != http.StatusOK {
		t.Fatalf("list group members status=%d body=%s", response.Code, response.Body.String())
	}
	var members struct {
		Items []store.Membership `json:"items"`
	}
	decodeResponse(t, response, &members)
	if len(members.Items) != 1 || members.Items[0].UserPublicID != user.PublicID {
		t.Fatalf("group members=%+v", members.Items)
	}
	response = hierarchyRequest(t, server, owner, http.MethodPost, "/api/v1/workspaces/"+workspace.PublicID+"/access-grants", `{"subjectType":"Group","subjectId":"`+group.PublicID+`","resourceType":"Workspace","resourceId":"`+workspace.PublicID+`","relation":"Viewer"}`, nil)
	if response.Code != http.StatusCreated {
		t.Fatalf("create access grant status=%d body=%s", response.Code, response.Body.String())
	}
	var grant store.AccessGrant
	decodeResponse(t, response, &grant)
	response = hierarchyRequest(t, server, owner, http.MethodGet, "/api/v1/workspaces/"+workspace.PublicID+"/access-grants", "", nil)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), grant.PublicID) {
		t.Fatalf("list access grants status=%d body=%s", response.Code, response.Body.String())
	}

	created, err := storage.FindUser(t.Context(), user.PublicID)
	if err != nil {
		t.Fatal(err)
	}
	viewer := createAPISession(t, storage, created.ID, "identity-viewer-session", "identity-viewer-csrf")
	response = hierarchyRequest(t, server, viewer, http.MethodPost, "/api/v1/workspaces/"+workspace.PublicID+"/groups", `{"name":"Forbidden"}`, nil)
	if response.Code != http.StatusForbidden {
		t.Fatalf("viewer created group status=%d body=%s", response.Code, response.Body.String())
	}
	response = hierarchyRequest(t, server, owner, http.MethodDelete, "/api/v1/workspaces/"+workspace.PublicID+"/access-grants/"+grant.PublicID, "", nil)
	if response.Code != http.StatusNoContent {
		t.Fatalf("delete access grant status=%d body=%s", response.Code, response.Body.String())
	}

	response = hierarchyRequest(t, server, owner, http.MethodGet, "/api/v1/workspaces/"+workspace.PublicID+"/audit-events", "", nil)
	if response.Code != http.StatusOK {
		t.Fatalf("list audit status=%d body=%s", response.Code, response.Body.String())
	}
	var audit struct {
		Items []store.AuditEventView `json:"items"`
	}
	decodeResponse(t, response, &audit)
	if len(audit.Items) < 3 {
		t.Fatalf("audit items=%d", len(audit.Items))
	}
}

func TestTOTPAPIRequiresTheSecondFactorAndIssuesAAL2Session(t *testing.T) {
	storage, _, server, owner := newHierarchyAPITestFixture(t)
	password := "correct horse battery staple"
	passwordHash, err := auth.HashPassword(password)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = storage.Pool.Exec(t.Context(), `UPDATE password_credentials SET password_hash=$1`, passwordHash); err != nil {
		t.Fatal(err)
	}
	backend := &recordingSecretStore{values: map[string]string{}, versions: map[string]int64{}}
	server.AuthenticationSecrets = backend
	server.PasswordResetKey = []byte("01234567890123456789012345678901")
	server.Config.TOTPEnabled = true

	response := hierarchyRequest(t, server, owner, http.MethodPost, "/api/v1/users/me/mfa/totp/enrollment", `{"password":"`+password+`"}`, nil)
	if response.Code != http.StatusCreated {
		t.Fatalf("begin TOTP status=%d body=%s", response.Code, response.Body.String())
	}
	var enrollment struct {
		ChallengeToken string `json:"challengeToken"`
		Secret         string `json:"secret"`
	}
	decodeResponse(t, response, &enrollment)
	code, err := totp.GenerateCode(enrollment.Secret, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	response = hierarchyRequest(t, server, owner, http.MethodPut, "/api/v1/users/me/mfa/totp/enrollment", `{"challengeToken":"`+enrollment.ChallengeToken+`","code":"`+code+`"}`, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("confirm TOTP status=%d body=%s", response.Code, response.Body.String())
	}

	var username string
	if err = storage.Pool.QueryRow(t.Context(), `SELECT username FROM users ORDER BY id LIMIT 1`).Scan(&username); err != nil {
		t.Fatal(err)
	}
	login := unauthenticatedRequest(t, server, http.MethodPost, "/api/v1/session", map[string]string{"username": username, "password": password})
	if login.Code != http.StatusAccepted {
		t.Fatalf("password stage status=%d body=%s", login.Code, login.Body.String())
	}
	var challenge struct {
		ChallengeToken string `json:"challengeToken"`
	}
	decodeResponse(t, login, &challenge)
	code, err = totp.GenerateCode(enrollment.Secret, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	completed := unauthenticatedRequest(t, server, http.MethodPost, "/api/v1/session/mfa/totp", map[string]string{"challengeToken": challenge.ChallengeToken, "code": code})
	if completed.Code != http.StatusOK {
		t.Fatalf("TOTP stage status=%d body=%s", completed.Code, completed.Body.String())
	}
	var session struct {
		AssuranceLevel string `json:"assuranceLevel"`
	}
	decodeResponse(t, completed, &session)
	if session.AssuranceLevel != "AAL2" || completed.Header().Get("Set-Cookie") == "" {
		t.Fatalf("session assurance=%q cookie=%q", session.AssuranceLevel, completed.Header().Get("Set-Cookie"))
	}
}

func unauthenticatedRequest(t *testing.T, server *Server, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(method, path, bytes.NewReader(encoded))
	request.Host = "console.example"
	request.Header.Set("Origin", "https://console.example")
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	return response
}
