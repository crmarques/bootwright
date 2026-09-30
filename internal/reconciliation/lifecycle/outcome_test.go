package lifecycle

import (
	"context"
	"errors"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/operationstore"
)

// Nothing retries, destroys or deletes past an unknown block, so reporting a
// failure as unknown strands the context. Only a failure the runner cannot
// account for earns it; a failure it watched the adapter reach leaves the block
// failed, which the capability's own idempotent path retries.
func TestOnlyAnUnaccountedAdapterFailureLeavesTheBlockUnknown(t *testing.T) {
	for name, err := range map[string]error{
		"adapter did not complete": diagnostics.NewFailure("lifecycle.state", "the adapter operation did not complete", ""),
		"bound secret missing":     diagnostics.NewFailure("secret.store", "a bound Secret is not available", ""),
		"boundary unavailable":     diagnostics.NewFailure("controller.identity", "the approved execution bundle is unavailable", ""),
	} {
		t.Run(name, func(t *testing.T) {
			if outcome := AttemptOutcome(err); outcome != reconciliation.OutcomeFailed {
				t.Fatalf("outcome = %q, want failed", outcome)
			}
		})
	}
	// A result the runner never received is not a diagnosis, so it is unknown
	// however the process ended.
	for name, err := range map[string]error{
		"incomplete result":   diagnostics.NewFailure("lifecycle.unknown", "the adapter structured result was incomplete", ""),
		"descendants held on": diagnostics.NewFailure("lifecycle.unknown", "adapter descendants retained the result channel", ""),
		"lost result":         errors.New("lost"),
		"cancelled":           context.Canceled,
	} {
		t.Run(name, func(t *testing.T) {
			if outcome := AttemptOutcome(err); outcome != reconciliation.OutcomeUnknown {
				t.Fatalf("outcome = %q, want unknown", outcome)
			}
		})
	}
}

// The engine's own transition table is what makes the distinction matter: a
// failed block is the operation's retry candidate, an unknown block stops it.
func TestTheTransitionTableRetriesFailedAndStopsAtUnknown(t *testing.T) {
	_, failed, err := reconciliation.AttemptTransition(reconciliation.OutcomeFailed)
	if err != nil || failed != reconciliation.BlockFailed {
		t.Fatalf("failed attempt = %q (%v)", failed, err)
	}
	_, stuck, err := reconciliation.AttemptTransition(reconciliation.OutcomeUnknown)
	if err != nil || stuck != reconciliation.BlockUnknown {
		t.Fatalf("unknown attempt = %q (%v)", stuck, err)
	}
}

// A completed operation calls for nothing. Every other value of this field is
// a recovery instruction, so naming `destroy` after a successful apply read as
// an instruction to tear down what had just been built.
func TestACompletedOperationCallsForNothing(t *testing.T) {
	for _, verb := range []reconciliation.Verb{reconciliation.Apply, reconciliation.Destroy} {
		if action := nextActionOver(verb, reconciliation.OperationDone, reconciliation.BlockDone); action != "none" {
			t.Fatalf("a done %s asks for %q", verb, action)
		}
	}
}

// An incomplete operation still names its own exact continuation, and one
// holding an unknown block names the resolution, whatever state the operation
// records, because those are instructions an operator must act on. A failed
// removal holding a block not done, an unknown one included, names the fresh
// removal that replaces it, and one whose blocks are all done the
// finalization that completes it.
func TestAnIncompleteOperationStillNamesItsContinuation(t *testing.T) {
	for _, row := range []struct {
		verb     reconciliation.Verb
		state    reconciliation.OperationState
		block    reconciliation.BlockState
		expected string
	}{
		{reconciliation.Apply, reconciliation.OperationFailed, reconciliation.BlockFailed, "continue-apply"},
		{reconciliation.Apply, reconciliation.OperationRunning, reconciliation.BlockRunning, "continue-apply"},
		{reconciliation.Apply, reconciliation.OperationUnknown, reconciliation.BlockUnknown, "resolve"},
		{reconciliation.Apply, reconciliation.OperationRunning, reconciliation.BlockUnknown, "resolve"},
		{reconciliation.Apply, reconciliation.OperationUnknown, reconciliation.BlockDone, "continue-apply"},
		{reconciliation.Destroy, reconciliation.OperationUnknown, reconciliation.BlockUnknown, "resolve"},
		{reconciliation.Destroy, reconciliation.OperationRunning, reconciliation.BlockRunning, "continue-destroy"},
		{reconciliation.Destroy, reconciliation.OperationFailed, reconciliation.BlockFailed, "destroy"},
		{reconciliation.Destroy, reconciliation.OperationFailed, reconciliation.BlockRunning, "destroy"},
		{reconciliation.Destroy, reconciliation.OperationFailed, reconciliation.BlockUnknown, "destroy"},
		{reconciliation.Destroy, reconciliation.OperationFailed, reconciliation.BlockDone, "continue-destroy"},
	} {
		if action := nextActionOver(row.verb, row.state, row.block); action != row.expected {
			t.Fatalf("a %s %s beside a block reading %s asks for %q, want %q", row.state, row.verb, row.block, action, row.expected)
		}
	}
}

// nextActionOver is what an operation of verb recorded in state calls for
// beside a done block and one more block reading block.
func nextActionOver(verb reconciliation.Verb, state reconciliation.OperationState, block reconciliation.BlockState) string {
	frozen := reconciliation.Plan{Verb: verb, Blocks: []reconciliation.Block{
		{BlockDefinition: reconciliation.BlockDefinition{ID: "alpha"}},
		{BlockDefinition: reconciliation.BlockDefinition{ID: "bravo"}},
	}}
	return nextAction(operationstore.Operation{Verb: verb, State: state}, frozen,
		map[string]reconciliation.BlockState{"alpha": reconciliation.BlockDone, "bravo": block})
}

// A next action names a transition, not a command. Concatenating it onto
// "bootwright " produced `bootwright none` for a finished context,
// `bootwright continue-apply` for an interrupted one and `bootwright resolve`
// for an unproved one, none of which an operator can run.
func TestANextStepIsACommandAnOperatorCanRun(t *testing.T) {
	for _, offer := range []struct {
		verb     reconciliation.Verb
		action   string
		expected string
	}{
		{reconciliation.Apply, "none", ""},
		{reconciliation.Apply, "continue-apply", "bootwright apply"},
		{reconciliation.Apply, "apply", "bootwright apply"},
		{reconciliation.Apply, "destroy", "bootwright destroy"},
		{reconciliation.Destroy, "continue-destroy", "bootwright destroy"},
		{reconciliation.Destroy, "destroy", "bootwright destroy"},
		// An unproved effect is resolved by repeating the operation that left
		// it, so each verb offers its own command rather than a `resolve` one.
		{reconciliation.Apply, "resolve", "bootwright apply"},
		{reconciliation.Destroy, "resolve", "bootwright destroy"},
	} {
		if command := nextCommand(offer.verb, offer.action); command != offer.expected {
			t.Fatalf("a %s %q offers %q, want %q", offer.verb, offer.action, command, offer.expected)
		}
	}
}
