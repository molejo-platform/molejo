package controller

import kubemetadata "github.com/molejo-platform/molejo/packages/kubernetes-api/metadata"

const (
	appDeploymentLabel = kubemetadata.AppDeploymentLabel
	managedByLabel     = kubemetadata.ManagedByLabel
	managedByValue     = kubemetadata.ManagedByOperator
)
