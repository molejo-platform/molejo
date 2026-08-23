package controller

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
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
