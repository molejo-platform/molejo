package controller

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	platformv1alpha1 "github.com/molejo-platform/molejo/packages/kubernetes-api/apis/platform/v1alpha1"
)

const (
	sharedGatewayName      = "molejo"
	sharedGatewayNamespace = "molejo-system"
	sharedGatewaySection   = "https-molejo"
)

func publicEndpoint(appDeployment *platformv1alpha1.AppDeployment, endpointType platformv1alpha1.AppDeploymentPublicEndpointType) (platformv1alpha1.AppDeploymentPublicEndpoint, bool) {
	for _, endpoint := range appDeployment.Spec.PublicEndpoints {
		if endpoint.Type == endpointType {
			return endpoint, true
		}
	}
	return platformv1alpha1.AppDeploymentPublicEndpoint{}, false
}

func conditionIsCurrent(condition *metav1.Condition, generation int64) bool {
	return condition != nil && condition.ObservedGeneration == generation
}
