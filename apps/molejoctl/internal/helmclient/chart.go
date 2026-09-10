package helmclient

import (
	"fmt"
	"strings"

	"helm.sh/helm/v4/pkg/chart"
	"helm.sh/helm/v4/pkg/chart/loader"
	chartv2 "helm.sh/helm/v4/pkg/chart/v2"
)

// LoadChart loads one prepared local Helm chart and validates its identity.
func LoadChart(path, expectedName, expectedVersion string) (chart.Charter, error) {
	path = strings.TrimSpace(path)
	loaded, err := loader.Load(path)
	if err != nil {
		return nil, fmt.Errorf("load chart %q: %w", path, err)
	}
	loadedV2, ok := loaded.(*chartv2.Chart)
	if !ok || loadedV2.Metadata == nil || loadedV2.Metadata.APIVersion != chartv2.APIVersionV2 {
		return nil, fmt.Errorf("chart %q must use Helm chart API v2", path)
	}
	if loadedV2.Metadata.Name != expectedName {
		return nil, fmt.Errorf("chart %q has name %q, want %q", path, loadedV2.Metadata.Name, expectedName)
	}
	if loadedV2.Metadata.Version != expectedVersion {
		return nil, fmt.Errorf("chart %q has version %q, want %q", path, loadedV2.Metadata.Version, expectedVersion)
	}
	return loaded, nil
}
