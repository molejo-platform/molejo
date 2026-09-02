package registrysetup

const (
	APIVersion = "config.molejo.dev/v1alpha1"
	Kind       = "RegistrySetup"

	ModeManagedPullSecret = "ManagedPullSecret"

	ManagedByLabel  = "app.kubernetes.io/managed-by"
	ManagedByValue  = "molejoctl"
	PartOfLabel     = "app.kubernetes.io/part-of"
	PartOfValue     = "molejo-platform"
	SetupLabel      = "platform.molejo.dev/registry-setup"
	RegistryHostKey = "platform.molejo.dev/registry-host"
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
	Registry       RegistrySpec       `yaml:"registry" json:"registry"`
	Authentication AuthenticationSpec `yaml:"authentication" json:"authentication"`
	Target         TargetSpec         `yaml:"target" json:"target"`
	Probe          ProbeSpec          `yaml:"probe" json:"probe"`
}

type RegistrySpec struct {
	Host string `yaml:"host" json:"host"`
}

type AuthenticationSpec struct {
	Mode       string `yaml:"mode" json:"mode"`
	SecretName string `yaml:"secretName" json:"secretName"`
}

type TargetSpec struct {
	Namespace      string `yaml:"namespace" json:"namespace"`
	ServiceAccount string `yaml:"serviceAccount" json:"serviceAccount"`
}

type ProbeSpec struct {
	Image string `yaml:"image" json:"image"`
}

type InitialOptions struct {
	Name           string
	Host           string
	SecretName     string
	Namespace      string
	ServiceAccount string
	ProbeImage     string
}

func Initial(options InitialOptions) Setup {
	return Setup{
		APIVersion: APIVersion,
		Kind:       Kind,
		Metadata:   Metadata{Name: options.Name},
		Spec: SetupSpec{
			Registry:       RegistrySpec{Host: options.Host},
			Authentication: AuthenticationSpec{Mode: ModeManagedPullSecret, SecretName: options.SecretName},
			Target:         TargetSpec{Namespace: options.Namespace, ServiceAccount: options.ServiceAccount},
			Probe:          ProbeSpec{Image: options.ProbeImage},
		},
	}
}

type Diagnostic struct {
	Field   string
	Message string
}

type SecretFacts struct {
	Exists  bool
	Owned   bool
	Valid   bool
	Matches bool
}

type Facts struct {
	NamespaceExists      bool
	ServiceAccountExists bool
	Secret               SecretFacts
	PullSecretAttached   bool
}

type OperationKind string

const (
	OperationEnsureSecret     OperationKind = "EnsureSecret"
	OperationAttachPullSecret OperationKind = "AttachPullSecret"
)

type Operation struct {
	Kind   OperationKind
	ID     string
	Detail string
}

type Plan struct {
	Diagnostics []Diagnostic
	Operations  []Operation
	Ready       bool
}

func (p Plan) Valid() bool { return len(p.Diagnostics) == 0 }

type Report struct {
	Setup   Setup
	Plan    Plan
	Changed bool
	Smoke   SmokeResult
}

type SmokeResult struct {
	PodName string
	Image   string
	ImageID string
}
