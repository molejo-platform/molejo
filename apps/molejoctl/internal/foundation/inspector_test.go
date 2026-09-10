package foundation

import (
	"context"
	"errors"
	"testing"

	"github.com/molejo-platform/molejo/apps/molejoctl/internal/capability"
	"github.com/molejo-platform/molejo/packages/capabilitycontract"
)

type fakeClient struct {
	version       string
	nodes         []Node
	storage       []string
	resourceErr   error
	deployments   map[string]bool
	deploymentErr error
}

func (f fakeClient) ServerVersion() (string, error)                          { return f.version, nil }
func (f fakeClient) Nodes(context.Context) ([]Node, error)                   { return f.nodes, nil }
func (f fakeClient) DefaultStorageClasses(context.Context) ([]string, error) { return f.storage, nil }

func (f fakeClient) Resources(string, ...string) error { return f.resourceErr }

func (f fakeClient) DeploymentAvailable(_ context.Context, namespace, name string) (bool, error) {
	if f.deploymentErr != nil {
		return false, f.deploymentErr
	}
	return f.deployments[namespace+"/"+name], nil
}

func TestInspectClassifiesK3sAndCapabilities(t *testing.T) {
	client := fakeClient{
		version: "v1.36.3+k3s1",
		nodes:   []Node{{Name: "node-1", Architecture: "amd64"}},
		storage: []string{"local-path"},
		deployments: map[string]bool{
			"molejo-system/platform-operator": true,
			"molejo-system/cluster-agent":     true,
		},
	}
	report, err := NewInspector(func(string) (Client, error) { return client, nil }).Inspect(context.Background(), "molejo-k3s")
	if err != nil {
		t.Fatal(err)
	}
	if report.Distribution != "k3s" || report.KubernetesVersion != "v1.36.3+k3s1" || len(report.Nodes) != 1 {
		t.Fatalf("report=%+v", report)
	}
	if report.Capabilities[0].Status != capability.StatusAvailable || report.Capabilities[2].Status != capability.StatusAvailable || report.Capabilities[3].Status != capability.StatusUnavailable {
		t.Fatalf("capabilities=%+v", report.Capabilities)
	}
	if report.Capabilities[0].ID != capabilitycontract.StorageRWO || report.Capabilities[0].ContractVersion != capabilitycontract.ContractVersion {
		t.Fatalf("storage capability=%+v", report.Capabilities[0])
	}
	if report.Capabilities[3].ReasonCode != capabilitycontract.ReasonRuntimeNotDeployed {
		t.Fatalf("control plane capability=%+v", report.Capabilities[3])
	}
}

func TestInspectPreservesOptionalCapabilityFailureAsObservation(t *testing.T) {
	client := fakeClient{version: "v1.36.3", resourceErr: errors.New("API absent"), deployments: map[string]bool{}}
	report, err := NewInspector(func(string) (Client, error) { return client, nil }).Inspect(context.Background(), "empty")
	if err != nil {
		t.Fatal(err)
	}
	if report.Distribution != "kubernetes" || report.Capabilities[1].Status != capability.StatusUnavailable {
		t.Fatalf("report=%+v", report)
	}
	if report.Capabilities[1].ID != capabilitycontract.PublicationHTTP || report.Capabilities[1].ReasonCode != capabilitycontract.ReasonAPIMissing {
		t.Fatalf("gateway capability=%+v", report.Capabilities[1])
	}
}
