package parameters

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

type OpenBaoConfig struct {
	Address                 string
	KubernetesAuthMount     string
	KubernetesRole          string
	ServiceAccountTokenFile string
	Mount                   string
	HTTPClient              *http.Client
}

type OpenBaoKV2 struct {
	address     string
	authMount   string
	role        string
	tokenFile   string
	mount       string
	http        *http.Client
	mu          sync.Mutex
	token       string
	tokenExpiry time.Time
}

func NewOpenBaoKV2(config OpenBaoConfig) (*OpenBaoKV2, error) {
	parsed, err := url.Parse(strings.TrimRight(strings.TrimSpace(config.Address), "/"))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return nil, fmt.Errorf("OpenBao address is invalid")
	}
	if parsed.Scheme != "https" && !strings.HasPrefix(parsed.Host, "127.0.0.1:") && !strings.HasPrefix(parsed.Host, "localhost:") {
		return nil, fmt.Errorf("OpenBao address must use HTTPS")
	}
	authMount := cleanSegment(config.KubernetesAuthMount, "kubernetes")
	mount := cleanSegment(config.Mount, "parameters")
	role := strings.TrimSpace(config.KubernetesRole)
	tokenFile := strings.TrimSpace(config.ServiceAccountTokenFile)
	if authMount == "" || mount == "" || role == "" || tokenFile == "" {
		return nil, fmt.Errorf("OpenBao auth mount, role, token file, and KV mount are required")
	}
	httpClient := config.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 10 * time.Second}
	}
	return &OpenBaoKV2{address: parsed.String(), authMount: authMount, role: role, tokenFile: tokenFile, mount: mount, http: httpClient}, nil
}

func (c *OpenBaoKV2) Put(ctx context.Context, reference, value string, expectedVersion int64) (int64, error) {
	if expectedVersion < 0 {
		return 0, ErrConflict
	}
	var response struct {
		Data struct {
			Version int64 `json:"version"`
		} `json:"data"`
	}
	status, err := c.request(ctx, http.MethodPost, c.dataPath(reference), map[string]any{"data": map[string]string{"value": value}, "options": map[string]int64{"cas": expectedVersion}}, &response, true)
	if err != nil {
		return 0, err
	}
	if status == http.StatusBadRequest {
		return 0, ErrConflict
	}
	if status < 200 || status >= 300 || response.Data.Version < 1 {
		return 0, ErrUnavailable
	}
	return response.Data.Version, nil
}

func (c *OpenBaoKV2) Get(ctx context.Context, reference string, version int64) (string, error) {
	if version < 1 {
		return "", ErrUnavailable
	}
	var response struct {
		Data struct {
			Data map[string]string `json:"data"`
		} `json:"data"`
	}
	status, err := c.request(ctx, http.MethodGet, c.dataPath(reference)+"?version="+strconv.FormatInt(version, 10), nil, &response, true)
	if err != nil || status < 200 || status >= 300 {
		return "", ErrUnavailable
	}
	value, ok := response.Data.Data["value"]
	if !ok {
		return "", ErrUnavailable
	}
	return value, nil
}

func (c *OpenBaoKV2) CurrentVersion(ctx context.Context, reference string) (int64, error) {
	var response struct {
		Data struct {
			CurrentVersion int64 `json:"current_version"`
		} `json:"data"`
	}
	status, err := c.request(ctx, http.MethodGet, "/v1/"+c.mount+"/metadata/"+safeReference(reference), nil, &response, true)
	if status == http.StatusNotFound {
		return 0, nil
	}
	if err != nil || status < 200 || status >= 300 || response.Data.CurrentVersion < 0 {
		return 0, ErrUnavailable
	}
	return response.Data.CurrentVersion, nil
}

func (c *OpenBaoKV2) Delete(ctx context.Context, reference string) error {
	status, err := c.request(ctx, http.MethodDelete, "/v1/"+c.mount+"/metadata/"+safeReference(reference), nil, nil, true)
	if err != nil || (status != http.StatusNoContent && (status < 200 || status >= 300)) {
		return ErrUnavailable
	}
	return nil
}

func (c *OpenBaoKV2) dataPath(reference string) string {
	return "/v1/" + c.mount + "/data/" + safeReference(reference)
}

func (c *OpenBaoKV2) request(ctx context.Context, method, path string, body any, output any, retryAuth bool) (int, error) {
	token, err := c.clientToken(ctx)
	if err != nil {
		return 0, ErrUnavailable
	}
	var payload []byte
	if body != nil {
		payload, err = json.Marshal(body)
		if err != nil {
			return 0, ErrUnavailable
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, c.address+path, bytes.NewReader(payload))
	if err != nil {
		return 0, ErrUnavailable
	}
	req.Header.Set("X-Vault-Token", token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	response, err := c.http.Do(req)
	if err != nil {
		return 0, ErrUnavailable
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusForbidden && retryAuth {
		c.invalidateToken()
		return c.request(ctx, method, path, body, output, false)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 64<<10))
		return response.StatusCode, nil
	}
	if output != nil {
		if err = json.NewDecoder(io.LimitReader(response.Body, 64<<10)).Decode(output); err != nil {
			return response.StatusCode, ErrUnavailable
		}
	}
	return response.StatusCode, nil
}

func (c *OpenBaoKV2) clientToken(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.token != "" && time.Now().Add(30*time.Second).Before(c.tokenExpiry) {
		return c.token, nil
	}
	jwt, err := os.ReadFile(c.tokenFile)
	if err != nil || strings.TrimSpace(string(jwt)) == "" {
		return "", ErrUnavailable
	}
	payload, _ := json.Marshal(map[string]string{"role": c.role, "jwt": strings.TrimSpace(string(jwt))})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.address+"/v1/auth/"+c.authMount+"/login", bytes.NewReader(payload))
	if err != nil {
		return "", ErrUnavailable
	}
	req.Header.Set("Content-Type", "application/json")
	response, err := c.http.Do(req)
	if err != nil {
		return "", ErrUnavailable
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 64<<10))
		return "", ErrUnavailable
	}
	var result struct {
		Auth struct {
			ClientToken   string `json:"client_token"`
			LeaseDuration int64  `json:"lease_duration"`
		} `json:"auth"`
	}
	if err = json.NewDecoder(io.LimitReader(response.Body, 64<<10)).Decode(&result); err != nil || result.Auth.ClientToken == "" || result.Auth.LeaseDuration < 1 {
		return "", ErrUnavailable
	}
	c.token = result.Auth.ClientToken
	c.tokenExpiry = time.Now().Add(time.Duration(result.Auth.LeaseDuration) * time.Second)
	return c.token, nil
}

func (c *OpenBaoKV2) invalidateToken() {
	c.mu.Lock()
	c.token = ""
	c.tokenExpiry = time.Time{}
	c.mu.Unlock()
}

func cleanSegment(value, fallback string) string {
	value = strings.Trim(strings.TrimSpace(value), "/")
	if value == "" {
		value = fallback
	}
	if strings.ContainsAny(value, "/?#") {
		return ""
	}
	return value
}

func safeReference(reference string) string {
	parts := strings.Split(strings.Trim(reference, "/"), "/")
	for index := range parts {
		parts[index] = url.PathEscape(parts[index])
	}
	return strings.Join(parts, "/")
}
