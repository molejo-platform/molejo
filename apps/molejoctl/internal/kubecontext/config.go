// Package kubecontext resolves explicitly selected kubeconfig contexts for CLI use cases.
package kubecontext

import (
	"fmt"
	"strings"
	"time"

	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// RESTConfig loads and validates one named kubeconfig context.
func RESTConfig(contextName string, timeout time.Duration) (*rest.Config, error) {
	contextName = strings.TrimSpace(contextName)
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
	restConfig.Timeout = timeout
	return restConfig, nil
}
