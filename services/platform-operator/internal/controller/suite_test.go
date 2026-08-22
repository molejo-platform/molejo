package controller

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"

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

	crdDirectory, err := filepath.Abs("../../../../deploy/crds")
	if err != nil {
		fmt.Fprintf(os.Stderr, "resolve CRD directory: %v\n", err)
		os.Exit(1)
	}
	testEnvironment = &envtest.Environment{
		CRDDirectoryPaths:     []string{crdDirectory},
		ErrorIfCRDPathMissing: true,
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
