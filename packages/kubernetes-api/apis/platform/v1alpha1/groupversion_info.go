// Package v1alpha1 contains the first Kubernetes API contract for Fruto Platform.
// +kubebuilder:object:generate=true
// +groupName=platform.fruto.calouro.tech
package v1alpha1

import (
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/scheme"
)

var (
	// GroupVersion identifies this API group and version.
	GroupVersion = schema.GroupVersion{Group: "platform.fruto.calouro.tech", Version: "v1alpha1"}

	// SchemeBuilder registers this API group and version.
	SchemeBuilder = &scheme.Builder{GroupVersion: GroupVersion}

	// AddToScheme adds this API group and version to a runtime scheme.
	AddToScheme = SchemeBuilder.AddToScheme
)
