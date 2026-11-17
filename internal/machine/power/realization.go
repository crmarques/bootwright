package power

import (
	"context"

	"github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

type EvidenceRealization struct {
	reader   RealizationEvidence
	realizes func(implementation string) bool
}

func NewEvidenceRealization(reader RealizationEvidence, realizes func(implementation string) bool) EvidenceRealization {
	return EvidenceRealization{reader: reader, realizes: realizes}
}

func (r EvidenceRealization) Realization(ctx context.Context, contextName, name string) (machine.OwnershipState, bool, error) {
	published, err := r.reader.Evidence(ctx, contextName, "Machine", name)
	if err != nil {
		return machine.OwnershipState{}, false, err
	}
	if state, found := r.realizationOf(published); found {
		return state, true, nil
	}
	applied, err := r.reader.AppliedEvidence(ctx, contextName, "Machine", name)
	if err != nil {
		return machine.OwnershipState{}, false, err
	}
	if state, found := r.realizationOf(applied); found && state.Verb == string(reconciliation.Apply) && state.State == string(reconciliation.BlockDone) {
		return machine.OwnershipState{Verb: machine.VerbDestroy, State: machine.BlockDone}, true, nil
	}
	return machine.OwnershipState{}, false, nil
}

func (r EvidenceRealization) realizationOf(published []lifecycle.BlockEvidence) (machine.OwnershipState, bool) {
	for _, block := range published {
		if r.realizes(block.Implementation) {
			return machine.OwnershipState{Verb: string(block.Verb), State: string(block.State)}, true
		}
	}
	return machine.OwnershipState{}, false
}
