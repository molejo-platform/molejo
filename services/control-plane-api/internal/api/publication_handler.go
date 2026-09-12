package api

import (
	"errors"
	"fmt"
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
	cursor, ok := validPublicationPage(w, r, params.Cursor, params.Limit, "domains")
	if !ok {
		return
	}
	limit := publicationPageSize(params.Limit)
	items, err := h.server.store.PublicationDomains(r.Context(), cursor.A, limit)
	if err != nil {
		writePublicationError(w, r, err)
		return
	}
	hasMore := len(items) > limit
	if hasMore {
		items = items[:limit]
	}
	var next any
	if hasMore {
		next = encodePublicationCursor(publicationCursor{Kind: "domains", A: items[len(items)-1].ID})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "hasMore": hasMore, "nextCursor": next})
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

func (h *generatedHandler) GetPublicationGrant(w http.ResponseWriter, r *http.Request, domainID, workspaceID, bindingID string) {
	if _, ok := h.authorizeInstallation(w, r, false, authorization.ManageBindings); !ok {
		return
	}
	item, err := h.server.store.PublicationGrant(r.Context(), domainID, workspaceID, bindingID)
	if err != nil {
		writePublicationError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
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
	cursor, ok := validPublicationPage(w, r, params.Cursor, params.Limit, "dependents")
	if !ok {
		return
	}
	limit := publicationPageSize(params.Limit)
	items, err := h.server.store.PublicationDependents(r.Context(), domainID, bindingID, cursor.A, cursor.B, cursor.C, limit)
	if err != nil {
		writePublicationError(w, r, err)
		return
	}
	hasMore := len(items) > limit
	if hasMore {
		items = items[:limit]
	}
	var next any
	if hasMore {
		last := items[len(items)-1]
		next = encodePublicationCursor(publicationCursor{Kind: "dependents", A: last.AppEnvironmentID, B: last.Hostname, C: last.Kind})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "hasMore": hasMore, "nextCursor": next})
}

func (h *generatedHandler) GetPublicationOptions(w http.ResponseWriter, r *http.Request, workspaceID string, params generated.GetPublicationOptionsParams) {
	_, workspace, ok := h.authorizeWorkspacePermission(w, r, workspaceID, false, authorization.ReadWorkspace)
	if !ok {
		return
	}
	cursor, ok := validPublicationPage(w, r, params.Cursor, params.Limit, "options")
	if !ok {
		return
	}
	limit := publicationPageSize(params.Limit)
	items, err := h.server.store.PublicationOptions(r.Context(), workspace.ID, params.ClusterId, cursor.A, cursor.B, limit)
	if err != nil {
		writePublicationError(w, r, err)
		return
	}
	hasMore := len(items) > limit
	if hasMore {
		items = items[:limit]
	}
	var next any
	if hasMore {
		last := items[len(items)-1]
		next = encodePublicationCursor(publicationCursor{Kind: "options", A: last.Domain.ID, B: last.BindingID})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "hasMore": hasMore, "nextCursor": next})
}

func writePublicationError(w http.ResponseWriter, r *http.Request, err error) {
	status, code, message, violationCode, violationMessage := publicationErrorDetails(err)
	var association domain.PublicationAssociationError
	if errors.As(err, &association) {
		field := publicationAssociationField(association)
		writeDetailedError(w, status, code, message, []errorViolation{{Field: field, Code: violationCode, Message: violationMessage}}, r)
		return
	}
	writeError(w, status, code, message, r)
}

func publicationAssociationField(association domain.PublicationAssociationError) string {
	field := fmt.Sprintf("/configuration/publicEndpoints/%d", association.EndpointIndex)
	if association.AddressIndex >= 0 {
		field += fmt.Sprintf("/addresses/%d", association.AddressIndex)
	}
	if association.Field != "" {
		field += "/" + association.Field
	}
	return field
}

func publicationErrorDetails(err error) (int, string, string, string, string) {
	status, code, message := http.StatusInternalServerError, "storage_failed", "publication state could not be persisted"
	violationCode, violationMessage := "publication_invalid", "review this publication address"
	switch {
	case errors.Is(err, store.ErrPublicationDependency):
		status, code, message = http.StatusConflict, "publication_has_dependents", "remove desired, applied and executable references before this change"
	case errors.Is(err, store.ErrVersionConflict), errors.Is(err, store.ErrConflict):
		status, code, message = http.StatusConflict, "version_conflict", "reload the current revision before retrying"
	case errors.Is(err, store.ErrPublicationConflict):
		status, code, message = http.StatusConflict, "publication_conflict", "the requested address is unavailable"
		violationCode, violationMessage = "address_conflict", "this address is already reserved"
	case errors.Is(err, store.ErrNotFound), errors.Is(err, store.ErrBindingNotFound), errors.Is(err, store.ErrClusterNotFound):
		status, code, message = http.StatusNotFound, "resource_not_found", "resource was not found"
	case errors.Is(err, domain.ErrPublicationName), errors.Is(err, kubernetesbinding.ErrHTTPBindingInvalid), errors.Is(err, kubernetesbinding.ErrHTTPListenerInvalid):
		status, code, message = http.StatusBadRequest, "publication_invalid", "publication configuration is invalid"
	case errors.Is(err, domain.ErrPublicationUnsupported), errors.Is(err, kubernetesbinding.ErrHTTPBindingUnsupported):
		status, code, message = http.StatusBadRequest, "publication_mode_unsupported", "this publication mode is not supported"
	case errors.Is(err, kubernetesbinding.ErrHTTPSelectionRequired):
		status, code, message = http.StatusBadRequest, "publication_listener_required", "select one of the eligible listeners"
		violationCode, violationMessage = "listener_required", "select a listener for this address"
	case errors.Is(err, kubernetesbinding.ErrHTTPDestinationUnavailable):
		status, code, message = http.StatusBadRequest, "publication_destination_unavailable", "the selected listener does not cover the requested name"
		violationCode, violationMessage = "listener_unavailable", "the selected listener does not cover this address"
	case errors.Is(err, domain.ErrPublicationNotGranted):
		status = http.StatusBadRequest
		code, message = "publication_not_granted", "the selected domain and destination are not granted in this context"
		violationCode, violationMessage = "domain_not_granted", "this destination is not granted to the Workspace"
	case errors.Is(err, domain.ErrPublicationReserved):
		status = http.StatusBadRequest
		code, message = "publication_reserved", "the requested name is reserved"
		violationCode, violationMessage = "name_reserved", "this name is reserved"
	case errors.Is(err, domain.ErrPublicationLimit):
		status = http.StatusBadRequest
		code, message = "publication_limit_exceeded", "publication exceeds the supported limit"
		violationCode, violationMessage = "address_limit", "use no more than ten addresses"
	}
	return status, code, message, violationCode, violationMessage
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
	cursor, ok := validPublicationPage(w, r, params.Cursor, params.Limit, "grants")
	if !ok {
		return
	}
	limit := publicationPageSize(params.Limit)
	items, err := h.server.store.PublicationGrants(r.Context(), id, cursor.A, cursor.B, limit)
	if err != nil {
		writePublicationError(w, r, err)
		return
	}
	hasMore := len(items) > limit
	if hasMore {
		items = items[:limit]
	}
	var next any
	if hasMore {
		last := items[len(items)-1]
		next = encodePublicationCursor(publicationCursor{Kind: "grants", A: last.WorkspaceID, B: last.BindingID})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "hasMore": hasMore, "nextCursor": next})
}

func validPublicationPage(w http.ResponseWriter, r *http.Request, raw *string, limit *int, kind string) (publicationCursor, bool) {
	if limit != nil && (*limit < 1 || *limit > maximumPublicationPageSize) {
		writeError(w, http.StatusBadRequest, "pagination_invalid", "limit must be between 1 and 100", r)
		return publicationCursor{}, false
	}
	cursor, err := decodePublicationCursor(raw, kind)
	if err != nil {
		writeError(w, http.StatusBadRequest, "pagination_invalid", "cursor is invalid for this collection", r)
		return publicationCursor{}, false
	}
	return cursor, true
}
