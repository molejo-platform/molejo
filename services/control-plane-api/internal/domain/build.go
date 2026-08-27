package domain

import (
	"errors"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	BuildPending   = "Pending"
	BuildRunning   = "Running"
	BuildSucceeded = "Succeeded"
	BuildFailed    = "Failed"
	BuildPlatform  = "linux/amd64"
)

var (
	commitSHAPattern  = regexp.MustCompile(`^[a-f0-9]{40}$`)
	digestPattern     = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)
	repositoryPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._:/-]*$`)
)

type Build struct {
	ID                     int64      `json:"-"`
	PublicID               string     `json:"id"`
	WorkspaceID            int64      `json:"-"`
	ProjectID              int64      `json:"-"`
	AppID                  int64      `json:"-"`
	ProjectPublicID        string     `json:"projectId"`
	AppPublicID            string     `json:"appId"`
	InstallationExternalID int64      `json:"-"`
	RepositoryID           int64      `json:"-"`
	RepositoryFullName     string     `json:"repository"`
	SourceBranch           string     `json:"branch"`
	CommitSHA              string     `json:"commitSha"`
	Platform               string     `json:"platform"`
	Status                 string     `json:"status"`
	Attempts               int        `json:"attempts"`
	WorkerID               string     `json:"-"`
	FencingToken           int64      `json:"-"`
	LeaseUntil             *time.Time `json:"-"`
	ErrorCode              string     `json:"errorCode,omitempty"`
	ErrorMessage           string     `json:"errorMessage,omitempty"`
	CreatedAt              time.Time  `json:"createdAt"`
	UpdatedAt              time.Time  `json:"updatedAt"`
}

type BuildLog struct {
	Sequence  int64     `json:"sequence"`
	Message   string    `json:"message"`
	CreatedAt time.Time `json:"createdAt"`
}

type Release struct {
	ID              int64     `json:"-"`
	PublicID        string    `json:"id"`
	WorkspaceID     int64     `json:"-"`
	ProjectID       int64     `json:"-"`
	AppID           int64     `json:"-"`
	ProjectPublicID string    `json:"projectId"`
	AppPublicID     string    `json:"appId"`
	BuildPublicID   string    `json:"buildId"`
	SourceBranch    string    `json:"branch"`
	CommitSHA       string    `json:"commitSha"`
	Image           string    `json:"image"`
	Platform        string    `json:"platform"`
	CreatedAt       time.Time `json:"createdAt"`
}

func ValidateCommitSHA(value string) error {
	if !commitSHAPattern.MatchString(value) {
		return errors.New("commit SHA must be a full lowercase SHA-1 object ID")
	}
	return nil
}

func NormalizeSourceBranch(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || utf8.RuneCountInString(value) > 255 {
		return "", errors.New("branch must contain between 1 and 255 characters")
	}
	for _, character := range value {
		if unicode.IsControl(character) {
			return "", errors.New("branch must not contain control characters")
		}
	}
	return value, nil
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
