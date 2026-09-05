package api

import (
	"crypto/sha256"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/molejo-platform/molejo/services/control-plane-api/internal/api/generated"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/audit"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/auth"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/authorization"
	controlagent "github.com/molejo-platform/molejo/services/control-plane-api/internal/clusteragent"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/domain"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/store"
)

const agentEnrollmentTTL = 10 * time.Minute

func (h *generatedHandler) CreateAgentInstallation(w http.ResponseWriter, r *http.Request) {
	h.createClusterInvitation(w, r, "agi")
}

func (h *generatedHandler) CreateCluster(w http.ResponseWriter, r *http.Request) {
	h.createClusterInvitation(w, r, "cls")
}

func (h *generatedHandler) createClusterInvitation(w http.ResponseWriter, r *http.Request, prefix string) {
	administrator, ok := h.authorizeInstallation(w, r, true, authorization.ManageAgents)
	if !ok {
		return
	}
	if h.server.agentSigner == nil {
		writeError(w, http.StatusServiceUnavailable, "agent_pairing_unavailable", "Agent pairing is not configured", r)
		return
	}
	var input struct {
		Name string `json:"name"`
	}
	if decodeJSON(r, &input) != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "request body is invalid", r)
		return
	}
	name := strings.TrimSpace(input.Name)
	if name == "" || len(name) > 80 {
		writeError(w, http.StatusBadRequest, "agent_installation_invalid", "Agent installation name is invalid", r)
		return
	}
	token, err := h.server.newToken(32)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "agent_invitation_failed", "Agent invitation could not be created", r)
		return
	}
	publicID, err := domain.NewPublicID(prefix)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "agent_invitation_failed", "Agent invitation could not be created", r)
		return
	}
	expiresAt := time.Now().UTC().Add(agentEnrollmentTTL)
	event := h.server.auditEvent(r, "cluster.create", "Cluster", publicID, audit.Succeeded)
	event.ActorUserID = &administrator.ID
	installation, err := h.server.store.CreateAgentInstallation(r.Context(), publicID, name, auth.HashToken(token), expiresAt, event)
	if err != nil {
		writeIdentityError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, generated.AgentEnrollmentInvitation{ClusterId: installation.PublicID, InstallationId: installation.PublicID, EnrollmentToken: &token, ExpiresAt: expiresAt})
}

func (h *generatedHandler) ListClusters(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.authorizeInstallation(w, r, false, authorization.ManageAgents); !ok {
		return
	}
	clusters, err := h.server.store.ListClusters(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "storage_failed", "clusters could not be listed", r)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": clusters})
}

func (h *generatedHandler) GetCluster(w http.ResponseWriter, r *http.Request, clusterID generated.ClusterId) {
	if _, ok := h.authorizeInstallation(w, r, false, authorization.ManageAgents); !ok {
		return
	}
	cluster, err := h.server.store.FindCluster(r.Context(), string(clusterID))
	if errors.Is(err, store.ErrClusterNotFound) {
		writeError(w, http.StatusNotFound, "cluster_not_found", "cluster was not found", r)
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "storage_failed", "cluster could not be read", r)
		return
	}
	writeJSON(w, http.StatusOK, cluster)
}

func (h *generatedHandler) CreateClusterEnrollmentInvitation(w http.ResponseWriter, r *http.Request, clusterID generated.ClusterId) {
	administrator, ok := h.authorizeInstallation(w, r, true, authorization.ManageAgents)
	if !ok {
		return
	}
	token, err := h.server.newToken(32)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "cluster_invitation_failed", "cluster invitation could not be created", r)
		return
	}
	expiresAt := time.Now().UTC().Add(agentEnrollmentTTL)
	event := h.server.auditEvent(r, "cluster.enrollment.reissue", "Cluster", string(clusterID), audit.Succeeded)
	event.ActorUserID = &administrator.ID
	err = h.server.store.ReissueClusterEnrollment(r.Context(), string(clusterID), auth.HashToken(token), expiresAt, event)
	switch {
	case errors.Is(err, store.ErrClusterNotFound):
		writeError(w, http.StatusNotFound, "cluster_not_found", "cluster was not found", r)
		return
	case errors.Is(err, store.ErrClusterNotPending):
		writeError(w, http.StatusConflict, "cluster_not_pending", "only a pending cluster can receive an enrollment invitation", r)
		return
	case err != nil:
		writeError(w, http.StatusInternalServerError, "cluster_invitation_failed", "cluster invitation could not be created", r)
		return
	}
	writeJSON(w, http.StatusCreated, generated.AgentEnrollmentInvitation{ClusterId: string(clusterID), InstallationId: string(clusterID), EnrollmentToken: &token, ExpiresAt: expiresAt})
}

func (h *generatedHandler) RevokeCluster(w http.ResponseWriter, r *http.Request, clusterID generated.ClusterId) {
	administrator, ok := h.authorizeInstallation(w, r, true, authorization.ManageAgents)
	if !ok {
		return
	}
	var input generated.ClusterRevocationInput
	if decodeJSON(r, &input) != nil || strings.TrimSpace(input.Reason) == "" {
		writeError(w, http.StatusBadRequest, "cluster_revocation_invalid", "revocation reason is required", r)
		return
	}
	event := h.server.auditEvent(r, "cluster.revoke", "Cluster", string(clusterID), audit.Succeeded)
	event.ActorUserID = &administrator.ID
	err := h.server.store.RevokeCluster(r.Context(), string(clusterID), input.Reason, time.Now().UTC(), event)
	if errors.Is(err, store.ErrClusterNotFound) {
		writeError(w, http.StatusNotFound, "cluster_not_found", "cluster was not found", r)
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "cluster_revocation_failed", "cluster could not be revoked", r)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *generatedHandler) EnrollAgent(w http.ResponseWriter, r *http.Request) {
	if h.server.agentSigner == nil {
		writeError(w, http.StatusServiceUnavailable, "agent_pairing_unavailable", "Agent pairing is not configured", r)
		return
	}
	var input generated.AgentEnrollmentInput
	if decodeJSON(r, &input) != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "request body is invalid", r)
		return
	}
	if input.CsrPem == nil || input.EnrollmentToken == nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "request body is invalid", r)
		return
	}
	csrPEM := []byte(*input.CsrPem)
	csrFingerprint := sha256.Sum256(csrPEM)
	event := h.server.auditEvent(r, "installation.agent.enroll", "AgentInstallation", "", audit.Succeeded)
	certificate, err := h.server.store.EnrollAgent(r.Context(), auth.HashToken(*input.EnrollmentToken), input.AttemptId, csrFingerprint[:], time.Now().UTC(), func(installationID string) (store.AgentCertificate, error) {
		issued, issueErr := h.server.agentSigner.Sign(installationID, csrPEM, time.Now().UTC())
		serverCA := h.server.agentServerCAPEM
		if len(serverCA) == 0 {
			serverCA = issued.CACertificatePEM
		}
		return store.AgentCertificate{CertificatePEM: issued.CertificatePEM, CACertificatePEM: issued.CACertificatePEM, ServerCAPEM: serverCA, Serial: issued.Serial, Fingerprint: issued.Fingerprint, NotAfter: issued.NotAfter, TrustBundleID: h.server.agentTrustBundleID}, issueErr
	}, event)
	if err != nil {
		h.auditEnrollmentRejection(r, err)
		switch {
		case errors.Is(err, controlagent.ErrInvalidCSR):
			writeError(w, http.StatusBadRequest, "agent_csr_invalid", "certificate request is invalid", r)
		case errors.Is(err, store.ErrAgentEnrollmentConsumed):
			writeError(w, http.StatusConflict, "agent_enrollment_consumed", "Agent enrollment was already consumed", r)
		case errors.Is(err, store.ErrAgentEnrollmentInvalid):
			writeError(w, http.StatusUnauthorized, "agent_enrollment_invalid", "Agent enrollment is invalid or expired", r)
		default:
			writeError(w, http.StatusInternalServerError, "agent_enrollment_failed", "Agent enrollment could not be completed", r)
		}
		return
	}
	certificatePEM := string(certificate.CertificatePEM)
	writeJSON(w, http.StatusOK, generated.AgentEnrollmentResult{InstallationId: certificate.InstallationID, CertificatePem: &certificatePEM, CaCertificatePem: string(certificate.CACertificatePEM), ServerCaCertificatePem: string(certificate.ServerCAPEM), TrustBundleId: certificate.TrustBundleID, ExpiresAt: certificate.NotAfter})
}

func (h *generatedHandler) auditEnrollmentRejection(r *http.Request, cause error) {
	reason := "failed"
	switch {
	case errors.Is(cause, controlagent.ErrInvalidCSR):
		reason = "invalid_csr"
	case errors.Is(cause, store.ErrAgentEnrollmentConsumed):
		reason = "consumed"
	case errors.Is(cause, store.ErrAgentEnrollmentInvalid):
		reason = "invalid_or_expired"
	}
	_ = h.server.recordAudit(r, audit.Event{Action: "installation.agent.enroll", TargetType: "AgentInstallation", Outcome: audit.Failed, Reason: reason})
}
