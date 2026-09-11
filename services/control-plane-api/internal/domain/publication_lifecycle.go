package domain

import "errors"

type WithdrawalState string

const (
	WithdrawalNone      WithdrawalState = "None"
	WithdrawalRequested WithdrawalState = "Requested"
	WithdrawalRemoving  WithdrawalState = "Removing"
	WithdrawalConfirmed WithdrawalState = "Confirmed"
)

// A request, a fenced executor and removed children are different facts. Only
// the final transition permits releasing execution claims.
func AdvanceWithdrawal(current, next WithdrawalState) error {
	switch current {
	case WithdrawalNone, WithdrawalRequested, WithdrawalRemoving, WithdrawalConfirmed:
	default:
		return errors.New("withdrawal_transition_invalid")
	}
	if current == next {
		return nil
	}
	allowed := map[WithdrawalState]WithdrawalState{WithdrawalNone: WithdrawalRequested, WithdrawalRequested: WithdrawalRemoving, WithdrawalRemoving: WithdrawalConfirmed}
	following, exists := allowed[current]
	if !exists || following != next {
		return errors.New("withdrawal_transition_invalid")
	}
	return nil
}

type HTTPAssociation struct {
	DomainID     string `json:"domainId"`
	Label        string `json:"label,omitempty"`
	BindingID    string `json:"bindingId"`
	ListenerName string `json:"listenerName,omitempty"`
	Hostname     string `json:"hostname,omitempty"`
}
