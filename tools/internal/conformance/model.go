package conformance

import "time"

const (
	ReportSchemaVersion = "molejo-conformance-report.v1alpha1"
	AlphaCoreProfileID  = "alpha-core"
	AlphaCoreVersion    = "v1"
)

type Status string

const (
	StatusPending Status = "PENDING"
	StatusPass    Status = "PASS"
	StatusFail    Status = "FAIL"
	StatusBlocked Status = "BLOCKED"
	StatusSkipped Status = "SKIPPED"
)

type Target struct {
	Endpoint           string `json:"endpoint"`
	ClusterID          string `json:"clusterId"`
	ExpectedClusterUID string `json:"expectedClusterUid,omitempty"`
	KubeContext        string `json:"kubeContext,omitempty"`
	Disposable         bool   `json:"disposable"`
	WorkspaceID        string `json:"workspaceId,omitempty"`
}

type TargetEvidence struct {
	Endpoint          string   `json:"endpoint"`
	ClusterID         string   `json:"clusterId"`
	ClusterUID        string   `json:"clusterUid,omitempty"`
	KubeContext       string   `json:"kubeContext,omitempty"`
	KubernetesVersion string   `json:"kubernetesVersion,omitempty"`
	AgentVersion      string   `json:"agentVersion,omitempty"`
	Capabilities      []string `json:"capabilities,omitempty"`
	Disposable        bool     `json:"disposable"`
}

type Profile struct {
	ID          string
	Version     string
	Description string
	Scenarios   []Scenario
}

type Scenario struct {
	ID          string
	Description string
	Required    bool
	Timeout     time.Duration
	Run         ScenarioFunc
}

type ScenarioFunc func(*ScenarioContext) error

type ProfileReference struct {
	ID      string `json:"id"`
	Version string `json:"version"`
}

type AssertionResult struct {
	ID         string    `json:"id"`
	Status     Status    `json:"status"`
	Observed   string    `json:"observed,omitempty"`
	FinishedAt time.Time `json:"finishedAt"`
}

type ScenarioResult struct {
	ID          string            `json:"id"`
	Description string            `json:"description"`
	Required    bool              `json:"required"`
	Status      Status            `json:"status"`
	Reason      string            `json:"reason,omitempty"`
	StartedAt   time.Time         `json:"startedAt"`
	FinishedAt  time.Time         `json:"finishedAt,omitempty"`
	DurationMS  int64             `json:"durationMs,omitempty"`
	Assertions  []AssertionResult `json:"assertions,omitempty"`
}

type ResourceRecord struct {
	Kind        string            `json:"kind"`
	ID          string            `json:"id"`
	RunID       string            `json:"runId"`
	CleanupPath string            `json:"cleanupPath,omitempty"`
	Headers     map[string]string `json:"headers,omitempty"`
	Required    bool              `json:"cleanupRequired"`
	State       string            `json:"state"`
}

type CleanupResult struct {
	Status     Status           `json:"status"`
	StartedAt  time.Time        `json:"startedAt,omitempty"`
	FinishedAt time.Time        `json:"finishedAt,omitempty"`
	Resources  []ResourceRecord `json:"resources,omitempty"`
	Reason     string           `json:"reason,omitempty"`
}

type RunOutputs struct {
	WorkspaceID      string `json:"workspaceId,omitempty"`
	Namespace        string `json:"namespace,omitempty"`
	AppEnvironmentID string `json:"appEnvironmentId,omitempty"`
}

type Report struct {
	SchemaVersion string           `json:"schemaVersion"`
	RunnerVersion string           `json:"runnerVersion"`
	RunID         string           `json:"runId"`
	Profile       ProfileReference `json:"profile"`
	Target        TargetEvidence   `json:"target"`
	Status        Status           `json:"status"`
	Reason        string           `json:"reason,omitempty"`
	StartedAt     time.Time        `json:"startedAt"`
	FinishedAt    time.Time        `json:"finishedAt,omitempty"`
	DurationMS    int64            `json:"durationMs,omitempty"`
	Scenarios     []ScenarioResult `json:"scenarios"`
	Outputs       RunOutputs       `json:"outputs,omitempty"`
	Resources     []ResourceRecord `json:"resources,omitempty"`
	Cleanup       CleanupResult    `json:"cleanup"`
}
