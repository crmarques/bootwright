package reconciliation

import "fmt"

type MutationOperation string

const (
	MutationNone    MutationOperation = "none"
	MutationPending MutationOperation = "pending"
	MutationFailed  MutationOperation = "failed"
	MutationUnknown MutationOperation = "unknown"
	MutationApplied MutationOperation = "applied"
)

type MutationOwnership string

const (
	OwnershipNone     MutationOwnership = "none"
	OwnershipRetained MutationOwnership = "retained"
)

// Evidence is the closed version-1 mutation record the context guard reads. It
// establishes local disposal and update restrictions only; it is not permission
// to run lifecycle work and never carries operation detail.
type Evidence struct {
	Operation MutationOperation
	Ownership MutationOwnership
}

func PristineEvidence() Evidence {
	return Evidence{Operation: MutationNone, Ownership: OwnershipNone}
}

func (e Evidence) Valid() bool {
	switch e.Operation {
	case MutationNone, MutationPending, MutationFailed, MutationUnknown, MutationApplied:
	default:
		return false
	}
	return e.Ownership == OwnershipNone || e.Ownership == OwnershipRetained
}

func (e Evidence) Bytes() ([]byte, error) {
	if !e.Valid() {
		return nil, stateError("context mutation evidence is not a recognized state")
	}
	return fmt.Appendf(nil, "{\"version\":1,\"operation\":%q,\"ownership\":%q}\n", e.Operation, e.Ownership), nil
}

// EvidenceFor maps an operation to the evidence its context must carry before
// the next effect. A destroy that completed releases ownership; every other
// terminal state retains it, so nothing disposable is collected while recovery
// or removal still depends on it.
func EvidenceFor(verb Verb, state OperationState) (Evidence, error) {
	if verb != Apply && verb != Destroy {
		return Evidence{}, stateError("lifecycle verb is not recognized")
	}
	if !ValidOperationState(state) {
		return Evidence{}, stateError("lifecycle operation state is not recognized")
	}
	if verb == Destroy && state == OperationDone {
		return PristineEvidence(), nil
	}
	switch state {
	case OperationDone:
		return Evidence{Operation: MutationApplied, Ownership: OwnershipRetained}, nil
	case OperationFailed:
		return Evidence{Operation: MutationFailed, Ownership: OwnershipRetained}, nil
	case OperationUnknown:
		return Evidence{Operation: MutationUnknown, Ownership: OwnershipRetained}, nil
	}
	return Evidence{Operation: MutationPending, Ownership: OwnershipRetained}, nil
}
