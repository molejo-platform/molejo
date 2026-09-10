package domain

import (
	"reflect"
	"time"
)

const (
	DeploymentChangeInitial      = "InitialDeployment"
	DeploymentChangeRelease      = "Release"
	DeploymentChangeScale        = "Scale"
	DeploymentChangeNetwork      = "Network"
	DeploymentChangeHealthChecks = "HealthChecks"
	DeploymentChangeResources    = "Resources"
	DeploymentChangeVariables    = "Variables"
	DeploymentChangeSecrets      = "Secrets"
)

type ConfigurationRevision struct {
	AppEnvironmentID       int64         `json:"-"`
	AppEnvironmentPublicID string        `json:"appEnvironmentId"`
	Version                int64         `json:"version"`
	Configuration          RuntimeConfig `json:"configuration"`
	CreatedBy              string        `json:"createdBy"`
	CreatedAt              time.Time     `json:"createdAt"`
}

type Deployment struct {
	ID                     int64          `json:"-"`
	PublicID               string         `json:"id"`
	WorkspaceID            int64          `json:"-"`
	AppEnvironmentID       int64          `json:"-"`
	AppID                  int64          `json:"-"`
	AppEnvironmentPublicID string         `json:"appEnvironmentId"`
	ReleasePublicID        string         `json:"releaseId"`
	Image                  string         `json:"-"`
	ConfigurationVersion   int64          `json:"configurationVersion"`
	Configuration          RuntimeConfig  `json:"configuration"`
	WorkloadKind           WorkloadKind   `json:"workloadKind"`
	AppVolumePublicID      string         `json:"appVolumeId,omitempty"`
	RequestedBy            ActorReference `json:"requestedBy"`
	State                  string         `json:"state"`
	Message                string         `json:"message,omitempty"`
	ObservedRelease        string         `json:"-"`
	CreatedAt              time.Time      `json:"createdAt"`
	UpdatedAt              time.Time      `json:"updatedAt"`
}

// DeploymentRequest is the provider-neutral intent used to create an immutable Deployment.
type DeploymentRequest struct {
	WorkspaceID                       int64
	AppEnvironmentPublicID            string
	DeploymentPublicID                string
	ReleasePublicID                   string
	ConfigurationVersion              int64
	ExpectedVersion                   int64
	ExpectedCurrentDeploymentPublicID string
	IdempotencyHash                   []byte
	PayloadHash                       []byte
}

type DeploymentTarget struct {
	ReleasePublicID      string `json:"releaseId"`
	ConfigurationVersion int64  `json:"configurationVersion"`
}

type DeploymentPreview struct {
	Current         *DeploymentTarget `json:"current,omitempty"`
	Target          DeploymentTarget  `json:"target"`
	Changes         []string          `json:"changes"`
	RolloutRequired bool              `json:"rolloutRequired"`
}

func PreviewDeployment(current *Deployment, releasePublicID string, revision ConfigurationRevision) DeploymentPreview {
	preview := DeploymentPreview{
		Target:          DeploymentTarget{ReleasePublicID: releasePublicID, ConfigurationVersion: revision.Version},
		Changes:         []string{},
		RolloutRequired: true,
	}
	if current == nil {
		preview.Changes = append(preview.Changes, DeploymentChangeInitial)
		return preview
	}
	preview.Current = &DeploymentTarget{ReleasePublicID: current.ReleasePublicID, ConfigurationVersion: current.ConfigurationVersion}
	if current.ReleasePublicID != releasePublicID {
		preview.Changes = append(preview.Changes, DeploymentChangeRelease)
	}
	before := NormalizeRuntimeConfig(current.Configuration)
	after := NormalizeRuntimeConfig(revision.Configuration)
	if before.Replicas != after.Replicas {
		preview.Changes = append(preview.Changes, DeploymentChangeScale)
	}
	if !reflect.DeepEqual(before.Ports, after.Ports) || !reflect.DeepEqual(before.PublicEndpoints, after.PublicEndpoints) {
		preview.Changes = append(preview.Changes, DeploymentChangeNetwork)
	}
	if !reflect.DeepEqual(before.Probes, after.Probes) {
		preview.Changes = append(preview.Changes, DeploymentChangeHealthChecks)
	}
	if !reflect.DeepEqual(before.Resources, after.Resources) {
		preview.Changes = append(preview.Changes, DeploymentChangeResources)
	}
	if !reflect.DeepEqual(before.Variables, after.Variables) {
		preview.Changes = append(preview.Changes, DeploymentChangeVariables)
	}
	if !reflect.DeepEqual(before.Parameters, after.Parameters) {
		preview.Changes = append(preview.Changes, DeploymentChangeSecrets)
	}
	preview.RolloutRequired = len(preview.Changes) > 0
	return preview
}
