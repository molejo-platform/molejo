package agent

import "testing"

func TestStateRemainsReadyBeforePairing(t *testing.T) {
	status := NewStatus()
	for _, state := range []State{StateUnconfigured, StateUnpaired, StateEnrolling, StateConnecting, StatePaired} {
		status.Set(state, "")
		if !status.Ready() {
			t.Fatalf("state %s made the initialized agent unready", state)
		}
	}
	status.Set(StateFailed, "identity secret is not writable")
	if status.Ready() {
		t.Fatal("failed state remained ready")
	}
	if got := status.Snapshot().Reason; got != "identity secret is not writable" {
		t.Fatalf("reason=%q", got)
	}
}

func TestStoppingStateIsUnreadyAndTerminal(t *testing.T) {
	status := NewStatus()
	status.Set(StatePaired, "")
	status.Stop()
	status.Set(StateConnecting, "connection interrupted")

	if status.Ready() {
		t.Fatal("stopping state remained ready")
	}
	if got := status.Snapshot(); got.State != StateStopping || got.Reason != "" {
		t.Fatalf("status=%+v", got)
	}
}

func TestBackoffIsBounded(t *testing.T) {
	backoff := NewBackoff(1)
	for range 100 {
		delay := backoff.Next()
		if delay < BackoffMinimum || delay > BackoffMaximum {
			t.Fatalf("delay=%s", delay)
		}
	}
}
