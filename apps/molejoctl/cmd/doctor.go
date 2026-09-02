package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

const (
	doctorTimeout   = 10 * time.Second
	systemNamespace = "molejo-system"
)

var errDoctorUnhealthy = errors.New("doctor found unhealthy components")

type doctorCheck struct {
	name    string
	detail  string
	healthy bool
}

type doctorReport struct {
	contextName string
	checks      []doctorCheck
}

func (r doctorReport) healthy() bool {
	for _, check := range r.checks {
		if !check.healthy {
			return false
		}
	}
	return len(r.checks) > 0
}

func (r doctorReport) writeTo(writer io.Writer) {
	_, _ = fmt.Fprintf(writer, "Molejo doctor\nContext: %s\n\n", r.contextName)
	for _, check := range r.checks {
		status := "PASS"
		if !check.healthy {
			status = "FAIL"
		}
		_, _ = fmt.Fprintf(writer, "%-5s %-22s %s\n", status, check.name, check.detail)
	}
	result := "healthy"
	if !r.healthy() {
		result = "unhealthy"
	}
	_, _ = fmt.Fprintf(writer, "\nResult: %s\n", result)
}

type doctorRunner interface {
	Run(context.Context, string) doctorReport
}

type doctorClient interface {
	ServerVersion() (string, error)
	Namespace(context.Context, string) error
	Resources(string, ...string) error
	DeploymentAvailability(context.Context, string, string) (int32, int32, error)
}

type doctorClientFactory func(string) (doctorClient, error)

type kubernetesDoctor struct {
	newClient doctorClientFactory
}

func newKubernetesDoctor() doctorRunner {
	return kubernetesDoctor{newClient: newDoctorClient}
}

func (d kubernetesDoctor) Run(parent context.Context, contextName string) doctorReport {
	report := doctorReport{contextName: contextName}
	client, err := d.newClient(contextName)
	if err != nil {
		report.checks = append(report.checks, failedCheck("Kubernetes API", err))
		return report
	}

	ctx, cancel := context.WithTimeout(parent, doctorTimeout)
	defer cancel()

	version, err := client.ServerVersion()
	if err != nil {
		report.checks = append(report.checks, failedCheck("Kubernetes API", err))
		return report
	}
	report.checks = append(report.checks, resultCheck("Kubernetes API", version, nil))

	err = client.Namespace(ctx, systemNamespace)
	report.checks = append(report.checks, resultCheck("Namespace", systemNamespace, err))

	err = client.Resources("platform.molejo.dev/v1alpha1", "appdeployments", "appvolumes")
	report.checks = append(report.checks, resultCheck("Molejo CRDs", "AppDeployment, AppVolume", err))

	err = errors.Join(
		client.Resources("gateway.networking.k8s.io/v1", "gateways", "httproutes"),
		client.Resources("gateway.networking.k8s.io/v1alpha2", "tcproutes"),
	)
	report.checks = append(report.checks, resultCheck("Gateway API CRDs", "Gateway, HTTPRoute, TCPRoute", err))

	report.checks = append(report.checks,
		deploymentCheck(ctx, client, "Platform Operator", "platform-operator"),
		deploymentCheck(ctx, client, "Cluster Agent", "cluster-agent"),
	)
	return report
}

func resultCheck(name, detail string, err error) doctorCheck {
	if err != nil {
		return failedCheck(name, err)
	}
	return doctorCheck{name: name, detail: detail, healthy: true}
}

func failedCheck(name string, err error) doctorCheck {
	return doctorCheck{name: name, detail: err.Error(), healthy: false}
}

func deploymentCheck(ctx context.Context, client doctorClient, label, name string) doctorCheck {
	available, desired, err := client.DeploymentAvailability(ctx, systemNamespace, name)
	if err != nil {
		return failedCheck(label, err)
	}
	detail := fmt.Sprintf("%d/%d available", available, desired)
	if available < desired {
		return doctorCheck{name: label, detail: detail, healthy: false}
	}
	return doctorCheck{name: label, detail: detail, healthy: true}
}

func newDoctorCommand(runner doctorRunner) *cobra.Command {
	var contextName string
	command := &cobra.Command{
		Use:   "doctor",
		Short: "Diagnose an installed Molejo cluster",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			contextName = strings.TrimSpace(contextName)
			if contextName == "" {
				return errors.New("context must not be empty")
			}
			report := runner.Run(command.Context(), contextName)
			report.writeTo(command.OutOrStdout())
			if !report.healthy() {
				return errDoctorUnhealthy
			}
			return nil
		},
	}
	command.Flags().StringVar(&contextName, "kube-context", "", "kubeconfig context to diagnose")
	_ = command.MarkFlagRequired("kube-context")
	return command
}

type realDoctorClient struct {
	kubernetes kubernetes.Interface
	discovery  discovery.DiscoveryInterface
}

func newDoctorClient(contextName string) (doctorClient, error) {
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	overrides := &clientcmd.ConfigOverrides{CurrentContext: contextName}
	clientConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, overrides)

	rawConfig, err := clientConfig.RawConfig()
	if err != nil {
		return nil, fmt.Errorf("load kubeconfig: %w", err)
	}
	if _, exists := rawConfig.Contexts[contextName]; !exists {
		return nil, fmt.Errorf("context %q not found", contextName)
	}

	restConfig, err := clientConfig.ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("configure context %q: %w", contextName, err)
	}
	restConfig.Timeout = doctorTimeout

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
