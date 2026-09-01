package domain

import "time"

type DeliveryPolicy struct {
	AppEnvironmentPublicID string    `json:"appEnvironmentId"`
	PushEnabled            bool      `json:"pushEnabled"`
	ReleaseEnabled         bool      `json:"releaseEnabled"`
	Version                int64     `json:"version"`
	UpdatedAt              time.Time `json:"updatedAt"`
}

type GitHubDelivery struct {
	ID                     int64
	DeliveryID             string
	EventType              string
	Action                 string
	InstallationExternalID int64
	RepositoryID           int64
	RepositoryFullName     string
	SourceBranch           string
	SourceRef              string
	CommitSHA              string
	TagName                string
	RepositoryIDs          []int64
	PayloadHash            []byte
	Status                 string
	Attempts               int
	WorkerID               string
	FencingToken           int64
	LeaseUntil             *time.Time
}

type CommitMetadata struct {
	SHA         string
	Title       string
	AuthorName  string
	AuthorLogin string
	CommittedAt *time.Time
}

type DeliveryCandidate struct {
	WorkspaceID            int64
	ProjectID              int64
	ProjectPublicID        string
	AppID                  int64
	AppPublicID            string
	AppEnvironmentID       int64
	AppEnvironmentPublicID string
	SourceBranch           string
	RequestedByActorID     int64
	PolicyVersion          int64
}

type DeliveryTarget struct {
	ID                     int64
	PublicID               string
	GitHubDeliveryID       int64
	WorkspaceID            int64
	ProjectID              int64
	AppID                  int64
	AppEnvironmentID       int64
	AppEnvironmentPublicID string
	RequestedByActorID     int64
	TriggerType            string
	SourceBranch           string
	CommitSHA              string
	BuildID                int64
	BuildPublicID          string
	ReleasePublicID        string
	DeploymentID           int64
	DeploymentPublicID     string
	Status                 string
	BuildStatus            string
	DeploymentStatus       string
}
