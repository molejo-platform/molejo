package store

import (
	"errors"
	"time"

	"github.com/molejo-platform/molejo/packages/kubernetesbinding"
)

var (
	ErrBindingNotFound    = errors.New("Kubernetes binding not found")
	ErrBindingUnsupported = errors.New("Kubernetes binding is unsupported")
)

type ClusterStorageBinding struct {
	ClusterID         string
	ClusterUID        string
	StorageProfileID  string
	StorageClassName  string
	Provisioner       string
	AccessModes       []string
	AllowExpansion    bool
	VolumeBindingMode string
	Health            kubernetesbinding.Health
	ReasonCode        string
	ObservedAt        *time.Time
	ExpiresAt         *time.Time
	Version           int64
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

type ClusterPublicationBinding struct {
	ClusterID            string
	ClusterUID           string
	GatewayNamespace     string
	GatewayName          string
	SectionName          string
	GatewayClassName     string
	GatewayClassAccepted bool
	GatewayProgrammed    bool
	ListenerReady        bool
	SupportedRouteKinds  []string
	Health               kubernetesbinding.Health
	ReasonCode           string
	ObservedAt           *time.Time
	ExpiresAt            *time.Time
	Version              int64
	CreatedAt            time.Time
	UpdatedAt            time.Time
}

func storageBindingTargetID(profileID string) string { return "storage:" + profileID }

const publicationBindingTargetID = "publication:http"
