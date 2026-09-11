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
	kubernetesbinding.HTTPBinding
	ClusterID  string                   `json:"clusterId"`
	ClusterUID string                   `json:"clusterUid"`
	Health     kubernetesbinding.Health `json:"health"`
	ReasonCode string                   `json:"reasonCode"`
	ObservedAt *time.Time               `json:"observedAt,omitempty"`
	ExpiresAt  *time.Time               `json:"-"`
	CreatedAt  time.Time                `json:"createdAt"`
	UpdatedAt  time.Time                `json:"updatedAt"`
}

func storageBindingTargetID(profileID string) string { return "storage:" + profileID }
