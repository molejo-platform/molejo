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
	// ReasonStatefulSetProgressing reports that the managed StatefulSet is converging.
	ReasonStatefulSetProgressing = "StatefulSetProgressing"
	// ReasonStatefulSetAvailable reports that the managed StatefulSet completed its rollout.
	ReasonStatefulSetAvailable = "StatefulSetAvailable"
	// ReasonProgressDeadlineExceeded reports that the managed Deployment stalled.
	ReasonProgressDeadlineExceeded = "ProgressDeadlineExceeded"
	// ReasonReplicaFailure reports that the managed Deployment cannot create or retain replicas.
	ReasonReplicaFailure = "ReplicaFailure"
	// ReasonOwnershipConflict reports that a required Kubernetes child is controlled elsewhere.
	ReasonOwnershipConflict = "OwnershipConflict"
	// ReasonReconcileFailed reports an operational reconciliation failure.
	ReasonReconcileFailed = "ReconcileFailed"
	// ReasonHTTPRouteProgressing reports that the public route is not accepted yet.
	ReasonHTTPRouteProgressing = "HTTPRouteProgressing"
	// ReasonHTTPRouteRejected reports that the configured Gateway rejected the public route.
	ReasonHTTPRouteRejected = "HTTPRouteRejected"
	// ReasonGatewayProgressing reports that the shared Gateway is not programmed yet.
	ReasonGatewayProgressing = "GatewayProgressing"
	// ReasonGatewayRejected reports that the shared Gateway or HTTPS listener is unavailable.
	ReasonGatewayRejected = "GatewayRejected"
	// ReasonPublicationRejected reports that a public endpoint was rejected by the shared Gateway.
	ReasonPublicationRejected = "PublicationRejected"

	// WorkloadStateless projects an AppDeployment into a Deployment.
	WorkloadStateless AppDeploymentWorkloadKind = "Stateless"
	// WorkloadStateful projects an AppDeployment into a single-replica StatefulSet.
	WorkloadStateful AppDeploymentWorkloadKind = "Stateful"
)

// AppDeploymentWorkloadKind discriminates the workload renderer.
// +kubebuilder:validation:Enum=Stateless;Stateful
type AppDeploymentWorkloadKind string

// AppDeploymentWorkload is a closed union for Stateless and Stateful intent.
// +kubebuilder:validation:XValidation:rule="self.kind != 'Stateless' || (has(self.stateless) && !has(self.stateful))",message="Stateless requires only the stateless branch"
// +kubebuilder:validation:XValidation:rule="self.kind != 'Stateful' || (has(self.stateful) && !has(self.stateless))",message="Stateful requires only the stateful branch"
type AppDeploymentWorkload struct {
	Kind      AppDeploymentWorkloadKind `json:"kind"`
	Stateless *StatelessWorkload        `json:"stateless,omitempty"`
	Stateful  *StatefulWorkload         `json:"stateful,omitempty"`
}

// StatelessWorkload marks a workload without persistent storage.
type StatelessWorkload struct{}

// StatefulWorkload attaches one durable AppVolume to the workload.
type StatefulWorkload struct {
	// VolumeRef is the namespaced AppVolume identity.
	// +kubebuilder:validation:Pattern="^vol-[a-z0-9](?:[-a-z0-9]*[a-z0-9])?$"
	// +kubebuilder:validation:MaxLength=63
	VolumeRef string `json:"volumeRef"`

	// MountPath is the absolute path made writable in the workload container.
	// +kubebuilder:validation:Pattern="^/[^[:cntrl:]]+$"
	// +kubebuilder:validation:MinLength=2
	// +kubebuilder:validation:MaxLength=255
	MountPath string `json:"mountPath"`
}

// AppDeploymentSpec declares the minimum workload intent understood by the platform operator.
// +kubebuilder:validation:XValidation:rule="has(self.port) || has(self.ports)",message="at least one legacy or named port is required"
// +kubebuilder:validation:XValidation:rule="self.workload.kind != 'Stateful' || !has(self.replicas) || self.replicas == 1",message="Stateful workloads require exactly one replica"
type AppDeploymentSpec struct {
	// Workload selects exactly one workload renderer.
	Workload AppDeploymentWorkload `json:"workload"`

	// Image is an immutable OCI image reference.
	// +kubebuilder:validation:Pattern="^[a-z0-9][a-z0-9._:/-]*@sha256:[a-f0-9]{64}$"
	Image string `json:"image"`

	// Replicas is the desired number of workload replicas.
	// +kubebuilder:default=1
	// +kubebuilder:validation:Minimum=1
	Replicas *int32 `json:"replicas,omitempty"`

	// Ports are stable, named TCP ports exposed by the workload Service.
	// +listType=map
	// +listMapKey=name
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=8
	Ports []AppDeploymentPort `json:"ports,omitempty"`

	// Deprecated compatibility projection for pre-multiport v1alpha1 objects.
	// +optional
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	Port int32 `json:"port,omitempty"`

	// Resources declares the required compute requests and limits.
	Resources AppDeploymentResources `json:"resources"`

	// Probes declares health checks against named workload ports.
	Probes AppDeploymentProbes `json:"probes"`

	// PublicEndpoints are allocation results produced by the control plane.
	// +listType=map
	// +listMapKey=name
	// +kubebuilder:validation:MaxItems=2
	// +kubebuilder:validation:XValidation:rule="self.filter(e, e.type == 'HTTP').size() <= 1 && self.filter(e, e.type == 'TCP').size() <= 1",message="at most one endpoint of each type is supported"
	PublicEndpoints []AppDeploymentPublicEndpoint `json:"publicEndpoints,omitempty"`

	// Variables are non-secret environment variables projected into the workload.
	// +listType=map
	// +listMapKey=name
	// +kubebuilder:validation:MaxItems=100
	Variables []AppDeploymentVariable `json:"variables,omitempty"`

	// ConfigMapRef names an immutable ConfigMap prepared by the control plane.
	// +optional
	ConfigMapRef string `json:"configMapRef,omitempty"`

	// SecretRef names an immutable Secret prepared by the control plane.
	// +optional
	SecretRef string `json:"secretRef,omitempty"`
}

// AppDeploymentPort is one named TCP port exposed by the workload.
type AppDeploymentPort struct {
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=15
	// +kubebuilder:validation:Pattern="^[a-z0-9](?:[-a-z0-9]*[a-z0-9])?$"
	Name string `json:"name"`

	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	ContainerPort int32 `json:"containerPort"`

	// +kubebuilder:validation:Enum=TCP
	Protocol corev1.Protocol `json:"protocol"`
}

// AppDeploymentPublicEndpointType selects the shared publication path.
// +kubebuilder:validation:Enum=HTTP;TCP
type AppDeploymentPublicEndpointType string

// AppDeploymentPublicEndpoint is a bounded public route allocated by the control plane.
// +kubebuilder:validation:XValidation:rule="self.type != 'TCP' || has(self.externalPort)",message="TCP publication requires an allocated external port"
// +kubebuilder:validation:XValidation:rule="self.type != 'HTTP' || !has(self.externalPort)",message="HTTP publication does not use an external port"
// +kubebuilder:validation:XValidation:rule="self.type != 'HTTP' || (has(self.addresses) && size(self.addresses) > 0 && !has(self.hostname) && !has(self.hostnameLabel))",message="HTTP requires resolved addresses only"
// +kubebuilder:validation:XValidation:rule="self.type != 'TCP' || !has(self.addresses)",message="TCP does not use HTTP addresses"
type AppDeploymentPublicEndpoint struct {
	// +listType=map
	// +listMapKey=hostname
	// +kubebuilder:validation:MaxItems=10
	Addresses []AppDeploymentHTTPAddress `json:"addresses,omitempty"`
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=15
	// +kubebuilder:validation:Pattern="^[a-z0-9](?:[-a-z0-9]*[a-z0-9])?$"
	Name string `json:"name"`

	Type AppDeploymentPublicEndpointType `json:"type"`

	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=15
	// +kubebuilder:validation:Pattern="^[a-z0-9](?:[-a-z0-9]*[a-z0-9])?$"
	PortName string `json:"portName"`

	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	HostnameLabel string `json:"hostnameLabel,omitempty"`

	// Hostname is the exact public name resolved by the control plane.
	// +kubebuilder:validation:MaxLength=253
	Hostname string `json:"hostname,omitempty"`

	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	ExternalPort *int32 `json:"externalPort,omitempty"`
}

// AppDeploymentVariable declares one non-secret environment variable.
type AppDeploymentVariable struct {
	// Name follows the portable environment variable identifier format.
	// +kubebuilder:validation:MaxLength=253
	// +kubebuilder:validation:Pattern="^[A-Za-z_][A-Za-z0-9_]*$"
	Name string `json:"name"`

	// Value is stored in clear text and must not contain secret material.
	// +kubebuilder:validation:MaxLength=4096
	Value string `json:"value"`
}

// AppDeploymentResources declares resource requests and limits in platform units.
// +kubebuilder:validation:XValidation:rule="self.requests.cpuMillis <= self.limits.cpuMillis",message="CPU requests must not exceed CPU limits"
// +kubebuilder:validation:XValidation:rule="self.requests.memoryMiB <= self.limits.memoryMiB",message="memory requests must not exceed memory limits"
type AppDeploymentResources struct {
	// Requests declares the minimum compute reserved for the workload.
	Requests AppDeploymentResourceValues `json:"requests"`

	// Limits declares the maximum compute available to the workload.
	Limits AppDeploymentResourceValues `json:"limits"`
}

// AppDeploymentResourceValues uses Kubernetes-independent CPU and memory units.
type AppDeploymentResourceValues struct {
	// CPUMillis is CPU capacity measured in millicores.
	// +kubebuilder:validation:Minimum=1
	CPUMillis int64 `json:"cpuMillis"`

	// MemoryMiB is memory capacity measured in mebibytes.
	// +kubebuilder:validation:Minimum=1
	MemoryMiB int64 `json:"memoryMiB"`
}

// AppDeploymentProbes declares startup, readiness, and liveness checks.
type AppDeploymentProbes struct {
	// Startup is optional for compatibility with existing v1alpha1 objects. The
	// operator falls back to readiness until the control plane rewrites them.
	// +optional
	Startup *AppDeploymentProbe `json:"startup,omitempty"`

	// Liveness identifies the endpoint used to detect an unhealthy process.
	Liveness AppDeploymentProbe `json:"liveness"`

	// Readiness identifies the endpoint used to admit the process to the Service.
	Readiness AppDeploymentProbe `json:"readiness"`
}

// AppDeploymentProbe identifies one HTTP or TCP check on a named port.
// +kubebuilder:validation:XValidation:rule="has(self.type) && self.type == 'TCP' || has(self.path)",message="HTTP and legacy probes require a path"
// +kubebuilder:validation:XValidation:rule="!has(self.type) || self.type != 'TCP' || !has(self.path)",message="TCP probes do not accept a path"
type AppDeploymentProbe struct {
	// Type defaults to HTTP only for existing v1alpha1 objects.
	// +optional
	// +kubebuilder:validation:Enum=HTTP;TCP
	Type string `json:"type,omitempty"`

	// PortName defaults to the legacy http port only for existing v1alpha1 objects.
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=15
	// +kubebuilder:validation:Pattern="^[a-z0-9](?:[-a-z0-9]*[a-z0-9])?$"
	PortName string `json:"portName,omitempty"`

	// Path is an absolute HTTP path served by the workload.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=2048
	// +kubebuilder:validation:Pattern="^/.*$"
	Path string `json:"path,omitempty"`
}

// AppDeploymentHTTPProbe is a source-compatible alias for the previous v1alpha1 name.
type AppDeploymentHTTPProbe = AppDeploymentProbe

// AppDeploymentEndpointStatus reports one independently reconciled publication.
type AppDeploymentEndpointStatus struct {
	// +listType=map
	// +listMapKey=hostname
	// +kubebuilder:validation:MaxItems=10
	Addresses []AppDeploymentHTTPAddressStatus `json:"addresses,omitempty"`
	Name      string                           `json:"name"`
	Type      AppDeploymentPublicEndpointType  `json:"type"`
	Ready     bool                             `json:"ready"`
	Reason    string                           `json:"reason,omitempty"`
}

// AppDeploymentStatus reports the observed workload state.
type AppDeploymentStatus struct {
	// ObservedGeneration is the latest generation successfully projected to the workload.
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// ObservedRelease is the immutable image reference projected to the workload.
	ObservedRelease string `json:"observedRelease,omitempty"`

	// WorkloadRef identifies the Deployment or StatefulSet managed for this resource.
	WorkloadRef *corev1.LocalObjectReference `json:"workloadRef,omitempty"`

	// Conditions describe readiness, progress, and degradation.
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// EndpointStatuses keep HTTP and TCP publication outcomes independent.
	// +listType=map
	// +listMapKey=name
	EndpointStatuses []AppDeploymentEndpointStatus `json:"endpointStatuses,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,shortName=appdep
// +kubebuilder:printcolumn:name="Image",type=string,JSONPath=`.spec.image`
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`
// +kubebuilder:validation:XValidation:rule="self.metadata.name.matches('^ap-[a-z0-9](?:[-a-z0-9]*[a-z0-9])?$') && size(self.metadata.name) <= 63",message="metadata.name must start with ap- and contain at most 63 lowercase DNS-compatible characters"

// AppDeployment represents the stable runtime intent for one AppEnvironment.
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

// AppDeploymentHTTPAddress fixes one association to its authorized destination.
type AppDeploymentHTTPAddress struct {
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	// +kubebuilder:validation:Pattern="^[a-z0-9](?:[-a-z0-9]*[a-z0-9])?(?:\\.[a-z0-9](?:[-a-z0-9]*[a-z0-9])?)*$"
	// +kubebuilder:validation:XValidation:rule="self.split('.').all(label, size(label) <= 63)",message="DNS labels cannot exceed 63 characters"
	Hostname    string          `json:"hostname"`
	Destination HTTPDestination `json:"destination"`
}

type HTTPDestination struct {
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=128
	BindingID string `json:"bindingId"`
	// +kubebuilder:validation:Minimum=1
	BindingRevision int64 `json:"bindingRevision"`
	// +kubebuilder:validation:Enum=kubernetes-http.v1alpha1
	SchemaVersion string `json:"schemaVersion"`
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=63
	// +kubebuilder:validation:Pattern="^[a-z0-9](?:[-a-z0-9]*[a-z0-9])?$"
	GatewayNamespace string `json:"gatewayNamespace"`
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	// +kubebuilder:validation:Pattern="^[a-z0-9](?:[-a-z0-9.]*[a-z0-9])?$"
	GatewayName string `json:"gatewayName"`
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=63
	// +kubebuilder:validation:Pattern="^[a-z0-9](?:[-a-z0-9]*[a-z0-9])?$"
	SectionName string `json:"sectionName"`
}

// Evidence describes runtime facts only. It does not attest DNS or served TLS.
type AppDeploymentHTTPAddressStatus struct {
	Hostname        string          `json:"hostname"`
	Destination     HTTPDestination `json:"destination"`
	RouteName       string          `json:"routeName"`
	RouteUID        string          `json:"routeUid"`
	RouteGeneration int64           `json:"routeGeneration"`
	GatewayUID      string          `json:"gatewayUid,omitempty"`
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions"`
}
