package api

import (
	"errors"
	"net/http"
	"time"

	"github.com/molejo-platform/molejo/packages/kubernetesbinding"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/api/generated"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/audit"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/authorization"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/store"
)

func (h *generatedHandler) ListClusterStorageBindings(w http.ResponseWriter, r *http.Request, clusterID generated.ClusterId) {
	if _, ok := h.authorizeInstallation(w, r, false, authorization.ManageBindings); !ok {
		return
	}
	items, err := h.server.store.ClusterStorageBindings(r.Context(), string(clusterID))
	if err != nil {
		writeKubernetesBindingError(w, r, err)
		return
	}
	responses := make([]generated.ClusterStorageBinding, 0, len(items))
	for _, item := range items {
		responses = append(responses, storageBindingResponse(item, time.Now().UTC()))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": responses})
}

func (h *generatedHandler) GetClusterStorageBinding(w http.ResponseWriter, r *http.Request, clusterID generated.ClusterId, storageProfileID string) {
	if _, ok := h.authorizeInstallation(w, r, false, authorization.ManageBindings); !ok {
		return
	}
	binding, err := h.server.store.ClusterStorageBinding(r.Context(), string(clusterID), storageProfileID)
	if err != nil {
		writeKubernetesBindingError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, storageBindingResponse(binding, time.Now().UTC()))
}

func (h *generatedHandler) PutClusterStorageBinding(w http.ResponseWriter, r *http.Request, clusterID generated.ClusterId, storageProfileID string, params generated.PutClusterStorageBindingParams) {
	administrator, ok := h.authorizeInstallation(w, r, true, authorization.ManageBindings)
	if !ok {
		return
	}
	var input generated.ClusterStorageBindingInput
	if decodeJSON(r, &input) != nil {
		writeError(w, http.StatusBadRequest, "storage_binding_invalid", "storage binding is invalid", r)
		return
	}
	var expectedVersion *int64
	if params.IfMatch != nil {
		value := int64(*params.IfMatch)
		expectedVersion = &value
	}
	event := h.server.auditEvent(r, "installation.binding.storage.put", "ClusterStorageBinding", string(clusterID), audit.Succeeded)
	binding, err := h.server.store.PutClusterStorageBinding(r.Context(), string(clusterID), storageProfileID, input.StorageClassName, administrator.ID, expectedVersion, event)
	if err != nil {
		writeKubernetesBindingError(w, r, err)
		return
	}
	status := http.StatusCreated
	if expectedVersion != nil {
		status = http.StatusOK
	}
	writeJSON(w, status, storageBindingResponse(binding, time.Now().UTC()))
}

func (h *generatedHandler) DeleteClusterStorageBinding(w http.ResponseWriter, r *http.Request, clusterID generated.ClusterId, storageProfileID string, params generated.DeleteClusterStorageBindingParams) {
	administrator, ok := h.authorizeInstallation(w, r, true, authorization.ManageBindings)
	if !ok {
		return
	}
	event := h.server.auditEvent(r, "installation.binding.storage.delete", "ClusterStorageBinding", string(clusterID), audit.Succeeded)
	if err := h.server.store.DeleteClusterStorageBinding(r.Context(), string(clusterID), storageProfileID, administrator.ID, int64(params.IfMatch), event); err != nil {
		writeKubernetesBindingError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *generatedHandler) GetClusterPublicationBinding(w http.ResponseWriter, r *http.Request, clusterID generated.ClusterId) {
	if _, ok := h.authorizeInstallation(w, r, false, authorization.ManageBindings); !ok {
		return
	}
	binding, err := h.server.store.ClusterPublicationBinding(r.Context(), string(clusterID))
	if err != nil {
		writePublicationError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, publicationBindingResponse(binding, time.Now().UTC()))
}

func (h *generatedHandler) PutClusterPublicationBinding(w http.ResponseWriter, r *http.Request, clusterID generated.ClusterId, params generated.PutClusterPublicationBindingParams) {
	administrator, ok := h.authorizeInstallation(w, r, true, authorization.ManageBindings)
	if !ok {
		return
	}
	var input generated.ClusterPublicationBindingInput
	if decodeJSON(r, &input) != nil {
		writeError(w, http.StatusBadRequest, "publication_binding_invalid", "publication binding is invalid", r)
		return
	}
	var expectedVersion *int64
	if params.IfMatch != nil {
		value := int64(*params.IfMatch)
		expectedVersion = &value
	}
	event := h.server.auditEvent(r, "installation.binding.publication.put", "ClusterPublicationBinding", string(clusterID), audit.Succeeded)
	binding, err := h.server.store.PutClusterPublicationBinding(r.Context(), string(clusterID), httpBindingInput(input), administrator.ID, expectedVersion, event)
	if err != nil {
		writePublicationError(w, r, err)
		return
	}
	status := http.StatusCreated
	if expectedVersion != nil {
		status = http.StatusOK
	}
	writeJSON(w, status, publicationBindingResponse(binding, time.Now().UTC()))
}

func (h *generatedHandler) DeleteClusterPublicationBinding(w http.ResponseWriter, r *http.Request, clusterID generated.ClusterId, params generated.DeleteClusterPublicationBindingParams) {
	administrator, ok := h.authorizeInstallation(w, r, true, authorization.ManageBindings)
	if !ok {
		return
	}
	event := h.server.auditEvent(r, "installation.binding.publication.delete", "ClusterPublicationBinding", string(clusterID), audit.Succeeded)
	if err := h.server.store.DeleteClusterPublicationBinding(r.Context(), string(clusterID), administrator.ID, int64(params.IfMatch), event); err != nil {
		writePublicationError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func storageBindingResponse(binding store.ClusterStorageBinding, now time.Time) generated.ClusterStorageBinding {
	health, reason := currentBindingHealth(binding.Health, binding.ReasonCode, binding.ExpiresAt, now)
	result := generated.ClusterStorageBinding{
		ClusterId: binding.ClusterID, StorageProfileId: binding.StorageProfileID, StorageClassName: binding.StorageClassName,
		Provisioner: binding.Provisioner, AccessModes: append([]string{}, binding.AccessModes...), AllowExpansion: binding.AllowExpansion,
		VolumeBindingMode: binding.VolumeBindingMode, Health: generated.ClusterStorageBindingHealth(health), ReasonCode: reason,
		Version: int(binding.Version), CreatedAt: binding.CreatedAt, UpdatedAt: binding.UpdatedAt,
	}
	if binding.ObservedAt != nil {
		observed := binding.ObservedAt.UTC()
		result.ObservedAt = &observed
	}
	return result
}

func httpBindingInput(input generated.ClusterPublicationBindingInput) kubernetesbinding.HTTPBinding {
	b := kubernetesbinding.HTTPBinding{SchemaVersion: string(input.SchemaVersion), GatewayNamespace: input.GatewayNamespace, GatewayName: input.GatewayName}
	for _, l := range input.Listeners {
		b.Listeners = append(b.Listeners, kubernetesbinding.HTTPListener{Name: l.Name, Hostname: l.Hostname})
	}
	return b
}

func publicationBindingResponse(binding store.ClusterPublicationBinding, now time.Time) store.ClusterPublicationBinding {
	binding.Health, binding.ReasonCode = currentBindingHealth(binding.Health, binding.ReasonCode, binding.ExpiresAt, now)
	return binding
}

func currentBindingHealth(health kubernetesbinding.Health, reason string, expiresAt *time.Time, now time.Time) (kubernetesbinding.Health, string) {
	if expiresAt == nil || !expiresAt.After(now) {
		return kubernetesbinding.HealthUnknown, "binding_observation_stale"
	}
	return health, reason
}

func writeKubernetesBindingError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, kubernetesbinding.ErrHTTPBindingInvalid), errors.Is(err, kubernetesbinding.ErrHTTPListenerInvalid), errors.Is(err, kubernetesbinding.ErrHTTPBindingUnsupported), errors.Is(err, store.ErrConflict), errors.Is(err, store.ErrPublicationDependency):
		writePublicationError(w, r, err)
	case errors.Is(err, store.ErrBindingNotFound), errors.Is(err, store.ErrClusterNotFound):
		writeError(w, http.StatusNotFound, "binding_not_found", "binding or cluster was not found", r)
	case errors.Is(err, store.ErrVersionConflict):
		writeError(w, http.StatusConflict, "version_conflict", "resource version does not match", r)
	case errors.Is(err, store.ErrStorageProfileUnavailable):
		writeError(w, http.StatusConflict, "storage_profile_unavailable", "storage profile is unavailable", r)
	case errors.Is(err, store.ErrBindingUnsupported):
		writeError(w, http.StatusBadRequest, "unsupported_binding_reference", "binding reference is not supported by this release", r)
	default:
		writeError(w, http.StatusBadRequest, "binding_invalid", "binding is invalid", r)
	}
}
