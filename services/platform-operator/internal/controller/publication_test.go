package controller

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	platformv1alpha1 "github.com/molejo-platform/molejo/packages/kubernetes-api/apis/platform/v1alpha1"
)

func TestAppDeploymentPublicationSchema(t *testing.T) {
	ctx := context.Background()
	namespace := createTestNamespace(t, "publication-schema")

	valid := newAppDeployment(namespace, "ap-publicvalid", testImage)
	valid.Spec.Exposure = platformv1alpha1.ExposurePublic
	valid.Spec.Slug = "public-valid"
	if err := testClient.Create(ctx, valid); err != nil {
		t.Fatalf("create valid public AppDeployment: %v", err)
	}

	tests := []struct {
		name   string
		mutate func(*platformv1alpha1.AppDeployment)
	}{
		{
			name: "rejects an unknown exposure",
			mutate: func(appDeployment *platformv1alpha1.AppDeployment) {
				appDeployment.Spec.Exposure = "External"
			},
		},
		{
			name: "requires a slug for public exposure",
			mutate: func(appDeployment *platformv1alpha1.AppDeployment) {
				appDeployment.Spec.Exposure = platformv1alpha1.ExposurePublic
			},
		},
		{
			name: "forbids a slug for private exposure",
			mutate: func(appDeployment *platformv1alpha1.AppDeployment) {
				appDeployment.Spec.Slug = "unexpected"
			},
		},
		{
			name: "rejects a non-DNS slug",
			mutate: func(appDeployment *platformv1alpha1.AppDeployment) {
				appDeployment.Spec.Exposure = platformv1alpha1.ExposurePublic
				appDeployment.Spec.Slug = "Invalid_Slug"
			},
		},
		{
			name: "rejects a slug longer than one DNS label",
			mutate: func(appDeployment *platformv1alpha1.AppDeployment) {
				appDeployment.Spec.Exposure = platformv1alpha1.ExposurePublic
				appDeployment.Spec.Slug = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
			},
		},
	}
	for index, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			appDeployment := newAppDeployment(namespace, fmt.Sprintf("ap-publicinvalid%02d", index), testImage)
			test.mutate(appDeployment)
			if err := testClient.Create(ctx, appDeployment); err == nil {
				t.Fatal("expected API validation to reject AppDeployment")
			}
		})
	}
}

func TestReconcileCreatesPublicHTTPRoute(t *testing.T) {
	ctx := context.Background()
	namespace := createTestNamespace(t, "public-route")
	appDeployment := newAppDeployment(namespace, "ap-publicroute", testImage)
	appDeployment.Spec.Exposure = platformv1alpha1.ExposurePublic
	appDeployment.Spec.Slug = "public-route"
	if err := testClient.Create(ctx, appDeployment); err != nil {
		t.Fatalf("create AppDeployment: %v", err)
	}

	reconciler := &AppDeploymentReconciler{Client: testClient, Scheme: testScheme}
	request := ctrl.Request{NamespacedName: types.NamespacedName{
		Name: appDeployment.Name, Namespace: namespace,
	}}
	if _, err := reconciler.Reconcile(ctx, request); err != nil {
		t.Fatalf("reconcile public AppDeployment: %v", err)
	}

	route := getHTTPRoute(t, ctx, request.NamespacedName)
	if !metav1.IsControlledBy(route, appDeployment) {
		t.Fatal("expected HTTPRoute to be controlled by AppDeployment")
	}
	if len(route.Spec.Hostnames) != 1 || route.Spec.Hostnames[0] != "public-route.molejo.dev" {
		t.Fatalf("unexpected HTTPRoute hostnames: %#v", route.Spec.Hostnames)
	}
	if len(route.Spec.ParentRefs) != 1 || route.Spec.ParentRefs[0].Name != "molejo" ||
		route.Spec.ParentRefs[0].Namespace == nil || *route.Spec.ParentRefs[0].Namespace != "molejo-system" ||
		route.Spec.ParentRefs[0].SectionName == nil || *route.Spec.ParentRefs[0].SectionName != "https-molejo" {
		t.Fatalf("unexpected HTTPRoute parent references: %#v", route.Spec.ParentRefs)
	}
	if len(route.Spec.Rules) != 1 || len(route.Spec.Rules[0].BackendRefs) != 1 {
		t.Fatalf("expected exactly one HTTPRoute backend, got %#v", route.Spec.Rules)
	}
	backend := route.Spec.Rules[0].BackendRefs[0].BackendRef
	if backend.Name != gatewayv1.ObjectName(appDeployment.Name) || backend.Port == nil ||
		*backend.Port != gatewayv1.PortNumber(appDeployment.Spec.Port) {
		t.Fatalf("unexpected HTTPRoute backend: %#v", backend)
	}
}

func TestReconcileRemovesPublicHTTPRouteWhenExposureBecomesPrivate(t *testing.T) {
	ctx := context.Background()
	namespace := createTestNamespace(t, "remove-route")
	appDeployment := newAppDeployment(namespace, "ap-removeroute", testImage)
	appDeployment.Spec.Exposure = platformv1alpha1.ExposurePublic
	appDeployment.Spec.Slug = "remove-route"
	if err := testClient.Create(ctx, appDeployment); err != nil {
		t.Fatalf("create AppDeployment: %v", err)
	}

	reconciler := &AppDeploymentReconciler{Client: testClient, Scheme: testScheme}
	key := client.ObjectKey{Name: appDeployment.Name, Namespace: namespace}
	request := ctrl.Request{NamespacedName: key}
	if _, err := reconciler.Reconcile(ctx, request); err != nil {
		t.Fatalf("reconcile public AppDeployment: %v", err)
	}
	_ = getHTTPRoute(t, ctx, key)

	stored := getAppDeployment(t, ctx, key)
	stored.Spec.Exposure = platformv1alpha1.ExposurePrivate
	stored.Spec.Slug = ""
	if err := testClient.Update(ctx, stored); err != nil {
		t.Fatalf("make AppDeployment private: %v", err)
	}
	if _, err := reconciler.Reconcile(ctx, request); err != nil {
		t.Fatalf("reconcile private AppDeployment: %v", err)
	}

	route := &gatewayv1.HTTPRoute{}
	err := testClient.Get(ctx, key, route)
	if !apierrors.IsNotFound(err) {
		t.Fatalf("expected HTTPRoute to be removed, got %v", err)
	}
}

func TestReconcilePublicHTTPRouteIsIdempotent(t *testing.T) {
	ctx := context.Background()
	namespace := createTestNamespace(t, "route-idempotent")
	appDeployment := newAppDeployment(namespace, "ap-routeidempotent", testImage)
	appDeployment.Spec.Exposure = platformv1alpha1.ExposurePublic
	appDeployment.Spec.Slug = "route-idempotent"
	if err := testClient.Create(ctx, appDeployment); err != nil {
		t.Fatalf("create AppDeployment: %v", err)
	}

	reconciler := &AppDeploymentReconciler{Client: testClient, Scheme: testScheme}
	key := client.ObjectKey{Name: appDeployment.Name, Namespace: namespace}
	request := ctrl.Request{NamespacedName: key}
	if _, err := reconciler.Reconcile(ctx, request); err != nil {
		t.Fatalf("initial reconcile: %v", err)
	}
	resourceVersion := getHTTPRoute(t, ctx, key).ResourceVersion
	if _, err := reconciler.Reconcile(ctx, request); err != nil {
		t.Fatalf("idempotent reconcile: %v", err)
	}
	if got := getHTTPRoute(t, ctx, key).ResourceVersion; got != resourceVersion {
		t.Fatalf("expected HTTPRoute resourceVersion %q to remain stable, got %q", resourceVersion, got)
	}
}

func TestReconcilePublicHTTPRouteDoesNotPatchAPIServerDefaults(t *testing.T) {
	ctx := context.Background()
	scheme := runtime.NewScheme()
	if err := platformv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("add AppDeployment scheme: %v", err)
	}
	if err := gatewayv1.Install(scheme); err != nil {
		t.Fatalf("add Gateway API scheme: %v", err)
	}

	appDeployment := newAppDeployment("ws-defaults", "ap-routedefaults", testImage)
	appDeployment.UID = types.UID("appdeployment-route-defaults")
	appDeployment.Spec.Exposure = platformv1alpha1.ExposurePublic
	appDeployment.Spec.Slug = "route-defaults"
	baseClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(appDeployment.DeepCopy()).Build()
	defaultingClient := &gatewayDefaultingClient{Client: baseClient}
	reconciler := &AppDeploymentReconciler{Client: defaultingClient, Scheme: scheme}

	if _, _, _, err := reconciler.applyHTTPPublication(ctx, appDeployment); err != nil {
		t.Fatalf("create HTTPRoute: %v", err)
	}
	defaultingClient.patchCalls = 0
	if _, _, _, err := reconciler.applyHTTPPublication(ctx, appDeployment); err != nil {
		t.Fatalf("reconcile defaulted HTTPRoute: %v", err)
	}
	if defaultingClient.patchCalls != 0 {
		t.Fatalf("expected no HTTPRoute PATCH after API defaults, got %d", defaultingClient.patchCalls)
	}
}

func TestGatewayCertificateFailureRevokesPublicReadinessWithoutAppChange(t *testing.T) {
	ctx := context.Background()
	systemNamespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "molejo-system"}}
	if err := testClient.Create(ctx, systemNamespace); err != nil && !apierrors.IsAlreadyExists(err) {
		t.Fatalf("create Gateway namespace: %v", err)
	}

	gateway := &gatewayv1.Gateway{
		ObjectMeta: metav1.ObjectMeta{Name: "molejo", Namespace: systemNamespace.Name},
		Spec: gatewayv1.GatewaySpec{
			GatewayClassName: "test-gateway-class",
			Listeners: []gatewayv1.Listener{{
				Name: "https-molejo", Port: 443, Protocol: gatewayv1.HTTPSProtocolType,
			}},
		},
	}
	if err := testClient.Create(ctx, gateway); err != nil {
		t.Fatalf("create shared Gateway: %v", err)
	}
	t.Cleanup(func() {
		_ = testClient.Delete(context.Background(), gateway)
	})
	setTestGatewayStatus(t, ctx, gateway, true)

	namespace := createTestNamespace(t, "gateway-readiness")
	appDeployment := newAppDeployment(namespace, "ap-gatewayreadiness", testImage)
	appDeployment.Spec.Exposure = platformv1alpha1.ExposurePublic
	appDeployment.Spec.Slug = "gateway-readiness"
	if err := testClient.Create(ctx, appDeployment); err != nil {
		t.Fatalf("create public AppDeployment: %v", err)
	}
	key := client.ObjectKeyFromObject(appDeployment)
	reconciler := &AppDeploymentReconciler{Client: testClient, Scheme: testScheme}
	request := ctrl.Request{NamespacedName: key}
	if _, err := reconciler.Reconcile(ctx, request); err != nil {
		t.Fatalf("create managed children: %v", err)
	}
	deployment := getDeployment(t, ctx, key)
	desired := desiredReplicas(appDeployment)
	deployment.Status.ObservedGeneration = deployment.Generation
	deployment.Status.Replicas = desired
	deployment.Status.UpdatedReplicas = desired
	deployment.Status.ReadyReplicas = desired
	deployment.Status.AvailableReplicas = desired
	if err := testClient.Status().Update(ctx, deployment); err != nil {
		t.Fatalf("mark Deployment ready: %v", err)
	}
	route := getHTTPRoute(t, ctx, key)
	route.Status = routeWithConditions(
		metav1.Condition{
			Type: string(gatewayv1.RouteConditionAccepted), Status: metav1.ConditionTrue,
			ObservedGeneration: route.Generation,
		},
		metav1.Condition{
			Type: string(gatewayv1.RouteConditionResolvedRefs), Status: metav1.ConditionTrue,
			ObservedGeneration: route.Generation,
		},
	).Status
	if err := testClient.Status().Update(ctx, route); err != nil {
		t.Fatalf("mark HTTPRoute accepted: %v", err)
	}
	if _, err := reconciler.Reconcile(ctx, request); err != nil {
		t.Fatalf("establish public readiness: %v", err)
	}
	stored := getAppDeployment(t, ctx, key)
	ready := meta.FindStatusCondition(stored.Status.Conditions, platformv1alpha1.ConditionReady)
	if ready == nil || ready.Status != metav1.ConditionTrue {
		t.Fatalf("expected healthy Gateway baseline to become Ready=True, got %#v", ready)
	}

	manager, err := ctrl.NewManager(testConfig, ctrl.Options{
		Scheme:                 testScheme,
		Metrics:                metricsserver.Options{BindAddress: "0"},
		HealthProbeBindAddress: "0",
	})
	if err != nil {
		t.Fatalf("create test manager: %v", err)
	}
	if err := (&AppDeploymentReconciler{
		Client: manager.GetClient(), Scheme: manager.GetScheme(),
	}).SetupWithManager(manager); err != nil {
		t.Fatalf("register test reconciler: %v", err)
	}
	managerCtx, cancelManager := context.WithCancel(context.Background())
	managerErrors := make(chan error, 1)
	go func() {
		managerErrors <- manager.Start(managerCtx)
	}()
	t.Cleanup(func() {
		cancelManager()
		select {
		case <-managerErrors:
		case <-time.After(5 * time.Second):
			t.Error("test manager did not stop")
		}
	})
	if !manager.GetCache().WaitForCacheSync(managerCtx) {
		t.Fatal("test manager cache did not synchronize")
	}

	setTestGatewayStatus(t, ctx, gateway, false)
	waitForCondition(t, 3*time.Second, "Gateway certificate failure to revoke public readiness", func() bool {
		stored := &platformv1alpha1.AppDeployment{}
		if err := testClient.Get(ctx, key, stored); err != nil {
			return false
		}
		ready := meta.FindStatusCondition(stored.Status.Conditions, platformv1alpha1.ConditionReady)
		degraded := meta.FindStatusCondition(stored.Status.Conditions, platformv1alpha1.ConditionDegraded)
		if ready == nil || ready.Status != metav1.ConditionFalse ||
			degraded == nil || degraded.Status != metav1.ConditionTrue {
			return false
		}
		return !strings.Contains(degraded.Message, "private-certificate-detail")
	})
}

func TestReconcileRejectsDuplicatePublicHostname(t *testing.T) {
	ctx := context.Background()
	firstNamespace := createTestNamespace(t, "hostname-owner")
	secondNamespace := createTestNamespace(t, "hostname-conflict")
	reconciler := &AppDeploymentReconciler{Client: testClient, Scheme: testScheme}

	first := newAppDeployment(firstNamespace, "ap-hostowner", testImage)
	first.Spec.Exposure = platformv1alpha1.ExposurePublic
	first.Spec.Slug = "globally-unique"
	if err := testClient.Create(ctx, first); err != nil {
		t.Fatalf("create hostname owner: %v", err)
	}
	firstRequest := ctrl.Request{NamespacedName: client.ObjectKeyFromObject(first)}
	if _, err := reconciler.Reconcile(ctx, firstRequest); err != nil {
		t.Fatalf("reconcile hostname owner: %v", err)
	}

	second := newAppDeployment(secondNamespace, "ap-hostconflict", testImage)
	if err := testClient.Create(ctx, second); err != nil {
		t.Fatalf("create private AppDeployment: %v", err)
	}
	secondRequest := ctrl.Request{NamespacedName: client.ObjectKeyFromObject(second)}
	if _, err := reconciler.Reconcile(ctx, secondRequest); err != nil {
		t.Fatalf("reconcile private AppDeployment: %v", err)
	}
	previousObservedGeneration := getAppDeployment(t, ctx, client.ObjectKeyFromObject(second)).Status.ObservedGeneration

	second = getAppDeployment(t, ctx, client.ObjectKeyFromObject(second))
	second.Spec.Exposure = platformv1alpha1.ExposurePublic
	second.Spec.Slug = first.Spec.Slug
	if err := testClient.Update(ctx, second); err != nil {
		t.Fatalf("request duplicate hostname: %v", err)
	}
	result, err := reconciler.Reconcile(ctx, secondRequest)
	if err != nil {
		t.Fatalf("reconcile duplicate hostname: %v", err)
	}
	if result.RequeueAfter != ownershipConflictRequeueAfter {
		t.Fatalf("expected bounded hostname conflict requeue, got %s", result.RequeueAfter)
	}
	stored := getAppDeployment(t, ctx, client.ObjectKeyFromObject(second))
	if stored.Status.ObservedGeneration != previousObservedGeneration {
		t.Fatalf("expected observed generation %d to be preserved, got %d",
			previousObservedGeneration, stored.Status.ObservedGeneration)
	}
	assertCondition(t, stored, platformv1alpha1.ConditionDegraded, metav1.ConditionTrue,
		platformv1alpha1.ReasonHostnameConflict)
}

func TestReconcileSelectsOneWinnerWhenHostnameClaimsAlreadyExist(t *testing.T) {
	ctx := context.Background()
	firstNamespace := createTestNamespace(t, "hostname-race-first")
	secondNamespace := createTestNamespace(t, "hostname-race-second")
	reconciler := &AppDeploymentReconciler{Client: testClient, Scheme: testScheme}

	first := newAppDeployment(firstNamespace, "ap-hostracefirst", testImage)
	first.Spec.Exposure = platformv1alpha1.ExposurePublic
	first.Spec.Slug = "simultaneous-claim"
	second := newAppDeployment(secondNamespace, "ap-hostracesecond", testImage)
	second.Spec.Exposure = platformv1alpha1.ExposurePublic
	second.Spec.Slug = first.Spec.Slug

	// Both claims deliberately exist before either reconciliation starts. This is
	// the ordering that a sequential owner-then-contender test does not exercise.
	if err := testClient.Create(ctx, first); err != nil {
		t.Fatalf("create first hostname claim: %v", err)
	}
	if err := testClient.Create(ctx, second); err != nil {
		t.Fatalf("create second hostname claim: %v", err)
	}

	requests := []ctrl.Request{
		{NamespacedName: client.ObjectKeyFromObject(first)},
		{NamespacedName: client.ObjectKeyFromObject(second)},
	}
	for _, request := range requests {
		if _, err := reconciler.Reconcile(ctx, request); err != nil {
			t.Fatalf("reconcile hostname claimant %s: %v", request.NamespacedName, err)
		}
	}

	listWinners := func() []gatewayv1.HTTPRoute {
		t.Helper()
		routes := &gatewayv1.HTTPRouteList{}
		if err := testClient.List(ctx, routes); err != nil {
			t.Fatalf("list HTTPRoutes: %v", err)
		}
		winners := make([]gatewayv1.HTTPRoute, 0, 1)
		for _, route := range routes.Items {
			if len(route.Spec.Hostnames) == 1 && route.Spec.Hostnames[0] == "simultaneous-claim.molejo.dev" {
				winners = append(winners, route)
			}
		}
		return winners
	}
	winners := listWinners()
	if len(winners) != 1 {
		t.Fatalf("expected exactly one deterministic hostname winner, got %d HTTPRoutes", len(winners))
	}

	winnerOwner := metav1.GetControllerOf(&winners[0])
	if winnerOwner == nil {
		t.Fatal("expected the hostname winner HTTPRoute to have a controller owner")
	}
	winnerUID := winnerOwner.UID
	for _, appDeployment := range []*platformv1alpha1.AppDeployment{first, second} {
		stored := getAppDeployment(t, ctx, client.ObjectKeyFromObject(appDeployment))
		degraded := meta.FindStatusCondition(stored.Status.Conditions, platformv1alpha1.ConditionDegraded)
		if stored.UID == winnerUID {
			if degraded != nil && degraded.Status == metav1.ConditionTrue &&
				degraded.Reason == platformv1alpha1.ReasonHostnameConflict {
				t.Fatalf("hostname winner %s must not report a hostname conflict", client.ObjectKeyFromObject(stored))
			}
			continue
		}
		if degraded == nil || degraded.Status != metav1.ConditionTrue ||
			degraded.Reason != platformv1alpha1.ReasonHostnameConflict {
			t.Fatalf("hostname loser %s must report HostnameConflict, got %#v",
				client.ObjectKeyFromObject(stored), degraded)
		}
	}

	// Reversing reconciliation order must not transfer the hostname to the other
	// claimant or leave both claimants without a route.
	for index := len(requests) - 1; index >= 0; index-- {
		if _, err := reconciler.Reconcile(ctx, requests[index]); err != nil {
			t.Fatalf("reconcile hostname claimant again %s: %v", requests[index].NamespacedName, err)
		}
	}
	winners = listWinners()
	if len(winners) != 1 {
		t.Fatalf("expected exactly one hostname winner after repeated reconciliation, got %d", len(winners))
	}
	stableOwner := metav1.GetControllerOf(&winners[0])
	if stableOwner == nil || stableOwner.UID != winnerUID {
		t.Fatalf("hostname ownership changed after repeated reconciliation: %#v", stableOwner)
	}
}

func TestReconcileConvergesDuplicateEstablishedHostnameRoutes(t *testing.T) {
	ctx := context.Background()
	firstNamespace := createTestNamespace(t, "hostname-established-first")
	secondNamespace := createTestNamespace(t, "hostname-established-second")
	reconciler := &AppDeploymentReconciler{Client: testClient, Scheme: testScheme}

	first := newAppDeployment(firstNamespace, "ap-establishedfirst", testImage)
	first.Spec.Exposure = platformv1alpha1.ExposurePublic
	first.Spec.Slug = "established-duplicate"
	second := newAppDeployment(secondNamespace, "ap-establishedsecond", testImage)
	second.Spec.Exposure = platformv1alpha1.ExposurePublic
	second.Spec.Slug = first.Spec.Slug
	for _, appDeployment := range []*platformv1alpha1.AppDeployment{first, second} {
		if err := testClient.Create(ctx, appDeployment); err != nil {
			t.Fatalf("create AppDeployment %s: %v", client.ObjectKeyFromObject(appDeployment), err)
		}
		stored := getAppDeployment(t, ctx, client.ObjectKeyFromObject(appDeployment))
		route := &gatewayv1.HTTPRoute{
			ObjectMeta: metav1.ObjectMeta{Name: stored.Name, Namespace: stored.Namespace},
			Spec: gatewayv1.HTTPRouteSpec{
				Hostnames: []gatewayv1.Hostname{gatewayv1.Hostname(publicHostname(stored.Spec.Slug))},
			},
		}
		if err := controllerutil.SetControllerReference(stored, route, testScheme); err != nil {
			t.Fatalf("set HTTPRoute owner %s: %v", client.ObjectKeyFromObject(stored), err)
		}
		if err := testClient.Create(ctx, route); err != nil {
			t.Fatalf("create established HTTPRoute %s: %v", client.ObjectKeyFromObject(route), err)
		}
	}

	for _, appDeployment := range []*platformv1alpha1.AppDeployment{second, first} {
		request := ctrl.Request{NamespacedName: client.ObjectKeyFromObject(appDeployment)}
		if _, err := reconciler.Reconcile(ctx, request); err != nil {
			t.Fatalf("reconcile duplicate hostname claimant %s: %v", request.NamespacedName, err)
		}
	}

	routes := &gatewayv1.HTTPRouteList{}
	if err := testClient.List(ctx, routes); err != nil {
		t.Fatalf("list HTTPRoutes: %v", err)
	}
	matching := 0
	for _, route := range routes.Items {
		if len(route.Spec.Hostnames) == 1 &&
			string(route.Spec.Hostnames[0]) == publicHostname(first.Spec.Slug) {
			matching++
		}
	}
	if matching != 1 {
		t.Fatalf("expected reconciliation to converge duplicate established claims to one HTTPRoute, got %d", matching)
	}
}

func TestReconcileCorrectsAndRecreatesPublicHTTPRoute(t *testing.T) {
	ctx := context.Background()
	namespace := createTestNamespace(t, "route-recovery")
	appDeployment := newAppDeployment(namespace, "ap-routerecovery", testImage)
	appDeployment.Spec.Exposure = platformv1alpha1.ExposurePublic
	appDeployment.Spec.Slug = "route-recovery"
	if err := testClient.Create(ctx, appDeployment); err != nil {
		t.Fatalf("create AppDeployment: %v", err)
	}

	reconciler := &AppDeploymentReconciler{Client: testClient, Scheme: testScheme}
	key := client.ObjectKeyFromObject(appDeployment)
	request := ctrl.Request{NamespacedName: key}
	if _, err := reconciler.Reconcile(ctx, request); err != nil {
		t.Fatalf("initial reconcile: %v", err)
	}

	route := getHTTPRoute(t, ctx, key)
	route.Spec.Hostnames = []gatewayv1.Hostname{"drift.molejo.dev"}
	if err := testClient.Update(ctx, route); err != nil {
		t.Fatalf("introduce HTTPRoute drift: %v", err)
	}
	if _, err := reconciler.Reconcile(ctx, request); err != nil {
		t.Fatalf("reconcile HTTPRoute drift: %v", err)
	}
	route = getHTTPRoute(t, ctx, key)
	if len(route.Spec.Hostnames) != 1 || string(route.Spec.Hostnames[0]) != publicHostname(appDeployment.Spec.Slug) {
		t.Fatalf("expected hostname drift to be corrected, got %#v", route.Spec.Hostnames)
	}

	previousUID := route.UID
	if err := testClient.Delete(ctx, route); err != nil {
		t.Fatalf("delete managed HTTPRoute: %v", err)
	}
	if _, err := reconciler.Reconcile(ctx, request); err != nil {
		t.Fatalf("reconcile deleted HTTPRoute: %v", err)
	}
	if route = getHTTPRoute(t, ctx, key); route.UID == previousUID {
		t.Fatal("expected deleted HTTPRoute to be recreated with a new UID")
	}
}

func TestEvaluatePublication(t *testing.T) {
	ready := workloadDecision{state: workloadStateReady, reason: platformv1alpha1.ReasonDeploymentAvailable}
	appDeployment := newAppDeployment("ws-test", "ap-publication", testImage)
	appDeployment.Spec.Exposure = platformv1alpha1.ExposurePublic
	appDeployment.Spec.Slug = "publication"
	staleRoute := routeWithConditions(
		metav1.Condition{Type: string(gatewayv1.RouteConditionAccepted), Status: metav1.ConditionTrue},
		metav1.Condition{Type: string(gatewayv1.RouteConditionResolvedRefs), Status: metav1.ConditionTrue},
	)
	staleRoute.Generation++
	wrongParentRoute := routeWithConditions(
		metav1.Condition{Type: string(gatewayv1.RouteConditionAccepted), Status: metav1.ConditionTrue},
		metav1.Condition{Type: string(gatewayv1.RouteConditionResolvedRefs), Status: metav1.ConditionTrue},
	)
	wrongParentRoute.Status.Parents[0].ParentRef.Namespace = nil
	wrongParentRoute.Status.Parents[0].ParentRef.SectionName = nil

	tests := []struct {
		name       string
		route      *gatewayv1.HTTPRoute
		wantState  workloadState
		wantReason string
	}{
		{name: "awaits a route", wantState: workloadStateProgressing, wantReason: platformv1alpha1.ReasonHTTPRouteProgressing},
		{
			name: "reports Gateway rejection",
			route: routeWithConditions(metav1.Condition{
				Type: string(gatewayv1.RouteConditionAccepted), Status: metav1.ConditionFalse,
			}),
			wantState: workloadStateDegraded, wantReason: platformv1alpha1.ReasonHTTPRouteRejected,
		},
		{
			name: "awaits resolved references",
			route: routeWithConditions(metav1.Condition{
				Type: string(gatewayv1.RouteConditionAccepted), Status: metav1.ConditionTrue,
			}),
			wantState: workloadStateProgressing, wantReason: platformv1alpha1.ReasonHTTPRouteProgressing,
		},
		{
			name: "reports invalid resolved references",
			route: routeWithConditions(
				metav1.Condition{Type: string(gatewayv1.RouteConditionAccepted), Status: metav1.ConditionTrue},
				metav1.Condition{
					Type: string(gatewayv1.RouteConditionResolvedRefs), Status: metav1.ConditionFalse,
					Reason: string(gatewayv1.RouteReasonBackendNotFound),
				},
			),
			wantState: workloadStateDegraded, wantReason: platformv1alpha1.ReasonHTTPRouteRejected,
		},
		{
			name:       "ignores conditions from an older route generation",
			route:      staleRoute,
			wantState:  workloadStateProgressing,
			wantReason: platformv1alpha1.ReasonHTTPRouteProgressing,
		},
		{
			name:       "ignores conditions from a different effective parent",
			route:      wrongParentRoute,
			wantState:  workloadStateProgressing,
			wantReason: platformv1alpha1.ReasonHTTPRouteProgressing,
		},
		{
			name: "preserves workload readiness after route convergence",
			route: routeWithConditions(
				metav1.Condition{Type: string(gatewayv1.RouteConditionAccepted), Status: metav1.ConditionTrue},
				metav1.Condition{Type: string(gatewayv1.RouteConditionResolvedRefs), Status: metav1.ConditionTrue},
			),
			wantState: workloadStateReady, wantReason: platformv1alpha1.ReasonDeploymentAvailable,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			decision := evaluatePublication(appDeployment, test.route, ready)
			if decision.state != test.wantState || decision.reason != test.wantReason {
				t.Fatalf("decision = %s/%s, want %s/%s", decision.state, decision.reason,
					test.wantState, test.wantReason)
			}
		})
	}
}

func TestEvaluatePublicationDoesNotMaskDegradedWorkload(t *testing.T) {
	appDeployment := newAppDeployment("ws-test", "ap-publication-degraded", testImage)
	appDeployment.Spec.Exposure = platformv1alpha1.ExposurePublic
	appDeployment.Spec.Slug = "publication-degraded"
	degraded := workloadDecision{
		state:  workloadStateDegraded,
		reason: platformv1alpha1.ReasonReplicaFailure,
	}
	pendingRoute := routeWithConditions(metav1.Condition{
		Type: string(gatewayv1.RouteConditionAccepted), Status: metav1.ConditionTrue,
	})
	rejectedRoute := routeWithConditions(metav1.Condition{
		Type: string(gatewayv1.RouteConditionAccepted), Status: metav1.ConditionFalse,
	})
	convergedRoute := routeWithConditions(
		metav1.Condition{Type: string(gatewayv1.RouteConditionAccepted), Status: metav1.ConditionTrue},
		metav1.Condition{Type: string(gatewayv1.RouteConditionResolvedRefs), Status: metav1.ConditionTrue},
	)

	for _, test := range []struct {
		name  string
		route *gatewayv1.HTTPRoute
	}{
		{name: "route does not exist yet"},
		{name: "route awaits resolved references", route: pendingRoute},
		{name: "route is rejected", route: rejectedRoute},
		{name: "route is converged", route: convergedRoute},
	} {
		t.Run(test.name, func(t *testing.T) {
			decision := evaluatePublication(appDeployment, test.route, degraded)
			if decision.state != workloadStateDegraded || decision.reason != platformv1alpha1.ReasonReplicaFailure {
				t.Fatalf("publication masked workload failure with %s/%s", decision.state, decision.reason)
			}
		})
	}
}

func TestEvaluatePublicationGatewayUsesFailureFirstPrecedence(t *testing.T) {
	ready := workloadDecision{
		state: workloadStateReady, reason: platformv1alpha1.ReasonDeploymentAvailable,
	}
	degraded := workloadDecision{
		state: workloadStateDegraded, reason: platformv1alpha1.ReasonReplicaFailure,
	}
	healthy := publicationGatewayWithConditions(
		metav1.Condition{Type: string(gatewayv1.GatewayConditionProgrammed), Status: metav1.ConditionTrue},
		metav1.Condition{Type: string(gatewayv1.ListenerConditionAccepted), Status: metav1.ConditionTrue},
		metav1.Condition{Type: string(gatewayv1.ListenerConditionProgrammed), Status: metav1.ConditionTrue},
		metav1.Condition{Type: string(gatewayv1.ListenerConditionResolvedRefs), Status: metav1.ConditionTrue},
	)
	pendingGatewayWithFailedCertificate := publicationGatewayWithConditions(
		metav1.Condition{Type: string(gatewayv1.GatewayConditionProgrammed), Status: metav1.ConditionUnknown},
		metav1.Condition{Type: string(gatewayv1.ListenerConditionAccepted), Status: metav1.ConditionTrue},
		metav1.Condition{Type: string(gatewayv1.ListenerConditionProgrammed), Status: metav1.ConditionFalse},
		metav1.Condition{
			Type: string(gatewayv1.ListenerConditionResolvedRefs), Status: metav1.ConditionFalse,
			Message: "secret private-certificate-detail is invalid",
		},
	)
	staleFailure := publicationGatewayWithConditions(
		metav1.Condition{
			Type: string(gatewayv1.GatewayConditionProgrammed), Status: metav1.ConditionFalse,
			ObservedGeneration: 2,
		},
		metav1.Condition{Type: string(gatewayv1.ListenerConditionAccepted), Status: metav1.ConditionTrue},
		metav1.Condition{Type: string(gatewayv1.ListenerConditionProgrammed), Status: metav1.ConditionTrue},
		metav1.Condition{Type: string(gatewayv1.ListenerConditionResolvedRefs), Status: metav1.ConditionTrue},
	)

	tests := []struct {
		name       string
		gateway    *gatewayv1.Gateway
		input      workloadDecision
		wantState  workloadState
		wantReason string
	}{
		{
			name: "missing Gateway is degraded", input: ready,
			wantState: workloadStateDegraded, wantReason: platformv1alpha1.ReasonGatewayRejected,
		},
		{
			name: "healthy Gateway preserves readiness", gateway: healthy, input: ready,
			wantState: workloadStateReady, wantReason: platformv1alpha1.ReasonDeploymentAvailable,
		},
		{
			name: "current listener failure wins over pending Gateway", gateway: pendingGatewayWithFailedCertificate,
			input: ready, wantState: workloadStateDegraded, wantReason: platformv1alpha1.ReasonGatewayRejected,
		},
		{
			name: "stale Gateway failure is progressing", gateway: staleFailure, input: ready,
			wantState: workloadStateProgressing, wantReason: platformv1alpha1.ReasonGatewayProgressing,
		},
		{
			name: "Gateway cannot mask workload degradation", gateway: healthy, input: degraded,
			wantState: workloadStateDegraded, wantReason: platformv1alpha1.ReasonReplicaFailure,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			decision := evaluatePublicationGateway(test.gateway, test.input)
			if decision.state != test.wantState || decision.reason != test.wantReason {
				t.Fatalf("decision = %s/%s, want %s/%s", decision.state, decision.reason,
					test.wantState, test.wantReason)
			}
			if strings.Contains(decision.message, "private-certificate-detail") {
				t.Fatalf("public decision leaked Gateway details: %q", decision.message)
			}
		})
	}
}

func TestEvaluatePublicationTreatsMultipleControllersAsAmbiguous(t *testing.T) {
	appDeployment := newAppDeployment("ws-test", "ap-publication-ambiguous", testImage)
	appDeployment.Spec.Exposure = platformv1alpha1.ExposurePublic
	appDeployment.Spec.Slug = "publication-ambiguous"
	ready := workloadDecision{
		state:  workloadStateReady,
		reason: platformv1alpha1.ReasonDeploymentAvailable,
	}
	route := routeWithConditions(
		metav1.Condition{Type: string(gatewayv1.RouteConditionAccepted), Status: metav1.ConditionTrue},
		metav1.Condition{Type: string(gatewayv1.RouteConditionResolvedRefs), Status: metav1.ConditionTrue},
	)
	conflictingParent := route.Status.Parents[0].DeepCopy()
	conflictingParent.ControllerName = "other.test/gateway-controller"
	conflictingParent.Conditions = []metav1.Condition{{
		Type:               string(gatewayv1.RouteConditionAccepted),
		Status:             metav1.ConditionFalse,
		ObservedGeneration: route.Generation,
	}}
	route.Status.Parents = append(route.Status.Parents, *conflictingParent)

	decision := evaluatePublication(appDeployment, route, ready)
	if decision.state != workloadStateProgressing ||
		decision.reason != platformv1alpha1.ReasonHTTPRouteProgressing {
		t.Fatalf("expected ambiguous Gateway writers to remain Progressing, got %s/%s",
			decision.state, decision.reason)
	}
}

func routeWithConditions(conditions ...metav1.Condition) *gatewayv1.HTTPRoute {
	const routeGeneration int64 = 1
	for index := range conditions {
		if conditions[index].ObservedGeneration == 0 {
			conditions[index].ObservedGeneration = routeGeneration
		}
		if conditions[index].Reason == "" {
			conditions[index].Reason = "TestCondition"
		}
		if conditions[index].LastTransitionTime.IsZero() {
			conditions[index].LastTransitionTime = metav1.Now()
		}
	}
	gatewayNamespace := gatewayv1.Namespace("molejo-system")
	httpsSection := gatewayv1.SectionName("https-molejo")
	return &gatewayv1.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{Generation: routeGeneration},
		Status: gatewayv1.HTTPRouteStatus{RouteStatus: gatewayv1.RouteStatus{
			Parents: []gatewayv1.RouteParentStatus{{
				ParentRef: gatewayv1.ParentReference{
					Name: "molejo", Namespace: &gatewayNamespace, SectionName: &httpsSection,
				},
				ControllerName: "molejo.test/gateway-controller",
				Conditions:     conditions,
			}},
		}},
	}
}

func publicationGatewayWithConditions(
	programmed metav1.Condition,
	listenerConditions ...metav1.Condition,
) *gatewayv1.Gateway {
	const generation int64 = 1
	if programmed.ObservedGeneration == 0 {
		programmed.ObservedGeneration = generation
	}
	if programmed.Reason == "" {
		programmed.Reason = "TestCondition"
	}
	if programmed.LastTransitionTime.IsZero() {
		programmed.LastTransitionTime = metav1.Now()
	}
	for index := range listenerConditions {
		if listenerConditions[index].ObservedGeneration == 0 {
			listenerConditions[index].ObservedGeneration = generation
		}
		if listenerConditions[index].Reason == "" {
			listenerConditions[index].Reason = "TestCondition"
		}
		if listenerConditions[index].LastTransitionTime.IsZero() {
			listenerConditions[index].LastTransitionTime = metav1.Now()
		}
	}
	return &gatewayv1.Gateway{
		ObjectMeta: metav1.ObjectMeta{Generation: generation},
		Status: gatewayv1.GatewayStatus{
			Conditions: []metav1.Condition{programmed},
			Listeners: []gatewayv1.ListenerStatus{{
				Name: sharedGatewaySection, Conditions: listenerConditions,
			}},
		},
	}
}

func getHTTPRoute(t *testing.T, ctx context.Context, key client.ObjectKey) *gatewayv1.HTTPRoute {
	t.Helper()
	route := &gatewayv1.HTTPRoute{}
	if err := testClient.Get(ctx, key, route); err != nil {
		t.Fatalf("get HTTPRoute %s: %v", key, err)
	}
	return route
}

func setTestGatewayStatus(
	t *testing.T,
	ctx context.Context,
	gateway *gatewayv1.Gateway,
	healthy bool,
) {
	t.Helper()
	if err := testClient.Get(ctx, client.ObjectKeyFromObject(gateway), gateway); err != nil {
		t.Fatalf("get shared Gateway: %v", err)
	}
	conditionStatus := metav1.ConditionTrue
	programmedReason := string(gatewayv1.GatewayReasonProgrammed)
	listenerReason := string(gatewayv1.ListenerReasonProgrammed)
	message := "The shared Gateway is programmed."
	if !healthy {
		conditionStatus = metav1.ConditionFalse
		programmedReason = string(gatewayv1.GatewayReasonListenersNotReady)
		listenerReason = string(gatewayv1.ListenerReasonInvalidCertificateRef)
		message = "certificate secret private-certificate-detail is invalid"
	}
	gateway.Status.Conditions = []metav1.Condition{{
		Type:               string(gatewayv1.GatewayConditionProgrammed),
		Status:             conditionStatus,
		ObservedGeneration: gateway.Generation,
		Reason:             programmedReason,
		Message:            message,
		LastTransitionTime: metav1.Now(),
	}}
	gateway.Status.Listeners = []gatewayv1.ListenerStatus{{
		Name: "https-molejo",
		Conditions: []metav1.Condition{
			{
				Type: string(gatewayv1.ListenerConditionAccepted), Status: conditionStatus,
				ObservedGeneration: gateway.Generation, Reason: listenerReason,
				Message: message, LastTransitionTime: metav1.Now(),
			},
			{
				Type: string(gatewayv1.ListenerConditionProgrammed), Status: conditionStatus,
				ObservedGeneration: gateway.Generation, Reason: listenerReason,
				Message: message, LastTransitionTime: metav1.Now(),
			},
			{
				Type: string(gatewayv1.ListenerConditionResolvedRefs), Status: conditionStatus,
				ObservedGeneration: gateway.Generation, Reason: listenerReason,
				Message: message, LastTransitionTime: metav1.Now(),
			},
		},
	}}
	if err := testClient.Status().Update(ctx, gateway); err != nil {
		t.Fatalf("update shared Gateway status: %v", err)
	}
}

func waitForCondition(t *testing.T, timeout time.Duration, description string, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", description)
}

type gatewayDefaultingClient struct {
	client.Client
	patchCalls int
}

func (defaulting *gatewayDefaultingClient) Create(
	ctx context.Context,
	object client.Object,
	options ...client.CreateOption,
) error {
	applyGatewayAPIDefaults(object)
	return defaulting.Client.Create(ctx, object, options...)
}

func (defaulting *gatewayDefaultingClient) Patch(
	ctx context.Context,
	object client.Object,
	patch client.Patch,
	options ...client.PatchOption,
) error {
	if _, ok := object.(*gatewayv1.HTTPRoute); ok {
		defaulting.patchCalls++
		applyGatewayAPIDefaults(object)
	}
	return defaulting.Client.Patch(ctx, object, patch, options...)
}

func applyGatewayAPIDefaults(object client.Object) {
	route, ok := object.(*gatewayv1.HTTPRoute)
	if !ok || len(route.Spec.ParentRefs) == 0 || len(route.Spec.Rules) == 0 ||
		len(route.Spec.Rules[0].BackendRefs) == 0 {
		return
	}
	group := gatewayv1.Group(gatewayv1.GroupName)
	kind := gatewayv1.Kind("Gateway")
	route.Spec.ParentRefs[0].Group = &group
	route.Spec.ParentRefs[0].Kind = &kind
	if len(route.Spec.Rules[0].Matches) == 0 {
		pathType := gatewayv1.PathMatchPathPrefix
		pathValue := "/"
		route.Spec.Rules[0].Matches = []gatewayv1.HTTPRouteMatch{{Path: &gatewayv1.HTTPPathMatch{
			Type: &pathType, Value: &pathValue,
		}}}
	}
	weight := int32(1)
	route.Spec.Rules[0].BackendRefs[0].Weight = &weight
}

func TestPublicEndpointHostnamePrefersControlPlaneResolution(t *testing.T) {
	endpoint := platformv1alpha1.AppDeploymentPublicEndpoint{HostnameLabel: "pg", Hostname: "pg.stateful.molejo.dev"}
	if got := publicEndpointHostname(endpoint); got != "pg.stateful.molejo.dev" {
		t.Fatalf("hostname=%q", got)
	}
}
