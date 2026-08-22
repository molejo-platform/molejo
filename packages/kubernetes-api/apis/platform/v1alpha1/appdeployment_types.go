package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	// ConditionReady reports whether the managed workload is available.
	ConditionReady = "Ready"
	// ConditionProgressing reports whether the managed workload is converging.
	ConditionProgressing = "Progressing"
	// ConditionDegraded reports whether reconciliation is blocked by an error.
	ConditionDegraded = "Degraded"

	// ReasonDeploymentProgressing reports that the managed Deployment is converging.
	ReasonDeploymentProgressing = "DeploymentProgressing"
	// ReasonDeploymentAvailable reports that the managed Deployment completed its rollout.
	ReasonDeploymentAvailable = "DeploymentAvailable"
	// ReasonProgressDeadlineExceeded reports that the managed Deployment stalled.
	ReasonProgressDeadlineExceeded = "ProgressDeadlineExceeded"
	// ReasonReplicaFailure reports that the managed Deployment cannot create or retain replicas.
	ReasonReplicaFailure = "ReplicaFailure"
	// ReasonOwnershipConflict reports that the required Deployment is controlled elsewhere.
	ReasonOwnershipConflict = "OwnershipConflict"
	// ReasonReconcileFailed reports an operational reconciliation failure.
	ReasonReconcileFailed = "ReconcileFailed"
)

// AppDeploymentSpec declares the minimum workload intent understood by the platform operator.
type AppDeploymentSpec struct {
	// Image is an immutable OCI image reference.
	// +kubebuilder:validation:Pattern="^[a-z0-9][a-z0-9._:/-]*@sha256:[a-f0-9]{64}$"
	Image string `json:"image"`

	// Replicas is the desired number of workload replicas.
	// +kubebuilder:default=1
	// +kubebuilder:validation:Minimum=1
	Replicas *int32 `json:"replicas,omitempty"`
}

// AppDeploymentStatus reports the observed workload state.
type AppDeploymentStatus struct {
	// ObservedGeneration is the latest generation successfully projected to the workload.
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// ObservedRelease is the immutable image reference projected to the workload.
	ObservedRelease string `json:"observedRelease,omitempty"`

	// WorkloadRef identifies the Deployment managed for this resource.
	WorkloadRef *corev1.LocalObjectReference `json:"workloadRef,omitempty"`

	// Conditions describe readiness, progress, and degradation.
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,shortName=appdep
// +kubebuilder:printcolumn:name="Image",type=string,JSONPath=`.spec.image`
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`
// +kubebuilder:validation:XValidation:rule="self.metadata.name.matches('^ap-[a-z0-9](?:[-a-z0-9]*[a-z0-9])?$') && size(self.metadata.name) <= 63",message="metadata.name must start with ap- and contain at most 63 lowercase DNS-compatible characters"

// AppDeployment represents an immutable application release projected onto Kubernetes.
type AppDeployment struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   AppDeploymentSpec   `json:"spec"`
	Status AppDeploymentStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// AppDeploymentList contains a list of AppDeployment resources.
type AppDeploymentList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []AppDeployment `json:"items"`
}

func init() {
	SchemeBuilder.Register(&AppDeployment{}, &AppDeploymentList{})
}
