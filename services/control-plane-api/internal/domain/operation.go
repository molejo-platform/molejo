package domain

import "time"

const (
	OperationPending         = "Pending"
	OperationRunning         = "Running"
	OperationSucceeded       = "Succeeded"
	OperationFailed          = "Failed"
	OperationEnsureWorkspace = "EnsureWorkspace"
	OperationApplyDeployment = "ApplyDeployment"
	OperationDeleteAppEnv    = "DeleteAppEnvironment"
	OperationEnsureVolume    = "EnsureVolume"
	OperationExpandVolume    = "ExpandVolume"
	OperationDeleteVolume    = "DeleteVolume"
)

type Operation struct {
	ID                     int64      `json:"-"`
	ClusterID              int64      `json:"-"`
	PublicID               string     `json:"id"`
	AppEnvironmentID       int64      `json:"-"`
	AppEnvironmentPublicID string     `json:"appEnvironmentId,omitempty"`
	DeploymentID           int64      `json:"-"`
	DeploymentPublicID     string     `json:"deploymentId,omitempty"`
	AppVolumeID            int64      `json:"-"`
	AppVolumePublicID      string     `json:"appVolumeId,omitempty"`
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
