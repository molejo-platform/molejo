// Package helmclient centralizes the shared Helm and OCI client wiring used by molejoctl.
package helmclient

import (
	"fmt"
	"io"

	"helm.sh/helm/v4/pkg/action"
	"helm.sh/helm/v4/pkg/cli"
	"helm.sh/helm/v4/pkg/registry"
)

// Client contains the initialized Helm collaborators for one context and namespace.
type Client struct {
	Configuration *action.Configuration
	Settings      *cli.EnvSettings
	Registry      *registry.Client
}

// New initializes Helm and its OCI registry client for one target.
func New(contextName, namespace string, output io.Writer) (Client, error) {
	if output == nil {
		output = io.Discard
	}
	settings := cli.New()
	settings.KubeContext = contextName
	settings.SetNamespace(namespace)
	registryClient, err := registry.NewClient(
		registry.ClientOptWriter(output),
		registry.ClientOptEnableCache(true),
	)
	if err != nil {
		return Client{}, fmt.Errorf("create OCI registry client: %w", err)
	}
	configuration := action.NewConfiguration()
	configuration.RegistryClient = registryClient
	if err = configuration.Init(settings.RESTClientGetter(), namespace, "secret"); err != nil {
		return Client{}, fmt.Errorf("configure Helm: %w", err)
	}
	return Client{Configuration: configuration, Settings: settings, Registry: registryClient}, nil
}
