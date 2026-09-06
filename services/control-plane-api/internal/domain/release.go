package domain

import (
	"errors"
	"regexp"
	"strings"
	"time"
)

const (
	ReleaseAvailable          = "Available"
	ReleaseExpired            = "Expired"
	ReleaseOriginExternal     = "External"
	ReleaseOriginManagedBuild = "ManagedBuild"
	ReleaseProvenanceDeclared = "Declared"
)

var (
	digestPattern     = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)
	repositoryPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._:/-]*$`)
)

type Release struct {
	ID                     int64          `json:"-"`
	PublicID               string         `json:"id"`
	WorkspaceID            int64          `json:"-"`
	ProjectID              int64          `json:"-"`
	AppID                  int64          `json:"-"`
	AppEnvironmentID       int64          `json:"-"`
	ProjectPublicID        string         `json:"projectId"`
	AppPublicID            string         `json:"appId"`
	AppEnvironmentPublicID string         `json:"appEnvironmentId,omitempty"`
	BuildPublicID          string         `json:"buildId,omitempty"`
	OriginKind             string         `json:"origin"`
	SourceProvider         string         `json:"sourceProvider"`
	SourceRepository       string         `json:"sourceRepository"`
	SourceRevision         string         `json:"sourceRevision"`
	SourceRef              string         `json:"sourceRef,omitempty"`
	ProducerKind           string         `json:"producer"`
	ProducerExternalID     string         `json:"producerExternalId,omitempty"`
	ProducerURL            string         `json:"producerUrl,omitempty"`
	ProvenanceStatus       string         `json:"provenanceStatus"`
	CreatedBy              ActorReference `json:"createdBy"`
	SourceBranch           string         `json:"branch,omitempty"`
	CommitSHA              string         `json:"commitSha,omitempty"`
	CommitTitle            string         `json:"commitTitle"`
	CommitAuthorName       string         `json:"commitAuthorName"`
	CommitAuthorLogin      string         `json:"commitAuthorLogin"`
	CommittedAt            *time.Time     `json:"committedAt,omitempty"`
	TriggerType            string         `json:"trigger,omitempty"`
	Image                  string         `json:"image"`
	Platform               string         `json:"platform,omitempty"`
	AvailabilityStatus     string         `json:"availabilityStatus"`
	ExpiredAt              *time.Time     `json:"expiredAt,omitempty"`
	CreatedAt              time.Time      `json:"createdAt"`
}

func ReleaseImageReference(repository, digest string) (string, error) {
	if !repositoryPattern.MatchString(repository) || strings.Contains(repository, "@") {
		return "", errors.New("image repository is invalid")
	}
	lastSlash := strings.LastIndexByte(repository, '/')
	if strings.Contains(repository[lastSlash+1:], ":") {
		return "", errors.New("image repository must not contain a tag")
	}
	if !digestPattern.MatchString(digest) {
		return "", errors.New("image digest is invalid")
	}
	return repository + "@" + digest, nil
}
