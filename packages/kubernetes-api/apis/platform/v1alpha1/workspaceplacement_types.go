package v1alpha1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

const (
	WorkspacePlacementConditionNamespaceReady      = "NamespaceReady"
	WorkspacePlacementConditionAgentAccessReady    = "AgentAccessReady"
	WorkspacePlacementConditionOperatorAccessReady = "OperatorAccessReady"
	WorkspacePlacementConditionPolicyReady         = "PolicyReady"
)

// WorkspacePlacementSpec is the closed cluster projection of one logical Workspace.
type WorkspacePlacementSpec struct {
	// +kubebuilder:validation:Pattern=`^ws-[a-z2-7]{20}$`
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="workspaceId is immutable"
	WorkspaceID string `json:"workspaceId"`
	// +kubebuilder:validation:Pattern=`^[a-z0-9](?:[-a-z0-9]{0,61}[a-z0-9])?$`
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="namespaceName is immutable"
	NamespaceName string `json:"namespaceName"`
	// +kubebuilder:validation:Enum=NamespacedRuntime
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="accessProfile is immutable"
	AccessProfile string `json:"accessProfile"`
	// +kubebuilder:validation:Enum=Ready;Deleted
	LifecycleState string `json:"lifecycleState"`
}

type WorkspacePlacementStatus struct {
	ObservedGeneration int64              `json:"observedGeneration,omitempty"`
	Conditions         []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster,shortName=wsp
// +kubebuilder:printcolumn:name="Workspace",type=string,JSONPath=`.spec.workspaceId`
// +kubebuilder:printcolumn:name="Namespace",type=string,JSONPath=`.spec.namespaceName`
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type==\"PolicyReady\")].status`
type WorkspacePlacement struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   WorkspacePlacementSpec   `json:"spec,omitempty"`
	Status WorkspacePlacementStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
type WorkspacePlacementList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []WorkspacePlacement `json:"items"`
}

func init() {
	SchemeBuilder.Register(&WorkspacePlacement{}, &WorkspacePlacementList{})
}
