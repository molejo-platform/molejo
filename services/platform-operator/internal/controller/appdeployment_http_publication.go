package controller

import (
	"context"
	"errors"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	platformv1alpha1 "github.com/molejo-platform/molejo/packages/kubernetes-api/apis/platform/v1alpha1"
)

func (r *AppDeploymentReconciler) applyHTTPPublication(
	ctx context.Context,
	appDeployment *platformv1alpha1.AppDeployment,
) (*gatewayv1.HTTPRoute, controllerutil.OperationResult, string, error) {
	route := &gatewayv1.HTTPRoute{ObjectMeta: metav1.ObjectMeta{
		Name: appDeployment.Name, Namespace: appDeployment.Namespace,
	}}
	endpoint, public := publicEndpoint(appDeployment, platformv1alpha1.AppDeploymentPublicEndpointType("HTTP"))
	if !public {
		if err := r.Get(ctx, client.ObjectKeyFromObject(route), route); err != nil {
			if apierrors.IsNotFound(err) {
				return nil, controllerutil.OperationResultNone, "", nil
			}
			return nil, controllerutil.OperationResultNone, "", err
		}
		if !metav1.IsControlledBy(route, appDeployment) {
			return nil, controllerutil.OperationResultNone, route.ResourceVersion, errOwnershipConflict
		}
		resourceVersion := route.ResourceVersion
		if err := r.Delete(ctx, route); err != nil && !apierrors.IsNotFound(err) {
			return nil, controllerutil.OperationResultNone, resourceVersion, fmt.Errorf("delete HTTPRoute: %w", err)
		}
		return nil, controllerutil.OperationResultUpdated, resourceVersion, nil
	}

	if len(appDeployment.Spec.PublicEndpoints) == 0 {
		if err := r.ensureHostnameAvailable(ctx, appDeployment); err != nil {
			if errors.Is(err, errHostnameConflict) {
				if cleanupErr := r.deleteOwnedHTTPRoute(ctx, appDeployment, route); cleanupErr != nil {
					return nil, controllerutil.OperationResultNone, "", cleanupErr
				}
			}
			return nil, controllerutil.OperationResultNone, "", err
		}
	}

	var resourceVersionBefore string
	operation, err := controllerutil.CreateOrPatch(ctx, r.Client, route, func() error {
		resourceVersionBefore = route.ResourceVersion
		if !route.CreationTimestamp.IsZero() && !metav1.IsControlledBy(route, appDeployment) {
			return errOwnershipConflict
		}
		if err := controllerutil.SetControllerReference(appDeployment, route, r.Scheme); err != nil {
			return fmt.Errorf("set HTTPRoute owner reference: %w", err)
		}
		return configureHTTPRoute(route, appDeployment, endpoint)
	})
	return route, operation, resourceVersionBefore, err
}

func configureHTTPRoute(
	route *gatewayv1.HTTPRoute,
	appDeployment *platformv1alpha1.AppDeployment,
	endpoint platformv1alpha1.AppDeploymentPublicEndpoint,
) error {
	if route.Labels == nil {
		route.Labels = map[string]string{}
	}
	route.Labels[appDeploymentLabel] = appDeployment.Name
	route.Labels[managedByLabel] = managedByValue

	gatewayGroup := gatewayv1.Group(gatewayv1.GroupName)
	gatewayKind := gatewayv1.Kind("Gateway")
	gatewayNamespace := gatewayv1.Namespace(sharedGatewayNamespace)
	httpsSection := gatewayv1.SectionName(sharedGatewaySection)
	port, found := portByName(appDeployment, endpoint.PortName)
	if !found {
		return fmt.Errorf("public HTTP endpoint references an unknown port")
	}
	backendPort := gatewayv1.PortNumber(port)
	backendGroup := gatewayv1.Group("")
	serviceKind := gatewayv1.Kind("Service")
	pathType := gatewayv1.PathMatchPathPrefix
	pathValue := "/"
	weight := int32(1)
	route.Spec = gatewayv1.HTTPRouteSpec{
		CommonRouteSpec: gatewayv1.CommonRouteSpec{ParentRefs: []gatewayv1.ParentReference{{
			Group: &gatewayGroup, Kind: &gatewayKind, Name: sharedGatewayName,
			Namespace: &gatewayNamespace, SectionName: &httpsSection,
		}}},
		Hostnames: []gatewayv1.Hostname{gatewayv1.Hostname(publicEndpointHostname(endpoint))},
		Rules: []gatewayv1.HTTPRouteRule{{
			Matches: []gatewayv1.HTTPRouteMatch{{Path: &gatewayv1.HTTPPathMatch{
				Type: &pathType, Value: &pathValue,
			}}},
			BackendRefs: []gatewayv1.HTTPBackendRef{{
				BackendRef: gatewayv1.BackendRef{BackendObjectReference: gatewayv1.BackendObjectReference{
					Group: &backendGroup, Kind: &serviceKind,
					Name: gatewayv1.ObjectName(appDeployment.Name), Port: &backendPort,
				}, Weight: &weight},
			}},
		}},
	}
	return nil
}

func (r *AppDeploymentReconciler) deleteOwnedHTTPRoute(
	ctx context.Context,
	appDeployment *platformv1alpha1.AppDeployment,
	route *gatewayv1.HTTPRoute,
) error {
	if err := r.Get(ctx, client.ObjectKeyFromObject(appDeployment), route); err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("get losing HTTPRoute claim: %w", err)
	}
	if !metav1.IsControlledBy(route, appDeployment) {
		return nil
	}
	if err := r.Delete(ctx, route); err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("delete losing HTTPRoute claim: %w", err)
	}
	return nil
}

func (r *AppDeploymentReconciler) ensureHostnameAvailable(
	ctx context.Context,
	appDeployment *platformv1alpha1.AppDeployment,
) error {
	appDeployments := &platformv1alpha1.AppDeploymentList{}
	if err := r.List(ctx, appDeployments); err != nil {
		return fmt.Errorf("list AppDeployments for hostname ownership: %w", err)
	}
	winner := appDeployment
	winnerHasRoute, err := r.hasPublicHostnameRoute(ctx, appDeployment)
	if err != nil {
		return err
	}
	for index := range appDeployments.Items {
		candidate := &appDeployments.Items[index]
		if appDeploymentPublicHostname(candidate) == "" ||
			appDeploymentPublicHostname(candidate) != appDeploymentPublicHostname(appDeployment) ||
			candidate.UID == appDeployment.UID {
			continue
		}
		candidateHasRoute, err := r.hasPublicHostnameRoute(ctx, candidate)
		if err != nil {
			return err
		}
		if (candidateHasRoute && !winnerHasRoute) ||
			(candidateHasRoute == winnerHasRoute && hostnameClaimPrecedes(candidate, winner)) {
			winner = candidate
			winnerHasRoute = candidateHasRoute
		}
	}
	if winner.UID != appDeployment.UID {
		return errHostnameConflict
	}
	return nil
}

func (r *AppDeploymentReconciler) hasPublicHostnameRoute(
	ctx context.Context,
	appDeployment *platformv1alpha1.AppDeployment,
) (bool, error) {
	route := &gatewayv1.HTTPRoute{}
	if err := r.Get(ctx, client.ObjectKeyFromObject(appDeployment), route); err != nil {
		if apierrors.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("get HTTPRoute for hostname ownership: %w", err)
	}
	return metav1.IsControlledBy(route, appDeployment) &&
		len(route.Spec.Hostnames) == 1 &&
		string(route.Spec.Hostnames[0]) == appDeploymentPublicHostname(appDeployment), nil
}

func hostnameClaimPrecedes(left, right *platformv1alpha1.AppDeployment) bool {
	if !left.CreationTimestamp.Time.Equal(right.CreationTimestamp.Time) {
		return left.CreationTimestamp.Before(&right.CreationTimestamp)
	}
	leftKey := left.Namespace + "/" + left.Name
	rightKey := right.Namespace + "/" + right.Name
	if leftKey != rightKey {
		return leftKey < rightKey
	}
	return string(left.UID) < string(right.UID)
}

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
		return workloadDecision{state: workloadStateProgressing, reason: platformv1alpha1.ReasonHTTPRouteProgressing,
			message: "The public HTTP route is being reconciled."}
	}
	conditions := publicationParentConditions(route)
	accepted := meta.FindStatusCondition(conditions, string(gatewayv1.RouteConditionAccepted))
	if accepted == nil || accepted.ObservedGeneration != route.Generation {
		return workloadDecision{state: workloadStateProgressing, reason: platformv1alpha1.ReasonHTTPRouteProgressing,
			message: "The public HTTP route is awaiting Gateway acceptance."}
	}
	if accepted.Status == metav1.ConditionFalse {
		return workloadDecision{state: workloadStateDegraded, reason: platformv1alpha1.ReasonHTTPRouteRejected,
			message: "The shared Gateway rejected the public HTTP route."}
	}
	resolved := meta.FindStatusCondition(conditions, string(gatewayv1.RouteConditionResolvedRefs))
	if accepted.Status != metav1.ConditionTrue || resolved == nil || resolved.ObservedGeneration != route.Generation {
		return workloadDecision{state: workloadStateProgressing, reason: platformv1alpha1.ReasonHTTPRouteProgressing,
			message: "The public HTTP route is awaiting resolved backend references."}
	}
	if resolved.Status == metav1.ConditionFalse {
		return workloadDecision{state: workloadStateDegraded, reason: platformv1alpha1.ReasonHTTPRouteRejected,
			message: "The public HTTP route has invalid backend references."}
	}
	if resolved.Status != metav1.ConditionTrue {
		return workloadDecision{state: workloadStateProgressing, reason: platformv1alpha1.ReasonHTTPRouteProgressing,
			message: "The public HTTP route is awaiting resolved backend references."}
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
