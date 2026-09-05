// Package automation defines non-human control-plane identities and their
// narrowly scoped permissions.
package automation

import (
	"errors"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	PermissionReleaseWrite     = "release.write"
	PermissionDeploymentCreate = "deployment.create"
)

type ServiceAccount struct {
	PublicID                 string    `json:"id"`
	WorkspacePublicID        string    `json:"workspaceId"`
	ProjectPublicID          string    `json:"projectId"`
	AppPublicID              string    `json:"appId"`
	Name                     string    `json:"name"`
	Status                   string    `json:"status"`
	DeploymentEnvironmentIDs []string  `json:"deploymentEnvironmentIds"`
	CreatedAt                time.Time `json:"createdAt"`
	UpdatedAt                time.Time `json:"updatedAt"`
}

type Credential struct {
	ServiceAccount ServiceAccount `json:"serviceAccount"`
	TokenID        string         `json:"tokenId"`
	Token          string         `json:"token"`
	ExpiresAt      time.Time      `json:"expiresAt"`
}

func NormalizeName(value string) (string, error) {
	value = strings.Join(strings.Fields(value), " ")
	if utf8.RuneCountInString(value) < 1 || utf8.RuneCountInString(value) > 80 || strings.IndexFunc(value, unicode.IsControl) >= 0 {
		return "", errors.New("service account name must contain 1 to 80 characters without controls")
	}
	return value, nil
}
