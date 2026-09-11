package controller

import (
	"context"
	"crypto/sha256"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	platformv1alpha1 "github.com/molejo-platform/molejo/packages/kubernetes-api/apis/platform/v1alpha1"
)

func httpRouteName(app *platformv1alpha1.AppDeployment, endpoint, hostname string) string {
	sum := sha256.Sum256([]byte(app.Name + "/" + endpoint + "/" + hostname))
	return fmt.Sprintf("http-%x", sum[:20])
}

func (r *AppDeploymentReconciler) applyHTTPAddress(ctx context.Context, app *platformv1alpha1.AppDeployment, endpoint platformv1alpha1.AppDeploymentPublicEndpoint, address platformv1alpha1.AppDeploymentHTTPAddress) (*gatewayv1.HTTPRoute, controllerutil.OperationResult, string, error) {
	route := &gatewayv1.HTTPRoute{ObjectMeta: metav1.ObjectMeta{Name: httpRouteName(app, endpoint.Name, address.Hostname), Namespace: app.Namespace}}
	before := ""
	operation, err := controllerutil.CreateOrPatch(ctx, r.Client, route, func() error {
		before = route.ResourceVersion
		if !route.CreationTimestamp.IsZero() && !metav1.IsControlledBy(route, app) {
			return errOwnershipConflict
		}
		if !route.DeletionTimestamp.IsZero() {
			return fmt.Errorf("HTTPRoute withdrawal is pending")
		}
		if err := controllerutil.SetControllerReference(app, route, r.Scheme); err != nil {
			return err
		}
		return configureHTTPRoute(route, app, endpoint, address)
	})
	return route, operation, before, err
}

// Only current-incarnation children are withdrawn. UID/resourceVersion
// preconditions prevent deleting a replacement between the read and the write.
func (r *AppDeploymentReconciler) withdrawHTTPAddresses(ctx context.Context, app *platformv1alpha1.AppDeployment, desired map[string]bool) (bool, error) {
	routes := &gatewayv1.HTTPRouteList{}
	if err := r.List(ctx, routes, client.InNamespace(app.Namespace)); err != nil {
		return false, err
	}
	pending := false
	for i := range routes.Items {
		route := &routes.Items[i]
		if desired[route.Name] || !metav1.IsControlledBy(route, app) {
			continue
		}
		if err := r.Delete(ctx, route, client.Preconditions{UID: &route.UID, ResourceVersion: &route.ResourceVersion}); err != nil && !apierrors.IsNotFound(err) {
			return false, err
		}
		r.recordHTTPRouteOperation(ctx, app, controllerutil.OperationResultUpdated, route.ResourceVersion, nil, workloadDecision{state: workloadStateProgressing, reason: "HTTPRouteWithdrawalPending"})
		remaining := &gatewayv1.HTTPRoute{}
		if err := r.Get(ctx, client.ObjectKeyFromObject(route), remaining); err == nil {
			pending = true
		} else if !apierrors.IsNotFound(err) {
			return false, err
		}
	}
	return pending, nil
}

func configureHTTPRoute(
	route *gatewayv1.HTTPRoute,
	appDeployment *platformv1alpha1.AppDeployment,
	endpoint platformv1alpha1.AppDeploymentPublicEndpoint,
	address platformv1alpha1.AppDeploymentHTTPAddress,
) error {
	if route.Labels == nil {
		route.Labels = map[string]string{}
	}
	route.Labels[appDeploymentLabel] = appDeployment.Name
	route.Labels[managedByLabel] = managedByValue

	gatewayGroup := gatewayv1.Group(gatewayv1.GroupName)
	gatewayKind := gatewayv1.Kind("Gateway")
	gatewayNamespace := gatewayv1.Namespace(address.Destination.GatewayNamespace)
	httpsSection := gatewayv1.SectionName(address.Destination.SectionName)
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
			Group: &gatewayGroup, Kind: &gatewayKind, Name: gatewayv1.ObjectName(address.Destination.GatewayName),
			Namespace: &gatewayNamespace, SectionName: &httpsSection,
		}}},
		Hostnames: []gatewayv1.Hostname{gatewayv1.Hostname(address.Hostname)},
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
