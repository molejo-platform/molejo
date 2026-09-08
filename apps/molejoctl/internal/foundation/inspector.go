// Package foundation inspects the Kubernetes substrate without changing it.
package foundation

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/kubernetes"

	"github.com/molejo-platform/molejo/apps/molejoctl/internal/capability"
	"github.com/molejo-platform/molejo/apps/molejoctl/internal/kubecontext"
	"github.com/molejo-platform/molejo/packages/capabilitycontract"
)

const inspectTimeout = 10 * time.Second

// Node is the foundation information Molejo needs from one Kubernetes node.
type Node struct {
	Name         string
	Architecture string
}

// Report is a read-only snapshot of the selected Kubernetes context.
type Report struct {
	ContextName           string
	KubernetesVersion     string
	Distribution          string
	Nodes                 []Node
	DefaultStorageClasses []string
	Capabilities          []capability.Observation
}

// Client is the minimal Kubernetes view consumed by the inspector.
type Client interface {
	ServerVersion() (string, error)
	Nodes(context.Context) ([]Node, error)
	DefaultStorageClasses(context.Context) ([]string, error)
	Resources(string, ...string) error
	DeploymentAvailable(context.Context, string, string) (bool, error)
}

// ClientFactory resolves the explicitly selected Kubernetes context.
type ClientFactory func(string) (Client, error)

// Inspector observes one Kubernetes foundation.
type Inspector interface {
	Inspect(context.Context, string) (Report, error)
}

type inspector struct {
	newClient ClientFactory
}

// New constructs the production foundation inspector.
func New() Inspector { return inspector{newClient: newClient} }

// NewInspector constructs an inspector with an explicit client factory.
func NewInspector(factory ClientFactory) Inspector { return inspector{newClient: factory} }

func (i inspector) Inspect(parent context.Context, contextName string) (Report, error) {
	client, err := i.newClient(contextName)
	if err != nil {
		return Report{}, err
	}
	ctx, cancel := context.WithTimeout(parent, inspectTimeout)
	defer cancel()

	version, err := client.ServerVersion()
	if err != nil {
		return Report{}, fmt.Errorf("inspect Kubernetes API: %w", err)
	}
	nodes, err := client.Nodes(ctx)
	if err != nil {
		return Report{}, fmt.Errorf("inspect nodes: %w", err)
	}
	storageClasses, err := client.DefaultStorageClasses(ctx)
	if err != nil {
		return Report{}, fmt.Errorf("inspect storage classes: %w", err)
	}

	report := Report{
		ContextName:           contextName,
		KubernetesVersion:     version,
		Distribution:          distribution(version),
		Nodes:                 nodes,
		DefaultStorageClasses: storageClasses,
	}
	report.Capabilities = append(report.Capabilities,
		storageObservation(storageClasses),
		apiObservation(client, capabilitycontract.PublicationHTTP, "Gateway API", capability.OwnershipRunbookManaged, "gateway.networking.k8s.io/v1", "gateways", "httproutes"),
		deploymentObservation(ctx, client, capabilitycontract.RuntimeWorkloadApply, "Molejo runtime", capability.OwnershipMolejoManaged, "molejo-system", "platform-operator", "cluster-agent"),
		deploymentObservation(ctx, client, capabilitycontract.ControlPlaneOperationEvents, "Molejo control plane", capability.OwnershipMolejoManaged, "molejo-control-plane", "control-plane-api", "console-web"),
	)
	return report, nil
}

func distribution(version string) string {
	lower := strings.ToLower(version)
	switch {
	case strings.Contains(lower, "k3s"):
		return "k3s"
	case strings.Contains(lower, "eks"):
		return "eks"
	case strings.Contains(lower, "gke"):
		return "gke"
	default:
		return "kubernetes"
	}
}

func storageObservation(classes []string) capability.Observation {
	observation := capability.Observation{ID: capabilitycontract.StorageRWO, ContractVersion: capabilitycontract.ContractVersion, Name: "Default storage", Ownership: capability.OwnershipProviderManaged}
	if len(classes) == 0 {
		observation.Status = capability.StatusUnavailable
		observation.ReasonCode = capabilitycontract.ReasonNoResource
		observation.Detail = "no default StorageClass"
		return observation
	}
	observation.Status = capability.StatusAvailable
	observation.Provider = strings.Join(classes, ", ")
	observation.Detail = "default StorageClass"
	return observation
}

func apiObservation(client Client, id capabilitycontract.ID, name string, ownership capability.Ownership, groupVersion string, resources ...string) capability.Observation {
	observation := capability.Observation{ID: id, ContractVersion: capabilitycontract.ContractVersion, Name: name, Ownership: ownership, Provider: groupVersion}
	if err := client.Resources(groupVersion, resources...); err != nil {
		observation.Status = capability.StatusUnavailable
		observation.ReasonCode = capabilitycontract.ReasonAPIMissing
		observation.Detail = err.Error()
		return observation
	}
	observation.Status = capability.StatusAvailable
	observation.Detail = strings.Join(resources, ", ")
	return observation
}

func deploymentObservation(ctx context.Context, client Client, id capabilitycontract.ID, name string, ownership capability.Ownership, namespace string, deployments ...string) capability.Observation {
	observation := capability.Observation{ID: id, ContractVersion: capabilitycontract.ContractVersion, Name: name, Ownership: ownership, Provider: namespace}
	for _, deployment := range deployments {
		available, err := client.DeploymentAvailable(ctx, namespace, deployment)
		if err != nil {
			observation.Status = capability.StatusUnknown
			observation.ReasonCode = capabilitycontract.ReasonProbeFailed
			observation.Detail = err.Error()
			return observation
		}
		if !available {
			observation.Status = capability.StatusUnavailable
			observation.ReasonCode = capabilitycontract.ReasonRuntimeNotDeployed
			observation.Detail = "not installed"
			return observation
		}
	}
	observation.Status = capability.StatusAvailable
	observation.Detail = strings.Join(deployments, ", ")
	return observation
}

type realClient struct {
	kubernetes kubernetes.Interface
	discovery  discovery.DiscoveryInterface
}

func newClient(contextName string) (Client, error) {
	restConfig, err := kubecontext.RESTConfig(contextName, inspectTimeout)
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
	return realClient{kubernetes: kubernetesClient, discovery: discoveryClient}, nil
}

func (c realClient) ServerVersion() (string, error) {
	version, err := c.discovery.ServerVersion()
	if err != nil {
		return "", err
	}
	return version.GitVersion, nil
}

func (c realClient) Nodes(ctx context.Context) ([]Node, error) {
	list, err := c.kubernetes.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	nodes := make([]Node, 0, len(list.Items))
	for _, item := range list.Items {
		nodes = append(nodes, Node{Name: item.Name, Architecture: item.Status.NodeInfo.Architecture})
	}
	sort.Slice(nodes, func(left, right int) bool { return nodes[left].Name < nodes[right].Name })
	return nodes, nil
}

func (c realClient) DefaultStorageClasses(ctx context.Context) ([]string, error) {
	list, err := c.kubernetes.StorageV1().StorageClasses().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	var names []string
	for _, item := range list.Items {
		annotations := item.Annotations
		if annotations["storageclass.kubernetes.io/is-default-class"] == "true" || annotations["storageclass.beta.kubernetes.io/is-default-class"] == "true" {
			names = append(names, item.Name)
		}
	}
	sort.Strings(names)
	return names, nil
}

func (c realClient) Resources(groupVersion string, required ...string) error {
	resources, err := c.discovery.ServerResourcesForGroupVersion(groupVersion)
	if err != nil {
		return err
	}
	present := make(map[string]struct{}, len(resources.APIResources))
	for _, resource := range resources.APIResources {
		present[resource.Name] = struct{}{}
	}
	for _, name := range required {
		if _, exists := present[name]; !exists {
			return fmt.Errorf("missing resource %s", name)
		}
	}
	return nil
}

func (c realClient) DeploymentAvailable(ctx context.Context, namespace, name string) (bool, error) {
	deployment, err := c.kubernetes.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	desired := int32(1)
	if deployment.Spec.Replicas != nil {
		desired = *deployment.Spec.Replicas
	}
	return deployment.Status.AvailableReplicas >= desired, nil
}
