package controller

import (
	"context"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	platformv1alpha1 "github.com/molejo-platform/molejo/packages/kubernetes-api/apis/platform/v1alpha1"
)

// AppDeploymentReconciler projects AppDeployment resources into typed Kubernetes workloads.
type AppDeploymentReconciler struct {
	client.Client
	APIReader           client.Reader
	Scheme              *runtime.Scheme
	Recorder            record.EventRecorder
	Tracer              trace.Tracer
	StatefulTolerations []corev1.Toleration
}

// +kubebuilder:rbac:groups=platform.molejo.dev,resources=appdeployments;appvolumes,verbs=get;list;watch
// +kubebuilder:rbac:groups=platform.molejo.dev,resources=appdeployments/status,verbs=get;update;patch
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
			attribute.String("molejo.appdeployment.namespace", req.Namespace),
			attribute.String("molejo.appdeployment.name", req.Name),
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
		attribute.String("molejo.appdeployment.uid", string(appDeployment.UID)),
		attribute.Int64("molejo.appdeployment.generation", appDeployment.Generation),
		attribute.Int64("molejo.appdeployment.observed_generation", appDeployment.Status.ObservedGeneration),
	)
	if !appDeployment.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, nil
	}

	workload, err := r.reconcileWorkload(ctx, appDeployment)
	if err != nil {
		return r.handleProjectionFailure(ctx, span, appDeployment, err)
	}
	if err := r.reconcileService(ctx, appDeployment, workload.decision); err != nil {
		return r.handleProjectionFailure(ctx, span, appDeployment, err)
	}
	publication, err := r.reconcilePublication(ctx, appDeployment, workload.decision)
	if err != nil {
		return r.handleProjectionFailure(ctx, span, appDeployment, err)
	}

	span.SetAttributes(
		attribute.String("molejo.reconciliation.state", string(publication.decision.state)),
		attribute.String("molejo.reconciliation.reason", publication.decision.reason),
	)
	if err := r.updateProjectedStatus(ctx, appDeployment, workload.reference, publication.decision, publication.statuses); err != nil {
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
