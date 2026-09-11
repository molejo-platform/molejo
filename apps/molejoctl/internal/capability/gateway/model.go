package gateway

const (
	APIVersion = "config.molejo.dev/v1alpha2"
	Kind       = "GatewaySetup"

	ProfileK3s              = "k3s"
	ControllerTraefik       = "traefik"
	ControllerManaged       = "managed"
	TraefikChart            = "oci://ghcr.io/traefik/helm/traefik"
	TraefikVersion          = "41.2.0"
	GatewayAPIController    = "traefik.io/gateway-controller"
	GatewayClassCRD         = "gatewayclasses.gateway.networking.k8s.io"
	GatewayCRD              = "gateways.gateway.networking.k8s.io"
	HTTPRouteCRD            = "httproutes.gateway.networking.k8s.io"
	GRPCRouteCRD            = "grpcroutes.gateway.networking.k8s.io"
	ReferenceGrantCRD       = "referencegrants.gateway.networking.k8s.io"
	TLSRouteCRD             = "tlsroutes.gateway.networking.k8s.io"
	BackendTLSPolicyCRD     = "backendtlspolicies.gateway.networking.k8s.io"
	ManagedByLabel          = "app.kubernetes.io/managed-by"
	ManagedByValue          = "molejoctl"
	DefaultGatewayNamespace = "molejo-system"
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
	Profile string      `yaml:"profile" json:"profile"`
	Gateway GatewaySpec `yaml:"gateway" json:"gateway"`
}

type GatewaySpec struct {
	Controller ControllerSpec `yaml:"controller" json:"controller"`
	Service    ServiceSpec    `yaml:"service" json:"service"`
	Instance   InstanceSpec   `yaml:"instance" json:"instance"`
}

type ControllerSpec struct {
	Management string `yaml:"management" json:"management"`
	Name       string `yaml:"name" json:"name"`
	Version    string `yaml:"version" json:"version"`
	Namespace  string `yaml:"namespace" json:"namespace"`
	ClassName  string `yaml:"className" json:"className"`
}

type ServiceSpec struct {
	Type          string `yaml:"type" json:"type"`
	HTTPNodePort  int32  `yaml:"httpNodePort" json:"httpNodePort"`
	HTTPSNodePort int32  `yaml:"httpsNodePort" json:"httpsNodePort"`
}

type InstanceSpec struct {
	Namespace string         `yaml:"namespace" json:"namespace"`
	Name      string         `yaml:"name" json:"name"`
	Listeners []ListenerSpec `yaml:"listeners" json:"listeners"`
}

type ListenerSpec struct {
	Name              string          `yaml:"name" json:"name"`
	Hostname          string          `yaml:"hostname" json:"hostname"`
	CertificateSecret ObjectReference `yaml:"certificateSecret" json:"certificateSecret"`
}

type ObjectReference struct {
	Namespace string `yaml:"namespace" json:"namespace"`
	Name      string `yaml:"name" json:"name"`
}

type Diagnostic struct {
	Field   string
	Message string
}

type ResourceFacts struct {
	Exists  bool
	Owned   bool
	Ready   bool
	Matches bool
}

type HelmFacts struct {
	Exists  bool
	Ready   bool
	Matches bool
	Version string
}

type Facts struct {
	GatewayClassCRD     bool
	GatewayCRD          bool
	HTTPRouteCRD        bool
	GRPCRouteCRD        bool
	ReferenceGrantCRD   bool
	TLSRouteCRD         bool
	BackendTLSPolicyCRD bool
	Certificate         bool
	NodePortConflicts   []string
	Controller          HelmFacts
	ControllerService   ResourceFacts
	GatewayClass        ResourceFacts
	Gateway             ResourceFacts
}

type OperationKind string

const (
	OperationEnsureHelmRelease OperationKind = "EnsureHelmRelease"
	OperationWaitGatewayClass  OperationKind = "WaitGatewayClass"
	OperationEnsureGateway     OperationKind = "EnsureGateway"
	OperationWaitGateway       OperationKind = "WaitGateway"
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

type InitialOptions struct {
	Name                 string
	Domain               string
	CertificateNamespace string
	CertificateName      string
}

func InitialK3s(options InitialOptions) Setup {
	return Setup{
		APIVersion: APIVersion,
		Kind:       Kind,
		Metadata:   Metadata{Name: options.Name},
		Spec: SetupSpec{
			Profile: ProfileK3s,
			Gateway: GatewaySpec{
				Controller: ControllerSpec{Management: ControllerManaged, Name: ControllerTraefik, Version: TraefikVersion, Namespace: "traefik-system", ClassName: "traefik"},
				Service:    ServiceSpec{Type: "NodePort", HTTPNodePort: 30080, HTTPSNodePort: 30443},
				Instance: InstanceSpec{
					Namespace: DefaultGatewayNamespace,
					Name:      "molejo",
					Listeners: []ListenerSpec{
						{Name: "https-apex", Hostname: options.Domain, CertificateSecret: ObjectReference{Namespace: options.CertificateNamespace, Name: options.CertificateName}},
						{Name: "https-molejo", Hostname: "*." + options.Domain, CertificateSecret: ObjectReference{Namespace: options.CertificateNamespace, Name: options.CertificateName}},
					},
				},
			},
		},
	}
}
