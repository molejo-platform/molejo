package controller

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	platformv1alpha1 "github.com/fruto-platform/fruto/packages/kubernetes-api/apis/platform/v1alpha1"
)

var (
	testClient      client.Client
	testConfig      *rest.Config
	testEnvironment *envtest.Environment
	testScheme      = runtime.NewScheme()
)

func TestMain(m *testing.M) {
	if err := corev1.AddToScheme(testScheme); err != nil {
		fmt.Fprintf(os.Stderr, "add core API to test scheme: %v\n", err)
		os.Exit(1)
	}
	if err := appsv1.AddToScheme(testScheme); err != nil {
		fmt.Fprintf(os.Stderr, "add apps API to test scheme: %v\n", err)
		os.Exit(1)
	}
	if err := platformv1alpha1.AddToScheme(testScheme); err != nil {
		fmt.Fprintf(os.Stderr, "add platform API to test scheme: %v\n", err)
		os.Exit(1)
	}
	if err := gatewayv1.Install(testScheme); err != nil {
		fmt.Fprintf(os.Stderr, "add Gateway API to test scheme: %v\n", err)
		os.Exit(1)
	}

	crdDirectory, err := filepath.Abs("../../../../deploy/crds")
	if err != nil {
		fmt.Fprintf(os.Stderr, "resolve CRD directory: %v\n", err)
		os.Exit(1)
	}
	testEnvironment = &envtest.Environment{
		CRDDirectoryPaths:     []string{crdDirectory},
		ErrorIfCRDPathMissing: true,
		CRDs: []*apiextensionsv1.CustomResourceDefinition{
			testHTTPRouteCRD(),
			testGatewayCRD(),
		},
	}
	testConfig, err = testEnvironment.Start()
	if err != nil {
		fmt.Fprintf(os.Stderr, "start envtest: %v\n", err)
		os.Exit(1)
	}
	testClient, err = client.New(testConfig, client.Options{Scheme: testScheme})
	if err != nil {
		fmt.Fprintf(os.Stderr, "create test client: %v\n", err)
		_ = testEnvironment.Stop()
		os.Exit(1)
	}

	exitCode := m.Run()
	if err := testEnvironment.Stop(); err != nil {
		fmt.Fprintf(os.Stderr, "stop envtest: %v\n", err)
		if exitCode == 0 {
			exitCode = 1
		}
	}
	os.Exit(exitCode)
}

func testGatewayCRD() *apiextensionsv1.CustomResourceDefinition {
	preserveUnknownFields := true
	return &apiextensionsv1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{
			Name: "gateways.gateway.networking.k8s.io",
			Annotations: map[string]string{
				"api-approved.kubernetes.io": "https://github.com/kubernetes-sigs/gateway-api/pull/2466",
			},
		},
		Spec: apiextensionsv1.CustomResourceDefinitionSpec{
			Group: "gateway.networking.k8s.io",
			Names: apiextensionsv1.CustomResourceDefinitionNames{
				Plural: "gateways", Singular: "gateway", Kind: "Gateway", ListKind: "GatewayList",
			},
			Scope: apiextensionsv1.NamespaceScoped,
			Versions: []apiextensionsv1.CustomResourceDefinitionVersion{{
				Name: "v1", Served: true, Storage: true,
				Schema: &apiextensionsv1.CustomResourceValidation{OpenAPIV3Schema: &apiextensionsv1.JSONSchemaProps{
					Type: "object", XPreserveUnknownFields: &preserveUnknownFields,
				}},
				Subresources: &apiextensionsv1.CustomResourceSubresources{
					Status: &apiextensionsv1.CustomResourceSubresourceStatus{},
				},
			}},
		},
	}
}

func testHTTPRouteCRD() *apiextensionsv1.CustomResourceDefinition {
	preserveUnknownFields := true
	return &apiextensionsv1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{
			Name: "httproutes.gateway.networking.k8s.io",
			Annotations: map[string]string{
				"api-approved.kubernetes.io": "https://github.com/kubernetes-sigs/gateway-api/pull/2466",
			},
		},
		Spec: apiextensionsv1.CustomResourceDefinitionSpec{
			Group: "gateway.networking.k8s.io",
			Names: apiextensionsv1.CustomResourceDefinitionNames{
				Plural: "httproutes", Singular: "httproute", Kind: "HTTPRoute", ListKind: "HTTPRouteList",
			},
			Scope: apiextensionsv1.NamespaceScoped,
			Versions: []apiextensionsv1.CustomResourceDefinitionVersion{{
				Name: "v1", Served: true, Storage: true,
				Schema: &apiextensionsv1.CustomResourceValidation{OpenAPIV3Schema: &apiextensionsv1.JSONSchemaProps{
					Type: "object", XPreserveUnknownFields: &preserveUnknownFields,
				}},
				Subresources: &apiextensionsv1.CustomResourceSubresources{
					Status: &apiextensionsv1.CustomResourceSubresourceStatus{},
				},
			}},
		},
	}
}
