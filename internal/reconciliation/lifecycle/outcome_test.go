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
