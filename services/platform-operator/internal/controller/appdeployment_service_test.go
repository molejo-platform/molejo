package controller

import (
	"context"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"

	platformv1alpha1 "github.com/molejo-platform/molejo/packages/kubernetes-api/apis/platform/v1alpha1"
)

func TestReconcileCreatesPrivateService(t *testing.T) {
	ctx := context.Background()
	namespace := createTestNamespace(t, "private-service")
	appDeployment := newAppDeployment(namespace, "ap-privateservice", testImage)
	if err := testClient.Create(ctx, appDeployment); err != nil {
		t.Fatalf("create AppDeployment: %v", err)
	}

	reconciler := &AppDeploymentReconciler{Client: testClient, Scheme: testScheme}
	request := ctrl.Request{NamespacedName: types.NamespacedName{Name: appDeployment.Name, Namespace: namespace}}
	if _, err := reconciler.Reconcile(ctx, request); err != nil {
		t.Fatalf("reconcile AppDeployment: %v", err)
	}

	service := &corev1.Service{}
	if err := testClient.Get(ctx, request.NamespacedName, service); err != nil {
		t.Fatalf("get private Service: %v", err)
	}
	if !metav1.IsControlledBy(service, appDeployment) {
		t.Fatal("expected Service to be controlled by AppDeployment")
	}
	if service.Spec.Type != corev1.ServiceTypeClusterIP {
		t.Fatalf("expected ClusterIP Service, got %s", service.Spec.Type)
	}
}

func TestReconcileCorrectsPrivateServiceDrift(t *testing.T) {
	ctx := context.Background()
	namespace := createTestNamespace(t, "service-drift")
	appDeployment := newAppDeployment(namespace, "ap-servicedrift", testImage)
	if err := testClient.Create(ctx, appDeployment); err != nil {
		t.Fatalf("create AppDeployment: %v", err)
	}

	reconciler := &AppDeploymentReconciler{Client: testClient, Scheme: testScheme}
	request := ctrl.Request{NamespacedName: types.NamespacedName{Name: appDeployment.Name, Namespace: namespace}}
	if _, err := reconciler.Reconcile(ctx, request); err != nil {
		t.Fatalf("initial reconcile: %v", err)
	}

	service := getService(t, ctx, request.NamespacedName)
	service.Spec.Type = corev1.ServiceTypeLoadBalancer
	service.Spec.Selector = map[string]string{"drift": "true"}
	service.Spec.ExternalIPs = []string{"192.0.2.10"}
	service.Spec.Ports = []corev1.ServicePort{{
		Name:     "drift",
		Protocol: corev1.ProtocolTCP,
		Port:     9999,
	}}
	if err := testClient.Update(ctx, service); err != nil {
		t.Fatalf("introduce Service drift: %v", err)
	}

	if _, err := reconciler.Reconcile(ctx, request); err != nil {
		t.Fatalf("reconcile Service drift: %v", err)
	}
	service = getService(t, ctx, request.NamespacedName)
	assertPrivateService(t, service, appDeployment)
}

func TestReconcileRecoversServiceWithLoadBalancerClassDrift(t *testing.T) {
	ctx := context.Background()
	namespace := createTestNamespace(t, "service-load-balancer-class-drift")
	appDeployment := newAppDeployment(namespace, "ap-lbclassdrift", testImage)
	if err := testClient.Create(ctx, appDeployment); err != nil {
		t.Fatalf("create AppDeployment: %v", err)
	}

	reconciler := &AppDeploymentReconciler{Client: testClient, Scheme: testScheme}
	request := ctrl.Request{NamespacedName: types.NamespacedName{Name: appDeployment.Name, Namespace: namespace}}
	if _, err := reconciler.Reconcile(ctx, request); err != nil {
		t.Fatalf("initial reconcile: %v", err)
	}

	service := getService(t, ctx, request.NamespacedName)
	loadBalancerClass := "example.test/load-balancer"
	service.Spec.Type = corev1.ServiceTypeLoadBalancer
	service.Spec.LoadBalancerClass = &loadBalancerClass
	if err := testClient.Update(ctx, service); err != nil {
		t.Fatalf("introduce LoadBalancerClass drift: %v", err)
	}
	service = getService(t, ctx, request.NamespacedName)
	if service.Spec.Type != corev1.ServiceTypeLoadBalancer || service.Spec.LoadBalancerClass == nil ||
		*service.Spec.LoadBalancerClass != loadBalancerClass {
		t.Fatalf("expected LoadBalancerClass drift precondition, got type=%s loadBalancerClass=%v",
			service.Spec.Type, service.Spec.LoadBalancerClass)
	}

	result, err := reconciler.Reconcile(ctx, request)
	if err != nil {
		t.Fatalf("reconcile LoadBalancerClass drift: %v", err)
	}
	if result.RequeueAfter != 0 {
		t.Errorf("expected LoadBalancerClass drift to converge without delayed retry, got %s", result.RequeueAfter)
	}

	service = getService(t, ctx, request.NamespacedName)
	if service.Spec.Type != corev1.ServiceTypeClusterIP || service.Spec.LoadBalancerClass != nil {
		t.Errorf("expected private Service after reconciliation, got type=%s loadBalancerClass=%v",
			service.Spec.Type, service.Spec.LoadBalancerClass)
	}
	stored := getAppDeployment(t, ctx, request.NamespacedName)
	degraded := meta.FindStatusCondition(stored.Status.Conditions, platformv1alpha1.ConditionDegraded)
	if degraded != nil && degraded.Status == metav1.ConditionTrue {
		t.Errorf("expected LoadBalancerClass drift recovery instead of Degraded=True, got reason=%s", degraded.Reason)
	}
}

func TestReconcileCorrectsServiceBehavioralDrift(t *testing.T) {
	ctx := context.Background()
	namespace := createTestNamespace(t, "service-behavioral-drift")
	appDeployment := newAppDeployment(namespace, "ap-servicebehavior", testImage)
	if err := testClient.Create(ctx, appDeployment); err != nil {
		t.Fatalf("create AppDeployment: %v", err)
	}

	reconciler := &AppDeploymentReconciler{Client: testClient, Scheme: testScheme}
	request := ctrl.Request{NamespacedName: types.NamespacedName{Name: appDeployment.Name, Namespace: namespace}}
	if _, err := reconciler.Reconcile(ctx, request); err != nil {
		t.Fatalf("initial reconcile: %v", err)
	}

	service := getService(t, ctx, request.NamespacedName)
	service.Spec.Type = corev1.ServiceTypeLoadBalancer
	service.Spec.PublishNotReadyAddresses = true
	service.Spec.SessionAffinity = corev1.ServiceAffinityClientIP
	service.Spec.SessionAffinityConfig = &corev1.SessionAffinityConfig{
		ClientIP: &corev1.ClientIPConfig{TimeoutSeconds: pointerTo(int32(3600))},
	}
	service.Spec.InternalTrafficPolicy = pointerTo(corev1.ServiceInternalTrafficPolicyLocal)
	service.Spec.TrafficDistribution = pointerTo(corev1.ServiceTrafficDistributionPreferSameZone)
	service.Spec.LoadBalancerSourceRanges = []string{"192.0.2.0/24"}
	if err := testClient.Update(ctx, service); err != nil {
		t.Fatalf("introduce Service behavioral drift: %v", err)
	}

	result, err := reconciler.Reconcile(ctx, request)
	if err != nil {
		t.Fatalf("reconcile Service behavioral drift: %v", err)
	}
	if result.RequeueAfter != 0 {
		t.Errorf("expected Service behavioral drift to converge without delayed retry, got %s",
			result.RequeueAfter)
	}
	service = getService(t, ctx, request.NamespacedName)
	resourceVersion := service.ResourceVersion
	if service.Spec.PublishNotReadyAddresses {
		t.Error("expected publishNotReadyAddresses drift to be cleared")
	}
	if service.Spec.SessionAffinity != corev1.ServiceAffinityNone || service.Spec.SessionAffinityConfig != nil {
		t.Errorf("expected session affinity drift to be cleared, got affinity=%s config=%#v",
			service.Spec.SessionAffinity, service.Spec.SessionAffinityConfig)
	}
	if service.Spec.InternalTrafficPolicy != nil &&
		*service.Spec.InternalTrafficPolicy == corev1.ServiceInternalTrafficPolicyLocal {
		t.Error("expected Local internal traffic policy drift to be cleared")
	}
	if service.Spec.TrafficDistribution != nil {
		t.Errorf("expected traffic distribution drift to be cleared, got %q", *service.Spec.TrafficDistribution)
	}
	if len(service.Spec.LoadBalancerSourceRanges) != 0 {
		t.Errorf("expected load balancer source ranges drift to be cleared, got %v",
			service.Spec.LoadBalancerSourceRanges)
	}
	stored := getAppDeployment(t, ctx, request.NamespacedName)
	degraded := meta.FindStatusCondition(stored.Status.Conditions, platformv1alpha1.ConditionDegraded)
	if degraded != nil && degraded.Status == metav1.ConditionTrue {
		t.Errorf("expected Service drift recovery instead of Degraded=True, got reason=%s", degraded.Reason)
	}
	if _, err := reconciler.Reconcile(ctx, request); err != nil {
		t.Fatalf("reconcile corrected Service: %v", err)
	}
	service = getService(t, ctx, request.NamespacedName)
	if service.ResourceVersion != resourceVersion {
		t.Errorf("expected corrected Service to remain stable, got resourceVersions %s then %s",
			resourceVersion, service.ResourceVersion)
	}
}

func TestServiceOwnershipConflictPreservesObservedGeneration(t *testing.T) {
	ctx := context.Background()
	namespace := createTestNamespace(t, "service-ownership")
	appDeployment := newAppDeployment(namespace, "ap-serviceownership", testImage)
	if err := testClient.Create(ctx, appDeployment); err != nil {
		t.Fatalf("create AppDeployment: %v", err)
	}
	unowned := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: appDeployment.Name, Namespace: namespace},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{"test": "unowned"},
			Ports: []corev1.ServicePort{{
				Name:     "http",
				Protocol: corev1.ProtocolTCP,
				Port:     8080,
			}},
		},
	}
	if err := testClient.Create(ctx, unowned); err != nil {
		t.Fatalf("create unowned Service: %v", err)
	}

	recorder := record.NewFakeRecorder(5)
	reconciler := &AppDeploymentReconciler{Client: testClient, Scheme: testScheme, Recorder: recorder}
	request := ctrl.Request{NamespacedName: types.NamespacedName{Name: appDeployment.Name, Namespace: namespace}}
	result, err := reconciler.Reconcile(ctx, request)
	if err != nil {
		t.Fatalf("expected Service ownership conflict in status, got %v", err)
	}
	if result.RequeueAfter != ownershipConflictRequeueAfter {
		t.Fatalf("expected ownership conflict requeue after %s, got %s",
			ownershipConflictRequeueAfter, result.RequeueAfter)
	}
	stored := getAppDeployment(t, ctx, request.NamespacedName)
	if stored.Status.ObservedGeneration != 0 || stored.Status.ObservedRelease != "" || stored.Status.WorkloadRef != nil {
		t.Fatalf("expected Service conflict to preserve observed fields, got %#v", stored.Status)
	}
	assertCondition(t, stored, platformv1alpha1.ConditionDegraded, metav1.ConditionTrue,
		platformv1alpha1.ReasonOwnershipConflict)
	select {
	case event := <-recorder.Events:
		if !strings.Contains(event, "DeploymentCreated") {
			t.Fatalf("expected the successful Deployment transition before the Service conflict, got %q", event)
		}
	default:
		t.Fatal("expected the successful Deployment transition to be recorded")
	}
}
