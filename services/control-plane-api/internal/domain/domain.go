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
	OperationSuperseded      = "Superseded"
	OperationEnsureWorkspace = "EnsureWorkspace"
)

var (
	namePattern  = regexp.MustCompile(`^[a-z0-9](?:[-a-z0-9]*[a-z0-9])?$`)
	imagePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._:/-]*@sha256:[a-f0-9]{64}$`)
	slugPattern  = regexp.MustCompile(`^[a-z0-9](?:[-a-z0-9]*[a-z0-9])?$`)
	appIDPattern = regexp.MustCompile(`^app-[a-z2-7]{20}$`)
	envIDPattern = regexp.MustCompile(`^env-[a-z2-7]{20}$`)
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

type Intent struct {
	Name          string    `json:"name"`
	AppID         string    `json:"appId,omitempty"`
	EnvironmentID string    `json:"environmentId,omitempty"`
	Image         string    `json:"image"`
	Replicas      int32     `json:"replicas"`
	Port          int32     `json:"port"`
	Resources     Resources `json:"resources"`
	Probes        Probes    `json:"probes"`
	Exposure      string    `json:"exposure"`
	Slug          string    `json:"slug,omitempty"`
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

type Deployment struct {
	ID                  int64      `json:"-"`
	PublicID            string     `json:"id"`
	WorkspaceID         int64      `json:"-"`
	ProjectID           int64      `json:"-"`
	AppID               int64      `json:"-"`
	EnvironmentID       int64      `json:"-"`
	ProjectPublicID     string     `json:"projectId"`
	AppPublicID         string     `json:"appId"`
	EnvironmentPublicID string     `json:"environmentId"`
	RuntimeName         string     `json:"-"`
	Intent              Intent     `json:"intent"`
	DesiredVersion      int64      `json:"version"`
	State               string     `json:"state"`
	ObservedVersion     int64      `json:"observedVersion,omitempty"`
	ObservedRelease     string     `json:"-"`
	Message             string     `json:"message,omitempty"`
	CreatedAt           time.Time  `json:"createdAt"`
	UpdatedAt           time.Time  `json:"updatedAt"`
	DeletionRequestedAt *time.Time `json:"-"`
	DeletedAt           *time.Time `json:"-"`
}

type Operation struct {
	ID                 int64      `json:"-"`
	PublicID           string     `json:"id"`
	DeploymentID       int64      `json:"-"`
	WorkspaceID        int64      `json:"-"`
	DeploymentPublicID string     `json:"deploymentId,omitempty"`
	ActorID            int64      `json:"-"`
	Kind               string     `json:"kind"`
	Status             string     `json:"status"`
	DesiredVersion     int64      `json:"desiredVersion"`
	Attempts           int        `json:"attempts"`
	Intent             Intent     `json:"-"`
	WorkerID           string     `json:"-"`
	FencingToken       int64      `json:"-"`
	LeaseUntil         *time.Time `json:"-"`
	ErrorCode          string     `json:"errorCode,omitempty"`
	ErrorMessage       string     `json:"errorMessage,omitempty"`
	CreatedAt          time.Time  `json:"createdAt"`
	UpdatedAt          time.Time  `json:"updatedAt"`
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

func NormalizeIntent(intent Intent) Intent {
	if intent.Replicas == 0 {
		intent.Replicas = 1
	}
	if intent.Exposure == "" {
		intent.Exposure = ExposurePrivate
	}
	return intent
}

func ValidateIntent(intent Intent, maxReplicas int32, maxCPU, maxMemory int64) error {
	if len(intent.Name) == 0 || len(intent.Name) > 63 || !namePattern.MatchString(intent.Name) {
		return errors.New("name must be a lowercase DNS label")
	}
	if !imagePattern.MatchString(intent.Image) {
		return errors.New("image must use an immutable sha256 digest")
	}
	if (intent.AppID != "" || intent.EnvironmentID != "") && ValidateHierarchyReferences(intent.AppID, intent.EnvironmentID) != nil {
		return errors.New("appId and environmentId must be valid and provided together")
	}
	if intent.Replicas < 1 || intent.Replicas > maxReplicas {
		return fmt.Errorf("replicas must be between 1 and %d", maxReplicas)
	}
	if intent.Port < 1 || intent.Port > 65535 {
		return errors.New("port must be between 1 and 65535")
	}
	if intent.Resources.Requests.CPUMillis < 1 || intent.Resources.Limits.CPUMillis < intent.Resources.Requests.CPUMillis || intent.Resources.Limits.CPUMillis > maxCPU {
		return errors.New("invalid CPU resources")
	}
	if intent.Resources.Requests.MemoryMiB < 1 || intent.Resources.Limits.MemoryMiB < intent.Resources.Requests.MemoryMiB || intent.Resources.Limits.MemoryMiB > maxMemory {
		return errors.New("invalid memory resources")
	}
	if !validPath(intent.Probes.Liveness.Path) || !validPath(intent.Probes.Readiness.Path) {
		return errors.New("probe paths must be absolute HTTP paths")
	}
	if intent.Exposure != ExposurePrivate && intent.Exposure != ExposurePublic {
		return errors.New("exposure must be Private or Public")
	}
	if intent.Exposure == ExposurePrivate && intent.Slug != "" {
		return errors.New("slug must be omitted for private exposure")
	}
	if intent.Exposure == ExposurePublic && (len(intent.Slug) == 0 || len(intent.Slug) > 63 || !slugPattern.MatchString(intent.Slug)) {
		return errors.New("slug is required and must be a lowercase DNS label for public exposure")
	}
	return nil
}

func validPath(path string) bool {
	return len(path) > 0 && len(path) <= 2048 && strings.HasPrefix(path, "/") && !strings.ContainsAny(path, "\r\n")
}
