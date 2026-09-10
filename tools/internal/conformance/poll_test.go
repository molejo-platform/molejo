package conformance

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestAwaitStopsWhenObservationIsReady(t *testing.T) {
	t.Parallel()

	attempts := 0
	err := await(t.Context(), time.Millisecond, "test state", func(context.Context) (bool, error) {
		attempts++
		return attempts == 2, nil
	})
	if err != nil || attempts != 2 {
		t.Fatalf("await() attempts=%d err=%v", attempts, err)
	}
}

func TestAwaitReturnsObservationError(t *testing.T) {
	t.Parallel()

	want := errors.New("broken")
	err := await(t.Context(), time.Millisecond, "test state", func(context.Context) (bool, error) { return false, want })
	if !errors.Is(err, want) {
		t.Fatalf("await() error=%v, want %v", err, want)
	}
}
