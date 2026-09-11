// Package kubernetesbinding defines the bounded contract used to verify the
// exact Kubernetes resources selected by an installation operator.
package kubernetesbinding

import (
	"errors"
	"regexp"
	"sort"
	"strings"
	"time"
)

const (
	MaxTargets     = 32
	ObservationTTL = 90 * time.Second
)

type Kind string

const (
	KindStorage         Kind = "Storage"
	KindPublicationHTTP Kind = "PublicationHTTP"
)

type Health string

const (
	HealthUnknown     Health = "Unknown"
	HealthHealthy     Health = "Healthy"
	HealthDegraded    Health = "Degraded"
	HealthUnavailable Health = "Unavailable"
)

type Target struct {
	ID          string
	Kind        Kind
	Version     int64
	Storage     *StorageTarget
	Publication *PublicationTarget
}

type StorageTarget struct {
	StorageClassName string
}

type PublicationTarget struct {
	GatewayNamespace string
	GatewayName      string
	SectionName      string
}

type Observation struct {
	ID          string
	Kind        Kind
	Version     int64
	Health      Health
	ReasonCode  string
	SampledAt   time.Time
	Storage     *StorageObservation
	Publication *PublicationObservation
}

type StorageObservation struct {
	StorageClassName  string
	Provisioner       string
	AccessModes       []string
	AllowExpansion    bool
	VolumeBindingMode string
}

type PublicationObservation struct {
	GatewayNamespace     string
	GatewayName          string
	SectionName          string
	GatewayClassName     string
	GatewayClassAccepted bool
	GatewayProgrammed    bool
	ListenerReady        bool
	SupportedRouteKinds  []string
}

var (
	idPattern     = regexp.MustCompile(`^[a-z][a-z0-9:-]{0,127}$`)
	dnsLabel      = regexp.MustCompile(`^[a-z0-9](?:[-a-z0-9]*[a-z0-9])?$`)
	reasonPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,127}$`)
)

func ValidateTarget(target Target) error {
	if !idPattern.MatchString(target.ID) || target.Version < 1 {
		return errors.New("binding target identity is invalid")
	}
	switch target.Kind {
	case KindStorage:
		if target.Storage == nil || target.Publication != nil || !validSubdomain(target.Storage.StorageClassName) {
			return errors.New("storage binding target is invalid")
		}
	case KindPublicationHTTP:
		if target.Publication == nil || target.Storage != nil || !validLabel(target.Publication.GatewayNamespace) || !validSubdomain(target.Publication.GatewayName) || !validLabel(target.Publication.SectionName) {
			return errors.New("publication binding target is invalid")
		}
	default:
		return errors.New("binding target kind is invalid")
	}
	return nil
}

func ValidateObservation(target Target, observation Observation, now time.Time) error {
	if err := ValidateTarget(target); err != nil {
		return err
	}
	if observation.ID != target.ID || observation.Kind != target.Kind || observation.Version != target.Version || !validHealth(observation.Health) || observation.SampledAt.IsZero() || observation.SampledAt.After(now.Add(time.Minute)) {
		return errors.New("binding observation envelope is invalid")
	}
	if observation.ReasonCode != "" && !reasonPattern.MatchString(observation.ReasonCode) {
		return errors.New("binding observation reason is invalid")
	}
	switch target.Kind {
	case KindStorage:
		if observation.Storage == nil || observation.Publication != nil || observation.Storage.StorageClassName != target.Storage.StorageClassName || !validSubdomain(observation.Storage.StorageClassName) || len(observation.Storage.Provisioner) > 253 || !validUnique(observation.Storage.AccessModes, 8, 64) || len(observation.Storage.VolumeBindingMode) > 64 {
			return errors.New("storage binding observation is invalid")
		}
	case KindPublicationHTTP:
		value := observation.Publication
		if value == nil || observation.Storage != nil || value.GatewayNamespace != target.Publication.GatewayNamespace || value.GatewayName != target.Publication.GatewayName || value.SectionName != target.Publication.SectionName || len(value.GatewayClassName) > 253 || !validUnique(value.SupportedRouteKinds, 8, 64) {
			return errors.New("publication binding observation is invalid")
		}
	}
	return nil
}

func EffectiveHealth(now time.Time, observation Observation) Health {
	if observation.SampledAt.IsZero() || !observation.SampledAt.Add(ObservationTTL).After(now) {
		return HealthUnknown
	}
	return observation.Health
}

func NormalizeStrings(values []string) []string {
	result := append([]string{}, values...)
	sort.Strings(result)
	return result
}

func validHealth(value Health) bool {
	return value == HealthUnknown || value == HealthHealthy || value == HealthDegraded || value == HealthUnavailable
}

func validLabel(value string) bool {
	return len(value) <= 63 && dnsLabel.MatchString(value)
}

func validSubdomain(value string) bool {
	if len(value) > 253 {
		return false
	}
	for _, label := range strings.Split(value, ".") {
		if !validLabel(label) {
			return false
		}
	}
	return true
}

func validUnique(values []string, maximum, maximumLength int) bool {
	if len(values) > maximum {
		return false
	}
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if strings.TrimSpace(value) == "" || len(value) > maximumLength {
			return false
		}
		if _, exists := seen[value]; exists {
			return false
		}
		seen[value] = struct{}{}
	}
	return true
}
