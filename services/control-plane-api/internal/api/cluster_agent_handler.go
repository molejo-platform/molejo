package api

import (
	"crypto/sha256"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/fruto-platform/fruto/services/control-plane-api/internal/api/generated"
	"github.com/fruto-platform/fruto/services/control-plane-api/internal/audit"
	"github.com/fruto-platform/fruto/services/control-plane-api/internal/auth"
	"github.com/fruto-platform/fruto/services/control-plane-api/internal/authorization"
	controlagent "github.com/fruto-platform/fruto/services/control-plane-api/internal/clusteragent"
	"github.com/fruto-platform/fruto/services/control-plane-api/internal/domain"
	"github.com/fruto-platform/fruto/services/control-plane-api/internal/store"
)

const agentEnrollmentTTL = 10 * time.Minute

func (h *generatedHandler) CreateAgentInstallation(w http.ResponseWriter, r *http.Request) {
	administrator, ok := h.authorizeInstallation(w, r, true, authorization.ManageAgents)
	if !ok {
		return
	}
	if h.server.AgentSigner == nil {
		writeError(w, http.StatusServiceUnavailable, "agent_pairing_unavailable", "Agent pairing is not configured", r)
		return
	}
	var input generated.AgentInstallationCreateInput
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
	publicID, err := domain.NewPublicID("agi")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "agent_invitation_failed", "Agent invitation could not be created", r)
		return
	}
	expiresAt := time.Now().UTC().Add(agentEnrollmentTTL)
	event := h.server.auditEvent(r, "installation.agent.create", "AgentInstallation", publicID, audit.Succeeded)
	event.ActorUserID = &administrator.ID
	installation, err := h.server.Store.CreateAgentInstallation(r.Context(), publicID, name, auth.HashToken(token), expiresAt, event)
	if err != nil {
		writeIdentityError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, generated.AgentEnrollmentInvitation{InstallationId: installation.PublicID, EnrollmentToken: &token, ExpiresAt: expiresAt})
}

func (h *generatedHandler) EnrollAgent(w http.ResponseWriter, r *http.Request) {
	if h.server.AgentSigner == nil {
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
	certificate, err := h.server.Store.EnrollAgent(r.Context(), auth.HashToken(*input.EnrollmentToken), input.AttemptId, csrFingerprint[:], time.Now().UTC(), func(installationID string) (store.AgentCertificate, error) {
		issued, issueErr := h.server.AgentSigner.Sign(installationID, csrPEM, time.Now().UTC())
		return store.AgentCertificate{CertificatePEM: issued.CertificatePEM, CACertificatePEM: issued.CACertificatePEM, Serial: issued.Serial, Fingerprint: issued.Fingerprint, NotAfter: issued.NotAfter}, issueErr
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
	writeJSON(w, http.StatusOK, generated.AgentEnrollmentResult{InstallationId: certificate.InstallationID, CertificatePem: &certificatePEM, CaCertificatePem: string(certificate.CACertificatePEM), ExpiresAt: certificate.NotAfter})
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
