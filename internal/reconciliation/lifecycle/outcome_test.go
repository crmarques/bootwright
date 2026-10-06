package lifecycle

import (
	"context"
	"errors"
	"slices"
	"strings"
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
// for an unproved one, none of which an operator can run. The command it
// names is bound to the context and carries every token its own decision
// requires: a continuation or resolution the whole frozen plan's, a
// replacement those of the blocks it retains, and a finalization none.
func TestANextStepIsACommandAnOperatorCanRun(t *testing.T) {
	frozen := func(verb reconciliation.Verb) reconciliation.Plan {
		return reconciliation.Plan{Verb: verb, Blocks: []reconciliation.Block{
			{BlockDefinition: reconciliation.BlockDefinition{ID: "alpha", Consumes: []string{"data-loss"}}},
			{BlockDefinition: reconciliation.BlockDefinition{ID: "bravo", Consumes: []string{"network-outage"}}},
		}}
	}
	for _, offer := range []struct {
		verb     reconciliation.Verb
		state    reconciliation.OperationState
		alpha    reconciliation.BlockState
		bravo    reconciliation.BlockState
		expected string
	}{
		{reconciliation.Apply, reconciliation.OperationDone, reconciliation.BlockDone, reconciliation.BlockDone, ""},
		{reconciliation.Apply, reconciliation.OperationFailed, reconciliation.BlockDone, reconciliation.BlockFailed,
			"bootwright apply --context lab-b --authorize data-loss --authorize network-outage"},
		{reconciliation.Apply, reconciliation.OperationPaused, reconciliation.BlockDone, reconciliation.BlockPending,
			"bootwright apply --context lab-b --authorize data-loss --authorize network-outage"},
		{reconciliation.Apply, reconciliation.OperationFailed, reconciliation.BlockDone, reconciliation.BlockDone, "bootwright apply --context lab-b"},
		{reconciliation.Destroy, reconciliation.OperationRunning, reconciliation.BlockDone, reconciliation.BlockRunning,
			"bootwright destroy --context lab-b --authorize data-loss --authorize network-outage"},
		{reconciliation.Destroy, reconciliation.OperationFailed, reconciliation.BlockDone, reconciliation.BlockFailed,
			"bootwright destroy --context lab-b --authorize network-outage"},
		{reconciliation.Destroy, reconciliation.OperationFailed, reconciliation.BlockDone, reconciliation.BlockDone, "bootwright destroy --context lab-b"},
		// An unproved effect is resolved by repeating the operation that left
		// it, so each verb offers its own command rather than a `resolve` one.
		{reconciliation.Apply, reconciliation.OperationUnknown, reconciliation.BlockUnknown, reconciliation.BlockPending,
			"bootwright apply --context lab-b --authorize data-loss --authorize network-outage"},
		{reconciliation.Destroy, reconciliation.OperationUnknown, reconciliation.BlockDone, reconciliation.BlockUnknown,
			"bootwright destroy --context lab-b --authorize data-loss --authorize network-outage"},
	} {
		operation := operationstore.Operation{Verb: offer.verb, State: offer.state}
		states := map[string]reconciliation.BlockState{"alpha": offer.alpha, "bravo": offer.bravo}
		if command := continuationCommand("lab-b", operation, frozen(offer.verb), states); command != offer.expected {
			t.Fatalf("a %s %s beside %s and %s offers %q, want %q", offer.state, offer.verb, offer.alpha, offer.bravo, command, offer.expected)
		}
	}
}

// An operation that did not complete names the exact command that takes it
// on, in its context: a failed apply its continuation, a failed destroy the
// fresh removal that replaces it, and an unknown apply both its resolution
// and the removal that takes back what it started.
func TestTerminalRemediesNameTheExactCommand(t *testing.T) {
	for name, test := range map[string]struct {
		arrange func(*testing.T, *harness)
		verb    reconciliation.Verb
		code    string
		message string
		want    []string
	}{
		"a failed apply": {
			arrange: func(*testing.T, *harness) {}, verb: reconciliation.Apply, code: "lifecycle.state", message: "the apply did not complete",
			want: []string{"continue it with bootwright apply --context lab"},
		},
		"an unknown apply": {
			arrange: func(t *testing.T, h *harness) {
				h.capability.outcomes = []Result{{Outcome: reconciliation.OutcomeUnknown}}
			},
			verb: reconciliation.Apply, code: "lifecycle.unknown", message: "an effect has an unresolved outcome",
			want: []string{"resolve it with bootwright apply --context lab, which observes", "take back what it started with bootwright destroy --context lab"},
		},
		"a failed destroy": {
			arrange: func(t *testing.T, h *harness) {
				if _, err := apply(h); err != nil {
					t.Fatal(err)
				}
			},
			verb: reconciliation.Destroy, code: "lifecycle.state", message: "the destroy did not complete",
			want: []string{"repeat bootwright destroy --context lab, which replaces it with a fresh removal of what it has not proved gone"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t, "artifact-server-lab")
			test.arrange(t, h)
			if len(h.capability.outcomes) == 0 {
				h.capability.outcomes = []Result{{Outcome: reconciliation.OutcomeFailed}}
			}
			var err error
			if test.verb == reconciliation.Destroy {
				_, err = h.service.Destroy(context.Background(), DestroyRequest{ContextName: testContextName, SkipConfirmation: true})
			} else {
				_, err = apply(h)
			}
			reported := diagnostics.Of(err)
			last := len(reported) - 1
			if last < 0 || reported[last].Code != test.code || reported[last].Message != test.message {
				t.Fatalf("the %s reported %+v", test.verb, reported)
			}
			for _, want := range test.want {
				if !strings.Contains(reported[last].Remediation, want) {
					t.Fatalf("the %s remedy %q names no %q", test.verb, reported[last].Remediation, want)
				}
			}
		})
	}
}

// A resolution that finds an effect never performed or partly realized names
// the command that takes it on again, in its context. An apply continues
// against its whole frozen plan, so the command carries every token that plan
// consumes, and the removal that takes back what it owns is offered beside
// it. A removal names no command of its own: the result's last diagnostic
// names the destroy that replaces it, with the tokens of what it retains, so
// one result never names the operation's verb in two spellings, and that
// command, run exactly as named, passes authorization.
func TestAResolutionRemedyNamesTheCommandThatTakesItOn(t *testing.T) {
	ctx := context.Background()
	for _, test := range []struct {
		verb    reconciliation.Verb
		effect  reconciliation.EffectState
		message string
		remedy  string
	}{
		{reconciliation.Apply, reconciliation.EffectNoEffect, "the frozen effect was never performed",
			"repeat bootwright apply --context lab --authorize data-loss to perform it"},
		{reconciliation.Apply, reconciliation.EffectPartial, "the frozen effect is partly realized and owned by this context",
			"repeat bootwright apply --context lab --authorize data-loss to converge it, or take back what it owns with bootwright destroy --context lab"},
		{reconciliation.Destroy, reconciliation.EffectNoEffect, "the frozen effect was never performed",
			"run the destroy this result's last diagnostic names, which performs it"},
		{reconciliation.Destroy, reconciliation.EffectPartial, "the frozen effect is partly realized and owned by this context",
			"run the destroy this result's last diagnostic names, which converges it"},
	} {
		t.Run(string(test.verb)+" "+string(test.effect), func(t *testing.T) {
			h := newPlannedHarness(t, []reconciliation.BlockDefinition{destructive("artifacts")})
			h.capability.consumes = map[string][]string{"artifacts": dataLoss()}
			run := func() error {
				if test.verb == reconciliation.Destroy {
					_, err := h.service.Destroy(ctx, DestroyRequest{ContextName: testContextName, Authorizations: dataLoss(), SkipConfirmation: true})
					return err
				}
				_, err := h.service.Apply(ctx, ApplyRequest{ContextName: testContextName, Authorizations: dataLoss(), SkipConfirmation: true})
				return err
			}
			if test.verb == reconciliation.Destroy {
				if _, err := h.service.Apply(ctx, ApplyRequest{ContextName: testContextName, Authorizations: dataLoss(), SkipConfirmation: true}); err != nil {
					t.Fatal(err)
				}
			}
			h.capability.outcomes = []Result{{Outcome: reconciliation.OutcomeUnknown}}
			if err := run(); firstCode(err) != "lifecycle.unknown" {
				t.Fatalf("the seeded unknown %s = %v", test.verb, err)
			}
			h.capability.observations = []Observation{{Effect: test.effect}}
			reported := diagnostics.Of(run())
			index := slices.IndexFunc(reported, func(entry diagnostics.Diagnostic) bool { return entry.Message == test.message })
			if index < 0 || reported[index].Remediation != test.remedy {
				t.Fatalf("the resolving %s reported %+v, want %q with %q", test.verb, reported, test.message, test.remedy)
			}
			command := "bootwright " + string(test.verb) + " --context " + testContextName
			for _, entry := range reported {
				for _, rest := range strings.Split(entry.Remediation, command)[1:] {
					if !strings.HasPrefix(rest, " --authorize data-loss") {
						t.Fatalf("the resolving %s names %s without its token in %q", test.verb, command, entry.Remediation)
					}
				}
			}
			tokens, named := namedTokens(reported[len(reported)-1].Remediation, command)
			if !named || !slices.Equal(tokens, dataLoss()) {
				t.Fatalf("the resolving %s closes with %+v, want %s --authorize data-loss", test.verb, reported[len(reported)-1], command)
			}
			requireAuthorizedIn(t, h, testContextName, test.verb, tokens, "")
		})
	}
}

// namedTokens reads the --authorize tokens that follow command where
// remediation names it.
func namedTokens(remediation, command string) ([]string, bool) {
	_, rest, found := strings.Cut(remediation, command)
	if !found {
		return nil, false
	}
	tokens := []string{}
	fields := strings.Fields(strings.ReplaceAll(rest, ",", " "))
	for index := 0; index+1 < len(fields) && fields[index] == "--authorize"; index += 2 {
		tokens = append(tokens, fields[index+1])
	}
	return tokens, true
}
