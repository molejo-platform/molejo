package domain

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	ExposurePrivate                      = "Private"
	ExposurePublic                       = "Public"
	PortProtocolTCP                      = "TCP"
	ProbeHTTP                            = "HTTP"
	ProbeTCP                             = "TCP"
	EndpointHTTP                         = "HTTP"
	EndpointTCP                          = "TCP"
	StatePending                         = "Pending"
	StateProgressing                     = "Progressing"
	StateReady                           = "Ready"
	StateDegraded                        = "Degraded"
	StateUnknown                         = "Unknown"
	Unknown                              = StateUnknown
	Progressing                          = StateProgressing
	Ready                                = StateReady
	Degraded                             = StateDegraded
	ParameterPlainText                   = "PlainText"
	ParameterSecret                      = "Secret"
	WorkloadStateless       WorkloadKind = "Stateless"
	WorkloadStateful        WorkloadKind = "Stateful"
	VolumeDesiredReady                   = "Ready"
	VolumeDesiredDeleted                 = "Deleted"
	VolumeStatePending                   = "Pending"
	VolumeStateProvisioning              = "Provisioning"
	VolumeStateReady                     = "Ready"
	VolumeStateExpanding                 = "Expanding"
	VolumeStateRetained                  = "Retained"
	VolumeStateDegraded                  = "Degraded"
	VolumeRetentionPreserve              = "Preserve"
)

var (
	imagePattern          = regexp.MustCompile(`^[a-z0-9][a-z0-9._:/-]*@sha256:[a-f0-9]{64}$`)
	slugPattern           = regexp.MustCompile(`^[a-z0-9](?:[-a-z0-9]*[a-z0-9])?$`)
	appIDPattern          = regexp.MustCompile(`^app-[a-z2-7]{20}$`)
	envIDPattern          = regexp.MustCompile(`^env-[a-z2-7]{20}$`)
	appEnvIDPattern       = regexp.MustCompile(`^aev-[a-z2-7]{20}$`)
	variableNamePattern   = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	parameterIDPattern    = regexp.MustCompile(`^par-[a-z2-7]{20}$`)
	parameterPathPattern  = regexp.MustCompile(`^/[a-zA-Z0-9._/-]{1,254}$`)
	storageProfilePattern = regexp.MustCompile(`^[a-z0-9](?:[-a-z0-9]*[a-z0-9])?$`)
)

// WorkloadKind is the immutable runtime classification selected for one AppEnvironment.
type WorkloadKind string

// VolumeRequest is the public storage intent accepted when a Stateful AppEnvironment is created.
type VolumeRequest struct {
	StorageProfileID string `json:"storageProfileId"`
	SizeGiB          int64  `json:"sizeGiB"`
	MountPath        string `json:"mountPath"`
}

// StorageProfile exposes portable product capabilities without infrastructure bindings.
type StorageProfile struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	MinimumSizeGiB  int64  `json:"minimumSizeGiB"`
	MaximumSizeGiB  int64  `json:"maximumSizeGiB"`
	AvailableGiB    int64  `json:"availableGiB"`
	Expandable      bool   `json:"expandable"`
	Snapshots       bool   `json:"snapshots"`
	AutomaticBackup bool   `json:"automaticBackup"`
	Durability      string `json:"durability"`
}

// AppVolume is durable storage owned by an AppEnvironment, independent from releases.
type AppVolume struct {
	ID                     int64      `json:"-"`
	PublicID               string     `json:"id"`
	WorkspaceID            int64      `json:"-"`
	AppEnvironmentID       int64      `json:"-"`
	AppEnvironmentPublicID string     `json:"appEnvironmentId"`
	StorageProfileID       string     `json:"storageProfileId"`
	RuntimeStorageClass    string     `json:"-"`
	StorageBindingVersion  int64      `json:"-"`
	SizeGiB                int64      `json:"sizeGiB"`
	MountPath              string     `json:"mountPath"`
	RetentionPolicy        string     `json:"retentionPolicy"`
	DesiredState           string     `json:"desiredState"`
	State                  string     `json:"state"`
	Message                string     `json:"message,omitempty"`
	Attached               bool       `json:"attached"`
	Version                int64      `json:"version"`
	CreatedAt              time.Time  `json:"createdAt"`
	UpdatedAt              time.Time  `json:"updatedAt"`
	DeletionRequestedAt    *time.Time `json:"-"`
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
	DomainID      string `json:"domainId"`
	HostnameLabel string `json:"hostnameLabel"`
	ExternalPort  int32  `json:"externalPort,omitempty"`
	Hostname      string `json:"-"`
}

type Variable struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type Parameter struct {
	ID             int64      `json:"-"`
	PublicID       string     `json:"id"`
	WorkspaceID    int64      `json:"-"`
	Path           string     `json:"path"`
	Kind           string     `json:"type"`
	Description    string     `json:"description"`
	CurrentVersion int64      `json:"currentVersion"`
	Version        int64      `json:"version"`
	Value          *string    `json:"value,omitempty"`
	Configured     bool       `json:"configured"`
	CreatedAt      time.Time  `json:"createdAt"`
	UpdatedAt      time.Time  `json:"updatedAt"`
	ArchivedAt     *time.Time `json:"-"`
}

type ParameterValue struct {
	PlainTextValue       *string
	SecretReference      SecretReference
	SecretBackendVersion SecretBackendVersion
	Fingerprint          SecretFingerprint
}

type SecretMutation struct {
	ParameterID            int64
	ParameterPublicID      string
	WorkspaceID            int64
	ParameterVersion       int64
	ResourceVersion        int64
	Reference              SecretReference
	ExpectedBackendVersion SecretBackendVersion
	BackendVersion         SecretBackendVersion
	State                  string
	CreatedAt              time.Time
}

type ParameterPurgeCandidate struct {
	ParameterID       int64
	ParameterPublicID string
	WorkspaceID       int64
	Kind              string
	SecretReference   SecretReference
}

type ParameterBinding struct {
	Name              string `json:"name"`
	ParameterPublicID string `json:"parameterId"`
	ParameterVersion  int64  `json:"parameterVersion"`
}

type ResolvedParameter struct {
	Binding              ParameterBinding
	Kind                 string
	PlainTextValue       string
	SecretReference      SecretReference
	SecretBackendVersion SecretBackendVersion
}

type RuntimeConfig struct {
	Replicas        int32              `json:"replicas"`
	Ports           []RuntimePort      `json:"ports"`
	Resources       Resources          `json:"resources"`
	Probes          Probes             `json:"probes"`
	PublicEndpoints []PublicEndpoint   `json:"publicEndpoints"`
	Variables       []Variable         `json:"variables"`
	Parameters      []ParameterBinding `json:"parameters"`
	Port            int32              `json:"-"`
	Exposure        string             `json:"-"`
	Slug            string             `json:"-"`
}

type Intent struct {
	Image                string           `json:"image"`
	Replicas             int32            `json:"replicas"`
	Ports                []RuntimePort    `json:"ports"`
	Resources            Resources        `json:"resources"`
	Probes               Probes           `json:"probes"`
	PublicEndpoints      []PublicEndpoint `json:"publicEndpoints"`
	Variables            []Variable       `json:"variables"`
	ConfigMapRef         string           `json:"configMapRef,omitempty"`
	SecretRef            string           `json:"secretRef,omitempty"`
	SecretVariables      []Variable       `json:"-"`
	ConfigurationVersion int64            `json:"-"`
	WorkloadKind         WorkloadKind     `json:"workloadKind"`
	Volume               *AppVolume       `json:"volume,omitempty"`
	Port                 int32            `json:"-"`
	Exposure             string           `json:"-"`
	Slug                 string           `json:"-"`
}

type Workspace struct {
	ID             int64     `json:"-"`
	PublicID       string    `json:"id"`
	Name           string    `json:"name"`
	Namespace      string    `json:"-"`
	Version        int64     `json:"version"`
	BootstrapState string    `json:"state"`
	CreatedAt      time.Time `json:"createdAt"`
	UpdatedAt      time.Time `json:"updatedAt"`
}

type Project struct {
	ID          int64      `json:"-"`
	PublicID    string     `json:"id"`
	WorkspaceID int64      `json:"-"`
	Name        string     `json:"name"`
	Version     int64      `json:"version"`
	CreatedAt   time.Time  `json:"createdAt"`
	UpdatedAt   time.Time  `json:"updatedAt"`
	ArchivedAt  *time.Time `json:"archivedAt,omitempty"`
}

type Environment struct {
	ID         int64      `json:"-"`
	PublicID   string     `json:"id"`
	ProjectID  int64      `json:"-"`
	Name       string     `json:"name"`
	Version    int64      `json:"version"`
	CreatedAt  time.Time  `json:"createdAt"`
	UpdatedAt  time.Time  `json:"updatedAt"`
	ArchivedAt *time.Time `json:"archivedAt,omitempty"`
}

type App struct {
	ID         int64      `json:"-"`
	PublicID   string     `json:"id"`
	ProjectID  int64      `json:"-"`
	Name       string     `json:"name"`
	Version    int64      `json:"version"`
	CreatedAt  time.Time  `json:"createdAt"`
	UpdatedAt  time.Time  `json:"updatedAt"`
	ArchivedAt *time.Time `json:"archivedAt,omitempty"`
}

type GitHubInstallation struct {
	ID                  int64     `json:"-"`
	PublicID            string    `json:"id"`
	WorkspaceID         int64     `json:"-"`
	ExternalID          int64     `json:"-"`
	AccountID           int64     `json:"-"`
	AccountLogin        string    `json:"accountLogin"`
	AccountType         string    `json:"accountType"`
	RepositorySelection string    `json:"repositorySelection"`
	CreatedAt           time.Time `json:"createdAt"`
}

type GitHubRepository struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	FullName      string `json:"fullName"`
	Private       bool   `json:"private"`
	DefaultBranch string `json:"defaultBranch"`
}

type GitHubSource struct {
	InstallationID string           `json:"installationId"`
	Repository     GitHubRepository `json:"repository"`
	ConnectedAt    time.Time        `json:"connectedAt"`
}

type AppEnvironment struct {
	ID                          int64         `json:"-"`
	PublicID                    string        `json:"id"`
	WorkspaceID                 int64         `json:"-"`
	ClusterID                   int64         `json:"-"`
	ClusterPublicID             string        `json:"clusterId"`
	ClusterUID                  string        `json:"-"`
	ProjectID                   int64         `json:"-"`
	AppID                       int64         `json:"-"`
	EnvironmentID               int64         `json:"-"`
	ProjectPublicID             string        `json:"projectId"`
	AppPublicID                 string        `json:"appId"`
	AppName                     string        `json:"appName"`
	EnvironmentPublicID         string        `json:"environmentId"`
	EnvironmentName             string        `json:"environmentName"`
	SourceBranch                string        `json:"branch"`
	WorkloadKind                WorkloadKind  `json:"workloadKind"`
	RuntimeName                 string        `json:"-"`
	Configuration               RuntimeConfig `json:"configuration"`
	ConfigurationVersion        int64         `json:"configurationVersion"`
	Version                     int64         `json:"version"`
	DesiredDeploymentPublicID   string        `json:"desiredDeploymentId,omitempty"`
	CurrentDeploymentPublicID   string        `json:"currentDeploymentId,omitempty"`
	CurrentReleasePublicID      string        `json:"currentReleaseId,omitempty"`
	DesiredConfigurationVersion int64         `json:"desiredConfigurationVersion,omitempty"`
	CurrentConfigurationVersion int64         `json:"currentConfigurationVersion,omitempty"`
	RuntimeObservedGeneration   int64         `json:"runtimeObservedGeneration,omitempty"`
	RuntimeObservedAt           *time.Time    `json:"runtimeObservedAt,omitempty"`
	State                       string        `json:"state"`
	Message                     string        `json:"message,omitempty"`
	CreatedAt                   time.Time     `json:"createdAt"`
	UpdatedAt                   time.Time     `json:"updatedAt"`
	DeletionRequestedAt         *time.Time    `json:"-"`
	ArchivedAt                  *time.Time    `json:"-"`
}

func NewPublicID(prefix string) (string, error) {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	encoded := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b)
	return prefix + "-" + strings.ToLower(encoded), nil
}

func NormalizeParameterPath(value string) (string, error) {
	path := strings.TrimSpace(value)
	if len(path) < 2 || len(path) > 255 || !parameterPathPattern.MatchString(path) || strings.Contains(path, "//") || strings.HasSuffix(path, "/") {
		return "", errors.New("path must start with / and contain only letters, numbers, dot, underscore, slash, or hyphen")
	}
	return path, nil
}

func ValidateParameter(kind, description, value string) error {
	if kind != ParameterPlainText && kind != ParameterSecret {
		return errors.New("type must be PlainText or Secret")
	}
	if utf8.RuneCountInString(description) > 500 || strings.IndexFunc(description, unicode.IsControl) >= 0 {
		return errors.New("description must contain at most 500 characters without control characters")
	}
	if len(value) == 0 || len(value) > 64<<10 || strings.ContainsRune(value, '\x00') {
		return errors.New("value must contain between 1 byte and 64 KiB without null bytes")
	}
	return nil
}

func ValidateParameterID(value string) error {
	if !parameterIDPattern.MatchString(value) {
		return errors.New("parameterId must be an opaque Parameter identifier")
	}
	return nil
}

func NormalizeHierarchyName(value string) (string, string, error) {
	for _, r := range value {
		if unicode.IsControl(r) && r != '\t' {
			return "", "", errors.New("name must not contain control characters")
		}
	}
	display := strings.Join(strings.Fields(value), " ")
	if display == "" || utf8.RuneCountInString(display) > 80 {
		return "", "", errors.New("name must contain between 1 and 80 characters")
	}
	return display, strings.ToLower(display), nil
}

func ValidateHierarchyReferences(appID, environmentID string) error {
	if !appIDPattern.MatchString(appID) {
		return errors.New("appId must be an opaque App identifier")
	}
	if !envIDPattern.MatchString(environmentID) {
		return errors.New("environmentId must be an opaque Environment identifier")
	}
	return nil
}

func ValidateAppEnvironmentID(value string) error {
	if !appEnvIDPattern.MatchString(value) {
		return errors.New("appEnvironmentId must be an opaque AppEnvironment identifier")
	}
	return nil
}

func ValidateEnvironmentID(value string) error {
	if !envIDPattern.MatchString(value) {
		return errors.New("environmentId must be an opaque Environment identifier")
	}
	return nil
}

func RuntimeName(publicID string) string {
	if strings.HasPrefix(publicID, "ap-") {
		return publicID
	}
	return "ap-" + strings.ReplaceAll(publicID, "_", "-")
}

func CanonicalJSON(value any) ([]byte, error) { return json.Marshal(value) }

func SHA256(value []byte) []byte {
	h := sha256.Sum256(value)
	return h[:]
}

func NormalizeRuntimeConfig(config RuntimeConfig) RuntimeConfig {
	if config.Replicas == 0 {
		config.Replicas = 1
	}
	if len(config.Ports) == 0 && config.Port != 0 {
		config.Ports = []RuntimePort{{Name: "http", ContainerPort: config.Port, Protocol: PortProtocolTCP}}
	} else if config.Ports == nil {
		config.Ports = []RuntimePort{}
	}
	for index := range config.Ports {
		if config.Ports[index].Protocol == "" {
			config.Ports[index].Protocol = PortProtocolTCP
		}
	}
	if len(config.PublicEndpoints) == 0 && config.Exposure == ExposurePublic && config.Slug != "" {
		config.PublicEndpoints = []PublicEndpoint{{Name: "web", Type: EndpointHTTP, PortName: config.Ports[0].Name, DomainID: "default", HostnameLabel: config.Slug}}
	} else if config.PublicEndpoints == nil {
		config.PublicEndpoints = []PublicEndpoint{}
	}
	for index := range config.PublicEndpoints {
		if config.PublicEndpoints[index].DomainID == "" {
			config.PublicEndpoints[index].DomainID = "default"
		}
	}
	config.Port, config.Exposure, config.Slug = 0, "", ""
	defaultPortName := ""
	if len(config.Ports) > 0 {
		defaultPortName = config.Ports[0].Name
	}
	normalizeProbe := func(probe Probe) Probe {
		if probe.Type == "" {
			probe.Type = ProbeHTTP
		}
		if probe.PortName == "" {
			probe.PortName = defaultPortName
		}
		return probe
	}
	config.Probes.Readiness = normalizeProbe(config.Probes.Readiness)
	config.Probes.Liveness = normalizeProbe(config.Probes.Liveness)
	if config.Probes.Startup.Type == "" && config.Probes.Startup.Path == "" {
		config.Probes.Startup = config.Probes.Readiness
	} else {
		config.Probes.Startup = normalizeProbe(config.Probes.Startup)
	}
	if config.Variables == nil {
		config.Variables = []Variable{}
	}
	if config.Parameters == nil {
		config.Parameters = []ParameterBinding{}
	}
	return config
}

func NormalizeIntent(intent Intent) Intent {
	if len(intent.Ports) == 0 && intent.Port != 0 {
		intent.Ports = []RuntimePort{{Name: "http", ContainerPort: intent.Port, Protocol: PortProtocolTCP}}
	}
	if len(intent.PublicEndpoints) == 0 && intent.Exposure == ExposurePublic && intent.Slug != "" {
		intent.PublicEndpoints = []PublicEndpoint{{Name: "web", Type: EndpointHTTP, PortName: "http", HostnameLabel: intent.Slug}}
	}
	configuration := NormalizeRuntimeConfig(ConfigurationFromIntent(intent))
	intent.Replicas = configuration.Replicas
	intent.Ports = configuration.Ports
	intent.Resources = configuration.Resources
	intent.Probes = configuration.Probes
	intent.PublicEndpoints = configuration.PublicEndpoints
	intent.Variables = configuration.Variables
	intent.Port, intent.Exposure, intent.Slug = 0, "", ""
	return intent
}

func IntentFromConfiguration(image string, configuration RuntimeConfig) Intent {
	configuration = NormalizeRuntimeConfig(configuration)
	return Intent{Image: image, Replicas: configuration.Replicas, Ports: configuration.Ports, Resources: configuration.Resources, Probes: configuration.Probes, PublicEndpoints: configuration.PublicEndpoints, Variables: configuration.Variables}
}

func ConfigurationFromIntent(intent Intent) RuntimeConfig {
	return RuntimeConfig{Replicas: intent.Replicas, Ports: intent.Ports, Resources: intent.Resources, Probes: intent.Probes, PublicEndpoints: intent.PublicEndpoints, Variables: intent.Variables, Parameters: []ParameterBinding{}, Port: intent.Port, Exposure: intent.Exposure, Slug: intent.Slug}
}

func ValidateRuntimeConfig(config RuntimeConfig, maxReplicas int32, maxCPU, maxMemory int64) error {
	if config.Replicas < 1 || config.Replicas > maxReplicas {
		return fmt.Errorf("replicas must be between 1 and %d", maxReplicas)
	}
	if len(config.Ports) < 1 || len(config.Ports) > 8 {
		return errors.New("configuration must contain between 1 and 8 ports")
	}
	portNames := make(map[string]struct{}, len(config.Ports))
	for _, port := range config.Ports {
		if !slugPattern.MatchString(port.Name) || len(port.Name) > 15 {
			return errors.New("port names must be unique lowercase labels of at most 15 characters")
		}
		if _, exists := portNames[port.Name]; exists {
			return errors.New("port names must be unique lowercase labels of at most 15 characters")
		}
		if port.ContainerPort < 1 || port.ContainerPort > 65535 || port.Protocol != PortProtocolTCP {
			return errors.New("ports must use TCP and a container port between 1 and 65535")
		}
		portNames[port.Name] = struct{}{}
	}
	if config.Resources.Requests.CPUMillis < 1 || config.Resources.Limits.CPUMillis < config.Resources.Requests.CPUMillis || config.Resources.Limits.CPUMillis > maxCPU {
		return errors.New("invalid CPU resources")
	}
	if config.Resources.Requests.MemoryMiB < 1 || config.Resources.Limits.MemoryMiB < config.Resources.Requests.MemoryMiB || config.Resources.Limits.MemoryMiB > maxMemory {
		return errors.New("invalid memory resources")
	}
	for _, probe := range []Probe{config.Probes.Startup, config.Probes.Readiness, config.Probes.Liveness} {
		if _, exists := portNames[probe.PortName]; !exists {
			return errors.New("probes must reference an existing port name")
		}
		if probe.Type == ProbeHTTP && !validPath(probe.Path) {
			return errors.New("HTTP probe paths must be absolute")
		}
		if probe.Type == ProbeTCP && probe.Path != "" {
			return errors.New("TCP probes cannot declare a path")
		}
		if probe.Type != ProbeHTTP && probe.Type != ProbeTCP {
			return errors.New("probe type must be HTTP or TCP")
		}
	}
	if len(config.PublicEndpoints) > 2 {
		return errors.New("configuration must contain at most 2 public endpoints")
	}
	endpointNames := make(map[string]struct{}, len(config.PublicEndpoints))
	endpointTypes := make(map[string]struct{}, len(config.PublicEndpoints))
	for _, endpoint := range config.PublicEndpoints {
		if !slugPattern.MatchString(endpoint.Name) || len(endpoint.Name) > 15 {
			return errors.New("public endpoint names must be unique lowercase labels of at most 15 characters")
		}
		if _, exists := endpointNames[endpoint.Name]; exists {
			return errors.New("public endpoint names must be unique lowercase labels of at most 15 characters")
		}
		if _, exists := endpointTypes[endpoint.Type]; exists || (endpoint.Type != EndpointHTTP && endpoint.Type != EndpointTCP) {
			return errors.New("configuration supports at most one HTTP and one TCP public endpoint")
		}
		if _, exists := portNames[endpoint.PortName]; !exists {
			return errors.New("public endpoints must reference an existing port name")
		}
		if len(endpoint.HostnameLabel) == 0 || len(endpoint.HostnameLabel) > 63 || !slugPattern.MatchString(endpoint.HostnameLabel) {
			return errors.New("public endpoint hostnameLabel must be a lowercase DNS label")
		}
		if len(endpoint.DomainID) == 0 || len(endpoint.DomainID) > 63 || !slugPattern.MatchString(endpoint.DomainID) {
			return errors.New("public endpoint domainId must be a lowercase DNS label")
		}
		if endpoint.Type == EndpointHTTP && endpoint.ExternalPort != 0 {
			return errors.New("HTTP public endpoints cannot declare an external port")
		}
		if endpoint.Type == EndpointTCP && endpoint.ExternalPort != 0 && (endpoint.ExternalPort < 1 || endpoint.ExternalPort > 65535) {
			return errors.New("TCP public endpoint external port is invalid")
		}
		endpointNames[endpoint.Name] = struct{}{}
		endpointTypes[endpoint.Type] = struct{}{}
	}
	if len(config.Variables)+len(config.Parameters) > 100 {
		return errors.New("configuration must contain at most 100 variables and parameters")
	}
	seen := make(map[string]struct{}, len(config.Variables)+len(config.Parameters))
	for _, variable := range config.Variables {
		if !variableNamePattern.MatchString(variable.Name) || len(variable.Name) > 253 {
			return errors.New("variable names must be valid environment variable identifiers")
		}
		if len(variable.Value) > 4096 || strings.ContainsAny(variable.Value, "\x00\r\n") {
			return errors.New("variable values must be at most 4096 characters without line breaks")
		}
		if _, exists := seen[variable.Name]; exists {
			return errors.New("variable names must be unique")
		}
		seen[variable.Name] = struct{}{}
	}
	for _, binding := range config.Parameters {
		if !variableNamePattern.MatchString(binding.Name) || len(binding.Name) > 253 {
			return errors.New("parameter binding names must be valid environment variable identifiers")
		}
		if err := ValidateParameterID(binding.ParameterPublicID); err != nil || binding.ParameterVersion < 1 {
			return errors.New("parameter bindings must reference an opaque Parameter identifier and positive version")
		}
		if _, exists := seen[binding.Name]; exists {
			return errors.New("variable and parameter binding names must be unique")
		}
		seen[binding.Name] = struct{}{}
	}
	return nil
}

func ValidateIntent(intent Intent, maxReplicas int32, maxCPU, maxMemory int64) error {
	if !imagePattern.MatchString(intent.Image) {
		return errors.New("image must use an immutable sha256 digest")
	}
	return ValidateRuntimeConfig(ConfigurationFromIntent(intent), maxReplicas, maxCPU, maxMemory)
}

// ValidateWorkloadConfiguration keeps Stateful and Stateless combinations explicit.
func ValidateWorkloadConfiguration(kind WorkloadKind, configuration RuntimeConfig, volume *VolumeRequest) error {
	switch kind {
	case WorkloadStateless:
		if volume != nil {
			return errors.New("stateless workloads cannot declare persistent storage")
		}
	case WorkloadStateful:
		if configuration.Replicas != 1 {
			return errors.New("stateful workloads require exactly one replica")
		}
		if volume == nil {
			return errors.New("stateful workloads require persistent storage")
		}
		if err := ValidateVolumeRequest(*volume); err != nil {
			return err
		}
	default:
		return errors.New("workloadKind must be Stateless or Stateful")
	}
	return nil
}

func ValidateVolumeRequest(volume VolumeRequest) error {
	if !storageProfilePattern.MatchString(volume.StorageProfileID) || len(volume.StorageProfileID) > 63 {
		return errors.New("storageProfileId must be a portable storage profile identifier")
	}
	if volume.SizeGiB < 1 {
		return errors.New("sizeGiB must be positive")
	}
	if volume.MountPath == "" || len(volume.MountPath) > 255 || !strings.HasPrefix(volume.MountPath, "/") || path.Clean(volume.MountPath) != volume.MountPath || volume.MountPath == "/" {
		return errors.New("mountPath must be an absolute normalized path below the filesystem root")
	}
	for _, reserved := range []string{"/dev", "/proc", "/sys"} {
		if volume.MountPath == reserved || strings.HasPrefix(volume.MountPath, reserved+"/") {
			return errors.New("mountPath uses a reserved runtime path")
		}
	}
	return nil
}

func ValidateVolumeExpansion(currentSizeGiB, requestedSizeGiB, maximumSizeGiB int64) error {
	if requestedSizeGiB <= currentSizeGiB {
		return errors.New("volume size can only increase")
	}
	if requestedSizeGiB > maximumSizeGiB {
		return errors.New("requested size exceeds the storage profile limit")
	}
	return nil
}

func ValidateVolumeRemoval(volume AppVolume) error {
	if volume.DesiredState != VolumeDesiredDeleted {
		return errors.New("volume removal was not requested")
	}
	if volume.Attached {
		return errors.New("volume must be detached before removal")
	}
	return nil
}

func validPath(path string) bool {
	return len(path) > 0 && len(path) <= 2048 && strings.HasPrefix(path, "/") && !strings.ContainsAny(path, "\r\n")
}
