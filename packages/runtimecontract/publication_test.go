package runtimecontract

import (
	"testing"

	"github.com/molejo-platform/molejo/packages/kubernetesbinding"
)

func TestPublicationBoundsAndClosedDestination(t *testing.T) {
	d := kubernetesbinding.HTTPDestination{BindingID: "binding", BindingRevision: 1, SchemaVersion: kubernetesbinding.HTTPBindingSchemaVersion, GatewayNamespace: "edge", GatewayName: "shared", SectionName: "https"}
	base := func() DeploymentIntent {
		return DeploymentIntent{Ports: []RuntimePort{{Name: "http", ContainerPort: 8080}}, PublicEndpoints: []PublicEndpoint{{Name: "web", Type: EndpointHTTP, PortName: "http", Addresses: []HTTPAddress{{Hostname: "example.test", Destination: d}, {Hostname: "www.example.test", Destination: d}}}}}
	}
	if err := ValidatePublication(base()); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*DeploymentIntent){
		func(i *DeploymentIntent) { i.PublicEndpoints[0].Hostname = "legacy.test" },
		func(i *DeploymentIntent) { i.PublicEndpoints[0].Addresses[0].Destination.SchemaVersion = "future" },
		func(i *DeploymentIntent) { i.PublicEndpoints[0].Addresses[0].Hostname = "*.example.test" },
		func(i *DeploymentIntent) { i.PublicEndpoints[0].Addresses[0].Hostname = "127.0.0.1" },
		func(i *DeploymentIntent) { i.PublicEndpoints[0].PortName = "missing" },
		func(i *DeploymentIntent) {
			i.PublicEndpoints[0].Addresses = append(i.PublicEndpoints[0].Addresses, i.PublicEndpoints[0].Addresses[0])
		},
		func(i *DeploymentIntent) { i.PublicEndpoints[0].Addresses = make([]HTTPAddress, 11) },
	} {
		intent := base()
		change(&intent)
		if ValidatePublication(intent) == nil {
			t.Fatal("invalid publication accepted")
		}
	}
}
