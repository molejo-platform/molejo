package identity

import (
	"errors"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

var usernamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{2,63}$`)

const (
	StatusInvited  = "Invited"
	StatusActive   = "Active"
	StatusDisabled = "Disabled"
	StatusLocked   = "Locked"
)

type User struct {
	ID          int64     `json:"-"`
	PublicID    string    `json:"id"`
	Username    string    `json:"username"`
	DisplayName string    `json:"displayName"`
	Status      string    `json:"status"`
	Version     int64     `json:"version"`
	AuthVersion int64     `json:"-"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

func NormalizeUsername(value string) (string, error) {
	username := strings.ToLower(strings.TrimSpace(value))
	if !usernamePattern.MatchString(username) {
		return "", errors.New("username must contain 3 to 64 lowercase ASCII letters, numbers, dot, underscore, or hyphen")
	}
	return username, nil
}

func NormalizeDisplayName(value string) (string, error) {
	name := strings.Join(strings.Fields(value), " ")
	if utf8.RuneCountInString(name) < 1 || utf8.RuneCountInString(name) > 120 || strings.IndexFunc(name, unicode.IsControl) >= 0 {
		return "", errors.New("display name must contain 1 to 120 characters without controls")
	}
	return name, nil
}

func ValidStatus(status string) bool {
	return status == StatusInvited || status == StatusActive || status == StatusDisabled || status == StatusLocked
}

func ValidAdministrativeStatus(status string) bool {
	return status == StatusActive || status == StatusDisabled || status == StatusLocked
}

func CanAdministrativelyTransition(current, next string) bool {
	if !ValidStatus(current) || !ValidAdministrativeStatus(next) {
		return false
	}
	return current != StatusInvited
}
