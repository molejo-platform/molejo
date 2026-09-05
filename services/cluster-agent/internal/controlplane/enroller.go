package controlplane

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/molejo-platform/molejo/services/cluster-agent/internal/agent"
	agentidentity "github.com/molejo-platform/molejo/services/cluster-agent/internal/identity"
)

const maxEnrollmentResponse = 64 << 10

type HTTPEnroller struct {
	endpoint string
	client   *http.Client
}

func NewHTTPEnroller(endpoint string, client *http.Client) (*HTTPEnroller, error) {
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.Path == "" {
		return nil, errors.New("Agent enrollment URL must be HTTPS")
	}
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	return &HTTPEnroller{endpoint: parsed.String(), client: client}, nil
}

func (e *HTTPEnroller) Enroll(ctx context.Context, enrollment agent.EnrollmentRequest) (agentidentity.Certificate, error) {
	body, err := json.Marshal(map[string]string{"enrollmentToken": enrollment.Token, "attemptId": enrollment.AttemptID, "csrPem": string(enrollment.CSRPEM)})
	if err != nil {
		return agentidentity.Certificate{}, fmt.Errorf("encode enrollment request: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, e.endpoint, bytes.NewReader(body))
	if err != nil {
		return agentidentity.Certificate{}, fmt.Errorf("create enrollment request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := e.client.Do(request)
	if err != nil {
		return agentidentity.Certificate{}, errors.New("Agent enrollment endpoint is unavailable")
	}
	defer response.Body.Close()
	limited := io.LimitReader(response.Body, maxEnrollmentResponse+1)
	responseBody, err := io.ReadAll(limited)
	if err != nil || len(responseBody) > maxEnrollmentResponse {
		return agentidentity.Certificate{}, errors.New("Agent enrollment response is invalid")
	}
	if response.StatusCode != http.StatusOK {
		return agentidentity.Certificate{}, fmt.Errorf("Agent enrollment was rejected with HTTP %d", response.StatusCode)
	}
	var output struct {
		InstallationID   string    `json:"installationId"`
		CertificatePEM   string    `json:"certificatePem"`
		CACertificatePEM string    `json:"caCertificatePem"`
		ServerCAPEM      string    `json:"serverCaCertificatePem"`
		TrustBundleID    string    `json:"trustBundleId"`
		ExpiresAt        time.Time `json:"expiresAt"`
	}
	if err = json.Unmarshal(responseBody, &output); err != nil || output.InstallationID == "" || output.CertificatePEM == "" || output.CACertificatePEM == "" || output.TrustBundleID == "" || output.ExpiresAt.IsZero() {
		return agentidentity.Certificate{}, errors.New("Agent enrollment response is invalid")
	}
	serverCA := output.ServerCAPEM
	if serverCA == "" {
		serverCA = output.CACertificatePEM
	}
	return agentidentity.Certificate{InstallationID: output.InstallationID, CertificatePEM: []byte(output.CertificatePEM), CACertificatePEM: []byte(output.CACertificatePEM), ServerCAPEM: []byte(serverCA), TrustBundleID: output.TrustBundleID, ExpiresAt: output.ExpiresAt}, nil
}
