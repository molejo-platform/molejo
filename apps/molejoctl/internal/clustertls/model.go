package clustertls

import "time"

const (
	APIVersion  = "platform.molejo.dev/v1alpha1"
	ProfileKind = "ClusterTLSProfile"
	BindingKind = "ClusterTLSBinding"

	ManagementManaged  = "Managed"
	ManagementExternal = "External"

	DriverExistingSecret = "existing-secret"
	DriverCertManager    = "cert-manager"
)

type Profile struct {
	APIVersion string      `yaml:"apiVersion" json:"apiVersion"`
	Kind       string      `yaml:"kind" json:"kind"`
	Metadata   Metadata    `yaml:"metadata" json:"metadata"`
	Spec       ProfileSpec `yaml:"spec" json:"spec"`
}

type Metadata struct {
	Name string `yaml:"name" json:"name"`
}

type ProfileSpec struct {
	Management  string          `yaml:"management" json:"management"`
	Domains     []string        `yaml:"domains" json:"domains"`
	Certificate CertificateSpec `yaml:"certificate" json:"certificate"`
}

type CertificateSpec struct {
	Driver          string          `yaml:"driver" json:"driver"`
	TargetSecretRef ObjectReference `yaml:"targetSecretRef" json:"targetSecretRef"`
	Config          map[string]any  `yaml:"config,omitempty" json:"config,omitempty"`
}

type ObjectReference struct {
	Namespace string `yaml:"namespace" json:"namespace"`
	Name      string `yaml:"name" json:"name"`
}

type Binding struct {
	APIVersion string        `yaml:"apiVersion" json:"apiVersion"`
	Kind       string        `yaml:"kind" json:"kind"`
	Metadata   Metadata      `yaml:"metadata" json:"metadata"`
	Spec       BindingSpec   `yaml:"spec" json:"spec"`
	Status     BindingStatus `yaml:"status" json:"status"`
}

type BindingSpec struct {
	Domains        []string        `yaml:"domains" json:"domains"`
	Termination    string          `yaml:"termination" json:"termination"`
	CertificateRef ObjectReference `yaml:"certificateRef" json:"certificateRef"`
	Driver         DriverReference `yaml:"driver" json:"driver"`
}

type DriverReference struct {
	ID      string `yaml:"id" json:"id"`
	Version string `yaml:"version" json:"version"`
}

type BindingStatus struct {
	NotBefore  time.Time   `yaml:"notBefore" json:"notBefore"`
	NotAfter   time.Time   `yaml:"notAfter" json:"notAfter"`
	Renewal    string      `yaml:"renewal" json:"renewal"`
	Conditions []Condition `yaml:"conditions" json:"conditions"`
}

type Condition struct {
	Type   string `yaml:"type" json:"type"`
	Status string `yaml:"status" json:"status"`
}

type CertificateFacts struct {
	Exists         bool
	Valid          bool
	KeyMatches     bool
	DomainsCovered bool
	NotBefore      time.Time
	NotAfter       time.Time
	Problem        string
}

type ManagedResourceFacts struct {
	Exists bool
	Owned  bool
	Ready  bool
}

type CertManagerFacts struct {
	Installed        bool
	CredentialExists bool
	Issuer           ManagedResourceFacts
	Certificate      ManagedResourceFacts
}

type Facts struct {
	Certificate CertificateFacts
	CertManager CertManagerFacts
	Binding     *Binding
}

type OperationKind string

const (
	OperationEnsureHelmRelease OperationKind = "EnsureHelmRelease"
	OperationEnsureObject      OperationKind = "EnsureObject"
	OperationWaitForCondition  OperationKind = "WaitForCondition"
	OperationWriteBinding      OperationKind = "WriteBinding"
)

type Operation struct {
	Kind    OperationKind
	ID      string
	Detail  string
	Helm    *HelmRelease
	Object  map[string]any
	Wait    *WaitTarget
	Binding *Binding
}

type HelmRelease struct {
	Name      string
	Namespace string
	Chart     string
	Version   string
	Values    map[string]any
}

type WaitTarget struct {
	GroupVersionResource string
	Namespace            string
	Name                 string
	Condition            string
}

type Diagnostic struct {
	Field   string
	Message string
}

type Plan struct {
	Diagnostics []Diagnostic
	Operations  []Operation
	Binding     *Binding
}

func (p Plan) Valid() bool { return len(p.Diagnostics) == 0 }
