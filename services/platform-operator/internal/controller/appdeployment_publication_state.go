package controller

import (
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	platformv1alpha1 "github.com/molejo-platform/molejo/packages/kubernetes-api/apis/platform/v1alpha1"
)

func evaluatePublication(
	appDeployment *platformv1alpha1.AppDeployment,
	route *gatewayv1.HTTPRoute,
	workload workloadDecision,
) workloadDecision {
	if _, public := publicEndpoint(appDeployment, platformv1alpha1.AppDeploymentPublicEndpointType("HTTP")); !public {
		return workload
	}
	if workload.state == workloadStateDegraded {
		return workload
	}
	if route == nil {
		return workloadDecision{
			state: workloadStateProgressing, reason: platformv1alpha1.ReasonHTTPRouteProgressing,
			message: "The public HTTP route is being reconciled.",
		}
	}
	conditions := publicationParentConditions(route)
	accepted := meta.FindStatusCondition(conditions, string(gatewayv1.RouteConditionAccepted))
	if accepted == nil || accepted.ObservedGeneration != route.Generation {
		return workloadDecision{
			state: workloadStateProgressing, reason: platformv1alpha1.ReasonHTTPRouteProgressing,
			message: "The public HTTP route is awaiting Gateway acceptance.",
		}
	}
	if accepted.Status == metav1.ConditionFalse {
		return workloadDecision{
			state: workloadStateDegraded, reason: platformv1alpha1.ReasonHTTPRouteRejected,
			message: "The shared Gateway rejected the public HTTP route.",
		}
	}
	resolved := meta.FindStatusCondition(conditions, string(gatewayv1.RouteConditionResolvedRefs))
	if accepted.Status != metav1.ConditionTrue || resolved == nil || resolved.ObservedGeneration != route.Generation {
		return workloadDecision{
			state: workloadStateProgressing, reason: platformv1alpha1.ReasonHTTPRouteProgressing,
			message: "The public HTTP route is awaiting resolved backend references.",
		}
	}
	if resolved.Status == metav1.ConditionFalse {
		return workloadDecision{
			state: workloadStateDegraded, reason: platformv1alpha1.ReasonHTTPRouteRejected,
			message: "The public HTTP route has invalid backend references.",
		}
	}
	if resolved.Status != metav1.ConditionTrue {
		return workloadDecision{
			state: workloadStateProgressing, reason: platformv1alpha1.ReasonHTTPRouteProgressing,
			message: "The public HTTP route is awaiting resolved backend references.",
		}
	}
	return workload
}

func publicationParentConditions(route *gatewayv1.HTTPRoute) []metav1.Condition {
	var conditions []metav1.Condition
	found := false
	for _, parent := range route.Status.Parents {
		if parent.ParentRef.Name != sharedGatewayName {
			continue
		}
		if parent.ParentRef.Group != nil && *parent.ParentRef.Group != gatewayv1.GroupName {
			continue
		}
		if parent.ParentRef.Kind != nil && *parent.ParentRef.Kind != "Gateway" {
			continue
		}
		if parent.ParentRef.Namespace == nil || *parent.ParentRef.Namespace != sharedGatewayNamespace {
			continue
		}
		if parent.ParentRef.SectionName == nil || *parent.ParentRef.SectionName != sharedGatewaySection {
			continue
		}
		if parent.ParentRef.Port != nil {
			continue
		}
		if found {
			return nil
		}
		found = true
		conditions = parent.Conditions
	}
	return conditions
}
