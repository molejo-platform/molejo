package controller

import (
	"context"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	platformv1alpha1 "github.com/molejo-platform/molejo/packages/kubernetes-api/apis/platform/v1alpha1"
)

func (r *AppDeploymentReconciler) getPublicationGateway(ctx context.Context, target platformv1alpha1.HTTPDestination) (*gatewayv1.Gateway, error) {
	gateway := &gatewayv1.Gateway{}
	err := r.Get(ctx, client.ObjectKey{Namespace: target.GatewayNamespace, Name: target.GatewayName}, gateway)
	if apierrors.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get shared Gateway: %w", err)
	}
	return gateway, nil
}

func evaluatePublicationGateway(
	gateway *gatewayv1.Gateway,
	publication workloadDecision,
	section string,
) workloadDecision {
	if publication.state == workloadStateDegraded {
		return publication
	}
	if gateway == nil {
		return workloadDecision{
			state: workloadStateDegraded, reason: platformv1alpha1.ReasonGatewayRejected,
			message: "The shared HTTPS Gateway is unavailable.",
		}
	}
	programmed := meta.FindStatusCondition(gateway.Status.Conditions, string(gatewayv1.GatewayConditionProgrammed))
	if conditionIsCurrent(programmed, gateway.Generation) && programmed.Status == metav1.ConditionFalse {
		return workloadDecision{
			state: workloadStateDegraded, reason: platformv1alpha1.ReasonGatewayRejected,
			message: "The shared HTTPS Gateway is unavailable.",
		}
	}

	listenerConditions, found := publicationGatewayListenerConditions(gateway, section)
	if !found {
		if !conditionIsCurrent(programmed, gateway.Generation) || programmed.Status != metav1.ConditionTrue {
			return workloadDecision{
				state: workloadStateProgressing, reason: platformv1alpha1.ReasonGatewayProgressing,
				message: "The shared HTTPS Gateway listener is converging.",
			}
		}
		return workloadDecision{
			state: workloadStateDegraded, reason: platformv1alpha1.ReasonGatewayRejected,
			message: "The shared HTTPS Gateway listener is unavailable.",
		}
	}
	conditions := []*metav1.Condition{
		programmed,
		meta.FindStatusCondition(listenerConditions, string(gatewayv1.ListenerConditionAccepted)),
		meta.FindStatusCondition(listenerConditions, string(gatewayv1.ListenerConditionProgrammed)),
		meta.FindStatusCondition(listenerConditions, string(gatewayv1.ListenerConditionResolvedRefs)),
	}
	for _, condition := range conditions {
		if conditionIsCurrent(condition, gateway.Generation) && condition.Status == metav1.ConditionFalse {
			return workloadDecision{
				state: workloadStateDegraded, reason: platformv1alpha1.ReasonGatewayRejected,
				message: "The shared HTTPS Gateway listener is unavailable.",
			}
		}
	}
	for _, condition := range conditions {
		if !conditionIsCurrent(condition, gateway.Generation) || condition.Status != metav1.ConditionTrue {
			return workloadDecision{
				state: workloadStateProgressing, reason: platformv1alpha1.ReasonGatewayProgressing,
				message: "The shared HTTPS Gateway listener is converging.",
			}
		}
	}
	return publication
}

func publicationGatewayListenerConditions(gateway *gatewayv1.Gateway, section string) ([]metav1.Condition, bool) {
	var conditions []metav1.Condition
	found := false
	for index := range gateway.Status.Listeners {
		listener := &gateway.Status.Listeners[index]
		if string(listener.Name) != section {
			continue
		}
		if found {
			return nil, false
		}
		found = true
		conditions = listener.Conditions
	}
	return conditions, found
}
