package controller

import (
	"context"

	"github.com/prometheus/client_golang/prometheus"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	controllermetrics "sigs.k8s.io/controller-runtime/pkg/metrics"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	platformv1alpha1 "github.com/molejo-platform/molejo/packages/kubernetes-api/apis/platform/v1alpha1"
)

const tracerName = "github.com/molejo-platform/molejo/services/platform-operator"

var stateTransitions = prometheus.NewCounterVec(prometheus.CounterOpts{
	Namespace: "molejo_platform_operator",
	Name:      "state_transitions_total",
	Help:      "Number of AppDeployment state transitions observed by the platform operator.",
}, []string{"state", "reason"})

func init() {
	controllermetrics.Registry.MustRegister(stateTransitions)
}

func (r *AppDeploymentReconciler) recordDeploymentOperation(
	ctx context.Context,
	appDeployment *platformv1alpha1.AppDeployment,
	operation controllerutil.OperationResult,
	resourceVersionBefore string,
	resourceVersionAfter string,
	decision workloadDecision,
) {
	if resourceVersionBefore != "" && resourceVersionBefore == resourceVersionAfter {
		return
	}
	var reason string
	switch operation {
	case controllerutil.OperationResultCreated:
		reason = "DeploymentCreated"
	case controllerutil.OperationResultUpdated, controllerutil.OperationResultUpdatedStatus,
		controllerutil.OperationResultUpdatedStatusOnly:
		reason = "DeploymentUpdated"
	default:
		return
	}

	ctrl.LoggerFrom(ctx).Info("managed Deployment changed",
		"uid", appDeployment.UID,
		"generation", appDeployment.Generation,
		"observedGeneration", appDeployment.Status.ObservedGeneration,
		"operation", string(operation),
		"state", decision.state,
		"reason", decision.reason,
	)
	if r.Recorder != nil {
		r.Recorder.Event(appDeployment, corev1.EventTypeNormal, reason, "The managed Deployment was reconciled.")
	}
}

func (r *AppDeploymentReconciler) recordServiceOperation(
	ctx context.Context,
	appDeployment *platformv1alpha1.AppDeployment,
	operation controllerutil.OperationResult,
	resourceVersionBefore string,
	resourceVersionAfter string,
	decision workloadDecision,
) {
	if resourceVersionBefore != "" && resourceVersionBefore == resourceVersionAfter {
		return
	}
	var reason string
	switch operation {
	case controllerutil.OperationResultCreated:
		reason = "ServiceCreated"
	case controllerutil.OperationResultUpdated, controllerutil.OperationResultUpdatedStatus,
		controllerutil.OperationResultUpdatedStatusOnly:
		reason = "ServiceUpdated"
	default:
		return
	}

	ctrl.LoggerFrom(ctx).Info("managed Service changed",
		"uid", appDeployment.UID,
		"generation", appDeployment.Generation,
		"observedGeneration", appDeployment.Status.ObservedGeneration,
		"operation", string(operation),
		"state", decision.state,
		"reason", decision.reason,
	)
	if r.Recorder != nil {
		r.Recorder.Event(appDeployment, corev1.EventTypeNormal, reason, "The managed Service was reconciled.")
	}
}

func (r *AppDeploymentReconciler) recordHTTPRouteOperation(
	ctx context.Context,
	appDeployment *platformv1alpha1.AppDeployment,
	operation controllerutil.OperationResult,
	resourceVersionBefore string,
	route *gatewayv1.HTTPRoute,
	decision workloadDecision,
) {
	if route != nil && resourceVersionBefore != "" && resourceVersionBefore == route.ResourceVersion {
		return
	}
	var reason string
	switch {
	case route == nil && operation == controllerutil.OperationResultUpdated:
		reason = "HTTPRouteDeleted"
	case operation == controllerutil.OperationResultCreated:
		reason = "HTTPRouteCreated"
	case operation == controllerutil.OperationResultUpdated:
		reason = "HTTPRouteUpdated"
	default:
		return
	}

	ctrl.LoggerFrom(ctx).Info("managed HTTPRoute changed",
		"uid", appDeployment.UID,
		"generation", appDeployment.Generation,
		"observedGeneration", appDeployment.Status.ObservedGeneration,
		"operation", string(operation),
		"state", decision.state,
		"reason", decision.reason,
	)
	if r.Recorder != nil {
		r.Recorder.Event(appDeployment, corev1.EventTypeNormal, reason, "The managed HTTPRoute was reconciled.")
	}
}

func (r *AppDeploymentReconciler) recordStateTransition(
	ctx context.Context,
	appDeployment *platformv1alpha1.AppDeployment,
	decision workloadDecision,
) {
	stateTransitions.WithLabelValues(string(decision.state), decision.reason).Inc()
	ctrl.LoggerFrom(ctx).Info("AppDeployment state changed",
		"uid", appDeployment.UID,
		"generation", appDeployment.Generation,
		"observedGeneration", appDeployment.Status.ObservedGeneration,
		"state", decision.state,
		"reason", decision.reason,
	)

	if r.Recorder == nil || decision.state == workloadStateProgressing {
		return
	}
	eventType := corev1.EventTypeNormal
	if decision.state == workloadStateDegraded {
		eventType = corev1.EventTypeWarning
	}
	r.Recorder.Event(appDeployment, eventType, decision.reason, decision.message)
}

func (r *AppDeploymentReconciler) tracer() trace.Tracer {
	if r.Tracer != nil {
		return r.Tracer
	}
	return noop.NewTracerProvider().Tracer(tracerName)
}
