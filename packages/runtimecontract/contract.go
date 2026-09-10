// Package runtimecontract defines the Kubernetes-neutral command payload shared
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

	ExposurePrivate = "Private"
	ExposurePublic  = "Public"
	EndpointHTTP    = "HTTP"
	EndpointTCP     = "TCP"

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

type WorkspacePlacementIntent struct {
	WorkspaceID    string `json:"workspaceId"`
	NamespaceName  string `json:"namespaceName"`
	AccessProfile  string `json:"accessProfile"`
	LifecycleState string `json:"lifecycleState"`
}

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
	Exposure             string           `json:"exposure,omitempty"`
	Slug                 string           `json:"slug,omitempty"`
}

type AppVolume struct {
	PublicID        string `json:"id"`
	MountPath       string `json:"mountPath"`
	SizeGiB         int64  `json:"sizeGiB"`
	RetentionPolicy string `json:"retentionPolicy"`
}

type VolumeIntent struct {
	RuntimeBinding  string `json:"runtimeBinding"`
	SizeGiB         int64  `json:"sizeGiB"`
	RetentionPolicy string `json:"retentionPolicy"`
	DesiredState    string `json:"desiredState"`
}

type ResourceValues struct {
	CPUMillis int64 `json:"cpuMillis"`
	MemoryMiB int64 `json:"memoryMiB"`
}

type Resources struct {
	Requests ResourceValues `json:"requests"`
	Limits   ResourceValues `json:"limits"`
}

type Probe struct {
	Type     string `json:"type"`
	PortName string `json:"portName"`
	Path     string `json:"path,omitempty"`
}

type Probes struct {
	Startup   Probe `json:"startup"`
	Liveness  Probe `json:"liveness"`
	Readiness Probe `json:"readiness"`
}

type RuntimePort struct {
	Name          string `json:"name"`
	ContainerPort int32  `json:"containerPort"`
	Protocol      string `json:"protocol"`
}

type PublicEndpoint struct {
	Name          string `json:"name"`
	Type          string `json:"type"`
	PortName      string `json:"portName"`
	HostnameLabel string `json:"hostnameLabel"`
	Hostname      string `json:"hostname"`
	ExternalPort  int32  `json:"externalPort,omitempty"`
}

type Variable struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}
