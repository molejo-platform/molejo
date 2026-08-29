package api

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/fruto-platform/fruto/services/control-plane-api/internal/api/generated"
	"github.com/fruto-platform/fruto/services/control-plane-api/internal/domain"
	"github.com/fruto-platform/fruto/services/control-plane-api/internal/store"
)

func (h *generatedHandler) ReceiveGitHubWebhook(w http.ResponseWriter, r *http.Request) {
	if len(h.server.GitHubWebhookSecret) < 32 || h.server.Store == nil {
		writeError(w, http.StatusServiceUnavailable, "github_webhook_unavailable", "GitHub webhook receiver is not configured", r)
		return
	}
	deliveryID := strings.TrimSpace(r.Header.Get("X-GitHub-Delivery"))
	eventType := strings.TrimSpace(r.Header.Get("X-GitHub-Event"))
	if deliveryID == "" || len(deliveryID) > 128 || !supportedGitHubEvent(eventType) {
		writeError(w, http.StatusBadRequest, "github_delivery_invalid", "GitHub delivery headers are invalid", r)
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		var maxBytesError *http.MaxBytesError
		if errors.As(err, &maxBytesError) {
			writeError(w, http.StatusRequestEntityTooLarge, "github_payload_too_large", "GitHub webhook payload is too large", r)
			return
		}
		writeError(w, http.StatusBadRequest, "github_payload_invalid", "GitHub webhook payload could not be read", r)
		return
	}
	if !validGitHubSignature(body, r.Header.Get("X-Hub-Signature-256"), h.server.GitHubWebhookSecret) {
		writeError(w, http.StatusUnauthorized, "github_signature_invalid", "GitHub webhook signature is invalid", r)
		return
	}
	var payload struct {
		Action       string `json:"action"`
		Ref          string `json:"ref"`
		After        string `json:"after"`
		Installation struct {
			ID int64 `json:"id"`
		} `json:"installation"`
		Repository struct {
			ID       int64  `json:"id"`
			FullName string `json:"full_name"`
		} `json:"repository"`
		Release struct {
			TagName         string `json:"tag_name"`
			TargetCommitish string `json:"target_commitish"`
			Draft           bool   `json:"draft"`
			Prerelease      bool   `json:"prerelease"`
		} `json:"release"`
		RepositoriesRemoved []struct {
			ID int64 `json:"id"`
		} `json:"repositories_removed"`
	}
	if err = json.Unmarshal(body, &payload); err != nil {
		writeError(w, http.StatusBadRequest, "github_payload_invalid", "GitHub webhook payload is invalid", r)
		return
	}
	delivery := domain.GitHubDelivery{DeliveryID: deliveryID, EventType: eventType, Action: strings.TrimSpace(payload.Action),
		InstallationExternalID: payload.Installation.ID, RepositoryID: payload.Repository.ID,
		RepositoryFullName: strings.TrimSpace(payload.Repository.FullName), SourceRef: strings.TrimSpace(payload.Ref),
		TagName: strings.TrimSpace(payload.Release.TagName), PayloadHash: domain.SHA256(body)}
	if eventType == "push" {
		if strings.HasPrefix(delivery.SourceRef, "refs/heads/") {
			delivery.SourceBranch = strings.TrimPrefix(delivery.SourceRef, "refs/heads/")
		}
		if payload.After != strings.Repeat("0", 40) {
			delivery.CommitSHA = strings.ToLower(strings.TrimSpace(payload.After))
		}
	}
	if eventType == "release" {
		delivery.SourceBranch = strings.TrimSpace(payload.Release.TargetCommitish)
		if payload.Release.Draft || payload.Release.Prerelease {
			delivery.Action = "ignored"
		}
	}
	for _, repository := range payload.RepositoriesRemoved {
		if repository.ID > 0 {
			delivery.RepositoryIDs = append(delivery.RepositoryIDs, repository.ID)
		}
	}
	if err = validateGitHubDelivery(delivery); err != nil {
		writeError(w, http.StatusBadRequest, "github_payload_invalid", err.Error(), r)
		return
	}
	_, duplicate, err := h.server.Store.AcceptGitHubDelivery(r.Context(), delivery)
	if errors.Is(err, store.ErrConflict) {
		writeError(w, http.StatusConflict, "github_delivery_conflict", "GitHub delivery ID was reused with different content", r)
		return
	}
	if err != nil {
		h.server.logger().Error("persist GitHub delivery", "request_id", requestID(r), "event", eventType, "error", err)
		writeError(w, http.StatusServiceUnavailable, "github_delivery_unavailable", "GitHub delivery could not be persisted", r)
		return
	}
	status := http.StatusAccepted
	if eventType == "ping" {
		status = http.StatusOK
	}
	writeJSON(w, status, map[string]any{"accepted": true, "duplicate": duplicate})
}

func (h *generatedHandler) GetAppEnvironmentDeliveryPolicy(w http.ResponseWriter, r *http.Request, workspaceID generated.WorkspaceId, projectID generated.ProjectId, appID generated.AppId, appEnvironmentID generated.AppEnvironmentId) {
	_, workspace, ok := h.authorizeWorkspace(w, r, string(workspaceID), false)
	if !ok {
		return
	}
	policy, err := h.server.Store.DeliveryPolicy(r.Context(), workspace.ID, string(projectID), string(appID), string(appEnvironmentID))
	if err != nil {
		writeDeliveryPolicyError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, policy)
}

func (h *generatedHandler) ReplaceAppEnvironmentDeliveryPolicy(w http.ResponseWriter, r *http.Request, workspaceID generated.WorkspaceId, projectID generated.ProjectId, appID generated.AppId, appEnvironmentID generated.AppEnvironmentId, params generated.ReplaceAppEnvironmentDeliveryPolicyParams) {
	actor, workspace, ok := h.authorizeWorkspace(w, r, string(workspaceID), true)
	if !ok {
		return
	}
	var input struct {
		PushEnabled    bool `json:"pushEnabled"`
		ReleaseEnabled bool `json:"releaseEnabled"`
	}
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "request body is invalid", r)
		return
	}
	policy, err := h.server.Store.PutDeliveryPolicy(r.Context(), workspace.ID, actor.ID, string(projectID), string(appID), string(appEnvironmentID), int64(params.IfMatch), input.PushEnabled, input.ReleaseEnabled)
	if err != nil {
		writeDeliveryPolicyError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, policy)
}

func supportedGitHubEvent(value string) bool {
	switch value {
	case "ping", "push", "release", "installation", "installation_repositories":
		return true
	default:
		return false
	}
}

func validGitHubSignature(body []byte, header string, secret []byte) bool {
	if !strings.HasPrefix(header, "sha256=") {
		return false
	}
	provided, err := hex.DecodeString(strings.TrimPrefix(header, "sha256="))
	if err != nil || len(provided) != sha256.Size {
		return false
	}
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write(body)
	expected := mac.Sum(nil)
	return subtle.ConstantTimeCompare(provided, expected) == 1
}

func validateGitHubDelivery(delivery domain.GitHubDelivery) error {
	if delivery.EventType == "ping" {
		return nil
	}
	if delivery.InstallationExternalID < 1 {
		return errors.New("GitHub installation is required")
	}
	if delivery.EventType == "push" {
		if delivery.RepositoryID < 1 || delivery.RepositoryFullName == "" || delivery.SourceRef == "" {
			return errors.New("GitHub repository and ref are required")
		}
		if delivery.CommitSHA != "" {
			return domain.ValidateCommitSHA(delivery.CommitSHA)
		}
		return nil
	}
	if delivery.EventType == "release" && delivery.Action == "published" {
		if delivery.RepositoryID < 1 || delivery.RepositoryFullName == "" || delivery.TagName == "" {
			return errors.New("published GitHub release requires repository and tag")
		}
	}
	return nil
}

func writeDeliveryPolicyError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "delivery_policy_not_found", "App Environment was not found", r)
	case errors.Is(err, store.ErrVersionConflict):
		writeError(w, http.StatusConflict, "delivery_policy_conflict", "delivery policy changed; reload before saving", r)
	default:
		writeError(w, http.StatusInternalServerError, "storage_failed", "delivery policy could not be persisted", r)
	}
}
