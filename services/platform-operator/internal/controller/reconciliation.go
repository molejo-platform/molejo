package controller

import (
	"context"
	"errors"
)

// Canceled work remains durable desired state for the next manager; it is not workload degradation.
func isCanceledReconciliation(ctx context.Context, err error) bool {
	return ctx.Err() == context.Canceled && errors.Is(err, context.Canceled)
}
