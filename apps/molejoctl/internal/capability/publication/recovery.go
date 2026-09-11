package publication

type MutationState string

const (
	MutationPlanned         MutationState = "Planned"
	MutationDispatching     MutationState = "Dispatching"
	MutationSucceeded       MutationState = "Succeeded"
	MutationResponseUnknown MutationState = "ResponseUnknown"
	MutationRecovered       MutationState = "Recovered"
	MutationConflict        MutationState = "Conflict"
	MutationFailed          MutationState = "Failed"
)

type Mutation struct {
	State MutationState
	Err   error
}

func BeginMutation() Mutation { return Mutation{State: MutationDispatching} }

func CompleteMutation(err error, responseUnknown bool) Mutation {
	if err == nil {
		return Mutation{State: MutationSucceeded}
	}
	if responseUnknown {
		return Mutation{State: MutationResponseUnknown, Err: err}
	}
	return Mutation{State: MutationFailed, Err: err}
}

func RecoverMutation(equal, found bool, err error) Mutation {
	if err != nil {
		return Mutation{State: MutationFailed, Err: err}
	}
	if found && equal {
		return Mutation{State: MutationRecovered}
	}
	return Mutation{State: MutationConflict}
}
