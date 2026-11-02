package lifecycle

import (
	"errors"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/operationstore"
)

// withCause keeps the block's own diagnostics beside the terminal state. The
// terminal failure alone says only that the operation did not complete, which
// is never the reason an operator needs. A cause that carries no diagnostics,
// such as a cancellation, is left for the boundary that recognizes it.
func withCause(cause, terminal error) error {
	reported := diagnostics.Of(cause)
	if len(reported) == 0 {
		return terminal
	}
	var only onlyExit
	if errors.As(cause, &only) {
		return cause
	}
	return &diagnostics.Failure{Diagnostics: append(reported, diagnostics.Of(terminal)...)}
}

// unproved covers both ways an effect loses its outcome: an attempt that
// reported unknown, and one whose executor died before it reported anything.
func unproved(state reconciliation.BlockState) bool {
	return state == reconciliation.BlockUnknown || state == reconciliation.BlockRunning
}

func pendingRemains(plan reconciliation.Plan, states map[string]reconciliation.BlockState) bool {
	for _, block := range plan.Blocks {
		if states[block.ID] != reconciliation.BlockDone {
			return true
		}
	}
	return false
}

// takeBackUnder is the removal that supersedes an operation its own build can
// no longer continue: a fresh destroy run under what this executable holds.
func takeBackUnder(contextName, under string) string {
	return "take back what this operation owns under " + under + " with " + contextCommand(contextName, string(reconciliation.Destroy))
}

// registeredWith names the build an operation registered under, as the
// version command spells it, or the record itself when it names none.
func registeredWith(executable operationstore.Executable) string {
	if identity := buildIdentity(executable); identity != "" {
		return "bootwright " + identity
	}
	return "the executable its operation.json records"
}

// unboundExit is the remedy of a continuation whose context the controller
// state records no binding for. Only a fresh apply binds, and an apply over an
// incomplete operation continues it or refuses, so no apply can bind here. The
// host is already proved the controller state's own: the removal that
// supersedes the operation takes back what it owns where one may, and the
// apply after it binds again. Otherwise only the binding's restoration or the
// context's deletion remains.
func unboundExit(view View, operation operationstore.Operation) string {
	const restore = "restore the controller state that recorded this context's binding from a matching backup"
	if supersedable(operation) {
		name := view.Identity().Name
		return "take back what this operation owns with " + contextCommand(name, string(reconciliation.Destroy)) +
			", after which " + contextCommand(name, string(reconciliation.Apply)) + " binds the context again, or " + restore
	}
	return restore + ", or " + deletionExit(view)
}
