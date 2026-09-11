package kubernetespublication

import (
	"context"
	"fmt"
	"testing"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
)

type restrictedReader struct {
	client.Reader
	reads     []string
	denyClass bool
}

func (r *restrictedReader) Get(ctx context.Context, key client.ObjectKey, obj client.Object, options ...client.GetOption) error {
	r.reads = append(r.reads, fmt.Sprintf("%T", obj))
	switch obj.(type) {
	case *gatewayv1.Gateway, *corev1.Namespace:
	case *gatewayv1.GatewayClass:
		if r.denyClass {
			return apierrors.NewForbidden(schema.GroupResource{Group: gatewayv1.GroupName, Resource: "gatewayclasses"}, key.Name, nil)
		}
	default:
		return fmt.Errorf("unexpected read %T", obj)
	}
	return r.Reader.Get(ctx, key, obj, options...)
}

func TestInspectionPreservesFactsWhenOneReadIsDenied(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := gatewayv1.Install(scheme); err != nil {
		t.Fatal(err)
	}
	hostname := gatewayv1.Hostname("*.example.test")
	from := gatewayv1.NamespacesFromSelector
	gateway := &gatewayv1.Gateway{ObjectMeta: metav1.ObjectMeta{Name: "external", Namespace: "edge", UID: "gateway-uid", Generation: 2}, Spec: gatewayv1.GatewaySpec{GatewayClassName: "external-class", Listeners: []gatewayv1.Listener{{Name: "https", Hostname: &hostname, Protocol: gatewayv1.HTTPSProtocolType, TLS: &gatewayv1.ListenerTLSConfig{}, AllowedRoutes: &gatewayv1.AllowedRoutes{Namespaces: &gatewayv1.RouteNamespaces{From: &from, Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"allowed": "true"}}}}}}}, Status: gatewayv1.GatewayStatus{Conditions: []metav1.Condition{{Type: "Programmed", Status: metav1.ConditionTrue, ObservedGeneration: 2, Reason: "Programmed"}}}}
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "workspace", Labels: map[string]string{"allowed": "true"}}}
	reader := &restrictedReader{Reader: fake.NewClientBuilder().WithScheme(scheme).WithObjects(gateway, ns).Build(), denyClass: true}
	target := Target{GatewayNamespace: "edge", GatewayName: "external", SectionName: "https", Hostname: "app.example.test", ConsumerNamespace: "workspace"}
	facts, err := Inspect(t.Context(), reader, target)
	if !apierrors.IsForbidden(err) || len(reader.reads) != 3 {
		t.Fatalf("expected bounded reads and denied class: %v %v", reader.reads, err)
	}
	for _, tc := range []struct {
		kind   string
		status metav1.ConditionStatus
	}{{"GatewayProgrammed", metav1.ConditionTrue}, {"GatewayClassAccepted", metav1.ConditionUnknown}, {"AttachmentAllowed", metav1.ConditionTrue}, {"ServedTLSVerified", metav1.ConditionUnknown}} {
		c := meta.FindStatusCondition(facts.Conditions, tc.kind)
		if c == nil || c.Status != tc.status {
			t.Fatalf("%s: %+v", tc.kind, c)
		}
	}
	gateway.Generation = 3
	facts = Evaluate(gateway, nil, ns, "https", "app.example.test")
	if c := meta.FindStatusCondition(facts.Conditions, "GatewayProgrammed"); c.Status != metav1.ConditionUnknown {
		t.Fatal("stale generation accepted")
	}
	ns.Labels = nil
	facts = Evaluate(gateway, nil, ns, "https", "example.test")
	for _, kind := range []string{"AttachmentAllowed", "ListenerCompatible"} {
		if c := meta.FindStatusCondition(facts.Conditions, kind); c.Status != metav1.ConditionFalse {
			t.Fatalf("%s did not reject", kind)
		}
	}
}
