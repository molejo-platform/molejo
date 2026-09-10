package controller

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	platformv1alpha1 "github.com/molejo-platform/molejo/packages/kubernetes-api/apis/platform/v1alpha1"
)

func (r *AppDeploymentReconciler) applyTCPPublication(ctx context.Context, appDeployment *platformv1alpha1.AppDeployment) (*gatewayv1alpha2.TCPRoute, error) {
	route := &gatewayv1alpha2.TCPRoute{ObjectMeta: metav1.ObjectMeta{Name: appDeployment.Name, Namespace: appDeployment.Namespace}}
	endpoint, public := publicEndpoint(appDeployment, platformv1alpha1.AppDeploymentPublicEndpointType("TCP"))
	if !public {
		if err := r.Get(ctx, client.ObjectKeyFromObject(route), route); err != nil {
			return nil, client.IgnoreNotFound(err)
		}
		if !metav1.IsControlledBy(route, appDeployment) {
			return nil, errOwnershipConflict
		}
		return nil, client.IgnoreNotFound(r.Delete(ctx, route))
	}
	if endpoint.ExternalPort == nil {
		return nil, fmt.Errorf("public TCP endpoint has no allocated external port")
	}
	backendPort, found := portByName(appDeployment, endpoint.PortName)
	if !found {
		return nil, fmt.Errorf("public TCP endpoint references an unknown port")
	}
	_, err := controllerutil.CreateOrPatch(ctx, r.Client, route, func() error {
		if !route.CreationTimestamp.IsZero() && !metav1.IsControlledBy(route, appDeployment) {
			return errOwnershipConflict
		}
		if err := controllerutil.SetControllerReference(appDeployment, route, r.Scheme); err != nil {
			return fmt.Errorf("set TCPRoute owner reference: %w", err)
		}
		configureTCPRoute(route, appDeployment, endpoint, backendPort)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return route, nil
}

func configureTCPRoute(
	route *gatewayv1alpha2.TCPRoute,
	appDeployment *platformv1alpha1.AppDeployment,
	endpoint platformv1alpha1.AppDeploymentPublicEndpoint,
	backendPort int32,
) {
	if route.Labels == nil {
		route.Labels = map[string]string{}
	}
	route.Labels[appDeploymentLabel] = appDeployment.Name
	route.Labels[managedByLabel] = managedByValue
	gatewayGroup := gatewayv1.Group(gatewayv1.GroupName)
	gatewayKind := gatewayv1.Kind("Gateway")
	gatewayNamespace := gatewayv1.Namespace(sharedGatewayNamespace)
	section := gatewayv1.SectionName(fmt.Sprintf("tcp-%d", *endpoint.ExternalPort))
	serviceGroup := gatewayv1.Group("")
	serviceKind := gatewayv1.Kind("Service")
	servicePort := gatewayv1.PortNumber(backendPort)
	weight := int32(1)
	route.Spec = gatewayv1alpha2.TCPRouteSpec{
		CommonRouteSpec: gatewayv1.CommonRouteSpec{ParentRefs: []gatewayv1.ParentReference{{Group: &gatewayGroup, Kind: &gatewayKind, Name: sharedGatewayName, Namespace: &gatewayNamespace, SectionName: &section}}},
		Rules:           []gatewayv1alpha2.TCPRouteRule{{BackendRefs: []gatewayv1.BackendRef{{BackendObjectReference: gatewayv1.BackendObjectReference{Group: &serviceGroup, Kind: &serviceKind, Name: gatewayv1.ObjectName(appDeployment.Name), Port: &servicePort}, Weight: &weight}}}},
	}
}

func evaluateTCPPublication(appDeployment *platformv1alpha1.AppDeployment, route *gatewayv1alpha2.TCPRoute) (*workloadDecision, *platformv1alpha1.AppDeploymentEndpointStatus) {
	endpoint, public := publicEndpoint(appDeployment, platformv1alpha1.AppDeploymentPublicEndpointType("TCP"))
	if !public {
		return nil, nil
	}
	status := &platformv1alpha1.AppDeploymentEndpointStatus{Name: endpoint.Name, Type: endpoint.Type, Reason: "TCPRouteProgressing"}
	progressing := &workloadDecision{state: workloadStateProgressing, reason: "TCPRouteProgressing", message: "The public TCP route is awaiting Gateway acceptance."}
	if route == nil {
		return progressing, status
	}
	conditions := tcpPublicationParentConditions(route, endpoint.ExternalPort)
	accepted := meta.FindStatusCondition(conditions, string(gatewayv1.RouteConditionAccepted))
	resolved := meta.FindStatusCondition(conditions, string(gatewayv1.RouteConditionResolvedRefs))
	if conditionIsCurrent(accepted, route.Generation) && accepted.Status == metav1.ConditionFalse || conditionIsCurrent(resolved, route.Generation) && resolved.Status == metav1.ConditionFalse {
		status.Reason = platformv1alpha1.ReasonPublicationRejected
		return &workloadDecision{state: workloadStateDegraded, reason: platformv1alpha1.ReasonPublicationRejected, message: "The shared Gateway rejected the public TCP route."}, status
	}
	if conditionIsCurrent(accepted, route.Generation) && accepted.Status == metav1.ConditionTrue && conditionIsCurrent(resolved, route.Generation) && resolved.Status == metav1.ConditionTrue {
		status.Ready = true
		status.Reason = "TCPRouteAccepted"
		return nil, status
	}
	return progressing, status
}

func tcpPublicationParentConditions(route *gatewayv1alpha2.TCPRoute, externalPort *int32) []metav1.Condition {
	if externalPort == nil {
		return nil
	}
	section := gatewayv1.SectionName(fmt.Sprintf("tcp-%d", *externalPort))
	for _, parent := range route.Status.Parents {
		if parent.ParentRef.Name == sharedGatewayName && parent.ParentRef.SectionName != nil && *parent.ParentRef.SectionName == section {
			return parent.Conditions
		}
	}
	return nil
}
