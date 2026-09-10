// Package metadata defines the stable ownership metadata shared by Molejo
// components that render or inspect application workloads.
package metadata

const (
	AppDeploymentLabel          = "platform.molejo.dev/app-deployment"
	ManagedByLabel              = "app.kubernetes.io/managed-by"
	ManagedByOperator           = "molejo-platform-operator"
	ControlPlaneOwnerAnnotation = "platform.molejo.dev/control-plane-owner"
	ControlPlaneOwner           = "molejo-control-plane"
	ApplicationContainer        = "app"
)
