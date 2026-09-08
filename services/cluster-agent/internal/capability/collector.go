// Package capability observes bounded, read-only Kubernetes facts needed by
// Molejo. It never installs or mutates the detected infrastructure.
package capability

import (
	"context"
	"sort"
	"sync"
	"time"

	authorizationv1 "k8s.io/api/authorization/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"

	"github.com/molejo-platform/molejo/packages/capabilitycontract"
)

type Collector struct {
	kubernetes kubernetes.Interface
	discovery  discovery.DiscoveryInterface
	dynamic    dynamic.Interface
	now        func() time.Time
	mu         sync.RWMutex
	snapshot   []capabilitycontract.Observation
}

func NewCollector(kubernetesClient kubernetes.Interface, discoveryClient discovery.DiscoveryInterface, dynamicClient dynamic.Interface) *Collector {
	return &Collector{kubernetes: kubernetesClient, discovery: discoveryClient, dynamic: dynamicClient, now: func() time.Time { return time.Now().UTC() }, snapshot: []capabilitycontract.Observation{}}
}

func (c *Collector) Run(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = 30 * time.Second
	}
	c.refresh(ctx)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.refresh(ctx)
		}
	}
}

func (c *Collector) Snapshot() ([]capabilitycontract.Observation, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if len(c.snapshot) == 0 {
		return []capabilitycontract.Observation{}, false
	}
	result := make([]capabilitycontract.Observation, len(c.snapshot))
	copy(result, c.snapshot)
	for index := range result {
		result[index].Limitations = append([]string(nil), result[index].Limitations...)
	}
	return result, true
}

func (c *Collector) refresh(ctx context.Context) {
	snapshot := c.collect(ctx)
	c.mu.Lock()
	c.snapshot = snapshot
	c.mu.Unlock()
}

func (c *Collector) collect(ctx context.Context) []capabilitycontract.Observation {
	now := c.now()
	items := []capabilitycontract.Observation{
		c.dynamicObservation(ctx, now, capabilitycontract.RuntimeWorkloadApply, schema.GroupVersionResource{Group: "platform.molejo.dev", Version: "v1alpha1", Resource: "appdeployments"}),
		c.dynamicObservation(ctx, now, capabilitycontract.RuntimeWorkloadObserve, schema.GroupVersionResource{Group: "platform.molejo.dev", Version: "v1alpha1", Resource: "appdeployments"}),
		c.podLogsObservation(ctx, now), c.eventsObservation(ctx, now), c.dynamicObservation(ctx, now, capabilitycontract.RuntimeMetricsCurrent, schema.GroupVersionResource{Group: "metrics.k8s.io", Version: "v1beta1", Resource: "pods"}),
		c.dynamicObservation(ctx, now, capabilitycontract.PublicationHTTP, schema.GroupVersionResource{Group: "gateway.networking.k8s.io", Version: "v1", Resource: "gateways"}),
		c.dynamicObservation(ctx, now, capabilitycontract.PublicationTCP, schema.GroupVersionResource{Group: "gateway.networking.k8s.io", Version: "v1alpha2", Resource: "tcproutes"}),
	}
	items = append(items, c.storageObservations(ctx, now)...)
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	return items
}

func (c *Collector) dynamicObservation(ctx context.Context, now time.Time, id capabilitycontract.ID, resource schema.GroupVersionResource) capabilitycontract.Observation {
	base := observation(id, now)
	if !c.resourceExists(resource.GroupVersion().String(), resource.Resource) {
		base.Support, base.Health, base.ReasonCode = capabilitycontract.SupportUnsupported, capabilitycontract.HealthUnavailable, capabilitycontract.ReasonAPIMissing
		return base
	}
	_, err := c.dynamic.Resource(resource).Namespace(metav1.NamespaceAll).List(ctx, metav1.ListOptions{Limit: 1})
	return outcome(base, err)
}

func (c *Collector) podLogsObservation(ctx context.Context, now time.Time) capabilitycontract.Observation {
	base := observation(capabilitycontract.RuntimeLogsCurrent, now)
	if !c.resourceExists("v1", "pods/log") {
		base.Support, base.Health, base.ReasonCode = capabilitycontract.SupportUnsupported, capabilitycontract.HealthUnavailable, capabilitycontract.ReasonAPIMissing
		return base
	}
	review, err := c.kubernetes.AuthorizationV1().SelfSubjectAccessReviews().Create(ctx, &authorizationv1.SelfSubjectAccessReview{Spec: authorizationv1.SelfSubjectAccessReviewSpec{ResourceAttributes: &authorizationv1.ResourceAttributes{Verb: "get", Group: "", Resource: "pods", Subresource: "log"}}}, metav1.CreateOptions{})
	base = outcome(base, err)
	if err == nil && !review.Status.Allowed {
		base.Health, base.ReasonCode = capabilitycontract.HealthUnavailable, capabilitycontract.ReasonAccessDenied
	}
	return base
}

func (c *Collector) eventsObservation(ctx context.Context, now time.Time) capabilitycontract.Observation {
	base := observation(capabilitycontract.RuntimeEventsCurrent, now)
	_, err := c.kubernetes.CoreV1().Events(metav1.NamespaceAll).List(ctx, metav1.ListOptions{Limit: 1})
	return outcome(base, err)
}

func (c *Collector) storageObservations(ctx context.Context, now time.Time) []capabilitycontract.Observation {
	rwo, expand := observation(capabilitycontract.StorageRWO, now), observation(capabilitycontract.StorageExpand, now)
	classes, err := c.kubernetes.StorageV1().StorageClasses().List(ctx, metav1.ListOptions{Limit: 100})
	if err != nil {
		return []capabilitycontract.Observation{outcome(rwo, err), outcome(expand, err)}
	}
	if len(classes.Items) == 0 {
		rwo.Health, rwo.ReasonCode = capabilitycontract.HealthUnavailable, capabilitycontract.ReasonNoResource
		expand.Health, expand.ReasonCode = capabilitycontract.HealthUnavailable, capabilitycontract.ReasonNoResource
		return []capabilitycontract.Observation{rwo, expand}
	}
	expand.Health, expand.ReasonCode = capabilitycontract.HealthUnavailable, capabilitycontract.ReasonNoResource
	for _, class := range classes.Items {
		if class.AllowVolumeExpansion != nil && *class.AllowVolumeExpansion {
			expand.Health, expand.ReasonCode = capabilitycontract.HealthHealthy, ""
			break
		}
	}
	return []capabilitycontract.Observation{rwo, expand}
}

func (c *Collector) resourceExists(groupVersion, resource string) bool {
	resources, err := c.discovery.ServerResourcesForGroupVersion(groupVersion)
	if err != nil || resources == nil {
		return false
	}
	for _, item := range resources.APIResources {
		if item.Name == resource {
			return true
		}
	}
	return false
}

func observation(id capabilitycontract.ID, now time.Time) capabilitycontract.Observation {
	return capabilitycontract.Observation{ID: id, ContractVersion: capabilitycontract.ContractVersion, Support: capabilitycontract.SupportSupported, Health: capabilitycontract.HealthHealthy, ProviderKind: "kubernetes", Limitations: []string{}, SampledAt: now}
}

func outcome(base capabilitycontract.Observation, err error) capabilitycontract.Observation {
	if err == nil {
		return base
	}
	base.Health = capabilitycontract.HealthUnknown
	base.ReasonCode = capabilitycontract.ReasonProbeFailed
	if apierrors.IsForbidden(err) || apierrors.IsUnauthorized(err) {
		base.Health = capabilitycontract.HealthUnavailable
		base.ReasonCode = capabilitycontract.ReasonAccessDenied
	} else if apierrors.IsNotFound(err) {
		base.Support, base.Health, base.ReasonCode = capabilitycontract.SupportUnsupported, capabilitycontract.HealthUnavailable, capabilitycontract.ReasonAPIMissing
	}
	return base
}
