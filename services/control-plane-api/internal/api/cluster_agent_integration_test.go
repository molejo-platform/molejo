package api

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"net/http"
	"strings"
	"testing"
	"time"

	controlagent "github.com/molejo-platform/molejo/services/control-plane-api/internal/clusteragent"
)

func TestAgentPairingAPIRequiresInstallationAdministratorAndReturnsTokenOnce(t *testing.T) {
	storage, _, server, owner := newHierarchyAPITestFixture(t)
	caCertificate, caKey := pairingTestCA(t)
	signer, err := controlagent.NewSigner(caCertificate, caKey, 7*24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	server.agentSigner = signer
	server.agentServerCAPEM = caCertificate
	server.agentTrustBundleID = strings.Repeat("a", 64)

	response := hierarchyRequest(t, server, owner, http.MethodPost, "/api/v1/admin/agent-installations", `{"name":"Lab cluster"}`, nil)
	if response.Code != http.StatusCreated {
		t.Fatalf("create invitation status=%d body=%s", response.Code, response.Body.String())
	}
	var invitation struct {
		InstallationID  string    `json:"installationId"`
		EnrollmentToken string    `json:"enrollmentToken"`
		ExpiresAt       time.Time `json:"expiresAt"`
	}
	decodeResponse(t, response, &invitation)
	if invitation.InstallationID == "" || len(invitation.EnrollmentToken) != 64 || !invitation.ExpiresAt.After(time.Now()) {
		t.Fatalf("invitation=%+v", invitation)
	}

	csr := pairingTestCSR(t)
	body, _ := json.Marshal(map[string]string{"enrollmentToken": invitation.EnrollmentToken, "attemptId": "ena-abcdefghijklmnopqrstuvwxyz", "csrPem": string(csr)})
	response = unauthenticatedRequest(t, server, http.MethodPost, "/agent/v1/enroll", json.RawMessage(body))
	if response.Code != http.StatusOK {
		t.Fatalf("enroll status=%d body=%s", response.Code, response.Body.String())
	}
	var enrolled struct {
		InstallationID string `json:"installationId"`
		CertificatePEM string `json:"certificatePem"`
	}
	decodeResponse(t, response, &enrolled)
	if enrolled.InstallationID != invitation.InstallationID || enrolled.CertificatePEM == "" {
		t.Fatalf("enrollment=%+v", enrolled)
	}
	var status string
	if err = storage.Pool.QueryRow(t.Context(), `SELECT status FROM agent_installations WHERE public_id=$1`, invitation.InstallationID).Scan(&status); err != nil || status != "Pending" {
		t.Fatalf("status=%q err=%v", status, err)
	}
}

func pairingTestCSR(t *testing.T) []byte {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{}, key)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: der})
}

func pairingTestCA(t *testing.T) ([]byte, []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	template := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "Molejo Agent CA"}, NotBefore: now.Add(-time.Minute), NotAfter: now.Add(24 * time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
}
