package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	VolumeRetentionPreserve AppVolumeRetentionPolicy = "Preserve"
	VolumeDesiredReady      AppVolumeDesiredState    = "Ready"
	VolumeDesiredDeleted    AppVolumeDesiredState    = "Deleted"
	VolumeStatePending      AppVolumeState           = "Pending"
	VolumeStateProvisioning AppVolumeState           = "Provisioning"
	VolumeStateReady        AppVolumeState           = "Ready"
	VolumeStateExpanding    AppVolumeState           = "Expanding"
	VolumeStateRetained     AppVolumeState           = "Retained"
	VolumeStateDegraded     AppVolumeState           = "Degraded"

	ReasonVolumeProvisioning = "VolumeProvisioning"
	ReasonVolumeReady        = "VolumeReady"
	ReasonVolumeExpanding    = "VolumeExpanding"
	ReasonVolumeRetained     = "VolumeRetained"
	ReasonVolumeFailed       = "VolumeFailed"
)

// +kubebuilder:validation:Enum=Preserve
type AppVolumeRetentionPolicy string

// +kubebuilder:validation:Enum=Ready;Deleted
type AppVolumeDesiredState string

// +kubebuilder:validation:Enum=Pending;Provisioning;Ready;Expanding;Retained;Degraded
type AppVolumeState string

// AppVolumeSpec declares one durable volume independently from releases.
type AppVolumeSpec struct {
	// StorageClassName is the internal profile binding resolved by the control plane.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	StorageClassName string `json:"storageClassName"`

	// SizeGiB is the requested capacity and can only increase.
	// +kubebuilder:validation:Minimum=1
	SizeGiB int64 `json:"sizeGiB"`

	// RetentionPolicy preserves data when a workload or release is removed.
	// +kubebuilder:default=Preserve
	RetentionPolicy AppVolumeRetentionPolicy `json:"retentionPolicy,omitempty"`

	// DesiredState distinguishes normal reconciliation from explicit retention.
	// +kubebuilder:default=Ready
	DesiredState AppVolumeDesiredState `json:"desiredState,omitempty"`
}

type AppVolumeStatus struct {
	ObservedGeneration int64                        `json:"observedGeneration,omitempty"`
	ObservedSizeGiB    int64                        `json:"observedSizeGiB,omitempty"`
	State              AppVolumeState               `json:"state,omitempty"`
	ClaimRef           *corev1.LocalObjectReference `json:"claimRef,omitempty"`
	Conditions         []metav1.Condition           `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,shortName=appvol
// +kubebuilder:printcolumn:name="Size",type=integer,JSONPath=`.spec.sizeGiB`
// +kubebuilder:printcolumn:name="State",type=string,JSONPath=`.status.state`
// +kubebuilder:validation:XValidation:rule="self.metadata.name.matches('^vol-[a-z0-9](?:[-a-z0-9]*[a-z0-9])?$') && size(self.metadata.name) <= 63",message="metadata.name must start with vol- and contain at most 63 lowercase DNS-compatible characters"
// +kubebuilder:validation:XValidation:rule="self.spec.storageClassName == oldSelf.spec.storageClassName",message="storageClassName is immutable"
// +kubebuilder:validation:XValidation:rule="self.spec.sizeGiB >= oldSelf.spec.sizeGiB",message="sizeGiB cannot decrease"

// AppVolume projects one product volume intent into a PersistentVolumeClaim.
type AppVolume struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   AppVolumeSpec   `json:"spec"`
	Status AppVolumeStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

type AppVolumeList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []AppVolume `json:"items"`
}

func init() {
	SchemeBuilder.Register(&AppVolume{}, &AppVolumeList{})
}
