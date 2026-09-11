package controller

import (
	"context"

	"go.opentelemetry.io/otel/attribute"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	platformv1alpha1 "github.com/molejo-platform/molejo/packages/kubernetes-api/apis/platform/v1alpha1"
)

type workloadProjection struct {
	reference corev1.LocalObjectReference
	decision  workloadDecision
}

type publicationProjection struct {
	decision workloadDecision
	statuses []platformv1alpha1.AppDeploymentEndpointStatus
}

func (r *AppDeploymentReconciler) reconcileWorkload(
	ctx context.Context,
	appDeployment *platformv1alpha1.AppDeployment,
) (workloadProjection, error) {
	tracer := r.tracer()
	var projection workloadProjection
	if appDeployment.Spec.Workload.Kind == platformv1alpha1.WorkloadStateful {
		applyCtx, applySpan := tracer.Start(ctx, "kubernetes.statefulset.apply")
		statefulSet, _, _, err := r.applyStatefulSet(applyCtx, appDeployment)
		finishSpan(applySpan, err)
		if err != nil {
			return workloadProjection{}, err
		}
		replaced, err := r.replaceStaleUnreadyStatefulPod(ctx, statefulSet)
		if err != nil {
			return workloadProjection{}, err
		}
		if replaced {
			projection.decision = workloadDecision{state: workloadStateProgressing, reason: "StatefulSetReplacingStalePod", message: "The managed StatefulSet is replacing an unhealthy Pod from an obsolete revision."}
		} else {
			projection.decision = evaluateWorkload(snapshotStatefulSet(statefulSet))
		}
		projection.reference = corev1.LocalObjectReference{Name: statefulSet.Name}
		if err := r.deleteOwnedDeployment(ctx, appDeployment); err != nil {
			return workloadProjection{}, err
		}
	} else {
		applyCtx, applySpan := tracer.Start(ctx, "kubernetes.deployment.apply")
		deployment, operation, versionBefore, err := r.applyDeployment(applyCtx, appDeployment)
		finishSpan(applySpan, err)
		if err != nil {
			return workloadProjection{}, err
		}
		projection.decision = evaluateWorkload(snapshotDeployment(deployment, desiredReplicas(appDeployment)))
		projection.reference = corev1.LocalObjectReference{Name: deployment.Name}
		r.recordDeploymentOperation(ctx, appDeployment, operation, versionBefore,
			deployment.ResourceVersion, projection.decision)
		if err := r.deleteOwnedStatefulSet(ctx, appDeployment); err != nil {
			return workloadProjection{}, err
		}
	}

	_, evaluateSpan := tracer.Start(ctx, "domain.workload.evaluate")
	evaluateSpan.SetAttributes(
		attribute.String("molejo.reconciliation.state", string(projection.decision.state)),
		attribute.String("molejo.reconciliation.reason", projection.decision.reason),
	)
	evaluateSpan.End()
	return projection, nil
}

func (r *AppDeploymentReconciler) reconcileService(
	ctx context.Context,
	appDeployment *platformv1alpha1.AppDeployment,
	decision workloadDecision,
) error {
	serviceCtx, serviceSpan := r.tracer().Start(ctx, "kubernetes.service.apply")
	service, operation, versionBefore, err := r.applyService(serviceCtx, appDeployment)
	finishSpan(serviceSpan, err)
	if err != nil {
		return err
	}
	r.recordServiceOperation(ctx, appDeployment, operation, versionBefore,
		service.ResourceVersion, decision)
	return nil
}

func (r *AppDeploymentReconciler) reconcilePublication(
	ctx context.Context,
	appDeployment *platformv1alpha1.AppDeployment,
	decision workloadDecision,
) (publicationProjection, error) {
	ctx, span := r.tracer().Start(ctx, "kubernetes.httproute.apply")
	defer span.End()
	statuses := []platformv1alpha1.AppDeploymentEndpointStatus{}
	desired := map[string]bool{}
	if endpoint, ok := publicEndpoint(appDeployment, "HTTP"); ok {
		status := platformv1alpha1.AppDeploymentEndpointStatus{Name: endpoint.Name, Type: endpoint.Type, Ready: true, Reason: "HTTPRouteAccepted"}
		endpointDecision := workloadDecision{state: workloadStateReady, reason: "HTTPRouteAccepted"}
		for _, address := range endpoint.Addresses {
			route, operation, before, err := r.applyHTTPAddress(ctx, appDeployment, endpoint, address)
			if err != nil {
				return publicationProjection{}, err
			}
			desired[route.Name] = true
			addressDecision := evaluatePublication(appDeployment, route, workloadDecision{state: workloadStateReady})
			gateway, err := r.getPublicationGateway(ctx, address.Destination)
			gatewayDecision := workloadDecision{state: workloadStateReady}
			if err == nil {
				gatewayDecision = evaluatePublicationGateway(gateway, gatewayDecision, address.Destination.SectionName)
			} else {
				gatewayDecision = workloadDecision{state: workloadStateProgressing, reason: "GatewayInspectionUnavailable", message: "Gateway inspection is unavailable."}
			}
			evidence := platformv1alpha1.AppDeploymentHTTPAddressStatus{Hostname: address.Hostname, Destination: address.Destination, RouteName: route.Name, RouteUID: string(route.UID), RouteGeneration: route.Generation}
			if gateway != nil {
				evidence.GatewayUID = string(gateway.UID)
			}
			evidence.Conditions = addressConditions(appDeployment, address, addressDecision, gatewayDecision)
			status.Addresses = append(status.Addresses, evidence)
			combined := combinePublicationDecision(addressDecision, gatewayDecision)
			endpointDecision = combinePublicationDecision(endpointDecision, combined)
			decision = combinePublicationDecision(decision, combined)
			r.recordHTTPRouteOperation(ctx, appDeployment, operation, before, route, decision)
		}
		status.Ready = endpointDecision.state == workloadStateReady
		status.Reason = endpointDecision.reason
		statuses = append(statuses, status)
	}
	pending, err := r.withdrawHTTPAddresses(ctx, appDeployment, desired)
	if err != nil {
		return publicationProjection{}, err
	}
	if pending {
		decision = combinePublicationDecision(decision, workloadDecision{state: workloadStateProgressing, reason: "HTTPRouteWithdrawalPending", message: "Waiting for owned HTTP routes to be removed."})
	}
	tcpRoute, err := r.applyTCPPublication(ctx, appDeployment)
	if err != nil {
		return publicationProjection{}, err
	}
	tcpDecision, tcpStatus := evaluateTCPPublication(appDeployment, tcpRoute)
	if tcpDecision != nil {
		decision = combinePublicationDecision(decision, *tcpDecision)
	}
	if tcpStatus != nil {
		statuses = append(statuses, *tcpStatus)
	}
	return publicationProjection{decision: decision, statuses: statuses}, nil
}

func combinePublicationDecision(current, next workloadDecision) workloadDecision {
	if current.state == workloadStateDegraded {
		return current
	}
	if next.state == workloadStateDegraded || (current.state == workloadStateReady && next.state != workloadStateReady) {
		return next
	}
	return current
}

func addressConditions(app *platformv1alpha1.AppDeployment, address platformv1alpha1.AppDeploymentHTTPAddress, route, gateway workloadDecision) []metav1.Condition {
	values := []metav1.Condition{}
	for _, item := range []struct {
		kind     string
		decision workloadDecision
	}{{"RouteReady", route}, {"GatewayReady", gateway}} {
		state := metav1.ConditionUnknown
		reason := item.decision.reason
		if item.decision.state == workloadStateReady {
			state = metav1.ConditionTrue
			reason = "Ready"
		} else if item.decision.state == workloadStateDegraded {
			state = metav1.ConditionFalse
		}
		if reason == "" {
			reason = "Pending"
		}
		values = append(values, metav1.Condition{Type: item.kind, Status: state, Reason: reason, ObservedGeneration: app.Generation, LastTransitionTime: metav1.Now()})
	}
	for _, kind := range []string{"ConnectivityVerified", "ServedTLSVerified"} {
		values = append(values, metav1.Condition{Type: kind, Status: metav1.ConditionUnknown, Reason: "NotInspected", ObservedGeneration: app.Generation, LastTransitionTime: metav1.Now()})
	}
	// Keep transition times stable across reconciles with identical facts.
	for _, oldEndpoint := range app.Status.EndpointStatuses {
		for _, oldAddress := range oldEndpoint.Addresses {
			if oldAddress.Hostname != address.Hostname || oldAddress.Destination != address.Destination {
				continue
			}
			for i := range values {
				for _, old := range oldAddress.Conditions {
					if old.Type == values[i].Type && old.Status == values[i].Status {
						values[i].LastTransitionTime = old.LastTransitionTime
					}
				}
			}
		}
	}
	return values
}
