package operationworker

import (
	"testing"
	"time"
)

func TestCommandDeadlineNeverExceedsTheOperationLease(t *testing.T) {
	now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	leaseUntil := now.Add(10 * time.Second)
	deadline, err := commandDeadline(now, 20*time.Second, &leaseUntil)
	if err != nil {
		t.Fatal(err)
	}
	want := leaseUntil.Add(-commandLeaseSafetyMargin)
	if !deadline.Equal(want) {
		t.Fatalf("deadline=%s, want %s", deadline, want)
	}
}

func TestCommandDeadlineRejectsAnExpiredExecutionWindow(t *testing.T) {
	now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	leaseUntil := now.Add(commandLeaseSafetyMargin)
	if _, err := commandDeadline(now, 20*time.Second, &leaseUntil); err == nil {
		t.Fatal("command deadline accepted a lease without an execution window")
	}
}

func TestValidateTimingRequiresSafetyMargin(t *testing.T) {
	if err := ValidateTiming(22*time.Second, 20*time.Second); err != nil {
		t.Fatal(err)
	}
	if err := ValidateTiming(21*time.Second, 20*time.Second); err == nil {
		t.Fatal("operation lease without the safety margin was accepted")
	}
}

func TestValidSpecHashAcceptsOnlyCanonicalSHA256(t *testing.T) {
	if !validSpecHash("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa") {
		t.Fatal("canonical SHA-256 was rejected")
	}
	for _, value := range []string{"", "abc", "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", "gggggggggggggggggggggggggggggggggggggggggggggggggggggggggggggggg"} {
		if validSpecHash(value) {
			t.Fatalf("invalid hash %q was accepted", value)
		}
	}
}
