package lifecycle

import (
	"slices"

	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/operationstore"
)

type transitionKind string

const (
	transitionFresh          transitionKind = "fresh"
	transitionContinue       transitionKind = "continue"
	transitionResolve        transitionKind = "resolve"
	transitionFinalize       transitionKind = "finalize"
	transitionReplaceRemoval transitionKind = "replace-removal"
	transitionSettle         transitionKind = "settle"
	transitionRefuse         transitionKind = "refuse"
)

type standing struct {
	held    bool
	verb    reconciliation.Verb
	state   reconciliation.OperationState
	pending bool
	unknown bool
}

func standingOf(operation operationstore.Operation, frozen reconciliation.Plan, states map[string]reconciliation.BlockState) standing {
	return standing{
		held: true, verb: operation.Verb, state: operation.State, pending: pendingRemains(frozen, states),
		unknown: slices.ContainsFunc(frozen.Blocks, func(block reconciliation.Block) bool { return states[block.ID] == reconciliation.BlockUnknown }),
	}
}

// classify is the state machine's one transition rule, over the verb asked for
// and the standing it meets; a completed operation's finalization also reads the context.
func classify(requested reconciliation.Verb, held standing) transitionKind {
	if !held.held {
		if requested == reconciliation.Destroy {
			return transitionSettle
		}
		return transitionFresh
	}
	incomplete := held.state != reconciliation.OperationDone
	if !held.pending && held.verb == requested && slices.Contains([]reconciliation.OperationState{
		reconciliation.OperationRunning, reconciliation.OperationUnknown, reconciliation.OperationFailed,
	}, held.state) {
		return transitionFinalize
	}
	switch {
	case requested == reconciliation.Destroy && (held.verb == reconciliation.Apply && incomplete ||
		held.verb == reconciliation.Destroy && held.state == reconciliation.OperationFailed):
		return transitionReplaceRemoval
	case incomplete && held.verb != requested:
		return transitionRefuse
	case incomplete && held.unknown:
		return transitionResolve
	case incomplete:
		return transitionContinue
	case held.verb == reconciliation.Destroy && held.pending:
		return transitionRefuse
	case held.verb == requested:
		return transitionSettle
	}
	return transitionFresh
}

func chosenVerb(held standing) reconciliation.Verb {
	switch {
	case !held.held:
		return reconciliation.Apply
	case held.state != reconciliation.OperationDone:
		return held.verb
	case held.verb == reconciliation.Apply:
		return reconciliation.Destroy
	}
	return reconciliation.Apply
}
