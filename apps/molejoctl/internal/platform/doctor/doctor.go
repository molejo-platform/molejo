// Package doctor diagnoses one Molejo installation in Kubernetes.
package doctor

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/kubernetes"

	"github.com/molejo-platform/molejo/apps/molejoctl/internal/kubecontext"
)

const (
	doctorTimeout   = 10 * time.Second
	systemNamespace = "molejo-system"
)

// Check is one diagnostic observation.
type Check struct {
	Name    string
	Detail  string
	Healthy bool
}

// Report contains every diagnostic observation for one context.
type Report struct {
	ContextName string
	Checks      []Check
}

// Healthy reports whether all non-empty checks passed.
func (r Report) Healthy() bool {
	for _, check := range r.Checks {
		if !check.Healthy {
			return false
		}
	}
	return len(r.Checks) > 0
}

// WriteTo renders the stable human-readable CLI report.
func (r Report) Render(writer io.Writer) {
	_, _ = fmt.Fprintf(writer, "Molejo doctor\nContext: %s\n\n", r.ContextName)
	for _, check := range r.Checks {
		status := "PASS"
		if !check.Healthy {
			status = "FAIL"
		}
		_, _ = fmt.Fprintf(writer, "%-5s %-22s %s\n", status, check.Name, check.Detail)
	}
	result := "healthy"
	if !r.Healthy() {
		result = "unhealthy"
	}
	_, _ = fmt.Fprintf(writer, "\nResult: %s\n", result)
}

// Runner executes the doctor use case.
type Runner interface {
	Run(context.Context, string) Report
}

// Client is the minimal Kubernetes view consumed by the doctor.
type Client interface {
	ServerVersion() (string, error)
	Namespace(context.Context, string) error
	Resources(string, ...string) error
	DeploymentAvailability(context.Context, string, string) (int32, int32, error)
}

// ClientFactory resolves the selected Kubernetes context.
type ClientFactory func(string) (Client, error)

type kubernetesDoctor struct {
	newClient ClientFactory
}

// New constructs the production Kubernetes doctor.
func New() Runner {
	return kubernetesDoctor{newClient: newDoctorClient}
}

// NewRunner constructs a doctor with an explicit client factory.
func NewRunner(factory ClientFactory) Runner { return kubernetesDoctor{newClient: factory} }

func (d kubernetesDoctor) Run(parent context.Context, contextName string) Report {
	report := Report{ContextName: contextName}
	client, err := d.newClient(contextName)
	if err != nil {
		report.Checks = append(report.Checks, failedCheck("Kubernetes API", err))
		return report
	}

	ctx, cancel := context.WithTimeout(parent, doctorTimeout)
	defer cancel()

	version, err := client.ServerVersion()
	if err != nil {
		report.Checks = append(report.Checks, failedCheck("Kubernetes API", err))
		return report
	}
	report.Checks = append(report.Checks, resultCheck("Kubernetes API", version, nil))

	err = client.Namespace(ctx, systemNamespace)
	report.Checks = append(report.Checks, resultCheck("Namespace", systemNamespace, err))

	err = client.Resources("platform.molejo.dev/v1alpha1", "appdeployments", "appvolumes")
	report.Checks = append(report.Checks, resultCheck("Molejo CRDs", "AppDeployment, AppVolume", err))

	err = errors.Join(
		client.Resources("gateway.networking.k8s.io/v1", "backendtlspolicies", "gatewayclasses", "gateways", "grpcroutes", "httproutes"),
		client.Resources("gateway.networking.k8s.io/v1beta1", "referencegrants"),
		client.Resources("gateway.networking.k8s.io/v1alpha2", "tcproutes", "tlsroutes"),
	)
	report.Checks = append(report.Checks, resultCheck("Gateway API CRDs", "GatewayClass, Gateway, HTTPRoute, GRPCRoute, TCPRoute, TLSRoute", err))

	report.Checks = append(report.Checks,
		deploymentCheck(ctx, client, "Platform Operator", "platform-operator"),
		deploymentCheck(ctx, client, "Cluster Agent", "cluster-agent"),
	)
	return report
}

func resultCheck(name, detail string, err error) Check {
	if err != nil {
		return failedCheck(name, err)
	}
	return Check{Name: name, Detail: detail, Healthy: true}
}

func failedCheck(name string, err error) Check {
	return Check{Name: name, Detail: err.Error(), Healthy: false}
}

func deploymentCheck(ctx context.Context, client Client, label, name string) Check {
	available, desired, err := client.DeploymentAvailability(ctx, systemNamespace, name)
	if err != nil {
		return failedCheck(label, err)
	}
	detail := fmt.Sprintf("%d/%d available", available, desired)
	if available < desired {
		return Check{Name: label, Detail: detail, Healthy: false}
	}
	return Check{Name: label, Detail: detail, Healthy: true}
}

type realDoctorClient struct {
	kubernetes kubernetes.Interface
	discovery  discovery.DiscoveryInterface
}

func newDoctorClient(contextName string) (Client, error) {
	restConfig, err := kubecontext.RESTConfig(contextName, doctorTimeout)
	if err != nil {
		return nil, err
	}

	kubernetesClient, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return nil, fmt.Errorf("create Kubernetes client: %w", err)
	}
	discoveryClient, err := discovery.NewDiscoveryClientForConfig(restConfig)
	if err != nil {
		return nil, fmt.Errorf("create discovery client: %w", err)
	}
	return realDoctorClient{kubernetes: kubernetesClient, discovery: discoveryClient}, nil
}

func (c realDoctorClient) ServerVersion() (string, error) {
	version, err := c.discovery.ServerVersion()
	if err != nil {
		return "", fmt.Errorf("unreachable: %w", err)
	}
	return version.GitVersion, nil
}

func (c realDoctorClient) Namespace(ctx context.Context, name string) error {
	if _, err := c.kubernetes.CoreV1().Namespaces().Get(ctx, name, metav1.GetOptions{}); err != nil {
		return fmt.Errorf("namespace %q unavailable: %w", name, err)
	}
	return nil
}

func (c realDoctorClient) Resources(groupVersion string, required ...string) error {
	resources, err := c.discovery.ServerResourcesForGroupVersion(groupVersion)
	if err != nil {
		return fmt.Errorf("API %s unavailable: %w", groupVersion, err)
	}
	present := make(map[string]struct{}, len(resources.APIResources))
	for _, resource := range resources.APIResources {
		present[resource.Name] = struct{}{}
	}
	var missing []string
	for _, name := range required {
		if _, exists := present[name]; !exists {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("API %s missing resources: %s", groupVersion, strings.Join(missing, ", "))
	}
	return nil
}

func (c realDoctorClient) DeploymentAvailability(
	ctx context.Context,
	namespace string,
	name string,
) (int32, int32, error) {
	deployment, err := c.kubernetes.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return 0, 0, fmt.Errorf("deployment %q unavailable: %w", name, err)
	}
	desired := int32(1)
	if deployment.Spec.Replicas != nil {
		desired = *deployment.Spec.Replicas
	}
	return deployment.Status.AvailableReplicas, desired, nil
}
