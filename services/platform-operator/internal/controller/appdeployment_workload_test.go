package controller

import (
	"context"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"

	platformv1alpha1 "github.com/molejo-platform/molejo/packages/kubernetes-api/apis/platform/v1alpha1"
)

func TestReconcileCorrectsSecuritySensitivePodSpecDrift(t *testing.T) {
	ctx := context.Background()
	namespace := createTestNamespace(t, "pod-security-drift")
	appDeployment := newAppDeployment(namespace, "ap-podsecuritydrift", testImage)
	if err := testClient.Create(ctx, appDeployment); err != nil {
		t.Fatalf("create AppDeployment: %v", err)
	}

	reconciler := &AppDeploymentReconciler{Client: testClient, Scheme: testScheme}
	request := ctrl.Request{NamespacedName: types.NamespacedName{Name: appDeployment.Name, Namespace: namespace}}
	if _, err := reconciler.Reconcile(ctx, request); err != nil {
		t.Fatalf("initial reconcile: %v", err)
	}

	deployment := getDeployment(t, ctx, request.NamespacedName)
	deployment.Spec.Template.Spec.HostNetwork = true
	deployment.Spec.Template.Spec.HostPID = true
	deployment.Spec.Template.Spec.HostIPC = true
	deployment.Spec.Template.Spec.NodeName = "drifted-node"
	deployment.Spec.Template.Spec.SchedulerName = "drifted-scheduler"
	deployment.Spec.Template.Spec.ReadinessGates = []corev1.PodReadinessGate{{
		ConditionType: "platform.example.test/ready",
	}}
	deployment.Spec.Template.Spec.RuntimeClassName = pointerTo("drifted-runtime")
	deployment.Spec.Template.Spec.SchedulingGates = []corev1.PodSchedulingGate{{Name: "drifted-gate"}}
	deployment.Spec.Template.Spec.ServiceAccountName = "drifted-service-account"
	deployment.Spec.Template.Spec.AutomountServiceAccountToken = pointerTo(true)
	deployment.Spec.Template.Spec.InitContainers = []corev1.Container{{
		Name:  "privileged-init",
		Image: testImage,
		SecurityContext: &corev1.SecurityContext{
			Privileged: pointerTo(true),
		},
	}}
	deployment.Spec.Template.Spec.Volumes = []corev1.Volume{{
		Name: "host-root",
		VolumeSource: corev1.VolumeSource{HostPath: &corev1.HostPathVolumeSource{
			Path: "/",
		}},
	}}
	if err := testClient.Update(ctx, deployment); err != nil {
		t.Fatalf("introduce security-sensitive PodSpec drift: %v", err)
	}

	if _, err := reconciler.Reconcile(ctx, request); err != nil {
		t.Fatalf("reconcile security-sensitive PodSpec drift: %v", err)
	}
	deployment = getDeployment(t, ctx, request.NamespacedName)
	resourceVersion := deployment.ResourceVersion
	podSpec := deployment.Spec.Template.Spec
	if podSpec.HostNetwork || podSpec.HostPID || podSpec.HostIPC {
		t.Errorf("expected host namespace drift to be cleared, got hostNetwork=%t hostPID=%t hostIPC=%t",
			podSpec.HostNetwork, podSpec.HostPID, podSpec.HostIPC)
	}
	if podSpec.NodeName != "" || podSpec.SchedulerName != corev1.DefaultSchedulerName ||
		len(podSpec.ReadinessGates) != 0 ||
		podSpec.RuntimeClassName != nil || len(podSpec.SchedulingGates) != 0 {
		t.Errorf("expected execution-control drift to be cleared, got %#v", podSpec)
	}
	if len(podSpec.InitContainers) != 0 {
		t.Errorf("expected injected init containers to be removed, got %#v", podSpec.InitContainers)
	}
	if len(podSpec.Volumes) != 0 {
		t.Errorf("expected injected volumes to be removed, got %#v", podSpec.Volumes)
	}
	if podSpec.ServiceAccountName != "" {
		t.Errorf("expected drifted service account to be cleared, got %q", podSpec.ServiceAccountName)
	}
	if podSpec.AutomountServiceAccountToken == nil || *podSpec.AutomountServiceAccountToken {
		t.Errorf("expected service account token automount to be explicitly disabled, got %v",
			podSpec.AutomountServiceAccountToken)
	}
	if _, err := reconciler.Reconcile(ctx, request); err != nil {
		t.Fatalf("reconcile corrected PodSpec: %v", err)
	}
	deployment = getDeployment(t, ctx, request.NamespacedName)
	if deployment.ResourceVersion != resourceVersion {
		t.Errorf("expected corrected Deployment to remain stable, got resourceVersions %s then %s",
			resourceVersion, deployment.ResourceVersion)
	}
}

func TestReconcileCorrectsDeploymentRolloutDrift(t *testing.T) {
	ctx := context.Background()
	namespace := createTestNamespace(t, "deployment-rollout-drift")
	appDeployment := newAppDeployment(namespace, "ap-rolloutdrift", testImage)
	if err := testClient.Create(ctx, appDeployment); err != nil {
		t.Fatalf("create AppDeployment: %v", err)
	}

	reconciler := &AppDeploymentReconciler{Client: testClient, Scheme: testScheme}
	request := ctrl.Request{NamespacedName: types.NamespacedName{Name: appDeployment.Name, Namespace: namespace}}
	if _, err := reconciler.Reconcile(ctx, request); err != nil {
		t.Fatalf("initial reconcile: %v", err)
	}

	deployment := getDeployment(t, ctx, request.NamespacedName)
	deployment.Spec.Paused = true
	deployment.Spec.Strategy = appsv1.DeploymentStrategy{Type: appsv1.RecreateDeploymentStrategyType}
	deployment.Spec.MinReadySeconds = 60
	deployment.Spec.ProgressDeadlineSeconds = pointerTo(int32(1200))
	deployment.Spec.RevisionHistoryLimit = pointerTo(int32(1))
	if err := testClient.Update(ctx, deployment); err != nil {
		t.Fatalf("introduce Deployment rollout drift: %v", err)
	}

	if _, err := reconciler.Reconcile(ctx, request); err != nil {
		t.Fatalf("reconcile Deployment rollout drift: %v", err)
	}
	deployment = getDeployment(t, ctx, request.NamespacedName)
	resourceVersion := deployment.ResourceVersion
	if deployment.Spec.Paused {
		t.Error("expected paused drift to be cleared")
	}
	strategy := deployment.Spec.Strategy
	if strategy.Type != appsv1.RollingUpdateDeploymentStrategyType || strategy.RollingUpdate == nil ||
		strategy.RollingUpdate.MaxUnavailable == nil || strategy.RollingUpdate.MaxUnavailable.String() != "25%" ||
		strategy.RollingUpdate.MaxSurge == nil || strategy.RollingUpdate.MaxSurge.String() != "25%" {
		t.Errorf("expected the default RollingUpdate strategy, got %#v", strategy)
	}
	if deployment.Spec.MinReadySeconds != 0 {
		t.Errorf("expected minReadySeconds drift to be cleared, got %d", deployment.Spec.MinReadySeconds)
	}
	if deployment.Spec.ProgressDeadlineSeconds == nil || *deployment.Spec.ProgressDeadlineSeconds != 600 {
		t.Errorf("expected the default 600 second progress deadline, got %v",
			deployment.Spec.ProgressDeadlineSeconds)
	}
	if deployment.Spec.RevisionHistoryLimit == nil || *deployment.Spec.RevisionHistoryLimit != 10 {
		t.Errorf("expected the default revision history limit, got %v", deployment.Spec.RevisionHistoryLimit)
	}

	if _, err := reconciler.Reconcile(ctx, request); err != nil {
		t.Fatalf("reconcile corrected Deployment rollout: %v", err)
	}
	deployment = getDeployment(t, ctx, request.NamespacedName)
	if deployment.ResourceVersion != resourceVersion {
		t.Errorf("expected corrected Deployment rollout to remain stable, got resourceVersions %s then %s",
			resourceVersion, deployment.ResourceVersion)
	}
}

func TestReconcileCreatesUpdatesAndRecoversDeployment(t *testing.T) {
	ctx := context.Background()
	namespace := createTestNamespace(t, "reconcile")
	appDeployment := newAppDeployment(namespace, "ap-reconcile0001", testImage)
	if err := testClient.Create(ctx, appDeployment); err != nil {
		t.Fatalf("create AppDeployment: %v", err)
	}

	reconciler := &AppDeploymentReconciler{Client: testClient, Scheme: testScheme}
	request := ctrl.Request{NamespacedName: types.NamespacedName{Name: appDeployment.Name, Namespace: namespace}}
	if _, err := reconciler.Reconcile(ctx, request); err != nil {
		t.Fatalf("initial reconcile: %v", err)
	}

	deployment := getDeployment(t, ctx, request.NamespacedName)
	if !metav1.IsControlledBy(deployment, appDeployment) {
		t.Fatal("expected Deployment to be controlled by AppDeployment")
	}
	if got := deployment.Spec.Template.Spec.Containers[0].Image; got != testImage {
		t.Fatalf("expected image %q, got %q", testImage, got)
	}
	if deployment.Spec.Replicas == nil || *deployment.Spec.Replicas != 1 {
		t.Fatalf("expected one replica, got %v", deployment.Spec.Replicas)
	}
	assertDeploymentRuntime(t, deployment, appDeployment.Spec)

	service := getService(t, ctx, request.NamespacedName)
	assertPrivateService(t, service, appDeployment)

	stored := getAppDeployment(t, ctx, request.NamespacedName)
	assertCondition(t, stored, platformv1alpha1.ConditionProgressing, metav1.ConditionTrue,
		platformv1alpha1.ReasonDeploymentProgressing)
	assertCondition(t, stored, platformv1alpha1.ConditionReady, metav1.ConditionFalse,
		platformv1alpha1.ReasonDeploymentProgressing)
	if stored.Status.ObservedGeneration != stored.Generation {
		t.Fatalf("expected observed generation %d, got %d", stored.Generation, stored.Status.ObservedGeneration)
	}

	deploymentResourceVersion := deployment.ResourceVersion
	serviceResourceVersion := service.ResourceVersion
	if _, err := reconciler.Reconcile(ctx, request); err != nil {
		t.Fatalf("idempotent reconcile: %v", err)
	}
	deployment = getDeployment(t, ctx, request.NamespacedName)
	if deployment.ResourceVersion != deploymentResourceVersion {
		t.Fatalf("expected idempotent reconcile to preserve Deployment resourceVersion, got %s then %s",
			deploymentResourceVersion, deployment.ResourceVersion)
	}
	service = getService(t, ctx, request.NamespacedName)
	if service.ResourceVersion != serviceResourceVersion {
		t.Fatalf("expected idempotent reconcile to preserve Service resourceVersion, got %s then %s",
			serviceResourceVersion, service.ResourceVersion)
	}

	stored.Spec.Image = otherImage
	stored.Spec.Replicas = pointerTo(int32(2))
	stored.Spec.Port = 9090
	stored.Spec.Resources.Requests = platformv1alpha1.AppDeploymentResourceValues{
		CPUMillis: 75,
		MemoryMiB: 96,
	}
	stored.Spec.Resources.Limits = platformv1alpha1.AppDeploymentResourceValues{
		CPUMillis: 750,
		MemoryMiB: 384,
	}
	stored.Spec.Probes.Liveness.Path = "/live"
	stored.Spec.Probes.Readiness.Path = "/ready"
	if err := testClient.Update(ctx, stored); err != nil {
		t.Fatalf("update AppDeployment: %v", err)
	}
	if _, err := reconciler.Reconcile(ctx, request); err != nil {
		t.Fatalf("reconcile updated AppDeployment: %v", err)
	}
	deployment = getDeployment(t, ctx, request.NamespacedName)
	if deployment.Spec.Template.Spec.Containers[0].Image != otherImage {
		t.Fatalf("expected updated image %q, got %q", otherImage, deployment.Spec.Template.Spec.Containers[0].Image)
	}
	if deployment.Spec.Replicas == nil || *deployment.Spec.Replicas != 2 {
		t.Fatalf("expected two replicas, got %v", deployment.Spec.Replicas)
	}
	assertDeploymentRuntime(t, deployment, stored.Spec)
	service = getService(t, ctx, request.NamespacedName)
	assertPrivateService(t, service, stored)
	if service.Spec.Ports[0].Port != 9090 {
		t.Fatalf("expected updated Service port 9090, got %d", service.Spec.Ports[0].Port)
	}

	previousUID := deployment.UID
	if err := testClient.Delete(ctx, deployment); err != nil {
		t.Fatalf("delete managed Deployment: %v", err)
	}
	if _, err := reconciler.Reconcile(ctx, request); err != nil {
		t.Fatalf("reconcile deleted child: %v", err)
	}
	deployment = getDeployment(t, ctx, request.NamespacedName)
	if deployment.UID == previousUID {
		t.Fatal("expected deleted Deployment to be recreated with a new UID")
	}

	previousServiceUID := service.UID
	if err := testClient.Delete(ctx, service); err != nil {
		t.Fatalf("delete managed Service: %v", err)
	}
	if _, err := reconciler.Reconcile(ctx, request); err != nil {
		t.Fatalf("reconcile deleted Service: %v", err)
	}
	service = getService(t, ctx, request.NamespacedName)
	if service.UID == previousServiceUID {
		t.Fatal("expected deleted Service to be recreated with a new UID")
	}
	assertPrivateService(t, service, stored)

	deployment.Status.ObservedGeneration = deployment.Generation
	deployment.Status.Replicas = 2
	deployment.Status.UpdatedReplicas = 2
	deployment.Status.ReadyReplicas = 2
	deployment.Status.AvailableReplicas = 2
	if err := testClient.Status().Update(ctx, deployment); err != nil {
		t.Fatalf("mark Deployment available: %v", err)
	}
	if _, err := reconciler.Reconcile(ctx, request); err != nil {
		t.Fatalf("reconcile available Deployment: %v", err)
	}
	stored = getAppDeployment(t, ctx, request.NamespacedName)
	assertCondition(t, stored, platformv1alpha1.ConditionReady, metav1.ConditionTrue,
		platformv1alpha1.ReasonDeploymentAvailable)
	assertCondition(t, stored, platformv1alpha1.ConditionProgressing, metav1.ConditionFalse,
		platformv1alpha1.ReasonDeploymentAvailable)
	assertCondition(t, stored, platformv1alpha1.ConditionDegraded, metav1.ConditionFalse,
		platformv1alpha1.ReasonDeploymentAvailable)
	if stored.Status.ObservedRelease != otherImage {
		t.Fatalf("expected observed release %q, got %q", otherImage, stored.Status.ObservedRelease)
	}
	if stored.Status.WorkloadRef == nil || stored.Status.WorkloadRef.Name != deployment.Name {
		t.Fatalf("expected workload reference %q, got %#v", deployment.Name, stored.Status.WorkloadRef)
	}
}

func TestOwnershipConflictPreservesObservedGeneration(t *testing.T) {
	ctx := context.Background()
	namespace := createTestNamespace(t, "ownership")
	appDeployment := newAppDeployment(namespace, "ap-ownership0001", testImage)
	if err := testClient.Create(ctx, appDeployment); err != nil {
		t.Fatalf("create AppDeployment: %v", err)
	}

	reconciler := &AppDeploymentReconciler{Client: testClient, Scheme: testScheme}
	request := ctrl.Request{NamespacedName: types.NamespacedName{Name: appDeployment.Name, Namespace: namespace}}
	if _, err := reconciler.Reconcile(ctx, request); err != nil {
		t.Fatalf("initial reconcile: %v", err)
	}
	stored := getAppDeployment(t, ctx, request.NamespacedName)
	previousObservedGeneration := stored.Status.ObservedGeneration

	managedDeployment := getDeployment(t, ctx, request.NamespacedName)
	if err := testClient.Delete(ctx, managedDeployment); err != nil {
		t.Fatalf("delete managed Deployment: %v", err)
	}
	stored.Spec.Image = otherImage
	if err := testClient.Update(ctx, stored); err != nil {
		t.Fatalf("update AppDeployment: %v", err)
	}

	unowned := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: appDeployment.Name, Namespace: namespace},
		Spec: appsv1.DeploymentSpec{
			Replicas: pointerTo(int32(1)),
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"test": "unowned"}},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"test": "unowned"}},
				Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "app", Image: testImage}}},
			},
		},
	}
	if err := testClient.Create(ctx, unowned); err != nil {
		t.Fatalf("create unowned Deployment: %v", err)
	}

	result, err := reconciler.Reconcile(ctx, request)
	if err != nil {
		t.Fatalf("expected ownership conflict to be reported as status, got %v", err)
	}
	if result.RequeueAfter != ownershipConflictRequeueAfter {
		t.Fatalf("expected ownership conflict requeue after %s, got %s",
			ownershipConflictRequeueAfter, result.RequeueAfter)
	}
	stored = getAppDeployment(t, ctx, request.NamespacedName)
	if stored.Status.ObservedGeneration != previousObservedGeneration {
		t.Fatalf("expected observed generation to remain %d, got %d",
			previousObservedGeneration, stored.Status.ObservedGeneration)
	}
	assertCondition(t, stored, platformv1alpha1.ConditionDegraded, metav1.ConditionTrue,
		platformv1alpha1.ReasonOwnershipConflict)
	if strings.Contains(meta.FindStatusCondition(stored.Status.Conditions, platformv1alpha1.ConditionDegraded).Message, "uid") {
		t.Fatal("expected degraded message to remain sanitized")
	}
}
