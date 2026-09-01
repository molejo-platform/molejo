package controller

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
	"sigs.k8s.io/yaml"

	platformv1alpha1 "github.com/molejo-platform/molejo/packages/kubernetes-api/apis/platform/v1alpha1"
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
	if err := gatewayv1alpha2.Install(testScheme); err != nil {
		fmt.Fprintf(os.Stderr, "add experimental Gateway API to test scheme: %v\n", err)
		os.Exit(1)
	}

	crdDirectory, err := filepath.Abs("../../../../deploy/crds")
	if err != nil {
		fmt.Fprintf(os.Stderr, "resolve CRD directory: %v\n", err)
		os.Exit(1)
	}
	gatewayCRDs, err := gatewayAPICRDs()
	if err != nil {
		fmt.Fprintf(os.Stderr, "load pinned Gateway API CRDs: %v\n", err)
		os.Exit(1)
	}
	testEnvironment = &envtest.Environment{
		CRDDirectoryPaths:     []string{crdDirectory},
		ErrorIfCRDPathMissing: true,
		CRDs:                  gatewayCRDs,
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

func gatewayAPICRDs() ([]*apiextensionsv1.CustomResourceDefinition, error) {
	moduleDirectory, err := exec.Command("go", "list", "-m", "-f={{.Dir}}", "sigs.k8s.io/gateway-api").Output()
	if err != nil {
		return nil, err
	}
	crdDirectory := filepath.Join(strings.TrimSpace(string(moduleDirectory)), "config", "crd", "experimental")
	files := []string{
		"gateway.networking.k8s.io_gateways.yaml",
		"gateway.networking.k8s.io_httproutes.yaml",
		"gateway.networking.k8s.io_tcproutes.yaml",
	}
	crds := make([]*apiextensionsv1.CustomResourceDefinition, 0, len(files))
	for _, name := range files {
		contents, readErr := os.ReadFile(filepath.Join(crdDirectory, name))
		if readErr != nil {
			return nil, readErr
		}
		crd := &apiextensionsv1.CustomResourceDefinition{}
		if unmarshalErr := yaml.Unmarshal(contents, crd); unmarshalErr != nil {
			return nil, unmarshalErr
		}
		crds = append(crds, crd)
	}
	return crds, nil
}
