package clustertls

import "time"

const (
	APIVersion = "config.molejo.dev/v1alpha1"
	SetupKind  = "TLSSetup"

	RecipeCertManagerCloudflare = "cert-manager-cloudflare"
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
	DNSNames        []string        `yaml:"dnsNames" json:"dnsNames"`
	TargetSecretRef ObjectReference `yaml:"targetSecretRef" json:"targetSecretRef"`
	Recipe          RecipeSpec      `yaml:"recipe,omitempty" json:"recipe,omitempty"`
}

type RecipeSpec struct {
	ID     string         `yaml:"id,omitempty" json:"id,omitempty"`
	Config map[string]any `yaml:"config,omitempty" json:"config,omitempty"`
}

type ObjectReference struct {
	Namespace string `yaml:"namespace" json:"namespace"`
	Name      string `yaml:"name" json:"name"`
}

type CertificateFacts struct {
	Exists          bool
	Valid           bool
	KeyMatches      bool
	DNSNamesCovered bool
	NotBefore       time.Time
	NotAfter        time.Time
	Problem         string
}

type ManagedResourceFacts struct {
	Exists  bool
	Owned   bool
	Ready   bool
	Matches bool
}

type CredentialFacts struct {
	Exists  bool
	Owned   bool
	Usable  bool
	Matches bool
}

type CertManagerFacts struct {
	NamespaceExists bool
	Installed       bool
	VersionMatches  bool
	Credential      CredentialFacts
	Issuer          ManagedResourceFacts
	Certificate     ManagedResourceFacts
}

type Facts struct {
	Certificate CertificateFacts
	CertManager CertManagerFacts
}

type OperationKind string

const (
	OperationEnsureNamespace        OperationKind = "EnsureNamespace"
	OperationEnsureCredentialSecret OperationKind = "EnsureCredentialSecret"
	OperationEnsureHelmRelease      OperationKind = "EnsureHelmRelease"
	OperationEnsureObject           OperationKind = "EnsureObject"
	OperationWaitForCondition       OperationKind = "WaitForCondition"
)

type Operation struct {
	Kind       OperationKind
	ID         string
	Detail     string
	Namespace  string
	Credential *ObjectReference
	Helm       *HelmRelease
	Object     map[string]any
	Wait       *WaitTarget
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
	Ready       bool
}

func (p Plan) Valid() bool { return len(p.Diagnostics) == 0 }
