package controller

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	controllermetrics "sigs.k8s.io/controller-runtime/pkg/metrics"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	platformv1alpha1 "github.com/fruto-platform/fruto/packages/kubernetes-api/apis/platform/v1alpha1"
)

const (
	appDeploymentLabel     = "platform.fruto.calouro.tech/app-deployment"
	managedByLabel         = "app.kubernetes.io/managed-by"
	managedByValue         = "fruto-platform-operator"
	containerName          = "app"
	httpPortName           = "http"
	tracerName             = "github.com/fruto-platform/fruto/services/platform-operator"
	sharedGatewayName      = "fruto"
	sharedGatewayNamespace = "fruto-system"
	sharedGatewaySection   = "https-molejo"

	ownershipConflictRequeueAfter = 5 * time.Minute
	persistentFailureRequeueAfter = 5 * time.Minute
)

var (
	errOwnershipConflict = errors.New("required child is not controlled by the AppDeployment")
	errHostnameConflict  = errors.New("public hostname is already owned by another AppDeployment")
	stateTransitions     = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "fruto_platform_operator",
		Name:      "state_transitions_total",
		Help:      "Number of AppDeployment state transitions observed by the platform operator.",
	}, []string{"state", "reason"})
)

func init() {
	controllermetrics.Registry.MustRegister(stateTransitions)
}

// AppDeploymentReconciler projects AppDeployment resources into typed Kubernetes workloads.
type AppDeploymentReconciler struct {
	client.Client
	APIReader           client.Reader
	Scheme              *runtime.Scheme
	Recorder            record.EventRecorder
	Tracer              trace.Tracer
	StatefulTolerations []corev1.Toleration
}

// +kubebuilder:rbac:groups=platform.fruto.calouro.tech,resources=appdeployments;appvolumes,verbs=get;list;watch
// +kubebuilder:rbac:groups=platform.fruto.calouro.tech,resources=appdeployments/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=apps,resources=deployments;statefulsets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=pods,verbs=get;delete
// +kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch;create;update;patch
// +kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=httproutes,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=tcproutes,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=gateways,verbs=get;list;watch

// Reconcile converges one AppDeployment and its selected workload kind.
func (r *AppDeploymentReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	tracer := r.tracer()
	ctx, span := tracer.Start(ctx, "platform-operator.appdeployment.reconcile",
		trace.WithAttributes(
			attribute.String("fruto.appdeployment.namespace", req.Namespace),
			attribute.String("fruto.appdeployment.name", req.Name),
		))
	defer span.End()

	logger := ctrl.LoggerFrom(ctx).WithValues(
		"namespace", req.Namespace,
		"appDeployment", req.Name,
	)
	if spanContext := span.SpanContext(); spanContext.IsValid() {
		logger = logger.WithValues(
			"trace_id", spanContext.TraceID().String(),
			"span_id", spanContext.SpanID().String(),
		)
	}
	ctx = ctrl.LoggerInto(ctx, logger)

	appDeployment := &platformv1alpha1.AppDeployment{}
	getCtx, getSpan := tracer.Start(ctx, "kubernetes.appdeployment.get")
	err := r.Get(getCtx, req.NamespacedName, appDeployment)
	finishSpan(getSpan, err)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		if markCanceledReconciliation(ctx, span, err) {
			return ctrl.Result{}, nil
		}
		markReconcileFailure(span, err)
		logReconcileFailure(ctx, appDeployment, err)
		return ctrl.Result{}, err
	}

	span.SetAttributes(
		attribute.String("fruto.appdeployment.uid", string(appDeployment.UID)),
		attribute.Int64("fruto.appdeployment.generation", appDeployment.Generation),
		attribute.Int64("fruto.appdeployment.observed_generation", appDeployment.Status.ObservedGeneration),
	)
	if !appDeployment.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, nil
	}

	var deployment *appsv1.Deployment
	var decision workloadDecision
	if appDeployment.Spec.Workload.Kind == platformv1alpha1.WorkloadStateful {
		applyCtx, applySpan := tracer.Start(ctx, "kubernetes.statefulset.apply")
		statefulSet, _, _, applyErr := r.applyStatefulSet(applyCtx, appDeployment)
		finishSpan(applySpan, applyErr)
		if applyErr != nil {
			return r.handleProjectionFailure(ctx, span, appDeployment, applyErr)
		}
		replaced, replaceErr := r.replaceStaleUnreadyStatefulPod(ctx, statefulSet)
		if replaceErr != nil {
			return r.handleProjectionFailure(ctx, span, appDeployment, replaceErr)
		}
		if replaced {
			decision = workloadDecision{state: workloadStateProgressing, reason: "StatefulSetReplacingStalePod", message: "The managed StatefulSet is replacing an unhealthy Pod from an obsolete revision."}
		} else {
			decision = evaluateWorkload(snapshotStatefulSet(statefulSet))
		}
		deployment = &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: statefulSet.Name, Namespace: statefulSet.Namespace}}
		if cleanupErr := r.deleteOwnedDeployment(ctx, appDeployment); cleanupErr != nil {
			return r.handleProjectionFailure(ctx, span, appDeployment, cleanupErr)
		}
	} else {
		applyCtx, applySpan := tracer.Start(ctx, "kubernetes.deployment.apply")
		var deploymentOperation controllerutil.OperationResult
		var deploymentVersionBefore string
		deployment, deploymentOperation, deploymentVersionBefore, err = r.applyDeployment(applyCtx, appDeployment)
		finishSpan(applySpan, err)
		if err != nil {
			return r.handleProjectionFailure(ctx, span, appDeployment, err)
		}
		decision = evaluateWorkload(snapshotDeployment(deployment, desiredReplicas(appDeployment)))
		r.recordDeploymentOperation(ctx, appDeployment, deploymentOperation, deploymentVersionBefore,
			deployment.ResourceVersion, decision)
		if cleanupErr := r.deleteOwnedStatefulSet(ctx, appDeployment); cleanupErr != nil {
			return r.handleProjectionFailure(ctx, span, appDeployment, cleanupErr)
		}
	}
	_, evaluateSpan := tracer.Start(ctx, "domain.workload.evaluate")
	evaluateSpan.SetAttributes(
		attribute.String("fruto.reconciliation.state", string(decision.state)),
		attribute.String("fruto.reconciliation.reason", decision.reason),
	)
	evaluateSpan.End()

	serviceCtx, serviceSpan := tracer.Start(ctx, "kubernetes.service.apply")
	service, serviceOperation, serviceVersionBefore, err := r.applyService(serviceCtx, appDeployment)
	finishSpan(serviceSpan, err)
	if err != nil {
		return r.handleProjectionFailure(ctx, span, appDeployment, err)
	}
	r.recordServiceOperation(ctx, appDeployment, serviceOperation, serviceVersionBefore,
		service.ResourceVersion, decision)

	publicationCtx, publicationSpan := tracer.Start(ctx, "kubernetes.httproute.apply")
	route, routeOperation, routeVersionBefore, err := r.applyPublication(publicationCtx, appDeployment)
	finishSpan(publicationSpan, err)
	if err != nil {
		return r.handleProjectionFailure(ctx, span, appDeployment, err)
	}
	decision = evaluatePublication(appDeployment, route, decision)
	if _, public := publicEndpoint(appDeployment, platformv1alpha1.AppDeploymentPublicEndpointType("HTTP")); public {
		gatewayCtx, gatewaySpan := tracer.Start(ctx, "kubernetes.gateway.get")
		gateway, gatewayErr := r.getPublicationGateway(gatewayCtx)
		finishSpan(gatewaySpan, gatewayErr)
		if gatewayErr != nil {
			return r.handleProjectionFailure(ctx, span, appDeployment, gatewayErr)
		}
		decision = evaluatePublicationGateway(gateway, decision)
	}
	r.recordHTTPRouteOperation(ctx, appDeployment, routeOperation, routeVersionBefore, route, decision)

	tcpRoute, err := r.applyTCPPublication(ctx, appDeployment)
	if err != nil {
		return r.handleProjectionFailure(ctx, span, appDeployment, err)
	}
	tcpDecision, tcpStatus := evaluateTCPPublication(appDeployment, tcpRoute)
	if tcpDecision != nil && decision.state != workloadStateDegraded {
		decision = *tcpDecision
	}
	endpointStatuses := publicationStatuses(appDeployment, route, tcpStatus)

	span.SetAttributes(
		attribute.String("fruto.reconciliation.state", string(decision.state)),
		attribute.String("fruto.reconciliation.reason", decision.reason),
	)
	if err := r.updateStatus(ctx, appDeployment, deployment, decision, true, endpointStatuses); err != nil {
		if markCanceledReconciliation(ctx, span, err) {
			return ctrl.Result{}, nil
		}
		markReconcileFailure(span, err)
		logReconcileFailure(ctx, appDeployment, err)
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// SetupWithManager registers the reconciler and watches its owned Kubernetes children.
func (r *AppDeploymentReconciler) SetupWithManager(manager ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(manager).
		Named("appdeployment").
		For(&platformv1alpha1.AppDeployment{}).
		Owns(&appsv1.Deployment{}).
		Owns(&appsv1.StatefulSet{}).
		Owns(&corev1.Service{}).
		Owns(&gatewayv1.HTTPRoute{}).
		Owns(&gatewayv1alpha2.TCPRoute{}).
		Watches(&gatewayv1.Gateway{}, handler.EnqueueRequestsFromMapFunc(r.mapGatewayToAppDeployments)).
		Watches(&platformv1alpha1.AppVolume{}, handler.EnqueueRequestsFromMapFunc(r.mapVolumeToAppDeployments)).
		Complete(r)
}

func (r *AppDeploymentReconciler) mapVolumeToAppDeployments(ctx context.Context, object client.Object) []reconcile.Request {
	volume, ok := object.(*platformv1alpha1.AppVolume)
	if !ok {
		return nil
	}
	items := &platformv1alpha1.AppDeploymentList{}
	if err := r.List(ctx, items, client.InNamespace(volume.Namespace)); err != nil {
		return nil
	}
	requests := make([]reconcile.Request, 0)
	for index := range items.Items {
		item := &items.Items[index]
		if item.Spec.Workload.Stateful != nil && item.Spec.Workload.Stateful.VolumeRef == volume.Name {
			requests = append(requests, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(item)})
		}
	}
	return requests
}

func (r *AppDeploymentReconciler) mapGatewayToAppDeployments(
	ctx context.Context,
	object client.Object,
) []reconcile.Request {
	if object.GetNamespace() != sharedGatewayNamespace || object.GetName() != sharedGatewayName {
		return nil
	}
	appDeployments := &platformv1alpha1.AppDeploymentList{}
	if err := r.List(ctx, appDeployments); err != nil {
		ctrl.LoggerFrom(ctx).Error(err, "unable to list public AppDeployments after Gateway change")
		return nil
	}
	requests := make([]reconcile.Request, 0, len(appDeployments.Items))
	for index := range appDeployments.Items {
		appDeployment := &appDeployments.Items[index]
		if _, public := publicEndpoint(appDeployment, platformv1alpha1.AppDeploymentPublicEndpointType("HTTP")); !public {
			continue
		}
		requests = append(requests, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(appDeployment)})
	}
	return requests
}

func (r *AppDeploymentReconciler) applyPublication(
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
	})
	return route, operation, resourceVersionBefore, err
}

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
		return nil
	})
	if err != nil {
		return nil, err
	}
	return route, nil
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

func (r *AppDeploymentReconciler) getPublicationGateway(ctx context.Context) (*gatewayv1.Gateway, error) {
	gateway := &gatewayv1.Gateway{}
	err := r.Get(ctx, client.ObjectKey{Namespace: sharedGatewayNamespace, Name: sharedGatewayName}, gateway)
	if apierrors.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get shared Gateway: %w", err)
	}
	return gateway, nil
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

func evaluatePublicationGateway(
	gateway *gatewayv1.Gateway,
	publication workloadDecision,
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

	listenerConditions, found := publicationGatewayListenerConditions(gateway)
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

func conditionIsCurrent(condition *metav1.Condition, generation int64) bool {
	return condition != nil && condition.ObservedGeneration == generation
}

func publicationGatewayListenerConditions(gateway *gatewayv1.Gateway) ([]metav1.Condition, bool) {
	var conditions []metav1.Condition
	found := false
	for index := range gateway.Status.Listeners {
		listener := &gateway.Status.Listeners[index]
		if listener.Name != sharedGatewaySection {
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

func (r *AppDeploymentReconciler) applyDeployment(
	ctx context.Context,
	appDeployment *platformv1alpha1.AppDeployment,
) (*appsv1.Deployment, controllerutil.OperationResult, string, error) {
	deployment := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{
		Name:      appDeployment.Name,
		Namespace: appDeployment.Namespace,
	}}
	var resourceVersionBefore string
	operation, err := controllerutil.CreateOrPatch(ctx, r.Client, deployment, func() error {
		resourceVersionBefore = deployment.ResourceVersion
		if !deployment.CreationTimestamp.IsZero() && !metav1.IsControlledBy(deployment, appDeployment) {
			return errOwnershipConflict
		}
		if err := controllerutil.SetControllerReference(appDeployment, deployment, r.Scheme); err != nil {
			return fmt.Errorf("set Deployment owner reference: %w", err)
		}

		replicas := desiredReplicas(appDeployment)
		selectorLabels := desiredSelectorLabels(appDeployment)
		runAsNonRoot := true
		allowPrivilegeEscalation := false
		readOnlyRootFilesystem := true
		automountServiceAccountToken := false
		maxUnavailable := intstr.FromString("25%")
		maxSurge := intstr.FromString("25%")
		progressDeadlineSeconds := int32(600)
		revisionHistoryLimit := int32(10)

		if deployment.Labels == nil {
			deployment.Labels = map[string]string{}
		}
		deployment.Labels[appDeploymentLabel] = appDeployment.Name
		deployment.Labels[managedByLabel] = managedByValue
		deployment.Spec.Replicas = &replicas
		deployment.Spec.Selector = &metav1.LabelSelector{MatchLabels: selectorLabels}
		deployment.Spec.Strategy = appsv1.DeploymentStrategy{
			Type: appsv1.RollingUpdateDeploymentStrategyType,
			RollingUpdate: &appsv1.RollingUpdateDeployment{
				MaxUnavailable: &maxUnavailable,
				MaxSurge:       &maxSurge,
			},
		}
		deployment.Spec.MinReadySeconds = 0
		deployment.Spec.RevisionHistoryLimit = &revisionHistoryLimit
		deployment.Spec.Paused = false
		deployment.Spec.ProgressDeadlineSeconds = &progressDeadlineSeconds
		deployment.Spec.Template.Labels = map[string]string{
			appDeploymentLabel: appDeployment.Name,
			managedByLabel:     managedByValue,
		}
		deployment.Spec.Template.Spec.SecurityContext = &corev1.PodSecurityContext{
			RunAsNonRoot: &runAsNonRoot,
			SeccompProfile: &corev1.SeccompProfile{
				Type: corev1.SeccompProfileTypeRuntimeDefault,
			},
		}
		deployment.Spec.Template.Spec.HostNetwork = false
		deployment.Spec.Template.Spec.HostPID = false
		deployment.Spec.Template.Spec.HostIPC = false
		deployment.Spec.Template.Spec.NodeName = ""
		deployment.Spec.Template.Spec.SchedulerName = corev1.DefaultSchedulerName
		deployment.Spec.Template.Spec.ReadinessGates = nil
		deployment.Spec.Template.Spec.RuntimeClassName = nil
		deployment.Spec.Template.Spec.SchedulingGates = nil
		deployment.Spec.Template.Spec.ServiceAccountName = ""
		deployment.Spec.Template.Spec.DeprecatedServiceAccount = ""
		deployment.Spec.Template.Spec.AutomountServiceAccountToken = &automountServiceAccountToken
		deployment.Spec.Template.Spec.InitContainers = nil
		deployment.Spec.Template.Spec.Volumes = nil
		environment := make([]corev1.EnvVar, 0, len(appDeployment.Spec.Variables))
		for _, variable := range appDeployment.Spec.Variables {
			environment = append(environment, corev1.EnvVar{Name: variable.Name, Value: variable.Value})
		}
		environmentFrom := []corev1.EnvFromSource{}
		if appDeployment.Spec.ConfigMapRef != "" {
			environmentFrom = append(environmentFrom, corev1.EnvFromSource{ConfigMapRef: &corev1.ConfigMapEnvSource{LocalObjectReference: corev1.LocalObjectReference{Name: appDeployment.Spec.ConfigMapRef}}})
		}
		if appDeployment.Spec.SecretRef != "" {
			environmentFrom = append(environmentFrom, corev1.EnvFromSource{SecretRef: &corev1.SecretEnvSource{LocalObjectReference: corev1.LocalObjectReference{Name: appDeployment.Spec.SecretRef}}})
		}
		deployment.Spec.Template.Spec.Containers = []corev1.Container{{
			Name:            containerName,
			Image:           appDeployment.Spec.Image,
			ImagePullPolicy: corev1.PullIfNotPresent,
			Env:             environment,
			EnvFrom:         environmentFrom,
			Ports:           desiredContainerPorts(appDeployment),
			Resources:       desiredResourceRequirements(appDeployment),
			StartupProbe:    desiredProbe(effectiveStartupProbe(appDeployment), 2, 30),
			ReadinessProbe:  desiredProbe(appDeployment.Spec.Probes.Readiness, 5, 3),
			LivenessProbe:   desiredProbe(appDeployment.Spec.Probes.Liveness, 10, 3),
			SecurityContext: &corev1.SecurityContext{
				AllowPrivilegeEscalation: &allowPrivilegeEscalation,
				ReadOnlyRootFilesystem:   &readOnlyRootFilesystem,
				Capabilities: &corev1.Capabilities{
					Drop: []corev1.Capability{"ALL"},
				},
			},
		}}

		return nil
	})
	return deployment, operation, resourceVersionBefore, err
}

func (r *AppDeploymentReconciler) applyStatefulSet(
	ctx context.Context,
	appDeployment *platformv1alpha1.AppDeployment,
) (*appsv1.StatefulSet, controllerutil.OperationResult, string, error) {
	statefulIntent := appDeployment.Spec.Workload.Stateful
	if statefulIntent == nil {
		return nil, controllerutil.OperationResultNone, "", errors.New("stateful workload intent is missing")
	}
	volume := &platformv1alpha1.AppVolume{}
	if err := r.Get(ctx, types.NamespacedName{Namespace: appDeployment.Namespace, Name: statefulIntent.VolumeRef}, volume); err != nil {
		return nil, controllerutil.OperationResultNone, "", fmt.Errorf("get AppVolume: %w", err)
	}
	if volume.Spec.DesiredState == platformv1alpha1.VolumeDesiredDeleted || !volume.DeletionTimestamp.IsZero() {
		return nil, controllerutil.OperationResultNone, "", errors.New("referenced AppVolume is not available")
	}

	statefulSet := &appsv1.StatefulSet{ObjectMeta: metav1.ObjectMeta{Name: appDeployment.Name, Namespace: appDeployment.Namespace}}
	var resourceVersionBefore string
	operation, err := controllerutil.CreateOrPatch(ctx, r.Client, statefulSet, func() error {
		resourceVersionBefore = statefulSet.ResourceVersion
		if !statefulSet.CreationTimestamp.IsZero() && !metav1.IsControlledBy(statefulSet, appDeployment) {
			return errOwnershipConflict
		}
		if err := controllerutil.SetControllerReference(appDeployment, statefulSet, r.Scheme); err != nil {
			return fmt.Errorf("set StatefulSet owner reference: %w", err)
		}
		if statefulSet.Labels == nil {
			statefulSet.Labels = map[string]string{}
		}
		statefulSet.Labels[appDeploymentLabel] = appDeployment.Name
		statefulSet.Labels[managedByLabel] = managedByValue
		labels := desiredSelectorLabels(appDeployment)
		replicas := int32(1)
		revisionHistoryLimit := int32(10)
		statefulSet.Spec.ServiceName = appDeployment.Name
		statefulSet.Spec.Replicas = &replicas
		statefulSet.Spec.Selector = &metav1.LabelSelector{MatchLabels: labels}
		statefulSet.Spec.PodManagementPolicy = appsv1.OrderedReadyPodManagement
		statefulSet.Spec.UpdateStrategy = appsv1.StatefulSetUpdateStrategy{Type: appsv1.RollingUpdateStatefulSetStrategyType}
		statefulSet.Spec.RevisionHistoryLimit = &revisionHistoryLimit
		statefulSet.Spec.VolumeClaimTemplates = nil
		statefulSet.Spec.PersistentVolumeClaimRetentionPolicy = nil
		statefulSet.Spec.Template = desiredPodTemplate(appDeployment)
		statefulSet.Spec.Template.Spec.Tolerations = append(
			[]corev1.Toleration(nil),
			r.StatefulTolerations...,
		)
		fsGroup := int64(65532)
		fsGroupChangePolicy := corev1.FSGroupChangeOnRootMismatch
		statefulSet.Spec.Template.Spec.SecurityContext.FSGroup = &fsGroup
		statefulSet.Spec.Template.Spec.SecurityContext.FSGroupChangePolicy = &fsGroupChangePolicy
		statefulSet.Spec.Template.Spec.Volumes = []corev1.Volume{{
			Name: "app-data",
			VolumeSource: corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
				ClaimName: statefulIntent.VolumeRef,
			}},
		}}
		statefulSet.Spec.Template.Spec.Containers[0].VolumeMounts = []corev1.VolumeMount{{Name: "app-data", MountPath: statefulIntent.MountPath}}
		return nil
	})
	return statefulSet, operation, resourceVersionBefore, err
}

func (r *AppDeploymentReconciler) replaceStaleUnreadyStatefulPod(ctx context.Context, statefulSet *appsv1.StatefulSet) (bool, error) {
	if statefulSet.Status.UpdateRevision == "" {
		return false, nil
	}
	pod := &corev1.Pod{}
	key := types.NamespacedName{Namespace: statefulSet.Namespace, Name: statefulSet.Name + "-0"}
	reader := r.APIReader
	if reader == nil {
		reader = r.Client
	}
	if err := reader.Get(ctx, key, pod); err != nil {
		if apierrors.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("get StatefulSet Pod: %w", err)
	}
	if !pod.DeletionTimestamp.IsZero() || !metav1.IsControlledBy(pod, statefulSet) || pod.Labels[appsv1.ControllerRevisionHashLabelKey] == "" || pod.Labels[appsv1.ControllerRevisionHashLabelKey] == statefulSet.Status.UpdateRevision {
		return false, nil
	}
	for _, condition := range pod.Status.Conditions {
		if condition.Type == corev1.PodReady && condition.Status == corev1.ConditionTrue {
			return false, nil
		}
	}
	if err := r.Delete(ctx, pod); err != nil && !apierrors.IsNotFound(err) {
		return false, fmt.Errorf("replace stale StatefulSet Pod: %w", err)
	}
	ctrl.LoggerFrom(ctx).Info("replacing unhealthy StatefulSet Pod from obsolete revision",
		"namespace", statefulSet.Namespace,
		"statefulSet", statefulSet.Name,
		"pod", pod.Name,
	)
	if r.Recorder != nil {
		r.Recorder.Event(statefulSet, corev1.EventTypeNormal, "StalePodReplaced", "An unhealthy Pod from an obsolete revision was replaced.")
	}
	return true, nil
}

func desiredPodTemplate(appDeployment *platformv1alpha1.AppDeployment) corev1.PodTemplateSpec {
	runAsNonRoot := true
	allowPrivilegeEscalation := false
	readOnlyRootFilesystem := true
	automountServiceAccountToken := false
	environment := make([]corev1.EnvVar, 0, len(appDeployment.Spec.Variables))
	for _, variable := range appDeployment.Spec.Variables {
		environment = append(environment, corev1.EnvVar{Name: variable.Name, Value: variable.Value})
	}
	environmentFrom := []corev1.EnvFromSource{}
	if appDeployment.Spec.ConfigMapRef != "" {
		environmentFrom = append(environmentFrom, corev1.EnvFromSource{ConfigMapRef: &corev1.ConfigMapEnvSource{LocalObjectReference: corev1.LocalObjectReference{Name: appDeployment.Spec.ConfigMapRef}}})
	}
	if appDeployment.Spec.SecretRef != "" {
		environmentFrom = append(environmentFrom, corev1.EnvFromSource{SecretRef: &corev1.SecretEnvSource{LocalObjectReference: corev1.LocalObjectReference{Name: appDeployment.Spec.SecretRef}}})
	}
	return corev1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{Labels: desiredSelectorLabels(appDeployment)},
		Spec: corev1.PodSpec{
			SecurityContext:              &corev1.PodSecurityContext{RunAsNonRoot: &runAsNonRoot, SeccompProfile: &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault}},
			AutomountServiceAccountToken: &automountServiceAccountToken,
			Containers: []corev1.Container{{
				Name: containerName, Image: appDeployment.Spec.Image, ImagePullPolicy: corev1.PullIfNotPresent,
				Env: environment, EnvFrom: environmentFrom,
				Ports:           desiredContainerPorts(appDeployment),
				Resources:       desiredResourceRequirements(appDeployment),
				StartupProbe:    desiredProbe(effectiveStartupProbe(appDeployment), 2, 30),
				ReadinessProbe:  desiredProbe(appDeployment.Spec.Probes.Readiness, 5, 3),
				LivenessProbe:   desiredProbe(appDeployment.Spec.Probes.Liveness, 10, 3),
				SecurityContext: &corev1.SecurityContext{AllowPrivilegeEscalation: &allowPrivilegeEscalation, ReadOnlyRootFilesystem: &readOnlyRootFilesystem, Capabilities: &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}}},
			}},
		},
	}
}

func (r *AppDeploymentReconciler) deleteOwnedDeployment(ctx context.Context, owner *platformv1alpha1.AppDeployment) error {
	deployment := &appsv1.Deployment{}
	if err := r.Get(ctx, client.ObjectKeyFromObject(owner), deployment); err != nil {
		return client.IgnoreNotFound(err)
	}
	if !metav1.IsControlledBy(deployment, owner) {
		return errOwnershipConflict
	}
	return client.IgnoreNotFound(r.Delete(ctx, deployment))
}

func (r *AppDeploymentReconciler) deleteOwnedStatefulSet(ctx context.Context, owner *platformv1alpha1.AppDeployment) error {
	statefulSet := &appsv1.StatefulSet{}
	if err := r.Get(ctx, client.ObjectKeyFromObject(owner), statefulSet); err != nil {
		return client.IgnoreNotFound(err)
	}
	if !metav1.IsControlledBy(statefulSet, owner) {
		return errOwnershipConflict
	}
	return client.IgnoreNotFound(r.Delete(ctx, statefulSet))
}

func (r *AppDeploymentReconciler) applyService(
	ctx context.Context,
	appDeployment *platformv1alpha1.AppDeployment,
) (*corev1.Service, controllerutil.OperationResult, string, error) {
	service := &corev1.Service{ObjectMeta: metav1.ObjectMeta{
		Name:      appDeployment.Name,
		Namespace: appDeployment.Namespace,
	}}
	var resourceVersionBefore string
	operation, err := controllerutil.CreateOrPatch(ctx, r.Client, service, func() error {
		resourceVersionBefore = service.ResourceVersion
		if !service.CreationTimestamp.IsZero() && !metav1.IsControlledBy(service, appDeployment) {
			return errOwnershipConflict
		}
		if err := controllerutil.SetControllerReference(appDeployment, service, r.Scheme); err != nil {
			return fmt.Errorf("set Service owner reference: %w", err)
		}

		if service.Labels == nil {
			service.Labels = map[string]string{}
		}
		service.Labels[appDeploymentLabel] = appDeployment.Name
		service.Labels[managedByLabel] = managedByValue
		service.Spec.Type = corev1.ServiceTypeClusterIP
		service.Spec.Selector = desiredSelectorLabels(appDeployment)
		ports := effectivePorts(appDeployment)
		service.Spec.Ports = make([]corev1.ServicePort, 0, len(ports))
		for _, port := range ports {
			service.Spec.Ports = append(service.Spec.Ports, corev1.ServicePort{Name: port.Name, Protocol: corev1.ProtocolTCP, Port: port.ContainerPort, TargetPort: intstr.FromString(port.Name)})
		}
		service.Spec.ExternalIPs = nil
		service.Spec.ExternalName = ""
		service.Spec.LoadBalancerIP = ""
		service.Spec.LoadBalancerClass = nil
		service.Spec.LoadBalancerSourceRanges = nil
		service.Spec.AllocateLoadBalancerNodePorts = nil
		service.Spec.HealthCheckNodePort = 0
		service.Spec.ExternalTrafficPolicy = ""
		service.Spec.PublishNotReadyAddresses = false
		service.Spec.SessionAffinity = corev1.ServiceAffinityNone
		service.Spec.SessionAffinityConfig = nil
		internalTrafficPolicy := corev1.ServiceInternalTrafficPolicyCluster
		service.Spec.InternalTrafficPolicy = &internalTrafficPolicy
		service.Spec.TrafficDistribution = nil

		return nil
	})
	return service, operation, resourceVersionBefore, err
}

func desiredSelectorLabels(appDeployment *platformv1alpha1.AppDeployment) map[string]string {
	return map[string]string{appDeploymentLabel: appDeployment.Name}
}

func desiredResourceRequirements(
	appDeployment *platformv1alpha1.AppDeployment,
) corev1.ResourceRequirements {
	requests := appDeployment.Spec.Resources.Requests
	limits := appDeployment.Spec.Resources.Limits
	return corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse(fmt.Sprintf("%dm", requests.CPUMillis)),
			corev1.ResourceMemory: resource.MustParse(fmt.Sprintf("%dMi", requests.MemoryMiB)),
		},
		Limits: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse(fmt.Sprintf("%dm", limits.CPUMillis)),
			corev1.ResourceMemory: resource.MustParse(fmt.Sprintf("%dMi", limits.MemoryMiB)),
		},
	}
}

func desiredProbe(probe platformv1alpha1.AppDeploymentProbe, periodSeconds int32, failureThreshold int32) *corev1.Probe {
	portName := probe.PortName
	if portName == "" {
		portName = httpPortName
	}
	handler := corev1.ProbeHandler{HTTPGet: &corev1.HTTPGetAction{Path: probe.Path, Port: intstr.FromString(portName), Scheme: corev1.URISchemeHTTP}}
	if probe.Type == "TCP" {
		handler = corev1.ProbeHandler{TCPSocket: &corev1.TCPSocketAction{Port: intstr.FromString(portName)}}
	}
	return &corev1.Probe{
		ProbeHandler:     handler,
		TimeoutSeconds:   2,
		PeriodSeconds:    periodSeconds,
		SuccessThreshold: 1,
		FailureThreshold: failureThreshold,
	}
}

func desiredHTTPProbe(path string, periodSeconds int32, failureThreshold int32) *corev1.Probe {
	return desiredProbe(platformv1alpha1.AppDeploymentProbe{Type: "HTTP", PortName: httpPortName, Path: path}, periodSeconds, failureThreshold)
}

func effectivePorts(appDeployment *platformv1alpha1.AppDeployment) []platformv1alpha1.AppDeploymentPort {
	if len(appDeployment.Spec.Ports) > 0 {
		return appDeployment.Spec.Ports
	}
	return []platformv1alpha1.AppDeploymentPort{{Name: httpPortName, ContainerPort: appDeployment.Spec.Port, Protocol: corev1.ProtocolTCP}}
}

func desiredContainerPorts(appDeployment *platformv1alpha1.AppDeployment) []corev1.ContainerPort {
	ports := effectivePorts(appDeployment)
	result := make([]corev1.ContainerPort, 0, len(ports))
	for _, port := range ports {
		result = append(result, corev1.ContainerPort{Name: port.Name, ContainerPort: port.ContainerPort, Protocol: corev1.ProtocolTCP})
	}
	return result
}

func effectiveStartupProbe(appDeployment *platformv1alpha1.AppDeployment) platformv1alpha1.AppDeploymentProbe {
	if appDeployment.Spec.Probes.Startup != nil {
		return *appDeployment.Spec.Probes.Startup
	}
	return appDeployment.Spec.Probes.Readiness
}

func portByName(appDeployment *platformv1alpha1.AppDeployment, name string) (int32, bool) {
	for _, port := range effectivePorts(appDeployment) {
		if port.Name == name {
			return port.ContainerPort, true
		}
	}
	return 0, false
}

func desiredReplicas(appDeployment *platformv1alpha1.AppDeployment) int32 {
	if appDeployment.Spec.Replicas == nil {
		return 1
	}
	return *appDeployment.Spec.Replicas
}

func (r *AppDeploymentReconciler) updateStatus(
	ctx context.Context,
	appDeployment *platformv1alpha1.AppDeployment,
	deployment *appsv1.Deployment,
	decision workloadDecision,
	advanceObserved bool,
	endpointStatuses []platformv1alpha1.AppDeploymentEndpointStatus,
) error {
	before := appDeployment.DeepCopy()
	transitioned := stateTransitioned(before, decision)

	if advanceObserved {
		appDeployment.Status.ObservedGeneration = appDeployment.Generation
		appDeployment.Status.ObservedRelease = appDeployment.Spec.Image
		appDeployment.Status.WorkloadRef = &corev1.LocalObjectReference{Name: deployment.Name}
	}
	if endpointStatuses != nil {
		appDeployment.Status.EndpointStatuses = endpointStatuses
	}
	applyDecision(appDeployment, decision)

	if err := r.patchStatusIfChanged(ctx, before, appDeployment); err != nil {
		return err
	}
	if transitioned {
		r.recordStateTransition(ctx, appDeployment, decision)
	}
	return nil
}

func applyDecision(appDeployment *platformv1alpha1.AppDeployment, decision workloadDecision) {
	ready := metav1.ConditionFalse
	progressing := metav1.ConditionFalse
	degraded := metav1.ConditionFalse

	switch decision.state {
	case workloadStateReady:
		ready = metav1.ConditionTrue
	case workloadStateProgressing:
		progressing = metav1.ConditionTrue
	case workloadStateDegraded:
		degraded = metav1.ConditionTrue
	}

	setCondition(appDeployment, platformv1alpha1.ConditionReady, ready, decision.reason, decision.message)
	setCondition(appDeployment, platformv1alpha1.ConditionProgressing, progressing, decision.reason, decision.message)
	setCondition(appDeployment, platformv1alpha1.ConditionDegraded, degraded, decision.reason, decision.message)
}

func stateTransitioned(appDeployment *platformv1alpha1.AppDeployment, decision workloadDecision) bool {
	condition := meta.FindStatusCondition(appDeployment.Status.Conditions, string(decision.state))
	return condition == nil || condition.Status != metav1.ConditionTrue || condition.Reason != decision.reason
}

func setCondition(
	appDeployment *platformv1alpha1.AppDeployment,
	conditionType string,
	status metav1.ConditionStatus,
	reason string,
	message string,
) {
	meta.SetStatusCondition(&appDeployment.Status.Conditions, metav1.Condition{
		Type:               conditionType,
		Status:             status,
		ObservedGeneration: appDeployment.Generation,
		Reason:             reason,
		Message:            message,
	})
}

func (r *AppDeploymentReconciler) patchStatusIfChanged(
	ctx context.Context,
	before *platformv1alpha1.AppDeployment,
	after *platformv1alpha1.AppDeployment,
) error {
	if equality.Semantic.DeepEqual(before.Status, after.Status) {
		return nil
	}

	patchCtx, patchSpan := r.tracer().Start(ctx, "kubernetes.appdeployment.status.patch")
	err := r.Status().Patch(patchCtx, after, client.MergeFromWithOptions(
		before,
		client.MergeFromWithOptimisticLock{},
	))
	finishSpan(patchSpan, err)
	if err != nil {
		return fmt.Errorf("patch AppDeployment status %s: %w", types.NamespacedName{
			Namespace: after.Namespace,
			Name:      after.Name,
		}, err)
	}
	return nil
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
