package lifecycle

import (
	"context"
	"errors"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
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
		if action := nextAction(verb, reconciliation.OperationDone); action != "none" {
			t.Fatalf("a done %s asks for %q", verb, action)
		}
	}
}

// An incomplete operation still names its own exact continuation, and an
// unknown one still names the resolution, because those are instructions an
// operator must act on.
func TestAnIncompleteOperationStillNamesItsContinuation(t *testing.T) {
	for state, expected := range map[reconciliation.OperationState]string{
		reconciliation.OperationFailed:  "continue-apply",
		reconciliation.OperationRunning: "continue-apply",
		reconciliation.OperationUnknown: "resolve",
	} {
		if action := nextAction(reconciliation.Apply, state); action != expected {
			t.Fatalf("a %s apply asks for %q, want %q", state, action, expected)
		}
	}
	if action := nextAction(reconciliation.Destroy, reconciliation.OperationFailed); action != "continue-destroy" {
		t.Fatalf("a failed destroy asks for %q", action)
	}
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
