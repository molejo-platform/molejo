package gateway

import (
	"context"

	"github.com/molejo-platform/molejo/packages/kubernetesbinding"
	"github.com/molejo-platform/molejo/packages/kubernetespublication"
)

// InspectConsumption is separate from Verify, which checks a managed recipe.
// Phase 3 exposes this through the authenticated operational journey.
func InspectConsumption(ctx context.Context, reader kubernetespublication.Reader, destination kubernetesbinding.HTTPDestination, hostname, consumerNamespace string) (kubernetespublication.Facts, error) {
	return kubernetespublication.Inspect(ctx, reader, destination, hostname, consumerNamespace)
}
