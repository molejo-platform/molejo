package controller

import (
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	platformv1alpha1 "github.com/molejo-platform/molejo/packages/kubernetes-api/apis/platform/v1alpha1"
)

const (
	sharedGatewayName      = "molejo"
	sharedGatewayNamespace = "molejo-system"
	sharedGatewaySection   = "https-molejo"
)

func publicationStatuses(appDeployment *platformv1alpha1.AppDeployment, httpRoute *gatewayv1.HTTPRoute, tcpStatus *platformv1alpha1.AppDeploymentEndpointStatus) []platformv1alpha1.AppDeploymentEndpointStatus {
	statuses := make([]platformv1alpha1.AppDeploymentEndpointStatus, 0, len(appDeployment.Spec.PublicEndpoints))
	if endpoint, public := publicEndpoint(appDeployment, platformv1alpha1.AppDeploymentPublicEndpointType("HTTP")); public {
		status := platformv1alpha1.AppDeploymentEndpointStatus{Name: endpoint.Name, Type: endpoint.Type, Reason: platformv1alpha1.ReasonHTTPRouteProgressing}
		if httpRoute != nil {
			conditions := publicationParentConditions(httpRoute)
			accepted := meta.FindStatusCondition(conditions, string(gatewayv1.RouteConditionAccepted))
			resolved := meta.FindStatusCondition(conditions, string(gatewayv1.RouteConditionResolvedRefs))
			if conditionIsCurrent(accepted, httpRoute.Generation) && accepted.Status == metav1.ConditionTrue && conditionIsCurrent(resolved, httpRoute.Generation) && resolved.Status == metav1.ConditionTrue {
				status.Ready = true
				status.Reason = "HTTPRouteAccepted"
			} else if conditionIsCurrent(accepted, httpRoute.Generation) && accepted.Status == metav1.ConditionFalse || conditionIsCurrent(resolved, httpRoute.Generation) && resolved.Status == metav1.ConditionFalse {
				status.Reason = platformv1alpha1.ReasonHTTPRouteRejected
			}
		}
		statuses = append(statuses, status)
	}
	if tcpStatus != nil {
		statuses = append(statuses, *tcpStatus)
	}
	return statuses
}

func publicHostname(slug string) string {
	return slug + ".molejo.dev"
}

func publicEndpointHostname(endpoint platformv1alpha1.AppDeploymentPublicEndpoint) string {
	if endpoint.Hostname != "" {
		return endpoint.Hostname
	}
	return publicHostname(endpoint.HostnameLabel)
}

func appDeploymentPublicHostname(appDeployment *platformv1alpha1.AppDeployment) string {
	if endpoint, ok := publicEndpoint(appDeployment, platformv1alpha1.AppDeploymentPublicEndpointType("HTTP")); ok {
		return publicEndpointHostname(endpoint)
	}
	return ""
}

func publicEndpoint(appDeployment *platformv1alpha1.AppDeployment, endpointType platformv1alpha1.AppDeploymentPublicEndpointType) (platformv1alpha1.AppDeploymentPublicEndpoint, bool) {
	for _, endpoint := range appDeployment.Spec.PublicEndpoints {
		if endpoint.Type == endpointType {
			return endpoint, true
		}
	}
	if endpointType == "HTTP" && appDeployment.Spec.Exposure == platformv1alpha1.ExposurePublic {
		return platformv1alpha1.AppDeploymentPublicEndpoint{Name: "web", Type: "HTTP", PortName: httpPortName, HostnameLabel: appDeployment.Spec.Slug}, true
	}
	return platformv1alpha1.AppDeploymentPublicEndpoint{}, false
}

func conditionIsCurrent(condition *metav1.Condition, generation int64) bool {
	return condition != nil && condition.ObservedGeneration == generation
}
