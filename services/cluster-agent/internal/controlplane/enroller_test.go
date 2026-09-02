package controlplane

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/molejo-platform/molejo/services/cluster-agent/internal/agent"
)

func TestHTTPEnrollerSendsBoundedEnrollmentRequest(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodPost || r.URL.Path != "/agent/v1/enroll" || r.Header.Get("Content-Type") != "application/json" {
			t.Fatalf("request=%s %s content-type=%q", r.Method, r.URL.Path, r.Header.Get("Content-Type"))
		}
		var input map[string]string
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			t.Fatal(err)
		}
		if input["enrollmentToken"] != "token" || input["attemptId"] != "attempt" || input["csrPem"] != "csr" {
			t.Fatalf("input=%v", input)
		}
		body, _ := json.Marshal(map[string]any{"installationId": "agi-abcdefghijklmnopqrst", "certificatePem": "certificate", "caCertificatePem": "ca", "expiresAt": time.Now().Add(time.Hour).UTC()})
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(string(body))), Header: make(http.Header)}, nil
	})}
	enroller, err := NewHTTPEnroller("https://control-plane.test/agent/v1/enroll", client)
	if err != nil {
		t.Fatal(err)
	}
	certificate, err := enroller.Enroll(t.Context(), agent.EnrollmentRequest{Token: "token", AttemptID: "attempt", CSRPEM: []byte("csr")})
	if err != nil || certificate.InstallationID != "agi-abcdefghijklmnopqrst" {
		t.Fatalf("certificate=%+v err=%v", certificate, err)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) { return f(request) }
