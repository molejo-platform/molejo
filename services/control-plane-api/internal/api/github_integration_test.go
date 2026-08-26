package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/fruto-platform/fruto/services/control-plane-api/internal/domain"
	"github.com/fruto-platform/fruto/services/control-plane-api/internal/githubapp"
)

func TestGitHubConnectionRequiresOwnerBrowserStateAndUserInstallationAccess(t *testing.T) {
	storage, workspaceID, ownerID, _ := newExecutorIntegrationFixture(t)
	workspace, err := storage.Workspace(context.Background(), workspaceID)
	if err != nil {
		t.Fatal(err)
	}
	config := DefaultConfig()
	config.PublicURL = "https://console.example"
	config.AllowedOrigin = "https://console.example"
	config.AllowedHosts = []string{"console.example"}
	server := NewServer(storage, nil, config, nil)
	server.GitHub = &fakeGitHubService{installation: githubapp.Installation{ID: 42, AccountID: 7, AccountLogin: "molejo", AccountType: "Organization", RepositorySelection: "selected"}, userAllowed: true}
	owner := createAPISession(t, storage, ownerID, "github-owner-session", "github-owner-csrf")

	connect := hierarchyRequest(t, server, owner, http.MethodPost, "/api/v1/workspaces/"+workspace.PublicID+"/github/installations/connect", "", nil)
	if connect.Code != http.StatusOK {
		t.Fatalf("connect status=%d body=%s", connect.Code, connect.Body.String())
	}
	var connected struct {
		AuthorizationURL string `json:"authorizationUrl"`
	}
	decodeResponse(t, connect, &connected)
	installURL, err := url.Parse(connected.AuthorizationURL)
	if err != nil {
		t.Fatal(err)
	}
	browserCookie := connect.Result().Cookies()[0]

	spoofed := callbackRequest(server, "/api/v1/github/installations/callback?installation_id=42&state="+url.QueryEscape(installURL.Query().Get("state")), nil)
	if spoofed.Code != http.StatusBadRequest {
		t.Fatalf("callback without browser state status=%d body=%s", spoofed.Code, spoofed.Body.String())
	}

	setup := callbackRequest(server, "/api/v1/github/installations/callback?installation_id=42&state="+url.QueryEscape(installURL.Query().Get("state")), browserCookie)
	if setup.Code != http.StatusFound {
		t.Fatalf("setup status=%d body=%s", setup.Code, setup.Body.String())
	}
	authorizationURL, err := url.Parse(setup.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	callback := callbackRequest(server, "/api/v1/github/callback?code=one-time-code&state="+url.QueryEscape(authorizationURL.Query().Get("state")), browserCookie)
	if callback.Code != http.StatusFound || callback.Header().Get("Location") != "https://console.example/admin?github=connected" {
		t.Fatalf("authorization status=%d location=%q body=%s", callback.Code, callback.Header().Get("Location"), callback.Body.String())
	}
	installations, err := storage.ListGitHubInstallations(context.Background(), workspaceID)
	if err != nil || len(installations) != 1 || installations[0].ExternalID != 42 {
		t.Fatalf("installations=%+v err=%v", installations, err)
	}

	testerID := insertTester(t, storage, workspaceID)
	tester := createAPISession(t, storage, testerID, "github-tester-session", "github-tester-csrf")
	forbidden := hierarchyRequest(t, server, tester, http.MethodPost, "/api/v1/workspaces/"+workspace.PublicID+"/github/installations/connect", "", nil)
	if forbidden.Code != http.StatusForbidden {
		t.Fatalf("tester connect status=%d body=%s", forbidden.Code, forbidden.Body.String())
	}
}

type fakeGitHubService struct {
	installation githubapp.Installation
	userAllowed  bool
}

func (f *fakeGitHubService) InstallationURL(state string) string {
	return "https://github.example/apps/molejo/installations/new?state=" + url.QueryEscape(state)
}

func (f *fakeGitHubService) UserAuthorizationURL(state string) string {
	return "https://github.example/login/oauth/authorize?state=" + url.QueryEscape(state)
}

func (f *fakeGitHubService) Installation(context.Context, int64) (githubapp.Installation, error) {
	return f.installation, nil
}

func (f *fakeGitHubService) UserCanAccessInstallation(context.Context, string, int64) (bool, error) {
	return f.userAllowed, nil
}

func (f *fakeGitHubService) Repositories(context.Context, int64) ([]domain.GitHubRepository, error) {
	return []domain.GitHubRepository{{ID: "99", Name: "platform", FullName: "molejo/platform", DefaultBranch: "main"}}, nil
}

func (f *fakeGitHubService) DeleteInstallation(context.Context, int64) error { return nil }

func callbackRequest(server *Server, path string, cookie *http.Cookie) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodGet, path, nil)
	request.Host = "console.example"
	if cookie != nil {
		request.AddCookie(cookie)
	}
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	return response
}
