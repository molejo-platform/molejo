package controller

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
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
	controllermetrics "sigs.k8s.io/controller-runtime/pkg/metrics"

	platformv1alpha1 "github.com/fruto-platform/fruto/packages/kubernetes-api/apis/platform/v1alpha1"
)

const (
	appDeploymentLabel = "platform.fruto.calouro.tech/app-deployment"
	managedByLabel     = "app.kubernetes.io/managed-by"
	managedByValue     = "fruto-platform-operator"
	containerName      = "app"
	httpPortName       = "http"
	tracerName         = "github.com/fruto-platform/fruto/services/platform-operator"

	ownershipConflictRequeueAfter = 5 * time.Minute
	persistentFailureRequeueAfter = 5 * time.Minute
)

var (
	errOwnershipConflict = errors.New("required child is not controlled by the AppDeployment")
	stateTransitions     = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "fruto_platform_operator",
		Name:      "state_transitions_total",
		Help:      "Number of AppDeployment state transitions observed by the platform operator.",
	}, []string{"state", "reason"})
)

func init() {
	controllermetrics.Registry.MustRegister(stateTransitions)
}

// AppDeploymentReconciler projects AppDeployment resources into Kubernetes Deployments.
type AppDeploymentReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
	Tracer   trace.Tracer
}

// +kubebuilder:rbac:groups=platform.fruto.calouro.tech,resources=appdeployments,verbs=get;list;watch
// +kubebuilder:rbac:groups=platform.fruto.calouro.tech,resources=appdeployments/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch
// +kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch;create;update;patch

// Reconcile converges one AppDeployment and its owned Deployment.
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
		markReconcileFailure(span, err)
		logReconcileFailure(ctx, appDeployment, err)
		return ctrl.Result{}, err
	}

	span.SetAttributes(
		attribute.String("fruto.appdeployment.uid", string(appDeployment.UID)),
		attribute.Int64("fruto.appdeployment.generation", appDeployment.Generation),
		attribute.Int64("fruto.appdeployment.observed_generation", appDeployment.Status.ObservedGeneration),
	)

	applyCtx, applySpan := tracer.Start(ctx, "kubernetes.deployment.apply")
	deployment, deploymentOperation, deploymentVersionBefore, err := r.applyDeployment(applyCtx, appDeployment)
	finishSpan(applySpan, err)
	if err != nil {
		return r.handleProjectionFailure(ctx, span, appDeployment, err)
	}
	_, evaluateSpan := tracer.Start(ctx, "domain.deployment.evaluate")
	decision := evaluateWorkload(snapshotDeployment(deployment, desiredReplicas(appDeployment)))
	evaluateSpan.SetAttributes(
		attribute.String("fruto.reconciliation.state", string(decision.state)),
		attribute.String("fruto.reconciliation.reason", decision.reason),
	)
	evaluateSpan.End()
	r.recordDeploymentOperation(ctx, appDeployment, deploymentOperation, deploymentVersionBefore,
		deployment.ResourceVersion, decision)

	serviceCtx, serviceSpan := tracer.Start(ctx, "kubernetes.service.apply")
	service, serviceOperation, serviceVersionBefore, err := r.applyService(serviceCtx, appDeployment)
	finishSpan(serviceSpan, err)
	if err != nil {
		return r.handleProjectionFailure(ctx, span, appDeployment, err)
	}
	r.recordServiceOperation(ctx, appDeployment, serviceOperation, serviceVersionBefore,
		service.ResourceVersion, decision)

	span.SetAttributes(
		attribute.String("fruto.reconciliation.state", string(decision.state)),
		attribute.String("fruto.reconciliation.reason", decision.reason),
	)
	if err := r.updateStatus(ctx, appDeployment, deployment, decision, true); err != nil {
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
		Owns(&corev1.Service{}).
		Complete(r)
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
		deployment.Spec.Template.Spec.Containers = []corev1.Container{{
			Name:            containerName,
			Image:           appDeployment.Spec.Image,
			ImagePullPolicy: corev1.PullIfNotPresent,
			Ports: []corev1.ContainerPort{{
				Name:          httpPortName,
				ContainerPort: appDeployment.Spec.Port,
				Protocol:      corev1.ProtocolTCP,
			}},
			Resources: desiredResourceRequirements(appDeployment),
			StartupProbe: desiredHTTPProbe(
				appDeployment.Spec.Probes.Readiness.Path,
				2,
				30,
			),
			ReadinessProbe: desiredHTTPProbe(
				appDeployment.Spec.Probes.Readiness.Path,
				5,
				3,
			),
			LivenessProbe: desiredHTTPProbe(
				appDeployment.Spec.Probes.Liveness.Path,
				10,
				3,
			),
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
		service.Spec.Ports = []corev1.ServicePort{{
			Name:       httpPortName,
			Protocol:   corev1.ProtocolTCP,
			Port:       appDeployment.Spec.Port,
			TargetPort: intstr.FromString(httpPortName),
		}}
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

func (r *AppDeploymentReconciler) handleProjectionFailure(
	ctx context.Context,
	span trace.Span,
	appDeployment *platformv1alpha1.AppDeployment,
	err error,
) (ctrl.Result, error) {
	if errors.Is(err, errOwnershipConflict) {
		decision := workloadDecision{
			state:   workloadStateDegraded,
			reason:  platformv1alpha1.ReasonOwnershipConflict,
			message: "A required Kubernetes child is not controlled by this AppDeployment.",
		}
		span.SetAttributes(
			attribute.String("fruto.reconciliation.state", string(decision.state)),
			attribute.String("fruto.reconciliation.reason", decision.reason),
		)
		if statusErr := r.updateStatus(ctx, appDeployment, nil, decision, false); statusErr != nil {
			markReconcileFailure(span, statusErr)
			logReconcileFailure(ctx, appDeployment, statusErr)
			return ctrl.Result{}, statusErr
		}
		return ctrl.Result{RequeueAfter: ownershipConflictRequeueAfter}, nil
	}

	markReconcileFailure(span, err)
	logReconcileFailure(ctx, appDeployment, err)
	if isPersistentReconcileError(err) {
		decision := workloadDecision{
			state:   workloadStateDegraded,
			reason:  platformv1alpha1.ReasonReconcileFailed,
			message: "A required Kubernetes child could not be reconciled.",
		}
		if statusErr := r.updateStatus(ctx, appDeployment, nil, decision, false); statusErr != nil {
			markReconcileFailure(span, statusErr)
			logReconcileFailure(ctx, appDeployment, statusErr)
			return ctrl.Result{}, statusErr
		}
		return ctrl.Result{RequeueAfter: persistentFailureRequeueAfter}, nil
	}
	return ctrl.Result{}, err
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

func desiredHTTPProbe(path string, periodSeconds int32, failureThreshold int32) *corev1.Probe {
	return &corev1.Probe{
		ProbeHandler: corev1.ProbeHandler{HTTPGet: &corev1.HTTPGetAction{
			Path:   path,
			Port:   intstr.FromString(httpPortName),
			Scheme: corev1.URISchemeHTTP,
		}},
		TimeoutSeconds:   2,
		PeriodSeconds:    periodSeconds,
		SuccessThreshold: 1,
		FailureThreshold: failureThreshold,
	}
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
) error {
	before := appDeployment.DeepCopy()
	transitioned := stateTransitioned(before, decision)

	if advanceObserved {
		appDeployment.Status.ObservedGeneration = appDeployment.Generation
		appDeployment.Status.ObservedRelease = appDeployment.Spec.Image
		appDeployment.Status.WorkloadRef = &corev1.LocalObjectReference{Name: deployment.Name}
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
	err := r.Status().Patch(patchCtx, after, client.MergeFrom(before))
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

func finishSpan(span trace.Span, err error) {
	if err != nil && !apierrors.IsNotFound(err) {
		markSpanError(span, err)
	}
	span.End()
}

func markSpanError(span trace.Span, err error) {
	span.RecordError(err)
	span.SetStatus(codes.Error, "operation failed")
}

func markReconcileFailure(span trace.Span, err error) {
	span.SetAttributes(
		attribute.String("fruto.reconciliation.state", string(workloadStateDegraded)),
		attribute.String("fruto.reconciliation.reason", platformv1alpha1.ReasonReconcileFailed),
	)
	markSpanError(span, err)
}

func logReconcileFailure(
	ctx context.Context,
	appDeployment *platformv1alpha1.AppDeployment,
	err error,
) {
	ctrl.LoggerFrom(ctx).Error(err, "AppDeployment reconciliation failed",
		"uid", appDeployment.UID,
		"generation", appDeployment.Generation,
		"observedGeneration", appDeployment.Status.ObservedGeneration,
		"state", workloadStateDegraded,
		"reason", platformv1alpha1.ReasonReconcileFailed,
	)
}

func isPersistentReconcileError(err error) bool {
	return apierrors.IsInvalid(err) ||
		apierrors.IsBadRequest(err) ||
		apierrors.IsForbidden(err) ||
		apierrors.IsUnauthorized(err) ||
		apierrors.IsMethodNotSupported(err)
}
