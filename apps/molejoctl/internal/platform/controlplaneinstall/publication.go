package controlplaneinstall

import (
	"errors"
	"regexp"
	"strings"
)

var publicHostPattern = regexp.MustCompile(`^(?:[a-z0-9](?:[-a-z0-9]*[a-z0-9])?\.)+[a-z]{2,63}$`)

func normalizeAndValidateOptions(options Options) (Options, error) {
	options.ContextName = strings.TrimSpace(options.ContextName)
	options.Version = strings.TrimSpace(options.Version)
	options.StorageClass = strings.TrimSpace(options.StorageClass)
	options.PublicHost = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(options.PublicHost), "."))
	options.GatewayNamespace = strings.TrimSpace(options.GatewayNamespace)
	options.GatewayName = strings.TrimSpace(options.GatewayName)
	options.GatewaySection = strings.TrimSpace(options.GatewaySection)
	if options.PublicHost == "" {
		return options, nil
	}
	if !publicHostPattern.MatchString(options.PublicHost) {
		return Options{}, errors.New("public host must be a DNS name without scheme, port, or path")
	}
	if options.GatewayNamespace == "" || options.GatewayName == "" || options.GatewaySection == "" {
		return Options{}, errors.New("public installation requires Gateway namespace, name, and section")
	}
	return options, nil
}

func controlPlaneChartValues(options Options, storageClass string) map[string]any {
	values := map[string]any{"postgresql": map[string]any{"storageClass": storageClass}}
	public := map[string]any{"enabled": false}
	if options.PublicHost != "" {
		public = map[string]any{
			"enabled": true,
			"host":    options.PublicHost,
			"gateway": map[string]any{"namespace": options.GatewayNamespace, "name": options.GatewayName, "sectionName": options.GatewaySection},
		}
	}
	values["public"] = public
	return values
}

func publicConfigurationMatches(values map[string]any, options Options) bool {
	if options.PublicHost == "" {
		return true
	}
	public, ok := values["public"].(map[string]any)
	if !ok || public["enabled"] != true || public["host"] != options.PublicHost {
		return false
	}
	gateway, ok := public["gateway"].(map[string]any)
	return ok && gateway["namespace"] == options.GatewayNamespace && gateway["name"] == options.GatewayName && gateway["sectionName"] == options.GatewaySection
}
