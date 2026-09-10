// Package workspacecontract defines provider-neutral Workspace placement
// vocabulary shared by the control plane and cluster components.
package workspacecontract

import "strings"

type ProvisioningMode string

const (
	ProvisioningDisabled   ProvisioningMode = "Disabled"
	ProvisioningNamespaced ProvisioningMode = "Namespaced"
)

func ParseProvisioningMode(value string) (ProvisioningMode, bool) {
	switch ProvisioningMode(strings.TrimSpace(value)) {
	case "", ProvisioningDisabled:
		return ProvisioningDisabled, true
	case ProvisioningNamespaced:
		return ProvisioningNamespaced, true
	default:
		return "", false
	}
}

const (
	AccessProfileNamespaced = "NamespacedRuntime"
	LifecycleReady          = "Ready"
	LifecycleDeleted        = "Deleted"
)
