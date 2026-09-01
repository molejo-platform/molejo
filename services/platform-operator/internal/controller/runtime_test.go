package controller

import (
	"context"
	"testing"

	platformv1alpha1 "github.com/molejo-platform/molejo/packages/kubernetes-api/apis/platform/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	gatewayv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
)

func TestDesiredResourceRequirements(t *testing.T) {
	appDeployment := newAppDeployment("ws-runtime", "ap-runtime", testImage)
	appDeployment.Spec.Resources.Requests.CPUMillis = 125
	appDeployment.Spec.Resources.Requests.MemoryMiB = 192
	appDeployment.Spec.Resources.Limits.CPUMillis = 875
	appDeployment.Spec.Resources.Limits.MemoryMiB = 640

	resources := desiredResourceRequirements(appDeployment)
	if got := resources.Requests.Cpu().MilliValue(); got != 125 {
		t.Fatalf("expected CPU request 125m, got %dm", got)
	}
	if got := resources.Requests.Memory().Value(); got != 192*1024*1024 {
		t.Fatalf("expected memory request 192Mi, got %d bytes", got)
	}
	if got := resources.Limits.Cpu().MilliValue(); got != 875 {
		t.Fatalf("expected CPU limit 875m, got %dm", got)
	}
	if got := resources.Limits.Memory().Value(); got != 640*1024*1024 {
		t.Fatalf("expected memory limit 640Mi, got %d bytes", got)
	}
}

func TestApplyTCPPublicationTargetsAllocatedListenerAndNamedPort(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := platformv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := gatewayv1alpha2.Install(scheme); err != nil {
		t.Fatal(err)
	}
	externalPort := int32(20003)
	appDeployment := newAppDeployment("ws-runtime", "ap-tcp-runtime", testImage)
	appDeployment.UID = types.UID("tcp-runtime-owner")
	appDeployment.Spec.Ports = []platformv1alpha1.AppDeploymentPort{{Name: "postgres", ContainerPort: 5432, Protocol: corev1.ProtocolTCP}}
	appDeployment.Spec.PublicEndpoints = []platformv1alpha1.AppDeploymentPublicEndpoint{{Name: "database", Type: "TCP", PortName: "postgres", HostnameLabel: "database", ExternalPort: &externalPort}}
	client := fake.NewClientBuilder().WithScheme(scheme).WithObjects(appDeployment).Build()
	reconciler := &AppDeploymentReconciler{Client: client, Scheme: scheme}
	route, err := reconciler.applyTCPPublication(context.Background(), appDeployment)
	if err != nil {
		t.Fatal(err)
	}
	if !metav1.IsControlledBy(route, appDeployment) || route.Spec.ParentRefs[0].SectionName == nil || *route.Spec.ParentRefs[0].SectionName != "tcp-20003" {
		t.Fatalf("unexpected TCPRoute parent or owner: %#v", route)
	}
	backend := route.Spec.Rules[0].BackendRefs[0]
	if backend.Port == nil || *backend.Port != 5432 {
		t.Fatalf("TCPRoute backend port=%v, want 5432", backend.Port)
	}
}

func TestDesiredHTTPProbePolicy(t *testing.T) {
	probe := desiredHTTPProbe("/readyz", 5, 3)
	if probe.HTTPGet == nil || probe.HTTPGet.Path != "/readyz" ||
		probe.HTTPGet.Port.StrVal != httpPortName || probe.HTTPGet.Scheme != corev1.URISchemeHTTP {
		t.Fatalf("unexpected HTTP probe handler: %#v", probe.HTTPGet)
	}
	if probe.TimeoutSeconds != 2 || probe.PeriodSeconds != 5 ||
		probe.SuccessThreshold != 1 || probe.FailureThreshold != 3 {
		t.Fatalf("unexpected HTTP probe policy: %#v", probe)
	}
}
