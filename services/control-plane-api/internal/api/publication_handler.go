package api

import (
	"errors"
	"net/http"

	"github.com/molejo-platform/molejo/packages/kubernetesbinding"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/api/generated"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/audit"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/authorization"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/domain"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/store"
)

func (h *generatedHandler) ListPublicationDomains(w http.ResponseWriter, r *http.Request, params generated.ListPublicationDomainsParams) {
	if _, ok := h.authorizeInstallation(w, r, false, authorization.ManageBindings); !ok {
		return
	}
	if !validPublicationPage(w, r, params.Offset) {
		return
	}
	items, err := h.server.store.PublicationDomains(r.Context(), publicationOffset(params.Offset))
	if err != nil {
		writePublicationError(w, r, err)
		return
	}
	hasMore := len(items) > 100
	if hasMore {
		items = items[:100]
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "hasMore": hasMore})
}

func (h *generatedHandler) PutPublicationDomain(w http.ResponseWriter, r *http.Request, id string, params generated.PutPublicationDomainParams) {
	actor, ok := h.authorizeInstallation(w, r, true, authorization.ManageBindings)
	if !ok {
		return
	}
	var input generated.PublicationDomainInput
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "request body is invalid", r)
		return
	}
	var expected *int64
	if params.IfMatch != nil {
		v := int64(*params.IfMatch)
		expected = &v
	}
	item, err := h.server.store.PutPublicationDomain(r.Context(), id, store.AdministrativeDomain{Name: input.Name, Kind: domain.PublicationDomainKind(input.Kind), ReservedNames: input.ReservedNames}, actor.ID, expected, h.server.auditEvent(r, "installation.publication.domain.put", "PublicationDomain", id, audit.Succeeded))
	if err != nil {
		writePublicationError(w, r, err)
		return
	}
	status := http.StatusCreated
	if expected != nil {
		status = http.StatusOK
	}
	writeJSON(w, status, item)
}

func (h *generatedHandler) DeletePublicationDomain(w http.ResponseWriter, r *http.Request, id string, params generated.DeletePublicationDomainParams) {
	actor, ok := h.authorizeInstallation(w, r, true, authorization.ManageBindings)
	if !ok {
		return
	}
	if err := h.server.store.DeletePublicationDomain(r.Context(), id, actor.ID, int64(params.IfMatch), h.server.auditEvent(r, "installation.publication.domain.delete", "PublicationDomain", id, audit.Succeeded)); err != nil {
		writePublicationError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *generatedHandler) PutPublicationGrant(w http.ResponseWriter, r *http.Request, domainID, workspaceID, bindingID string) {
	h.mutatePublicationGrant(w, r, domainID, workspaceID, bindingID, false)
}

func (h *generatedHandler) DeletePublicationGrant(w http.ResponseWriter, r *http.Request, domainID, workspaceID, bindingID string) {
	h.mutatePublicationGrant(w, r, domainID, workspaceID, bindingID, true)
}

func (h *generatedHandler) mutatePublicationGrant(w http.ResponseWriter, r *http.Request, domainID, workspaceID, bindingID string, remove bool) {
	actor, ok := h.authorizeInstallation(w, r, true, authorization.ManageBindings)
	if !ok {
		return
	}
	action := "installation.publication.grant.put"
	if remove {
		action = "installation.publication.grant.delete"
	}
	event := h.server.auditEvent(r, action, "PublicationGrant", domainID, audit.Succeeded)
	event.Metadata = map[string]any{"workspaceId": workspaceID, "bindingId": bindingID}
	if err := h.server.store.SetPublicationGrant(r.Context(), domainID, workspaceID, bindingID, actor.ID, remove, event); err != nil {
		writePublicationError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *generatedHandler) GetPublicationDependents(w http.ResponseWriter, r *http.Request, params generated.GetPublicationDependentsParams) {
	if _, ok := h.authorizeInstallation(w, r, false, authorization.ManageBindings); !ok {
		return
	}
	domainID, bindingID := "", ""
	if params.DomainId != nil {
		domainID = *params.DomainId
	}
	if params.BindingId != nil {
		bindingID = *params.BindingId
	}
	if domainID == "" && bindingID == "" {
		writePublicationError(w, r, domain.ErrPublicationName)
		return
	}
	if !validPublicationPage(w, r, params.Offset) {
		return
	}
	items, err := h.server.store.PublicationDependents(r.Context(), domainID, bindingID, publicationOffset(params.Offset))
	if err != nil {
		writePublicationError(w, r, err)
		return
	}
	hasMore := len(items) > 100
	if hasMore {
		items = items[:100]
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "hasMore": hasMore})
}

func (h *generatedHandler) GetPublicationOptions(w http.ResponseWriter, r *http.Request, workspaceID string, params generated.GetPublicationOptionsParams) {
	_, workspace, ok := h.authorizeWorkspacePermission(w, r, workspaceID, false, authorization.ReadWorkspace)
	if !ok {
		return
	}
	if !validPublicationPage(w, r, params.Offset) {
		return
	}
	items, err := h.server.store.PublicationOptions(r.Context(), workspace.ID, params.ClusterId, publicationOffset(params.Offset))
	if err != nil {
		writePublicationError(w, r, err)
		return
	}
	hasMore := len(items) > 100
	if hasMore {
		items = items[:100]
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "hasMore": hasMore})
}

func writePublicationError(w http.ResponseWriter, r *http.Request, err error) {
	status, code, message := http.StatusInternalServerError, "storage_failed", "publication state could not be persisted"
	switch {
	case errors.Is(err, store.ErrPublicationDependency):
		status, code, message = http.StatusConflict, "publication_has_dependents", "remove desired, applied and executable references before this change"
	case errors.Is(err, store.ErrVersionConflict), errors.Is(err, store.ErrConflict):
		status, code, message = http.StatusConflict, "version_conflict", "reload the current revision before retrying"
	case errors.Is(err, store.ErrPublicationConflict):
		status, code, message = http.StatusConflict, "publication_conflict", "the requested address is unavailable"
	case errors.Is(err, store.ErrNotFound), errors.Is(err, store.ErrBindingNotFound), errors.Is(err, store.ErrClusterNotFound):
		status, code, message = http.StatusNotFound, "resource_not_found", "resource was not found"
	case errors.Is(err, domain.ErrPublicationName), errors.Is(err, kubernetesbinding.ErrHTTPBindingInvalid), errors.Is(err, kubernetesbinding.ErrHTTPListenerInvalid):
		status, code, message = http.StatusBadRequest, "publication_invalid", "publication configuration is invalid"
	case errors.Is(err, domain.ErrPublicationUnsupported), errors.Is(err, kubernetesbinding.ErrHTTPBindingUnsupported):
		status, code, message = http.StatusBadRequest, "publication_mode_unsupported", "this publication mode is not supported"
	case errors.Is(err, kubernetesbinding.ErrHTTPSelectionRequired):
		writeDetailedError(w, http.StatusBadRequest, "publication_listener_required", "select one of the eligible listeners", []errorViolation{{Field: "/configuration/publicEndpoints", Code: "listener_required", Message: "set listenerName on the ambiguous address"}}, r)
		return
	case errors.Is(err, kubernetesbinding.ErrHTTPDestinationUnavailable):
		status, code, message = http.StatusBadRequest, "publication_destination_unavailable", "the selected listener does not cover the requested name"
	case errors.Is(err, domain.ErrPublicationNotGranted):
		status = http.StatusBadRequest
		code, message = "publication_not_granted", "the selected domain and destination are not granted in this context"
	case errors.Is(err, domain.ErrPublicationReserved):
		status = http.StatusBadRequest
		code, message = "publication_reserved", "the requested name is reserved"
	case errors.Is(err, domain.ErrPublicationLimit):
		status = http.StatusBadRequest
		code, message = "publication_limit_exceeded", "publication exceeds the supported limit"
	}
	writeError(w, status, code, message, r)
}

func publicationOffset(offset *int) int {
	if offset == nil {
		return 0
	}
	return *offset
}

func (h *generatedHandler) GetPublicationDomain(w http.ResponseWriter, r *http.Request, id string) {
	if _, ok := h.authorizeInstallation(w, r, false, authorization.ManageBindings); !ok {
		return
	}
	item, err := h.server.store.PublicationDomain(r.Context(), id)
	if err != nil {
		writePublicationError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (h *generatedHandler) ListPublicationGrants(w http.ResponseWriter, r *http.Request, id string, params generated.ListPublicationGrantsParams) {
	if _, ok := h.authorizeInstallation(w, r, false, authorization.ManageBindings); !ok {
		return
	}
	if !validPublicationPage(w, r, params.Offset) {
		return
	}
	items, err := h.server.store.PublicationGrants(r.Context(), id, publicationOffset(params.Offset))
	if err != nil {
		writePublicationError(w, r, err)
		return
	}
	hasMore := len(items) > 100
	if hasMore {
		items = items[:100]
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "hasMore": hasMore})
}

func validPublicationPage(w http.ResponseWriter, r *http.Request, offset *int) bool {
	if offset != nil && (*offset < 0 || *offset > 1000000) {
		writeError(w, http.StatusBadRequest, "pagination_invalid", "offset must be between 0 and 1000000", r)
		return false
	}
	return true
}
