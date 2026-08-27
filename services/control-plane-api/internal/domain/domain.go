package domain

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	ExposurePrivate          = "Private"
	ExposurePublic           = "Public"
	StatePending             = "Pending"
	StateProgressing         = "Progressing"
	StateReady               = "Ready"
	StateDegraded            = "Degraded"
	StateUnknown             = "Unknown"
	Unknown                  = StateUnknown
	Progressing              = StateProgressing
	Ready                    = StateReady
	Degraded                 = StateDegraded
	OperationPending         = "Pending"
	OperationRunning         = "Running"
	OperationSucceeded       = "Succeeded"
	OperationFailed          = "Failed"
	OperationEnsureWorkspace = "EnsureWorkspace"
	OperationApplyDeployment = "ApplyDeployment"
	OperationDeleteAppEnv    = "DeleteAppEnvironment"
)

var (
	imagePattern        = regexp.MustCompile(`^[a-z0-9][a-z0-9._:/-]*@sha256:[a-f0-9]{64}$`)
	slugPattern         = regexp.MustCompile(`^[a-z0-9](?:[-a-z0-9]*[a-z0-9])?$`)
	appIDPattern        = regexp.MustCompile(`^app-[a-z2-7]{20}$`)
	envIDPattern        = regexp.MustCompile(`^env-[a-z2-7]{20}$`)
	appEnvIDPattern     = regexp.MustCompile(`^aev-[a-z2-7]{20}$`)
	variableNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
)

type ResourceValues struct {
	CPUMillis int64 `json:"cpuMillis"`
	MemoryMiB int64 `json:"memoryMiB"`
}

type Resources struct {
	Requests ResourceValues `json:"requests"`
	Limits   ResourceValues `json:"limits"`
}

type Probe struct {
	Path string `json:"path"`
}

type Probes struct {
	Liveness  Probe `json:"liveness"`
	Readiness Probe `json:"readiness"`
}

type Variable struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type RuntimeConfig struct {
	Replicas  int32      `json:"replicas"`
	Port      int32      `json:"port"`
	Resources Resources  `json:"resources"`
	Probes    Probes     `json:"probes"`
	Exposure  string     `json:"exposure"`
	Slug      string     `json:"slug,omitempty"`
	Variables []Variable `json:"variables"`
}

type Intent struct {
	Image     string     `json:"image"`
	Replicas  int32      `json:"replicas"`
	Port      int32      `json:"port"`
	Resources Resources  `json:"resources"`
	Probes    Probes     `json:"probes"`
	Exposure  string     `json:"exposure"`
	Slug      string     `json:"slug,omitempty"`
	Variables []Variable `json:"variables"`
}

type Actor struct {
	ID   int64
	Key  string
	Role string
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
	ID                        int64         `json:"-"`
	PublicID                  string        `json:"id"`
	WorkspaceID               int64         `json:"-"`
	ProjectID                 int64         `json:"-"`
	AppID                     int64         `json:"-"`
	EnvironmentID             int64         `json:"-"`
	ProjectPublicID           string        `json:"projectId"`
	AppPublicID               string        `json:"appId"`
	EnvironmentPublicID       string        `json:"environmentId"`
	EnvironmentName           string        `json:"environmentName"`
	SourceBranch              string        `json:"branch"`
	RuntimeName               string        `json:"-"`
	Configuration             RuntimeConfig `json:"configuration"`
	ConfigurationVersion      int64         `json:"configurationVersion"`
	Version                   int64         `json:"version"`
	DesiredDeploymentPublicID string        `json:"desiredDeploymentId,omitempty"`
	CurrentDeploymentPublicID string        `json:"currentDeploymentId,omitempty"`
	CurrentReleasePublicID    string        `json:"currentReleaseId,omitempty"`
	State                     string        `json:"state"`
	Message                   string        `json:"message,omitempty"`
	CreatedAt                 time.Time     `json:"createdAt"`
	UpdatedAt                 time.Time     `json:"updatedAt"`
	DeletionRequestedAt       *time.Time    `json:"-"`
	ArchivedAt                *time.Time    `json:"-"`
}

type Deployment struct {
	ID                     int64         `json:"-"`
	PublicID               string        `json:"id"`
	WorkspaceID            int64         `json:"-"`
	AppEnvironmentID       int64         `json:"-"`
	AppID                  int64         `json:"-"`
	AppEnvironmentPublicID string        `json:"appEnvironmentId"`
	ReleasePublicID        string        `json:"releaseId"`
	Image                  string        `json:"-"`
	ConfigurationVersion   int64         `json:"configurationVersion"`
	Configuration          RuntimeConfig `json:"configuration"`
	State                  string        `json:"state"`
	Message                string        `json:"message,omitempty"`
	ObservedRelease        string        `json:"-"`
	CreatedAt              time.Time     `json:"createdAt"`
	UpdatedAt              time.Time     `json:"updatedAt"`
}

type Operation struct {
	ID                     int64      `json:"-"`
	PublicID               string     `json:"id"`
	AppEnvironmentID       int64      `json:"-"`
	AppEnvironmentPublicID string     `json:"appEnvironmentId,omitempty"`
	DeploymentID           int64      `json:"-"`
	DeploymentPublicID     string     `json:"deploymentId,omitempty"`
	WorkspaceID            int64      `json:"-"`
	ActorID                int64      `json:"-"`
	Kind                   string     `json:"kind"`
	Status                 string     `json:"status"`
	DesiredVersion         int64      `json:"desiredVersion"`
	Attempts               int        `json:"attempts"`
	WorkerID               string     `json:"-"`
	FencingToken           int64      `json:"-"`
	LeaseUntil             *time.Time `json:"-"`
	ErrorCode              string     `json:"errorCode,omitempty"`
	ErrorMessage           string     `json:"errorMessage,omitempty"`
	CreatedAt              time.Time  `json:"createdAt"`
	UpdatedAt              time.Time  `json:"updatedAt"`
}

func NewPublicID(prefix string) (string, error) {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	encoded := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b)
	return prefix + "-" + strings.ToLower(encoded), nil
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
	if config.Exposure == "" {
		config.Exposure = ExposurePrivate
	}
	if config.Variables == nil {
		config.Variables = []Variable{}
	}
	return config
}

func NormalizeIntent(intent Intent) Intent {
	configuration := NormalizeRuntimeConfig(ConfigurationFromIntent(intent))
	intent.Replicas = configuration.Replicas
	intent.Port = configuration.Port
	intent.Resources = configuration.Resources
	intent.Probes = configuration.Probes
	intent.Exposure = configuration.Exposure
	intent.Slug = configuration.Slug
	intent.Variables = configuration.Variables
	return intent
}

func IntentFromConfiguration(image string, configuration RuntimeConfig) Intent {
	configuration = NormalizeRuntimeConfig(configuration)
	return Intent{Image: image, Replicas: configuration.Replicas, Port: configuration.Port, Resources: configuration.Resources, Probes: configuration.Probes, Exposure: configuration.Exposure, Slug: configuration.Slug, Variables: configuration.Variables}
}

func ConfigurationFromIntent(intent Intent) RuntimeConfig {
	return RuntimeConfig{Replicas: intent.Replicas, Port: intent.Port, Resources: intent.Resources, Probes: intent.Probes, Exposure: intent.Exposure, Slug: intent.Slug, Variables: intent.Variables}
}

func ValidateRuntimeConfig(config RuntimeConfig, maxReplicas int32, maxCPU, maxMemory int64) error {
	if config.Replicas < 1 || config.Replicas > maxReplicas {
		return fmt.Errorf("replicas must be between 1 and %d", maxReplicas)
	}
	if config.Port < 1 || config.Port > 65535 {
		return errors.New("port must be between 1 and 65535")
	}
	if config.Resources.Requests.CPUMillis < 1 || config.Resources.Limits.CPUMillis < config.Resources.Requests.CPUMillis || config.Resources.Limits.CPUMillis > maxCPU {
		return errors.New("invalid CPU resources")
	}
	if config.Resources.Requests.MemoryMiB < 1 || config.Resources.Limits.MemoryMiB < config.Resources.Requests.MemoryMiB || config.Resources.Limits.MemoryMiB > maxMemory {
		return errors.New("invalid memory resources")
	}
	if !validPath(config.Probes.Liveness.Path) || !validPath(config.Probes.Readiness.Path) {
		return errors.New("probe paths must be absolute HTTP paths")
	}
	if config.Exposure != ExposurePrivate && config.Exposure != ExposurePublic {
		return errors.New("exposure must be Private or Public")
	}
	if config.Exposure == ExposurePrivate && config.Slug != "" {
		return errors.New("slug must be omitted for private exposure")
	}
	if config.Exposure == ExposurePublic && (len(config.Slug) == 0 || len(config.Slug) > 63 || !slugPattern.MatchString(config.Slug)) {
		return errors.New("slug is required and must be a lowercase DNS label for public exposure")
	}
	seen := make(map[string]struct{}, len(config.Variables))
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
	return nil
}

func ValidateIntent(intent Intent, maxReplicas int32, maxCPU, maxMemory int64) error {
	if !imagePattern.MatchString(intent.Image) {
		return errors.New("image must use an immutable sha256 digest")
	}
	return ValidateRuntimeConfig(ConfigurationFromIntent(intent), maxReplicas, maxCPU, maxMemory)
}

func validPath(path string) bool {
	return len(path) > 0 && len(path) <= 2048 && strings.HasPrefix(path, "/") && !strings.ContainsAny(path, "\r\n")
}
