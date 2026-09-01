package githubapp

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/molejo-platform/molejo/services/control-plane-api/internal/domain"
)

const apiVersion = "2026-03-10"

var ErrNotFound = errors.New("github resource not found")

type Installation struct {
	ID                  int64
	AccountID           int64
	AccountLogin        string
	AccountType         string
	RepositorySelection string
}

type Service interface {
	InstallationURL(state string) string
	UserAuthorizationURL(state string) string
	Installation(context.Context, int64) (Installation, error)
	UserCanAccessInstallation(context.Context, string, int64) (bool, error)
	Repositories(context.Context, int64) ([]domain.GitHubRepository, error)
	ResolveCommit(context.Context, int64, int64, string) (string, error)
	Commit(context.Context, int64, int64, string) (domain.CommitMetadata, error)
	Archive(context.Context, int64, int64, string, io.Writer) error
	DeleteInstallation(context.Context, int64) error
}

type Config struct {
	AppID        int64
	Slug         string
	ClientID     string
	ClientSecret string
	PrivateKey   []byte
	CallbackURL  string
	APIBaseURL   string
	WebBaseURL   string
	HTTPClient   *http.Client
	Now          func() time.Time
}

type Client struct {
	appID        int64
	slug         string
	clientID     string
	clientSecret string
	privateKey   *rsa.PrivateKey
	callbackURL  string
	apiBaseURL   string
	webBaseURL   string
	httpClient   *http.Client
	now          func() time.Time
}

func New(config Config) (*Client, error) {
	if config.AppID < 1 {
		return nil, errors.New("github app configuration is incomplete")
	}
	oauthValues := []string{config.Slug, config.ClientID, config.ClientSecret, config.CallbackURL}
	oauthConfigured := 0
	for _, value := range oauthValues {
		if strings.TrimSpace(value) != "" {
			oauthConfigured++
		}
	}
	if oauthConfigured != 0 && oauthConfigured != len(oauthValues) {
		return nil, errors.New("github app OAuth configuration is incomplete")
	}
	key, err := parsePrivateKey(config.PrivateKey)
	if err != nil {
		return nil, fmt.Errorf("github app private key: %w", err)
	}
	if config.APIBaseURL == "" {
		config.APIBaseURL = "https://api.github.com"
	}
	if config.WebBaseURL == "" {
		config.WebBaseURL = "https://github.com"
	}
	if config.HTTPClient == nil {
		config.HTTPClient = &http.Client{Timeout: 10 * time.Second}
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	return &Client{appID: config.AppID, slug: strings.TrimSpace(config.Slug), clientID: strings.TrimSpace(config.ClientID), clientSecret: strings.TrimSpace(config.ClientSecret), privateKey: key, callbackURL: config.CallbackURL, apiBaseURL: strings.TrimRight(config.APIBaseURL, "/"), webBaseURL: strings.TrimRight(config.WebBaseURL, "/"), httpClient: config.HTTPClient, now: config.Now}, nil
}

func (c *Client) InstallationURL(state string) string {
	return c.webBaseURL + "/apps/" + url.PathEscape(c.slug) + "/installations/new?state=" + url.QueryEscape(state)
}

func (c *Client) UserAuthorizationURL(state string) string {
	values := url.Values{"client_id": {c.clientID}, "redirect_uri": {c.callbackURL}, "state": {state}}
	return c.webBaseURL + "/login/oauth/authorize?" + values.Encode()
}

func (c *Client) Installation(ctx context.Context, installationID int64) (Installation, error) {
	var response struct {
		ID      int64 `json:"id"`
		Account struct {
			ID    int64  `json:"id"`
			Login string `json:"login"`
			Type  string `json:"type"`
		} `json:"account"`
		RepositorySelection string `json:"repository_selection"`
	}
	if err := c.appRequest(ctx, http.MethodGet, "/app/installations/"+strconv.FormatInt(installationID, 10), nil, &response); err != nil {
		return Installation{}, err
	}
	return Installation{ID: response.ID, AccountID: response.Account.ID, AccountLogin: response.Account.Login, AccountType: response.Account.Type, RepositorySelection: response.RepositorySelection}, nil
}

func (c *Client) UserCanAccessInstallation(ctx context.Context, code string, installationID int64) (allowed bool, resultErr error) {
	var tokenResponse struct {
		AccessToken string `json:"access_token"`
		Error       string `json:"error"`
	}
	body, _ := json.Marshal(map[string]string{"client_id": c.clientID, "client_secret": c.clientSecret, "code": code, "redirect_uri": c.callbackURL})
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.webBaseURL+"/login/oauth/access_token", bytes.NewReader(body))
	if err != nil {
		return false, err
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json")
	response, err := c.httpClient.Do(request)
	if err != nil {
		return false, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return false, responseError(response)
	}
	if err = decodeLimited(response.Body, &tokenResponse); err != nil {
		return false, err
	}
	if tokenResponse.Error != "" || tokenResponse.AccessToken == "" {
		return false, errors.New("github user authorization was rejected")
	}
	defer func() {
		if err := c.revokeUserToken(ctx, tokenResponse.AccessToken); err != nil && resultErr == nil {
			allowed = false
			resultErr = err
		}
	}()
	for page := 1; page <= 10; page++ {
		var installations struct {
			Installations []struct {
				ID int64 `json:"id"`
			} `json:"installations"`
		}
		path := "/user/installations?per_page=100&page=" + strconv.Itoa(page)
		if err = c.tokenRequest(ctx, http.MethodGet, path, tokenResponse.AccessToken, nil, &installations); err != nil {
			return false, err
		}
		for _, installation := range installations.Installations {
			if installation.ID == installationID {
				return true, nil
			}
		}
		if len(installations.Installations) < 100 {
			return false, nil
		}
	}
	return false, nil
}

func (c *Client) revokeUserToken(ctx context.Context, token string) error {
	body, _ := json.Marshal(map[string]string{"access_token": token})
	request, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.apiBaseURL+"/applications/"+url.PathEscape(c.clientID)+"/token", bytes.NewReader(body))
	if err != nil {
		return err
	}
	request.SetBasicAuth(c.clientID, c.clientSecret)
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-GitHub-Api-Version", apiVersion)
	response, err := c.httpClient.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusNoContent && response.StatusCode != http.StatusNotFound {
		return responseError(response)
	}
	return nil
}

func (c *Client) Repositories(ctx context.Context, installationID int64) ([]domain.GitHubRepository, error) {
	token, err := c.installationToken(ctx, installationID)
	if err != nil {
		return nil, err
	}
	items := []domain.GitHubRepository{}
	for page := 1; page <= 10; page++ {
		var response struct {
			Repositories []struct {
				ID            int64  `json:"id"`
				Name          string `json:"name"`
				FullName      string `json:"full_name"`
				Private       bool   `json:"private"`
				DefaultBranch string `json:"default_branch"`
			} `json:"repositories"`
		}
		path := "/installation/repositories?per_page=100&page=" + strconv.Itoa(page)
		if err = c.tokenRequest(ctx, http.MethodGet, path, token, nil, &response); err != nil {
			return nil, err
		}
		for _, repository := range response.Repositories {
			items = append(items, domain.GitHubRepository{ID: strconv.FormatInt(repository.ID, 10), Name: repository.Name, FullName: repository.FullName, Private: repository.Private, DefaultBranch: repository.DefaultBranch})
		}
		if len(response.Repositories) < 100 {
			break
		}
	}
	return items, nil
}

func (c *Client) ResolveCommit(ctx context.Context, installationID, repositoryID int64, ref string) (string, error) {
	commit, err := c.Commit(ctx, installationID, repositoryID, ref)
	return commit.SHA, err
}

func (c *Client) Commit(ctx context.Context, installationID, repositoryID int64, ref string) (domain.CommitMetadata, error) {
	if repositoryID < 1 || strings.TrimSpace(ref) == "" {
		return domain.CommitMetadata{}, errors.New("repository and ref are required")
	}
	token, err := c.installationToken(ctx, installationID)
	if err != nil {
		return domain.CommitMetadata{}, err
	}
	var response struct {
		SHA    string `json:"sha"`
		Commit struct {
			Message string `json:"message"`
			Author  struct {
				Name string    `json:"name"`
				Date time.Time `json:"date"`
			} `json:"author"`
		} `json:"commit"`
		Author *struct {
			Login string `json:"login"`
		} `json:"author"`
	}
	path := "/repositories/" + strconv.FormatInt(repositoryID, 10) + "/commits/" + url.PathEscape(ref)
	if err = c.tokenRequest(ctx, http.MethodGet, path, token, nil, &response); err != nil {
		return domain.CommitMetadata{}, err
	}
	if err = domain.ValidateCommitSHA(response.SHA); err != nil {
		return domain.CommitMetadata{}, errors.New("github returned an invalid commit SHA")
	}
	title := strings.TrimSpace(strings.SplitN(response.Commit.Message, "\n", 2)[0])
	titleRunes := []rune(title)
	if len(titleRunes) > 500 {
		title = string(titleRunes[:500])
	}
	authorLogin := ""
	if response.Author != nil {
		authorLogin = strings.TrimSpace(response.Author.Login)
	}
	var committedAt *time.Time
	if !response.Commit.Author.Date.IsZero() {
		value := response.Commit.Author.Date.UTC()
		committedAt = &value
	}
	return domain.CommitMetadata{SHA: response.SHA, Title: title, AuthorName: strings.TrimSpace(response.Commit.Author.Name), AuthorLogin: authorLogin, CommittedAt: committedAt}, nil
}

func (c *Client) Archive(ctx context.Context, installationID, repositoryID int64, commitSHA string, destination io.Writer) error {
	if repositoryID < 1 {
		return errors.New("repository is required")
	}
	if err := domain.ValidateCommitSHA(commitSHA); err != nil {
		return err
	}
	token, err := c.installationToken(ctx, installationID)
	if err != nil {
		return err
	}
	path := "/repositories/" + strconv.FormatInt(repositoryID, 10) + "/tarball/" + commitSHA
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.apiBaseURL+path, nil)
	if err != nil {
		return err
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("X-GitHub-Api-Version", apiVersion)
	response, err := c.httpClient.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return responseError(response)
	}
	limited := &io.LimitedReader{R: response.Body, N: (256 << 20) + 1}
	if _, err = io.Copy(destination, limited); err != nil {
		return err
	}
	if limited.N <= 0 {
		return errors.New("github source archive exceeds the size limit")
	}
	return nil
}

func (c *Client) DeleteInstallation(ctx context.Context, installationID int64) error {
	err := c.appRequest(ctx, http.MethodDelete, "/app/installations/"+strconv.FormatInt(installationID, 10), nil, nil)
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	return err
}

func (c *Client) installationToken(ctx context.Context, installationID int64) (string, error) {
	var response struct {
		Token string `json:"token"`
	}
	if err := c.appRequest(ctx, http.MethodPost, "/app/installations/"+strconv.FormatInt(installationID, 10)+"/access_tokens", nil, &response); err != nil {
		return "", err
	}
	if response.Token == "" {
		return "", errors.New("github returned an empty installation token")
	}
	return response.Token, nil
}

func (c *Client) appRequest(ctx context.Context, method, path string, body io.Reader, output any) error {
	token, err := c.jwt()
	if err != nil {
		return err
	}
	return c.tokenRequest(ctx, method, path, token, body, output)
}

func (c *Client) tokenRequest(ctx context.Context, method, path, token string, body io.Reader, output any) error {
	request, err := http.NewRequestWithContext(ctx, method, c.apiBaseURL+path, body)
	if err != nil {
		return err
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("X-GitHub-Api-Version", apiVersion)
	response, err := c.httpClient.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		if response.StatusCode == http.StatusNotFound {
			return ErrNotFound
		}
		return responseError(response)
	}
	if output == nil || response.StatusCode == http.StatusNoContent || response.StatusCode == http.StatusAccepted {
		return nil
	}
	return decodeLimited(response.Body, output)
}

func (c *Client) jwt() (string, error) {
	now := c.now()
	header, _ := json.Marshal(map[string]string{"alg": "RS256", "typ": "JWT"})
	payload, _ := json.Marshal(map[string]any{"iat": now.Add(-60 * time.Second).Unix(), "exp": now.Add(9 * time.Minute).Unix(), "iss": strconv.FormatInt(c.appID, 10)})
	unsigned := base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(payload)
	digest := sha256.Sum256([]byte(unsigned))
	signature, err := rsa.SignPKCS1v15(rand.Reader, c.privateKey, crypto.SHA256, digest[:])
	if err != nil {
		return "", err
	}
	return unsigned + "." + base64.RawURLEncoding.EncodeToString(signature), nil
}

func parsePrivateKey(value []byte) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode(value)
	if block == nil {
		return nil, errors.New("invalid PEM")
	}
	if key, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return key, nil
	}
	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, errors.New("unsupported private key")
	}
	rsaKey, ok := key.(*rsa.PrivateKey)
	if !ok {
		return nil, errors.New("private key is not RSA")
	}
	return rsaKey, nil
}

func decodeLimited(reader io.Reader, output any) error {
	return json.NewDecoder(io.LimitReader(reader, 2<<20)).Decode(output)
}

func responseError(response *http.Response) error {
	return fmt.Errorf("github API returned HTTP %d", response.StatusCode)
}
