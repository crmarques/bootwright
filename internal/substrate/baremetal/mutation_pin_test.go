package baremetal

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

func described(err error) string {
	if err == nil {
		return ""
	}
	if found := diagnostics.Of(err); len(found) != 0 {
		return fmt.Sprintf("%+v", found)
	}
	return err.Error()
}

// The claim's mutations and its apply's resolution are pinned over each form
// of the machine's evidence, so moving them onto the shared lifecycle skeleton
// changes none.
func TestEveryClaimMutationAndResolutionReadsEachObservation(t *testing.T) {
	frozen, err := reconciliation.NewPlan(reconciliation.Apply, planOf(t, reconciliation.Apply).Definitions)
	if err != nil {
		t.Fatal(err)
	}
	call := lifecycle.Execution{Operation: "op-1", Attempt: 1, Block: frozen.Blocks[0]}
	digest, request := call.Block.RequestDigest, fixtureRequest(t)
	addresses := request.Addresses()
	changed, unknown := reconciliation.OutcomeChanged, reconciliation.OutcomeUnknown
	for name, test := range map[string]struct {
		evidence       json.RawMessage
		apply, destroy reconciliation.Outcome
		observe        reconciliation.EffectState
	}{
		"proved":   {encode(t, Evidence{Addresses: addresses, Postcondition: true, Power: "On", Request: digest, Serial: "SN1", UUID: "uuid-1"}), changed, unknown, reconciliation.EffectCompleted},
		"released": {encode(t, Evidence{Absent: true, Addresses: []string{}, Postcondition: true, Request: digest}), unknown, changed, reconciliation.EffectUnknown},
		"unproved": {encode(t, Evidence{Addresses: addresses, Power: "On", Request: digest, UUID: "uuid-1"}), unknown, unknown, reconciliation.EffectUnknown},
		"partial":  {encode(t, Evidence{Addresses: addresses[:1], Postcondition: true, Power: "On", Request: digest, UUID: "uuid-1"}), unknown, unknown, reconciliation.EffectUnknown},
		"another":  {encode(t, Evidence{Addresses: addresses, Postcondition: true, Power: "On", Request: "another", UUID: "uuid-1"}), unknown, unknown, reconciliation.EffectUnknown},
		"garbage":  {json.RawMessage(`{`), unknown, unknown, reconciliation.EffectUnknown},
	} {
		t.Run(name, func(t *testing.T) {
			for verb, want := range map[string]struct {
				outcome reconciliation.Outcome
				refusal error
			}{
				"apply":   {test.apply, ValidatePresence(test.evidence, request, digest)},
				"destroy": {test.destroy, ValidateAbsence(test.evidence, digest)},
			} {
				runner := &scriptedRunner{result: lifecycle.RunResult{Outcome: reconciliation.OutcomeChanged, Evidence: test.evidence}}
				mutate := NewMachine(runner).Apply
				if verb == "destroy" {
					mutate = NewMachine(runner).Destroy
				}
				result, err := mutate(context.Background(), call)
				wantEvidence := json.RawMessage(nil)
				if want.outcome != unknown {
					wantEvidence = test.evidence
				}
				if result.Outcome != want.outcome || !bytes.Equal(result.Evidence, wantEvidence) || described(err) != described(want.refusal) {
					t.Fatalf("%s = %+v (%s), want %s (%s)", verb, result, described(err), want.outcome, described(want.refusal))
				}
				if len(runner.requests) != 1 || runner.requests[0].Operation != verb {
					t.Fatalf("%s ran %+v", verb, runner.requests)
				}
			}
			runner := &scriptedRunner{result: lifecycle.RunResult{Outcome: reconciliation.OutcomeChanged, Evidence: test.evidence}}
			observation, err := NewMachine(runner).Observe(context.Background(), call)
			wantOutcome := reconciliation.Outcome("")
			if test.observe == reconciliation.EffectCompleted {
				wantOutcome = changed
			}
			if err != nil || observation.Effect != test.observe || observation.Outcome != wantOutcome || !bytes.Equal(observation.Evidence, test.evidence) {
				t.Fatalf("observation = %+v (%v), want %s", observation, err, test.observe)
			}
		})
	}
	unreachable := errors.New("unreachable")
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	proved := encode(t, Evidence{Addresses: addresses, Postcondition: true, Power: "On", Request: digest, Serial: "SN1", UUID: "uuid-1"})
	for name, test := range map[string]struct {
		ctx     context.Context
		runner  Runner
		outcome reconciliation.Outcome
		refusal string
	}{
		"runner error":     {context.Background(), &scriptedRunner{err: unreachable}, unknown, described(unreachable)},
		"unusable outcome": {context.Background(), &scriptedRunner{result: lifecycle.RunResult{Outcome: "", Evidence: proved}}, unknown, described(baremetalNoUsableOutcome.Fail("the machine adapter reported no usable outcome", ""))},
		"no adapter":       {context.Background(), nil, reconciliation.OutcomeFailed, described(baremetalUnconfigured.Fail("the machine adapter is not configured", ""))},
		"cancelled":        {cancelled, &scriptedRunner{result: lifecycle.RunResult{Outcome: "changed", Evidence: proved}}, reconciliation.OutcomeFailed, described(context.Canceled)},
	} {
		t.Run(name, func(t *testing.T) {
			for _, mutate := range []func(context.Context, lifecycle.Execution) (lifecycle.Result, error){NewMachine(test.runner).Apply, NewMachine(test.runner).Destroy} {
				result, err := mutate(test.ctx, call)
				if result.Outcome != test.outcome || result.Evidence != nil || described(err) != test.refusal {
					t.Fatalf("mutation = %+v (%s), want %s (%s)", result, described(err), test.outcome, test.refusal)
				}
				if result.Outcome == reconciliation.OutcomeUnknown && lifecycle.AttemptOutcome(err) != result.Outcome {
					t.Fatalf("mutation returns unknown with a stop that records its attempt %s: %s", lifecycle.AttemptOutcome(err), described(err))
				}
			}
		})
	}
}
