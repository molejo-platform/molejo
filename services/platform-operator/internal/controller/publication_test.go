package controller

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	platformv1alpha1 "github.com/molejo-platform/molejo/packages/kubernetes-api/apis/platform/v1alpha1"
)

func testHTTPAddress(hostname string) platformv1alpha1.AppDeploymentHTTPAddress {
	return platformv1alpha1.AppDeploymentHTTPAddress{Hostname: hostname, Destination: platformv1alpha1.HTTPDestination{BindingID: "binding-one", BindingRevision: 1, SchemaVersion: "kubernetes-http.v1alpha1", GatewayNamespace: "molejo-system", GatewayName: "molejo", SectionName: "https-molejo"}}
}

func testHTTPEndpoint(names ...string) platformv1alpha1.AppDeploymentPublicEndpoint {
	e := platformv1alpha1.AppDeploymentPublicEndpoint{Name: "web", Type: "HTTP", PortName: httpPortName}
	for _, name := range names {
		e.Addresses = append(e.Addresses, testHTTPAddress(name))
	}
	return e
}

func TestHTTPAssociationsReconcileIndependently(t *testing.T) {
	ctx := t.Context()
	namespace := createTestNamespace(t, "multi-address")
	app := newAppDeployment(namespace, "ap-multiaddress", testImage)
	endpoint := testHTTPEndpoint("example.test", "website.example.test")
	endpoint.Addresses[0].Destination.SectionName = "https-apex"
	app.Spec.PublicEndpoints = []platformv1alpha1.AppDeploymentPublicEndpoint{endpoint}
	if err := testClient.Create(ctx, app); err != nil {
		t.Fatal(err)
	}
	r := &AppDeploymentReconciler{Client: testClient, Scheme: testScheme}
	key := client.ObjectKeyFromObject(app)
	reconcile := func() {
		t.Helper()
		if _, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: key}); err != nil {
			t.Fatal(err)
		}
	}
	reconcile()
	first := getHTTPRoute(t, ctx, client.ObjectKey{Namespace: namespace, Name: httpRouteName(app, "web", "example.test")})
	second := getHTTPRoute(t, ctx, client.ObjectKey{Namespace: namespace, Name: httpRouteName(app, "web", "website.example.test")})
	if first.Spec.ParentRefs[0].SectionName == nil || *first.Spec.ParentRefs[0].SectionName != "https-apex" || *second.Spec.ParentRefs[0].SectionName != "https-molejo" {
		t.Fatal("destination was not preserved")
	}
	for _, route := range []*gatewayv1.HTTPRoute{first, second} {
		if !metav1.IsControlledBy(route, app) || route.Spec.Rules[0].BackendRefs[0].Name != gatewayv1.ObjectName(app.Name) || *route.Spec.Rules[0].BackendRefs[0].Port != 8080 {
			t.Fatal("ownership or shared backend mismatch")
		}
	}
	reconcile()
	if got := getHTTPRoute(t, ctx, client.ObjectKeyFromObject(second)); got.ResourceVersion != second.ResourceVersion {
		t.Fatal("idempotent reconciliation patched route")
	}
	stored := getAppDeployment(t, ctx, key)
	if len(stored.Status.EndpointStatuses) != 1 || len(stored.Status.EndpointStatuses[0].Addresses) != 2 {
		t.Fatal("address observations missing")
	}
	for _, a := range stored.Status.EndpointStatuses[0].Addresses {
		if c := meta.FindStatusCondition(a.Conditions, "ServedTLSVerified"); c == nil || c.Status != metav1.ConditionUnknown {
			t.Fatal("runtime claimed TLS verification")
		}
	}
	// Gateway events target the configured Gateway, including external names.
	events := r.mapGatewayToAppDeployments(ctx, &gatewayv1.Gateway{ObjectMeta: metav1.ObjectMeta{Name: "molejo", Namespace: "molejo-system"}})
	matched := false
	for _, event := range events {
		if event.NamespacedName == key {
			matched = true
		}
	}
	if !matched {
		t.Fatal("Gateway watch did not find its consumer")
	}
	if events := r.mapGatewayToAppDeployments(ctx, &gatewayv1.Gateway{ObjectMeta: metav1.ObjectMeta{Name: "unrelated", Namespace: "elsewhere"}}); len(events) != 0 {
		t.Fatal("unrelated Gateway selected consumers")
	}
	// A removed index label does not conceal an owned route. A finalizer must
	// keep withdrawal pending until the route is actually gone.
	first.Labels = nil
	first.Finalizers = []string{"test.molejo.dev/hold"}
	if err := testClient.Update(ctx, first); err != nil {
		t.Fatal(err)
	}
	stored.Spec.PublicEndpoints[0].Addresses = stored.Spec.PublicEndpoints[0].Addresses[1:]
	if err := testClient.Update(ctx, stored); err != nil {
		t.Fatal(err)
	}
	reconcile()
	blocked := getHTTPRoute(t, ctx, client.ObjectKeyFromObject(first))
	if blocked.DeletionTimestamp.IsZero() {
		t.Fatal("owned route was not withdrawn")
	}
	if pending, err := r.withdrawHTTPAddresses(ctx, stored, map[string]bool{second.Name: true}); err != nil || !pending {
		t.Fatalf("finalizer did not keep withdrawal pending: %v %v", pending, err)
	}
	blocked.Finalizers = nil
	if err := testClient.Update(ctx, blocked); err != nil {
		t.Fatal(err)
	}
	reconcile()
	if err := testClient.Get(ctx, client.ObjectKeyFromObject(first), &gatewayv1.HTTPRoute{}); !apierrors.IsNotFound(err) {
		t.Fatalf("removed route remains: %v", err)
	}
	if got := getHTTPRoute(t, ctx, client.ObjectKeyFromObject(second)); got.UID != second.UID || got.ResourceVersion != second.ResourceVersion {
		t.Fatal("remaining association changed")
	}
	// A foreign replacement at the deterministic name is never adopted.
	if err := testClient.Delete(ctx, second); err != nil {
		t.Fatal(err)
	}
	foreign := second.DeepCopy()
	foreign.UID = ""
	foreign.ResourceVersion = ""
	foreign.OwnerReferences = nil
	if err := testClient.Create(ctx, foreign); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := r.applyHTTPAddress(ctx, stored, stored.Spec.PublicEndpoints[0], stored.Spec.PublicEndpoints[0].Addresses[0]); !errors.Is(err, errOwnershipConflict) {
		t.Fatalf("foreign route not rejected: %v", err)
	}
}

func TestHTTPAssociationSchemaRejectsInvalidContracts(t *testing.T) {
	ns := createTestNamespace(t, "address-schema")
	cases := []struct {
		name   string
		change func(*platformv1alpha1.AppDeployment)
	}{
		{"duplicate", func(a *platformv1alpha1.AppDeployment) {
			a.Spec.PublicEndpoints[0].Addresses = append(a.Spec.PublicEndpoints[0].Addresses, a.Spec.PublicEndpoints[0].Addresses[0])
		}},
		{"wildcard", func(a *platformv1alpha1.AppDeployment) {
			a.Spec.PublicEndpoints[0].Addresses[0].Hostname = "*.example.test"
		}},
		{"too-many", func(a *platformv1alpha1.AppDeployment) {
			for i := 0; i < 10; i++ {
				a.Spec.PublicEndpoints[0].Addresses = append(a.Spec.PublicEndpoints[0].Addresses, testHTTPAddress(fmt.Sprintf("a%d.example.test", i)))
			}
		}},
		{"no-destination", func(a *platformv1alpha1.AppDeployment) {
			a.Spec.PublicEndpoints[0].Addresses[0].Destination.BindingID = ""
		}},
		{"unknown-version", func(a *platformv1alpha1.AppDeployment) {
			a.Spec.PublicEndpoints[0].Addresses[0].Destination.SchemaVersion = "future"
		}},
		{"legacy-hostname", func(a *platformv1alpha1.AppDeployment) { a.Spec.PublicEndpoints[0].Hostname = "example.test" }},
		{"multiple-http", func(a *platformv1alpha1.AppDeployment) {
			e := testHTTPEndpoint("another.test")
			e.Name = "other"
			a.Spec.PublicEndpoints = append(a.Spec.PublicEndpoints, e)
		}},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := newAppDeployment(ns, fmt.Sprintf("ap-invalid%d", i), testImage)
			a.Spec.PublicEndpoints = []platformv1alpha1.AppDeploymentPublicEndpoint{testHTTPEndpoint("example.test")}
			tc.change(a)
			if err := testClient.Create(t.Context(), a); err == nil {
				t.Fatal("schema accepted invalid contract")
			}
		})
	}
}

func TestEvaluatePublication(t *testing.T) {
	ready := workloadDecision{state: workloadStateReady, reason: platformv1alpha1.ReasonDeploymentAvailable}
	appDeployment := newAppDeployment("ws-test", "ap-publication", testImage)
	appDeployment.Spec.PublicEndpoints = []platformv1alpha1.AppDeploymentPublicEndpoint{testHTTPEndpoint("publication.molejo.dev")}
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
	appDeployment.Spec.PublicEndpoints = []platformv1alpha1.AppDeploymentPublicEndpoint{testHTTPEndpoint("publication-degraded.molejo.dev")}
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
			decision := evaluatePublicationGateway(test.gateway, test.input, sharedGatewaySection)
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
	appDeployment.Spec.PublicEndpoints = []platformv1alpha1.AppDeploymentPublicEndpoint{testHTTPEndpoint("publication-ambiguous.molejo.dev")}
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
		Spec:       gatewayv1.HTTPRouteSpec{CommonRouteSpec: gatewayv1.CommonRouteSpec{ParentRefs: []gatewayv1.ParentReference{{Name: "molejo", Namespace: &gatewayNamespace, SectionName: &httpsSection}}}},
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

func TestAddressFactsRemainIndependentAndFailureWinsAggregation(t *testing.T) {
	app := newAppDeployment("workspace", "ap-facts", testImage)
	app.Generation = 2
	rejected := workloadDecision{state: workloadStateDegraded, reason: "HTTPRouteRejected"}
	ready := workloadDecision{state: workloadStateReady}
	pending := workloadDecision{state: workloadStateProgressing, reason: "Pending"}
	conditions := addressConditions(app, testHTTPAddress("example.test"), rejected, ready)
	if meta.FindStatusCondition(conditions, "RouteReady").Status != metav1.ConditionFalse || meta.FindStatusCondition(conditions, "GatewayReady").Status != metav1.ConditionTrue {
		t.Fatal("independent Gateway evidence was lost")
	}
	for _, values := range [][]workloadDecision{{ready, rejected, pending}, {pending, ready, rejected}, {rejected, pending, ready}} {
		result := ready
		for _, value := range values {
			result = combinePublicationDecision(result, value)
		}
		if result.state != workloadStateDegraded || result.reason != "HTTPRouteRejected" {
			t.Fatalf("failure masked by list order: %+v", result)
		}
	}
}
