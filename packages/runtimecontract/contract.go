// Package runtimecontract defines the bounded command payload shared
// by the control plane and an outbound cluster Agent.
package runtimecontract

const (
	OperationEnsureWorkspace          = "EnsureWorkspace"
	OperationEnsureWorkspacePlacement = "EnsureWorkspacePlacement"
	OperationApplyDeployment          = "ApplyDeployment"
	OperationDeleteAppEnv             = "DeleteAppEnvironment"
	OperationEnsureVolume             = "EnsureVolume"
	OperationExpandVolume             = "ExpandVolume"
	OperationDeleteVolume             = "DeleteVolume"

	EndpointHTTP = "HTTP"
	EndpointTCP  = "TCP"

	WorkloadStateless = "Stateless"
	WorkloadStateful  = "Stateful"

	StatePending     = "Pending"
	StateProgressing = "Progressing"
	StateReady       = "Ready"
	StateDegraded    = "Degraded"
	StateUnknown     = "Unknown"

	VolumeDesiredReady   = "Ready"
	VolumeDesiredDeleted = "Deleted"
	VolumeStatePending   = "Pending"
	VolumeStateReady     = "Ready"
	VolumeStateRetained  = "Retained"
)

// Payload contains exactly the fields required by one runtime operation.
type Payload struct {
	Namespace  string                    `json:"namespace"`
	Name       string                    `json:"name,omitempty"`
	Deployment *DeploymentIntent         `json:"deployment,omitempty"`
	Volume     *VolumeIntent             `json:"volume,omitempty"`
	Placement  *WorkspacePlacementIntent `json:"placement,omitempty"`
}

// WorkspacePlacementIntent requests the bounded namespace and access projection
// for one logical Workspace.
type WorkspacePlacementIntent struct {
	WorkspaceID    string `json:"workspaceId"`
	NamespaceName  string `json:"namespaceName"`
	AccessProfile  string `json:"accessProfile"`
	LifecycleState string `json:"lifecycleState"`
}

// DeploymentIntent carries runtime intent with concrete publication destinations.
type DeploymentIntent struct {
	Image                string           `json:"image"`
	Replicas             int32            `json:"replicas"`
	Ports                []RuntimePort    `json:"ports"`
	Resources            Resources        `json:"resources"`
	Probes               Probes           `json:"probes"`
	PublicEndpoints      []PublicEndpoint `json:"publicEndpoints"`
	Variables            []Variable       `json:"variables"`
	SecretVariables      []Variable       `json:"secretVariables,omitempty"`
	ConfigurationVersion int64            `json:"configurationVersion"`
	WorkloadKind         string           `json:"workloadKind"`
	Volume               *AppVolume       `json:"volume,omitempty"`
	Port                 int32            `json:"port,omitempty"`
}

// AppVolume binds a deployment intent to one independently managed volume.
type AppVolume struct {
	PublicID        string `json:"id"`
	MountPath       string `json:"mountPath"`
	SizeGiB         int64  `json:"sizeGiB"`
	RetentionPolicy string `json:"retentionPolicy"`
}

// VolumeIntent declares the lifecycle of one durable volume projection.
type VolumeIntent struct {
	RuntimeBinding  string `json:"runtimeBinding"`
	SizeGiB         int64  `json:"sizeGiB"`
	RetentionPolicy string `json:"retentionPolicy"`
	DesiredState    string `json:"desiredState"`
}

// ResourceValues expresses compute in portable millicores and mebibytes.
type ResourceValues struct {
	CPUMillis int64 `json:"cpuMillis"`
	MemoryMiB int64 `json:"memoryMiB"`
}

// Resources keeps requests and limits explicit at the transport boundary.
type Resources struct {
	Requests ResourceValues `json:"requests"`
	Limits   ResourceValues `json:"limits"`
}

// Probe declares one HTTP or TCP health check against a named port.
type Probe struct {
	Type     string `json:"type"`
	PortName string `json:"portName"`
	Path     string `json:"path,omitempty"`
}

// Probes carries the complete startup, liveness, and readiness policy.
type Probes struct {
	Startup   Probe `json:"startup"`
	Liveness  Probe `json:"liveness"`
	Readiness Probe `json:"readiness"`
}

// RuntimePort is one stable named TCP port exposed by the runtime Service.
type RuntimePort struct {
	Name          string `json:"name"`
	ContainerPort int32  `json:"containerPort"`
	Protocol      string `json:"protocol"`
}

// PublicEndpoint is a Control Plane allocation, not user-supplied Gateway state.
type PublicEndpoint struct {
	Addresses     []HTTPAddress `json:"addresses,omitempty"`
	Name          string        `json:"name"`
	Type          string        `json:"type"`
	PortName      string        `json:"portName"`
	HostnameLabel string        `json:"hostnameLabel,omitempty"`
	Hostname      string        `json:"hostname,omitempty"`
	ExternalPort  int32         `json:"externalPort,omitempty"`
}

// Variable is one environment variable transported to the runtime projection.
// Secret values may transit this type but must never be logged or persisted.
type Variable struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}
