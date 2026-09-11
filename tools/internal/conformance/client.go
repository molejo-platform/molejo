package conformance

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"strings"
	"time"
)

type Client struct {
	baseURL *url.URL
	host    string
	origin  string
	csrf    string
	http    *http.Client
}

type StatusError struct {
	Code int
	Body string
}

func (e *StatusError) Error() string { return fmt.Sprintf("HTTP %d: %s", e.Code, e.Body) }

func NewClient(endpoint, serverName, host, origin, caFile string) (*Client, error) {
	baseURL, err := url.Parse(endpoint)
	if err != nil || baseURL.Scheme != "https" || baseURL.Host == "" {
		return nil, fmt.Errorf("endpoint must be an absolute HTTPS URL")
	}
	certificate, err := os.ReadFile(caFile)
	if err != nil {
		return nil, fmt.Errorf("read API CA: %w", err)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(certificate) {
		return nil, fmt.Errorf("API CA contains no certificates")
	}
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, fmt.Errorf("create cookie jar: %w", err)
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: roots, ServerName: serverName}
	httpClient := &http.Client{Transport: transport, Jar: jar, Timeout: 30 * time.Second}
	httpClient.CheckRedirect = func(request *http.Request, via []*http.Request) error {
		if request.URL.Scheme != baseURL.Scheme || request.URL.Host != baseURL.Host {
			return fmt.Errorf("refuse redirect outside Control Plane origin")
		}
		if len(via) >= 3 {
			return fmt.Errorf("refuse redirect chain longer than three requests")
		}
		return nil
	}
	return &Client{
		baseURL: baseURL,
		host:    host,
		origin:  origin,
		http:    httpClient,
	}, nil
}

func (c *Client) Login(ctx context.Context, password string) error {
	var session struct {
		CSRFToken string `json:"csrfToken"`
	}
	if err := c.do(ctx, http.MethodPost, "/api/v1/session", map[string]string{"username": "owner", "password": password}, nil, []int{http.StatusOK}, &session); err != nil {
		return fmt.Errorf("login: %w", err)
	}
	if session.CSRFToken == "" {
		return fmt.Errorf("login returned an empty CSRF token")
	}
	c.csrf = session.CSRFToken
	return nil
}

func (c *Client) Logout(ctx context.Context) error {
	return c.do(ctx, http.MethodDelete, "/api/v1/session", nil, nil, []int{http.StatusNoContent}, nil)
}

func (c *Client) Get(ctx context.Context, path string, output any) error {
	return c.do(ctx, http.MethodGet, path, nil, nil, []int{http.StatusOK}, output)
}

func (c *Client) Post(ctx context.Context, path string, body, output any, headers map[string]string, statuses ...int) error {
	if len(statuses) == 0 {
		statuses = []int{http.StatusOK, http.StatusCreated, http.StatusAccepted}
	}
	return c.do(ctx, http.MethodPost, path, body, headers, statuses, output)
}

func (c *Client) Delete(ctx context.Context, path string, output any, headers map[string]string, statuses ...int) error {
	if len(statuses) == 0 {
		statuses = []int{http.StatusOK, http.StatusAccepted, http.StatusNoContent}
	}
	return c.do(ctx, http.MethodDelete, path, nil, headers, statuses, output)
}

func (c *Client) Stream(ctx context.Context, path, marker string) error {
	request, err := c.request(ctx, http.MethodGet, path, nil, nil)
	if err != nil {
		return err
	}
	response, err := c.http.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		contents, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		return &StatusError{Code: response.StatusCode, Body: safeErrorBody(contents)}
	}
	return readLogStream(response.Body, marker)
}

func readLogStream(reader io.Reader, marker string) error {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 4096), 1<<20)
	event, data := "message", strings.Builder{}
	for scanner.Scan() {
		line := scanner.Text()
		if line != "" {
			if value, found := strings.CutPrefix(line, "event:"); found {
				event = strings.TrimSpace(value)
			}
			if value, found := strings.CutPrefix(line, "data:"); found {
				data.WriteString(strings.TrimSpace(value))
			}
			continue
		}
		switch event {
		case "logs":
			if strings.Contains(data.String(), marker) {
				return nil
			}
		case "telemetry-error":
			return fmt.Errorf("live logs became unavailable: %s", data.String())
		case "end":
			return fmt.Errorf("live logs ended before marker %q: %s", marker, data.String())
		}
		event, data = "message", strings.Builder{}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	return fmt.Errorf("live logs ended before marker %q", marker)
}

func (c *Client) do(ctx context.Context, method, path string, body any, headers map[string]string, statuses []int, output any) error {
	request, err := c.request(ctx, method, path, body, headers)
	if err != nil {
		return err
	}
	response, err := c.http.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	accepted := false
	for _, status := range statuses {
		accepted = accepted || response.StatusCode == status
	}
	if !accepted {
		contents, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		return &StatusError{Code: response.StatusCode, Body: safeErrorBody(contents)}
	}
	if output == nil || response.StatusCode == http.StatusNoContent {
		return nil
	}
	if err = json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(output); err != nil {
		return fmt.Errorf("decode %s %s response: %w", method, path, err)
	}
	return nil
}

func (c *Client) request(ctx context.Context, method, path string, body any, headers map[string]string) (*http.Request, error) {
	reference, err := url.Parse(path)
	if err != nil || !strings.HasPrefix(path, "/") || strings.HasPrefix(path, "//") || reference.IsAbs() || reference.Host != "" {
		return nil, fmt.Errorf("invalid API path %q", path)
	}
	var contents io.Reader
	if body != nil {
		encoded, encodeErr := json.Marshal(body)
		if encodeErr != nil {
			return nil, fmt.Errorf("encode request: %w", encodeErr)
		}
		contents = bytes.NewReader(encoded)
	}
	request, err := http.NewRequestWithContext(ctx, method, c.baseURL.ResolveReference(reference).String(), contents)
	if err != nil {
		return nil, err
	}
	request.Host = c.host
	request.Header.Set("Accept", "application/json")
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if method != http.MethodGet && method != http.MethodHead {
		request.Header.Set("Origin", c.origin)
		if c.csrf != "" {
			request.Header.Set("X-CSRF-Token", c.csrf)
		}
	}
	for name, value := range headers {
		request.Header.Set(name, value)
	}
	return request, nil
}

func safeErrorBody(contents []byte) string {
	var body struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	if json.Unmarshal(contents, &body) == nil && (body.Code != "" || body.Message != "") {
		return strings.TrimSpace(body.Code + ": " + body.Message)
	}
	return "response body omitted"
}
