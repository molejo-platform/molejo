package kubernetespublication

import (
	"context"
	"fmt"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/molejo-platform/molejo/apps/molejoctl/internal/capability/publication"
	platformv1alpha1 "github.com/molejo-platform/molejo/packages/kubernetes-api/apis/platform/v1alpha1"
	"github.com/molejo-platform/molejo/packages/kubernetesbinding"
)

type recordingReader struct {
	client.Reader
	types []string
}

func (r *recordingReader) Get(ctx context.Context, key client.ObjectKey, object client.Object, options ...client.GetOption) error {
	r.types = append(r.types, fmt.Sprintf("%T", object))
	return r.Reader.Get(ctx, key, object, options...)
}

func TestObserverUsesReadOnlyFactsWithoutSecretAccess(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = gatewayv1.Install(scheme)
	_ = platformv1alpha1.AddToScheme(scheme)
	hostname := gatewayv1.Hostname("example.test")
	all := gatewayv1.NamespacesFromAll
	terminate := gatewayv1.TLSModeTerminate
	objects := []client.Object{
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "kube-system", UID: "cluster-uid"}},
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "molejo-system"}},
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "workspace-one"}},
		&platformv1alpha1.WorkspacePlacement{ObjectMeta: metav1.ObjectMeta{Name: "ws-abcdefghijklmnopqrst"}, Spec: platformv1alpha1.WorkspacePlacementSpec{WorkspaceID: "ws-abcdefghijklmnopqrst", NamespaceName: "workspace-one"}},
		&gatewayv1.GatewayClass{ObjectMeta: metav1.ObjectMeta{Name: "external", Generation: 1}, Status: gatewayv1.GatewayClassStatus{Conditions: []metav1.Condition{{Type: "Accepted", Status: metav1.ConditionTrue, ObservedGeneration: 1}}}},
		&gatewayv1.Gateway{ObjectMeta: metav1.ObjectMeta{Name: "molejo", Namespace: "molejo-system", UID: "gateway-uid", Generation: 1}, Spec: gatewayv1.GatewaySpec{GatewayClassName: "external", Listeners: []gatewayv1.Listener{{Name: "https-apex", Hostname: &hostname, Protocol: gatewayv1.HTTPSProtocolType, TLS: &gatewayv1.ListenerTLSConfig{Mode: &terminate}, AllowedRoutes: &gatewayv1.AllowedRoutes{Namespaces: &gatewayv1.RouteNamespaces{From: &all}}}}}, Status: gatewayv1.GatewayStatus{Conditions: []metav1.Condition{{Type: "Programmed", Status: metav1.ConditionTrue, ObservedGeneration: 1}}, Listeners: []gatewayv1.ListenerStatus{{Name: "https-apex", SupportedKinds: []gatewayv1.RouteGroupKind{{Kind: "HTTPRoute"}}, Conditions: []metav1.Condition{{Type: "Accepted", Status: metav1.ConditionTrue, ObservedGeneration: 1}, {Type: "Programmed", Status: metav1.ConditionTrue, ObservedGeneration: 1}, {Type: "ResolvedRefs", Status: metav1.ConditionTrue, ObservedGeneration: 1}}}}}},
	}
	reader := &recordingReader{Reader: fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()}
	observer := &Observer{reader: reader}
	setup := publication.Setup{Spec: publication.SetupSpec{Binding: publication.BindingSpec{SchemaVersion: kubernetesbinding.HTTPBindingSchemaVersion, GatewayNamespace: "molejo-system", GatewayName: "molejo", Listeners: []kubernetesbinding.HTTPListener{{Name: "https-apex", Hostname: "example.test"}}}, Domains: []publication.DomainSpec{{ID: "home", Kind: "Exact", Name: "example.test", WorkspaceIDs: []string{"ws-abcdefghijklmnopqrst"}}}}}
	report, err := observer.Inspect(t.Context(), setup)
	if err != nil || len(report.Diagnostics) != 0 || report.ClusterUID != "cluster-uid" {
		t.Fatalf("inspection failed: %+v %v", report, err)
	}
	for _, objectType := range reader.types {
		if objectType == "*v1.Secret" {
			t.Fatalf("observer read Secret: %v", reader.types)
		}
	}
}
