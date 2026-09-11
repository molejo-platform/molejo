// Package controlplane is the imperative HTTP boundary used by molejoctl. It
// owns ephemeral authentication and translates wire responses into typed data.
package controlplane

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"strings"
	"time"

	controlplanev1alpha1 "github.com/molejo-platform/molejo/contracts/molejo/controlplane/v1alpha1"
)

const maximumResponseBytes = 2 << 20

type Config struct {
	Endpoint string
	CAFile   string
	Timeout  time.Duration
}

type SecretReader interface {
	ReadSecret(prompt string) ([]byte, error)
}

type APIError struct {
	StatusCode int
	Code       string
	Message    string
	RequestID  string
	Retryable  bool
}

func (e *APIError) Error() string {
	if e.RequestID == "" {
		return fmt.Sprintf("control plane: %s (%s)", e.Message, e.Code)
	}
	return fmt.Sprintf("control plane: %s (%s, request %s)", e.Message, e.Code, e.RequestID)
}

type Client struct {
	endpoint *url.URL
	http     *http.Client
	api      *controlplanev1alpha1.Client
	csrf     string
}

func New(config Config) (*Client, error) {
	endpoint, err := url.Parse(strings.TrimSpace(config.Endpoint))
	if err != nil || endpoint.Scheme != "https" || endpoint.Host == "" || endpoint.User != nil || endpoint.RawQuery != "" || endpoint.Fragment != "" || endpoint.RawPath != "" || endpoint.Path != "" && endpoint.Path != "/" {
		return nil, errors.New("control-plane must be an absolute HTTPS origin without credentials, query, fragment, or path")
	}
	endpoint.Path = ""
	pool, err := x509.SystemCertPool()
	if err != nil {
		return nil, fmt.Errorf("load system certificate pool: %w", err)
	}
	if pool == nil {
		pool = x509.NewCertPool()
	}
	if config.CAFile != "" {
		file, openErr := os.Open(config.CAFile)
		if openErr != nil {
			return nil, fmt.Errorf("read CA file: %w", openErr)
		}
		pem, readErr := io.ReadAll(io.LimitReader(file, (1<<20)+1))
		closeErr := file.Close()
		if readErr != nil {
			return nil, fmt.Errorf("read CA file: %w", readErr)
		}
		if closeErr != nil {
			return nil, fmt.Errorf("close CA file: %w", closeErr)
		}
		if len(pem) > 1<<20 || !pool.AppendCertsFromPEM(pem) {
			return nil, errors.New("CA file does not contain a valid bounded PEM certificate")
		}
	}
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, fmt.Errorf("create ephemeral cookie jar: %w", err)
	}
	if config.Timeout <= 0 {
		config.Timeout = 15 * time.Second
	}
	httpClient := &http.Client{
		Timeout:   config.Timeout,
		Jar:       jar,
		Transport: &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: pool}},
	}
	httpClient.CheckRedirect = func(req *http.Request, _ []*http.Request) error {
		if req.URL.Scheme != endpoint.Scheme || !strings.EqualFold(req.URL.Host, endpoint.Host) {
			return errors.New("cross-origin redirect refused")
		}
		return nil
	}
	client := &Client{endpoint: endpoint, http: httpClient}
	apiClient, err := controlplanev1alpha1.NewClient(endpoint.String(), controlplanev1alpha1.WithHTTPClient(httpClient), controlplanev1alpha1.WithRequestEditorFn(client.editRequest))
	if err != nil {
		return nil, fmt.Errorf("create generated control-plane client: %w", err)
	}
	client.api = apiClient
	return client, nil
}

func (c *Client) editRequest(_ context.Context, req *http.Request) error {
	if req.Method != http.MethodGet && req.Method != http.MethodHead {
		req.Header.Set("Origin", c.endpoint.Scheme+"://"+c.endpoint.Host)
		if c.csrf != "" {
			req.Header.Set("X-CSRF-Token", c.csrf)
		}
	}
	return nil
}

func (c *Client) Authenticate(ctx context.Context, username string, secrets SecretReader) error {
	password, err := secrets.ReadSecret("Password: ")
	if err != nil {
		return err
	}
	passwordText := string(password)
	clear(password)
	response, err := c.api.Login(ctx, &controlplanev1alpha1.LoginParams{Origin: c.endpoint.String()}, controlplanev1alpha1.LoginJSONRequestBody{Username: username, Password: &passwordText})
	passwordText = ""
	if err != nil {
		return fmt.Errorf("login request: %w", err)
	}
	defer response.Body.Close()
	switch response.StatusCode {
	case http.StatusOK:
		var session controlplanev1alpha1.Session
		if err = decode(response, &session); err != nil {
			return err
		}
		c.csrf = session.CsrfToken
		if !session.InstallationCapabilities.ManageBindings {
			c.revokeRejectedSession(ctx)
			return errors.New("authenticated user cannot manage publication bindings")
		}
		return nil
	case http.StatusAccepted:
		var challenge controlplanev1alpha1.MFAChallenge
		if err = decode(response, &challenge); err != nil {
			return err
		}
		code, readErr := secrets.ReadSecret("TOTP: ")
		if readErr != nil {
			return readErr
		}
		codeText, token := string(code), challenge.ChallengeToken
		clear(code)
		response2, requestErr := c.api.CompleteTOTPLogin(ctx, &controlplanev1alpha1.CompleteTOTPLoginParams{Origin: c.endpoint.String()}, controlplanev1alpha1.CompleteTOTPLoginJSONRequestBody{ChallengeToken: &token, Code: &codeText})
		codeText, token = "", ""
		if requestErr != nil {
			return fmt.Errorf("complete MFA request: %w", requestErr)
		}
		defer response2.Body.Close()
		if response2.StatusCode != http.StatusOK {
			return responseError(response2)
		}
		var session controlplanev1alpha1.Session
		if err = decode(response2, &session); err != nil {
			return err
		}
		c.csrf = session.CsrfToken
		if !session.InstallationCapabilities.ManageBindings {
			c.revokeRejectedSession(ctx)
			return errors.New("authenticated user cannot manage publication bindings")
		}
		return nil
	default:
		return responseError(response)
	}
}

func (c *Client) revokeRejectedSession(ctx context.Context) {
	logoutCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	_ = c.Logout(logoutCtx)
}

func (c *Client) Logout(ctx context.Context) error {
	if c.csrf == "" {
		return nil
	}
	response, err := c.api.Logout(ctx, &controlplanev1alpha1.LogoutParams{Origin: c.endpoint.String(), XCSRFToken: c.csrf})
	if err != nil {
		return err
	}
	defer response.Body.Close()
	c.csrf = ""
	if response.StatusCode != http.StatusNoContent && response.StatusCode != http.StatusUnauthorized {
		return responseError(response)
	}
	return nil
}

func decode(response *http.Response, target any) error {
	data, err := io.ReadAll(io.LimitReader(response.Body, maximumResponseBytes+1))
	if err != nil {
		return fmt.Errorf("read control-plane response: %w", err)
	}
	if len(data) > maximumResponseBytes {
		return errors.New("control-plane response exceeds size limit")
	}
	if err = json.Unmarshal(data, target); err != nil {
		return fmt.Errorf("decode control-plane response: %w", err)
	}
	return nil
}

func responseError(response *http.Response) error {
	var body controlplanev1alpha1.Error
	if err := decode(response, &body); err != nil {
		return &APIError{StatusCode: response.StatusCode, Code: "unexpected_response", Message: http.StatusText(response.StatusCode)}
	}
	retryable := body.Retryable != nil && *body.Retryable
	return &APIError{StatusCode: response.StatusCode, Code: body.Code, Message: body.Message, RequestID: body.RequestId, Retryable: retryable}
}

func IsNotFound(err error) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound
}
