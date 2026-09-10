// Package automation defines non-human control-plane identities and their
// narrowly scoped permissions.
package automation

import (
	"errors"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/molejo-platform/molejo/services/control-plane-api/internal/domain"
)

const (
	PermissionReleaseWrite     Permission = "release.write"
	PermissionDeploymentCreate Permission = "deployment.create"
	defaultCredentialTTL                  = 90 * 24 * time.Hour
	maximumCredentialTTL                  = 365 * 24 * time.Hour
	minimumCredentialTTL                  = time.Minute
	maximumEnvironmentScope               = 20
)

type Permission string

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
	TokenID   string    `json:"tokenId"`
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expiresAt"`
}

type Token struct {
	PublicID   string     `json:"id"`
	ExpiresAt  time.Time  `json:"expiresAt"`
	RevokedAt  *time.Time `json:"revokedAt,omitempty"`
	LastUsedAt *time.Time `json:"lastUsedAt,omitempty"`
	CreatedAt  time.Time  `json:"createdAt"`
}

func NormalizeName(value string) (string, error) {
	value = strings.Join(strings.Fields(value), " ")
	if utf8.RuneCountInString(value) < 1 || utf8.RuneCountInString(value) > 80 || strings.IndexFunc(value, unicode.IsControl) >= 0 {
		return "", errors.New("service account name must contain 1 to 80 characters without controls")
	}
	return value, nil
}

func ValidateEnvironmentScope(values []string) error {
	if len(values) > maximumEnvironmentScope {
		return errors.New("service account environment scope exceeds the maximum")
	}
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if domain.ValidateAppEnvironmentID(value) != nil {
			return errors.New("service account environment scope contains an invalid ID")
		}
		if _, exists := seen[value]; exists {
			return errors.New("service account environment scope contains a duplicate ID")
		}
		seen[value] = struct{}{}
	}
	return nil
}

func ResolveCredentialExpiry(now time.Time, requested *time.Time) (time.Time, error) {
	expiresAt := now.Add(defaultCredentialTTL).UTC()
	if requested != nil {
		expiresAt = requested.UTC()
	}
	if expiresAt.Before(now.Add(minimumCredentialTTL)) || expiresAt.After(now.Add(maximumCredentialTTL)) {
		return time.Time{}, errors.New("credential expiry must be between one minute and 365 days")
	}
	return expiresAt, nil
}
