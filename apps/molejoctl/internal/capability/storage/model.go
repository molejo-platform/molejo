// Package storage verifies an operator-selected Kubernetes StorageClass.
package storage

import (
	"errors"
	"fmt"
	"strings"

	storagev1 "k8s.io/api/storage/v1"
)

const DefaultProbeImage = "registry.k8s.io/e2e-test-images/busybox:1.36.1-1"

type Options struct {
	ContextName      string
	StorageClassName string
	ProbeImage       string
}

type Report struct {
	StorageClassName  string
	Provisioner       string
	VolumeBindingMode string
	AllowExpansion    bool
	ProbeNamespace    string
}

func reportFor(class *storagev1.StorageClass) (Report, error) {
	if class == nil {
		return Report{}, errors.New("StorageClass is required")
	}
	if strings.TrimSpace(class.Provisioner) == "" {
		return Report{}, fmt.Errorf("StorageClass %q has no provisioner", class.Name)
	}
	bindingMode := string(storagev1.VolumeBindingImmediate)
	if class.VolumeBindingMode != nil {
		bindingMode = string(*class.VolumeBindingMode)
	}
	return Report{
		StorageClassName:  class.Name,
		Provisioner:       class.Provisioner,
		VolumeBindingMode: bindingMode,
		AllowExpansion:    class.AllowVolumeExpansion != nil && *class.AllowVolumeExpansion,
	}, nil
}
