package api

import (
	"errors"
	"net/http"

	"github.com/molejo-platform/molejo/services/control-plane-api/internal/api/generated"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/audit"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/authorization"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/historicalmetrics"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/store"
)

func (h *generatedHandler) GetHistoricalMetricBinding(w http.ResponseWriter, r *http.Request, clusterID generated.ClusterId) {
	if _, ok := h.authorizeInstallation(w, r, false, authorization.ManageBindings); !ok {
		return
	}
	binding, err := h.server.historicalMetricBindings.Get(r.Context(), string(clusterID))
	if err != nil {
		writeHistoricalMetricBindingError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, historicalMetricBindingResponse(binding))
}

func (h *generatedHandler) PutHistoricalMetricBinding(w http.ResponseWriter, r *http.Request, clusterID generated.ClusterId, params generated.PutHistoricalMetricBindingParams) {
	administrator, ok := h.authorizeInstallation(w, r, true, authorization.ManageBindings)
	if !ok {
		return
	}
	var input generated.HistoricalMetricBindingInput
	if decodeJSON(r, &input) != nil || input.Endpoint == nil || input.Provider != generated.HistoricalMetricBindingInputProviderPrometheusCompatible {
		writeError(w, http.StatusBadRequest, "historical_metric_binding_invalid", "historical metric binding is invalid", r)
		return
	}
	var expectedVersion *int64
	if params.IfMatch != nil {
		value := int64(*params.IfMatch)
		expectedVersion = &value
	}
	event := h.server.auditEvent(r, "installation.binding.historical_metrics.put", "ClusterHistoricalMetricBinding", string(clusterID), audit.Succeeded)
	event.ActorUserID = &administrator.ID
	binding, err := h.server.historicalMetricBindings.Configure(r.Context(), string(clusterID), *input.Endpoint, administrator.ID, expectedVersion, event)
	if err != nil {
		writeHistoricalMetricBindingError(w, r, err)
		return
	}
	status := http.StatusCreated
	if expectedVersion != nil {
		status = http.StatusOK
	}
	writeJSON(w, status, historicalMetricBindingResponse(binding))
}

func (h *generatedHandler) DeleteHistoricalMetricBinding(w http.ResponseWriter, r *http.Request, clusterID generated.ClusterId, params generated.DeleteHistoricalMetricBindingParams) {
	administrator, ok := h.authorizeInstallation(w, r, true, authorization.ManageBindings)
	if !ok {
		return
	}
	event := h.server.auditEvent(r, "installation.binding.historical_metrics.delete", "ClusterHistoricalMetricBinding", string(clusterID), audit.Succeeded)
	event.ActorUserID = &administrator.ID
	if err := h.server.historicalMetricBindings.Delete(r.Context(), string(clusterID), administrator.ID, int64(params.IfMatch), event); err != nil {
		writeHistoricalMetricBindingError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func historicalMetricBindingResponse(binding historicalmetrics.Binding) generated.HistoricalMetricBinding {
	return generated.HistoricalMetricBinding{
		ClusterId:   binding.ClusterID,
		Provider:    generated.HistoricalMetricBindingProvider(binding.Provider),
		Health:      generated.HistoricalMetricBindingHealth(binding.Health),
		Conformant:  binding.Conformant,
		ReasonCode:  binding.ReasonCode,
		Limitations: append([]string{}, binding.Limitations...),
		ObservedAt:  binding.ObservedAt,
		Version:     int(binding.Version),
		CreatedAt:   binding.CreatedAt,
		UpdatedAt:   binding.UpdatedAt,
	}
}

func writeHistoricalMetricBindingError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, historicalmetrics.ErrNotFound), errors.Is(err, store.ErrClusterNotFound):
		writeError(w, http.StatusNotFound, "historical_metric_binding_not_found", "historical metric binding or cluster was not found", r)
	case errors.Is(err, store.ErrVersionConflict):
		writeError(w, http.StatusConflict, "version_conflict", "resource version does not match", r)
	default:
		writeError(w, http.StatusBadRequest, "historical_metric_binding_invalid", "historical metric binding is invalid", r)
	}
}
