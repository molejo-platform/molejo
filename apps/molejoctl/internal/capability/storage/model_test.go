package storage

import (
	"testing"

	storagev1 "k8s.io/api/storage/v1"
)

func TestReportForStorageClass(t *testing.T) {
	expansion := true
	mode := storagev1.VolumeBindingWaitForFirstConsumer
	report, err := reportFor(&storagev1.StorageClass{
		Provisioner:          "example.csi.io",
		AllowVolumeExpansion: &expansion,
		VolumeBindingMode:    &mode,
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.Provisioner != "example.csi.io" || !report.AllowExpansion || report.VolumeBindingMode != string(mode) {
		t.Fatalf("unexpected report: %+v", report)
	}
}

func TestProbeResourcesAreScopedAndHardened(t *testing.T) {
	pvc, pod := probeResources("probe", "local-path", "example.invalid/probe@sha256:abc")
	if pvc.Namespace != "probe" || *pvc.Spec.StorageClassName != "local-path" || pvc.Spec.AccessModes[0] != "ReadWriteOnce" {
		t.Fatalf("unexpected PVC: %+v", pvc.Spec)
	}
	container := pod.Spec.Containers[0]
	if pod.Namespace != "probe" || container.SecurityContext == nil || container.SecurityContext.AllowPrivilegeEscalation == nil || *container.SecurityContext.AllowPrivilegeEscalation {
		t.Fatalf("probe Pod is not scoped and hardened: %+v", pod.Spec)
	}
	if pod.Spec.AutomountServiceAccountToken == nil || *pod.Spec.AutomountServiceAccountToken {
		t.Fatal("probe Pod mounts a service account token")
	}
}
