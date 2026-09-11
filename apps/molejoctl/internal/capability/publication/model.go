// Package publication contains the deterministic policy for configuring HTTP
// publication. Network, terminal, filesystem, and Kubernetes effects live in
// adapters outside this package.
package publication

import (
	"context"

	"github.com/molejo-platform/molejo/packages/kubernetesbinding"
)

const (
	APIVersion = "config.molejo.dev/v1alpha1"
	Kind       = "HTTPPublicationSetup"
)

type Setup struct {
	APIVersion string    `yaml:"apiVersion" json:"apiVersion"`
	Kind       string    `yaml:"kind" json:"kind"`
	Metadata   Metadata  `yaml:"metadata" json:"metadata"`
	Spec       SetupSpec `yaml:"spec" json:"spec"`
}

type Metadata struct {
	Name string `yaml:"name" json:"name"`
}

type SetupSpec struct {
	ClusterID string       `yaml:"clusterId" json:"clusterId"`
	Binding   BindingSpec  `yaml:"binding" json:"binding"`
	Domains   []DomainSpec `yaml:"domains" json:"domains"`
}

type BindingSpec struct {
	SchemaVersion    string                           `yaml:"schemaVersion" json:"schemaVersion"`
	GatewayNamespace string                           `yaml:"gatewayNamespace" json:"gatewayNamespace"`
	GatewayName      string                           `yaml:"gatewayName" json:"gatewayName"`
	Listeners        []kubernetesbinding.HTTPListener `yaml:"listeners" json:"listeners"`
}

type DomainSpec struct {
	ID            string   `yaml:"id" json:"id"`
	Kind          string   `yaml:"kind" json:"kind"`
	Name          string   `yaml:"name" json:"name"`
	ReservedNames []string `yaml:"reservedNames" json:"reservedNames"`
	WorkspaceIDs  []string `yaml:"workspaceIds" json:"workspaceIds"`
}

type Domain struct {
	ID            string
	Kind          string
	Name          string
	ReservedNames []string
	Version       int64
}

type Binding struct {
	ID         string
	Revision   int64
	ClusterID  string
	ClusterUID string
	Spec       BindingSpec
	Health     string
	ReasonCode string
}

type Grant struct {
	DomainID    string
	WorkspaceID string
	BindingID   string
}

type Current struct {
	Binding *Binding
	Domains map[string]Domain
	Grants  map[string]Grant
}

type Diagnostic struct {
	Field   string `json:"field"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

type OperationKind string

const (
	EnsureBinding OperationKind = "EnsureBinding"
	EnsureDomain  OperationKind = "EnsureDomain"
	EnsureGrant   OperationKind = "EnsureGrant"
)

type Operation struct {
	Kind        OperationKind `json:"kind"`
	ID          string        `json:"id"`
	Detail      string        `json:"detail"`
	Domain      *DomainSpec   `json:"-"`
	WorkspaceID string        `json:"-"`
	IfMatch     *int64        `json:"-"`
}

type Plan struct {
	Diagnostics []Diagnostic `json:"diagnostics"`
	Warnings    []Diagnostic `json:"warnings"`
	Operations  []Operation  `json:"operations"`
}

type Inspection struct {
	ClusterUID  string
	Diagnostics []Diagnostic
	Warnings    []Diagnostic
}

type Observer interface {
	Inspect(context.Context, Setup) (Inspection, error)
}

type ObserverFactory func(string) (Observer, error)

func (p Plan) Valid() bool { return len(p.Diagnostics) == 0 }
func (p Plan) Ready() bool { return p.Valid() && len(p.Operations) == 0 }

func GrantKey(domainID, workspaceID string) string { return domainID + "\x00" + workspaceID }
