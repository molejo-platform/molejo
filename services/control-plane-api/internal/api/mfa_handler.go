package api

import (
	"crypto/subtle"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/pquerna/otp/totp"

	"github.com/molejo-platform/molejo/services/control-plane-api/internal/api/generated"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/audit"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/auth"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/parameters"
)

const authenticationChallengeTTL = 5 * time.Minute

func (h *generatedHandler) GetOwnMFA(w http.ResponseWriter, r *http.Request) {
	user, ok := h.authorizeUser(w, r, false)
	if !ok {
		return
	}
	status, err := h.server.store.UserMFAStatus(r.Context(), user.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "storage_failed", "MFA status could not be read", r)
		return
	}
	writeJSON(w, http.StatusOK, status)
}

func (h *generatedHandler) BeginTOTPEnrollment(w http.ResponseWriter, r *http.Request, _ generated.BeginTOTPEnrollmentParams) {
	user, ok := h.authorizeUser(w, r, true)
	if !ok {
		return
	}
	if !h.server.config.TOTPEnabled || len(h.server.passwordResetKey) < 32 {
		writeError(w, http.StatusServiceUnavailable, "totp_unavailable", "TOTP is not configured", r)
		return
	}
	var input generated.PasswordConfirmation
	if decodeJSON(r, &input) != nil || input.Password == nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "request body is invalid", r)
		return
	}
	_, passwordHash, err := h.server.store.AuthenticateUser(r.Context(), user.Username)
	if err != nil || !auth.VerifyPassword(*input.Password, passwordHash) {
		writeError(w, http.StatusBadRequest, "current_password_invalid", "current password is invalid", r)
		return
	}
	status, err := h.server.store.UserMFAStatus(r.Context(), user.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "storage_failed", "MFA status could not be read", r)
		return
	}
	if status.TOTPEnabled {
		writeError(w, http.StatusConflict, "totp_already_enabled", "TOTP is already enabled", r)
		return
	}
	key, err := totp.Generate(totp.GenerateOpts{Issuer: "Molejo", AccountName: user.Username})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "totp_enrollment_failed", "TOTP enrollment could not start", r)
		return
	}
	reference := fmt.Sprintf("identity/totp/%s", user.PublicID)
	version, err := h.server.authenticationSecrets.Put(r.Context(), reference, key.Secret(), 0)
	if errors.Is(err, parameters.ErrConflict) {
		currentVersion, inspectErr := h.server.authenticationSecrets.CurrentVersion(r.Context(), reference)
		if inspectErr == nil {
			version, err = h.server.authenticationSecrets.Put(r.Context(), reference, key.Secret(), currentVersion)
		}
	}
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "totp_unavailable", "TOTP is temporarily unavailable", r)
		return
	}
	challengeToken, err := h.server.newToken(32)
	if err != nil {
		_ = h.server.authenticationSecrets.Delete(r.Context(), reference)
		writeError(w, http.StatusInternalServerError, "totp_enrollment_failed", "TOTP enrollment could not start", r)
		return
	}
	payload := map[string]any{"secretReference": reference, "secretVersion": version}
	if err = h.server.store.CreateAuthenticationChallenge(r.Context(), auth.HashToken(challengeToken), user.ID, "TOTPEnrollment", payload, time.Now().Add(authenticationChallengeTTL)); err != nil {
		_ = h.server.authenticationSecrets.Delete(r.Context(), reference)
		writeError(w, http.StatusInternalServerError, "totp_enrollment_failed", "TOTP enrollment could not start", r)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"challengeToken": challengeToken, "secret": key.Secret(), "otpAuthUrl": key.URL()})
}

func (h *generatedHandler) ConfirmTOTPEnrollment(w http.ResponseWriter, r *http.Request, _ generated.ConfirmTOTPEnrollmentParams) {
	user, ok := h.authorizeUser(w, r, true)
	if !ok {
		return
	}
	var input generated.TOTPEnrollmentConfirmation
	if decodeJSON(r, &input) != nil || input.ChallengeToken == nil || input.Code == nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "request body is invalid", r)
		return
	}
	challenge, err := h.server.store.AuthenticationChallenge(r.Context(), auth.HashToken(*input.ChallengeToken), "TOTPEnrollment")
	if err != nil || challenge.User.ID != user.ID {
		writeError(w, http.StatusBadRequest, "totp_challenge_invalid", "TOTP challenge is invalid or expired", r)
		return
	}
	reference, version, ok := enrollmentSecret(challenge.Payload)
	if !ok {
		writeError(w, http.StatusBadRequest, "totp_challenge_invalid", "TOTP challenge is invalid or expired", r)
		return
	}
	secret, err := h.server.authenticationSecrets.Get(r.Context(), reference, version)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "totp_unavailable", "TOTP is temporarily unavailable", r)
		return
	}
	if _, valid := validTOTPStep(secret, *input.Code, time.Now()); !valid {
		_ = h.server.store.RecordAuthenticationChallengeFailure(r.Context(), challenge.ID)
		writeError(w, http.StatusBadRequest, "totp_code_invalid", "TOTP code is invalid", r)
		return
	}
	recoveryCodes := make([]string, 10)
	recoveryHashes := make([][]byte, 10)
	for index := range recoveryCodes {
		recoveryCodes[index], err = auth.NewResetCode()
		if err != nil {
			writeError(w, http.StatusInternalServerError, "totp_enrollment_failed", "TOTP enrollment could not complete", r)
			return
		}
		recoveryHashes[index] = auth.HashResetCode(h.server.passwordResetKey, recoveryCodes[index])
	}
	event := h.server.auditEvent(r, "authentication.totp.enable", "User", user.PublicID, audit.Succeeded)
	event.ActorUserID = &user.ID
	if err = h.server.store.EnableTOTP(r.Context(), challenge.ID, user.ID, reference, version, recoveryHashes, event); err != nil {
		writeIdentityError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"recoveryCodes": recoveryCodes})
}

func (h *generatedHandler) CompleteTOTPLogin(w http.ResponseWriter, r *http.Request, _ generated.CompleteTOTPLoginParams) {
	if !h.server.originAllowed(r) {
		writeError(w, http.StatusForbidden, "origin_forbidden", "request origin is not allowed", r)
		return
	}
	if len(h.server.passwordResetKey) < 32 {
		writeError(w, http.StatusServiceUnavailable, "totp_unavailable", "TOTP is not configured", r)
		return
	}
	var input generated.TOTPChallengeInput
	if decodeJSON(r, &input) != nil || input.ChallengeToken == nil || input.Code == nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "request body is invalid", r)
		return
	}
	challenge, err := h.server.store.AuthenticationChallenge(r.Context(), auth.HashToken(*input.ChallengeToken), "Login")
	if err != nil {
		writeError(w, http.StatusBadRequest, "totp_challenge_invalid", "TOTP challenge is invalid or expired", r)
		return
	}
	credential, err := h.server.store.TOTPCredential(r.Context(), challenge.User.ID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "totp_challenge_invalid", "TOTP challenge is invalid or expired", r)
		return
	}
	event := h.server.auditEvent(r, "authentication.mfa.complete", "User", challenge.User.PublicID, audit.Succeeded)
	event.ActorUserID = &challenge.User.ID
	code := strings.ToUpper(strings.TrimSpace(*input.Code))
	if strings.Contains(code, "-") {
		err = h.server.store.CompleteRecoveryChallenge(r.Context(), challenge.ID, challenge.User.ID, auth.HashResetCode(h.server.passwordResetKey, code), event)
	} else {
		secret, getErr := h.server.authenticationSecrets.Get(r.Context(), credential.Reference, credential.Version)
		if getErr != nil {
			writeError(w, http.StatusServiceUnavailable, "totp_unavailable", "TOTP is temporarily unavailable", r)
			return
		}
		step, valid := validTOTPStep(secret, code, time.Now())
		if !valid || step <= credential.LastUsedStep {
			err = parameters.ErrConflict
		} else {
			err = h.server.store.CompleteTOTPChallenge(r.Context(), challenge.ID, challenge.User.ID, step, event)
		}
	}
	if err != nil {
		_ = h.server.store.RecordAuthenticationChallengeFailure(r.Context(), challenge.ID)
		writeError(w, http.StatusBadRequest, "totp_code_invalid", "TOTP code or recovery code is invalid", r)
		return
	}
	h.server.issueSession(w, r, challenge.User, "AAL2")
}

func (h *generatedHandler) DisableOwnTOTP(w http.ResponseWriter, r *http.Request, _ generated.DisableOwnTOTPParams) {
	user, ok := h.authorizeUser(w, r, true)
	if !ok {
		return
	}
	var input generated.PasswordConfirmation
	if decodeJSON(r, &input) != nil || input.Password == nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "request body is invalid", r)
		return
	}
	_, passwordHash, err := h.server.store.AuthenticateUser(r.Context(), user.Username)
	if err != nil || !auth.VerifyPassword(*input.Password, passwordHash) {
		writeError(w, http.StatusBadRequest, "current_password_invalid", "current password is invalid", r)
		return
	}
	event := h.server.auditEvent(r, "authentication.totp.disable", "User", user.PublicID, audit.Succeeded)
	event.ActorUserID = &user.ID
	reference, err := h.server.store.DisableTOTP(r.Context(), user.ID, event)
	if err != nil {
		writeIdentityError(w, r, err)
		return
	}
	if err = h.server.authenticationSecrets.Delete(r.Context(), reference); err != nil {
		h.server.logger().Error("TOTP secret deletion failed", "request_id", requestID(r), "user_id", user.PublicID, "error", err)
	}
	h.server.clearSessionCookies(w, r)
	w.WriteHeader(http.StatusNoContent)
}

func enrollmentSecret(payload map[string]any) (string, int64, bool) {
	reference, referenceOK := payload["secretReference"].(string)
	versionNumber, versionOK := payload["secretVersion"].(float64)
	version := int64(versionNumber)
	return reference, version, referenceOK && versionOK && reference != "" && version > 0 && float64(version) == versionNumber
}

func validTOTPStep(secret, code string, now time.Time) (int64, bool) {
	for offset := -1; offset <= 1; offset++ {
		candidateTime := now.Add(time.Duration(offset) * 30 * time.Second)
		expected, err := totp.GenerateCode(secret, candidateTime)
		if err == nil && subtle.ConstantTimeCompare([]byte(expected), []byte(code)) == 1 {
			return candidateTime.Unix() / 30, true
		}
	}
	return 0, false
}
