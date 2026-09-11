package domain

import "testing"

func TestWithdrawalFSMRejectsSkippingAndReopening(t *testing.T) {
	if AdvanceWithdrawal(WithdrawalConfirmed, "") == nil || AdvanceWithdrawal("invalid", "invalid") == nil {
		t.Fatal("invalid state accepted")
	}
	states := []WithdrawalState{WithdrawalNone, WithdrawalRequested, WithdrawalRemoving, WithdrawalConfirmed}
	for i, from := range states {
		for j, to := range states {
			err := AdvanceWithdrawal(from, to)
			valid := j == i || j == i+1
			if (err == nil) != valid {
				t.Fatalf("%s -> %s: %v", from, to, err)
			}
		}
	}
}
