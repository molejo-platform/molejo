package conformance

import (
	"context"
	"fmt"
	"time"
)

func await(ctx context.Context, interval time.Duration, description string, observe func(context.Context) (bool, error)) error {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		ready, err := observe(ctx)
		if err != nil {
			return fmt.Errorf("observe %s: %w", description, err)
		}
		if ready {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("wait for %s: %w", description, ctx.Err())
		case <-ticker.C:
		}
	}
}
