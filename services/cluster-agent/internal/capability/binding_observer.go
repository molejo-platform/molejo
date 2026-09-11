package capability

import (
	"context"
	"sort"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/molejo-platform/molejo/packages/kubernetesbinding"
)

var (
	gatewayResource      = schema.GroupVersionResource{Group: "gateway.networking.k8s.io", Version: "v1", Resource: "gateways"}
	gatewayClassResource = schema.GroupVersionResource{Group: "gateway.networking.k8s.io", Version: "v1", Resource: "gatewayclasses"}
)

func (c *Collector) ObserveBindings(ctx context.Context, targets []kubernetesbinding.Target) ([]kubernetesbinding.Observation, bool) {
	if len(targets) > kubernetesbinding.MaxTargets {
		return nil, false
	}
	now := c.now()
	result := make([]kubernetesbinding.Observation, 0, len(targets))
	for _, target := range targets {
		if kubernetesbinding.ValidateTarget(target) != nil {
			return nil, false
		}
		switch target.Kind {
		case kubernetesbinding.KindStorage:
			result = append(result, c.observeStorageBinding(ctx, target, now))
		case kubernetesbinding.KindPublicationHTTP:
			result = append(result, c.observePublicationBinding(ctx, target, now))
		}
	}
	return result, true
}

func (c *Collector) observeStorageBinding(ctx context.Context, target kubernetesbinding.Target, now time.Time) kubernetesbinding.Observation {
	result := kubernetesbinding.Observation{
		ID: target.ID, Kind: target.Kind, Version: target.Version, Health: kubernetesbinding.HealthHealthy, SampledAt: now,
		Storage: &kubernetesbinding.StorageObservation{StorageClassName: target.Storage.StorageClassName, AccessModes: []string{"ReadWriteOnce"}},
	}
	class, err := c.kubernetes.StorageV1().StorageClasses().Get(ctx, target.Storage.StorageClassName, metav1.GetOptions{})
	if err != nil {
		result.Health, result.ReasonCode = bindingFailure(err)
		return result
	}
	result.Storage.Provisioner = class.Provisioner
	result.Storage.AllowExpansion = class.AllowVolumeExpansion != nil && *class.AllowVolumeExpansion
	if class.VolumeBindingMode != nil {
		result.Storage.VolumeBindingMode = string(*class.VolumeBindingMode)
	}
	return result
}

func (c *Collector) observePublicationBinding(ctx context.Context, target kubernetesbinding.Target, now time.Time) kubernetesbinding.Observation {
	reference := target.Publication
	result := kubernetesbinding.Observation{
		ID: target.ID, Kind: target.Kind, Version: target.Version, Health: kubernetesbinding.HealthUnknown, SampledAt: now,
		Publication: &kubernetesbinding.PublicationObservation{GatewayNamespace: reference.GatewayNamespace, GatewayName: reference.GatewayName, SectionName: reference.SectionName, SupportedRouteKinds: []string{}},
	}
	gateway, err := c.dynamic.Resource(gatewayResource).Namespace(reference.GatewayNamespace).Get(ctx, reference.GatewayName, metav1.GetOptions{})
	if err != nil {
		result.Health, result.ReasonCode = bindingFailure(err)
		return result
	}
	result.Publication.GatewayClassName, _, _ = unstructured.NestedString(gateway.Object, "spec", "gatewayClassName")
	result.Publication.GatewayProgrammed = conditionTrue(gateway, "Programmed")
	listeners, _, _ := unstructured.NestedSlice(gateway.Object, "status", "listeners")
	for _, raw := range listeners {
		listener, ok := raw.(map[string]any)
		if !ok || stringField(listener, "name") != reference.SectionName {
			continue
		}
		result.Publication.ListenerReady = nestedConditionsTrue(listener, gateway.GetGeneration(), "Accepted", "Programmed", "ResolvedRefs")
		if supported, ok := listener["supportedKinds"].([]any); ok {
			for _, value := range supported {
				if kind, ok := value.(map[string]any); ok {
					if name := stringField(kind, "kind"); name != "" {
						result.Publication.SupportedRouteKinds = append(result.Publication.SupportedRouteKinds, name)
					}
				}
			}
		}
		break
	}
	sort.Strings(result.Publication.SupportedRouteKinds)
	if result.Publication.GatewayClassName != "" {
		class, classErr := c.dynamic.Resource(gatewayClassResource).Get(ctx, result.Publication.GatewayClassName, metav1.GetOptions{})
		if classErr != nil {
			result.Health, result.ReasonCode = bindingFailure(classErr)
			return result
		}
		result.Publication.GatewayClassAccepted = conditionTrue(class, "Accepted")
	}
	supportsHTTPRoute := false
	for _, kind := range result.Publication.SupportedRouteKinds {
		supportsHTTPRoute = supportsHTTPRoute || kind == "HTTPRoute"
	}
	switch {
	case result.Publication.GatewayClassAccepted && result.Publication.GatewayProgrammed && result.Publication.ListenerReady && supportsHTTPRoute:
		result.Health = kubernetesbinding.HealthHealthy
	case result.Publication.GatewayClassName == "":
		result.Health, result.ReasonCode = kubernetesbinding.HealthUnavailable, "gateway_class_missing"
	default:
		result.Health, result.ReasonCode = kubernetesbinding.HealthDegraded, "gateway_not_ready"
	}
	return result
}

func bindingFailure(err error) (kubernetesbinding.Health, string) {
	switch {
	case apierrors.IsNotFound(err):
		return kubernetesbinding.HealthUnavailable, "binding_resource_not_found"
	case apierrors.IsForbidden(err), apierrors.IsUnauthorized(err):
		return kubernetesbinding.HealthUnknown, "binding_access_denied"
	default:
		return kubernetesbinding.HealthUnknown, "binding_probe_failed"
	}
}

func conditionTrue(object *unstructured.Unstructured, conditionType string) bool {
	conditions, _, _ := unstructured.NestedSlice(object.Object, "status", "conditions")
	return conditionSliceTrue(conditions, conditionType, "True", object.GetGeneration())
}

func nestedConditionsTrue(object map[string]any, generation int64, required ...string) bool {
	conditions, _, _ := unstructured.NestedSlice(object, "conditions")
	for _, conditionType := range required {
		if !conditionSliceTrue(conditions, conditionType, "True", generation) {
			return false
		}
	}
	return true
}

func conditionSliceTrue(conditions []any, conditionType, expectedStatus string, generation int64) bool {
	if expectedStatus == "" {
		expectedStatus = "True"
	}
	for _, raw := range conditions {
		condition, ok := raw.(map[string]any)
		observed, _, _ := unstructured.NestedInt64(condition, "observedGeneration")
		if ok && observed == generation && stringField(condition, "type") == conditionType && stringField(condition, "status") == expectedStatus {
			return true
		}
	}
	return false
}

func stringField(object map[string]any, field string) string {
	value, _ := object[field].(string)
	return value
}
