package installation

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

type pinnedForm struct {
	evidence         json.RawMessage
	apply, destroy   reconciliation.Outcome
	applyRefusal     error
	destroyRefusal   error
	observe, removal reconciliation.EffectState
}

func pinnedForms(t *testing.T, request Request, unregister bool) map[string]pinnedForm {
	marker, err := MarkerFor(request, "digest")
	if err != nil {
		t.Fatal(err)
	}
	other, _ := MarkerFor(request, "another")
	changed, unknown := reconciliation.OutcomeChanged, reconciliation.OutcomeUnknown
	row := func(evidence json.RawMessage, apply, destroy reconciliation.Outcome, observe, removal reconciliation.EffectState) pinnedForm {
		form := pinnedForm{evidence: evidence, apply: apply, destroy: destroy, observe: observe, removal: removal}
		if apply != changed {
			form.applyRefusal = ValidatePresence(evidence, request, "digest", string(marker))
		}
		if destroy != changed {
			form.destroyRefusal = ValidateAbsence(evidence, "digest")
			if form.destroyRefusal == nil {
				form.destroyRefusal = ValidateReleased(evidence, request, "digest", unregister)
			}
		}
		return form
	}
	encode := func(evidence Evidence) json.RawMessage {
		data, err := json.Marshal(evidence)
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	return map[string]pinnedForm{
		"present":    row(completeEvidence(request, "digest", string(marker)), changed, unknown, reconciliation.EffectCompleted, reconciliation.EffectNoEffect),
		"absent":     row(encode(Evidence{Absent: true, Postcondition: true, Request: "digest"}), unknown, changed, reconciliation.EffectUnknown, reconciliation.EffectCompleted),
		"fresh":      row(encode(Evidence{Power: "Off", Request: "digest"}), unknown, unknown, reconciliation.EffectNoEffect, reconciliation.EffectCompleted),
		"partial":    row(mutate(request, "digest", string(marker), func(e *Evidence) { e.Postcondition = false }), unknown, unknown, reconciliation.EffectPartial, reconciliation.EffectPartial),
		"unfinished": row(encode(Evidence{Marker: string(marker), Power: "On", Private: true, Tree: true, Request: "digest"}), unknown, unknown, reconciliation.EffectPartial, reconciliation.EffectPartial),
		"work left":  row(encode(Evidence{Power: "Off", Request: "digest", Work: true}), unknown, unknown, reconciliation.EffectNoEffect, reconciliation.EffectPartial),
		"foreign":    row(encode(Evidence{Marker: string(other), Power: "On", Request: "digest"}), unknown, unknown, reconciliation.EffectUnknown, reconciliation.EffectCompleted),
		"another":    row(encode(Evidence{Absent: true, Postcondition: true, Request: "another"}), unknown, unknown, reconciliation.EffectUnknown, reconciliation.EffectUnknown),
		"garbage":    row(json.RawMessage(`{`), unknown, unknown, reconciliation.EffectUnknown, reconciliation.EffectUnknown),
	}
}

// Every mutation and resolution is pinned over each observation form, so
// moving them onto the shared lifecycle helpers changes none.
func TestEveryInstallationMutationAndResolutionReadsEachObservation(t *testing.T) {
	call, request := execution(t, "digest")
	for name, form := range pinnedForms(t, request, false) {
		t.Run(name, func(t *testing.T) {
			for verb, want := range map[string]struct {
				outcome reconciliation.Outcome
				refusal error
			}{"apply": {form.apply, form.applyRefusal}, "destroy": {form.destroy, form.destroyRefusal}} {
				runner := &fakeRunner{result: lifecycle.RunResult{Outcome: reconciliation.OutcomeChanged, Evidence: form.evidence}}
				capability := New(runner)
				mutate := capability.Apply
				if verb == "destroy" {
					mutate = capability.Destroy
				}
				result, err := mutate(context.Background(), call)
				wantEvidence := json.RawMessage(nil)
				if want.outcome == reconciliation.OutcomeChanged {
					wantEvidence = form.evidence
				}
				if result.Outcome != want.outcome || !bytes.Equal(result.Evidence, wantEvidence) || described(err) != described(want.refusal) {
					t.Fatalf("%s = %+v (%s), want %s (%s)", verb, result, described(err), want.outcome, described(want.refusal))
				}
				if (want.outcome != reconciliation.OutcomeChanged) != (err != nil) {
					t.Fatalf("%s refusal = %v", verb, err)
				}
				if len(runner.requests) != 1 || runner.requests[0].Operation != verb || runner.requests[0].Block != call.Block.ID {
					t.Fatalf("%s ran %+v", verb, runner.requests)
				}
			}
			for verb, want := range map[string]reconciliation.EffectState{"apply": form.observe, "destroy": form.removal} {
				runner := &fakeRunner{result: lifecycle.RunResult{Outcome: reconciliation.OutcomeUnchanged, Evidence: form.evidence}}
				capability := New(runner)
				observe := capability.Observe
				if verb == "destroy" {
					observe = capability.ObserveRemoval
				}
				observation, err := observe(context.Background(), call)
				if err != nil || observation.Effect != want || observation.Outcome != "" || !bytes.Equal(observation.Evidence, form.evidence) {
					t.Fatalf("%s resolution = %+v (%v), want %s", verb, observation.Effect, err, want)
				}
				if len(runner.requests) != 1 || runner.requests[0].Operation != "observe" || runner.requests[0].Block != call.Block.ID {
					t.Fatalf("%s resolution ran %+v", verb, runner.requests)
				}
			}
		})
	}
}

// A registering installation reads its guest's registration on a removal that
// unregisters, and refuses a removal or an observation that proves it released
// no registration it created.
func TestEveryRegisteringInstallationMutationAndResolutionReadsItsRegistration(t *testing.T) {
	request := pinnedRequest(t, labCatalog(registeringProfile(), rhelEntitlement()))
	marker, _ := MarkerFor(request, "digest")
	encode := func(evidence Evidence) json.RawMessage {
		data, _ := json.Marshal(evidence)
		return data
	}
	registered := func(state string) json.RawMessage {
		registration := RegistrationEvidence{State: state}
		if state == registrationUnregistered {
			registration.Consumer = ownedConsumer
		}
		return encode(Evidence{Absent: true, Postcondition: true, Registration: registration, Request: "digest"})
	}
	unread := encode(Evidence{Absent: true, Postcondition: true, Request: "digest"})
	owned := encode(Evidence{Marker: string(marker), Power: "Off", Registration: RegistrationEvidence{Consumer: ownedConsumer, State: registrationOwned}, Request: "digest"})
	fresh := encode(Evidence{Power: "Off", Request: "digest"})
	for name, test := range map[string]struct {
		proved   bool
		evidence json.RawMessage
		destroy  reconciliation.Outcome
		apply    reconciliation.EffectState
		removal  reconciliation.EffectState
	}{
		"unregistered":                   {true, registered("unregistered"), reconciliation.OutcomeChanged, reconciliation.EffectUnknown, reconciliation.EffectCompleted},
		"absent":                         {true, registered("absent"), reconciliation.OutcomeChanged, reconciliation.EffectUnknown, reconciliation.EffectCompleted},
		"unread, proved":                 {true, unread, reconciliation.OutcomeUnknown, reconciliation.EffectUnknown, reconciliation.EffectInconclusive},
		"owned, proved":                  {true, owned, reconciliation.OutcomeUnknown, reconciliation.EffectPartial, reconciliation.EffectPartial},
		"unread, never prepared":         {false, unread, reconciliation.OutcomeChanged, reconciliation.EffectUnknown, reconciliation.EffectCompleted},
		"registration read, unprepared":  {false, registered("absent"), reconciliation.OutcomeUnknown, reconciliation.EffectUnknown, reconciliation.EffectInconclusive},
		"nothing yet, never prepared":    {false, fresh, reconciliation.OutcomeUnknown, reconciliation.EffectNoEffect, reconciliation.EffectCompleted},
		"nothing yet, registered before": {true, fresh, reconciliation.OutcomeUnknown, reconciliation.EffectPartial, reconciliation.EffectInconclusive},
	} {
		t.Run(name, func(t *testing.T) {
			call := encryptionExecution(t, request, nil)
			if test.proved {
				call = proving(t, call, request)
			}
			runner := &fakeRunner{result: lifecycle.RunResult{Outcome: "changed", Evidence: test.evidence}}
			result, err := New(runner).Destroy(context.Background(), call)
			wantEvidence := json.RawMessage(nil)
			if test.destroy == reconciliation.OutcomeChanged {
				wantEvidence = test.evidence
			}
			if result.Outcome != test.destroy || !bytes.Equal(result.Evidence, wantEvidence) || (err != nil) != (test.destroy != reconciliation.OutcomeChanged) {
				t.Fatalf("destroy = %+v (%s), want %s", result, described(err), test.destroy)
			}
			if test.destroy == reconciliation.OutcomeUnknown {
				want := ValidateAbsence(test.evidence, "digest")
				if want == nil {
					want = ValidateReleased(test.evidence, request, "digest", test.proved)
				}
				if described(err) != described(want) {
					t.Fatalf("destroy refused with %s, want %s", described(err), described(want))
				}
			}
			runner = &fakeRunner{result: lifecycle.RunResult{Outcome: "unchanged", Evidence: test.evidence}}
			observation, err := New(runner).Observe(context.Background(), call)
			if err != nil || observation.Effect != test.apply {
				t.Fatalf("observe = %s (%v), want %s", observation.Effect, err, test.apply)
			}
			runner = &fakeRunner{result: lifecycle.RunResult{Outcome: "unchanged", Evidence: test.evidence}}
			observation, err = New(runner).ObserveRemoval(context.Background(), call)
			if err != nil || observation.Effect != test.removal {
				t.Fatalf("removal observation = %s (%v), want %s", observation.Effect, err, test.removal)
			}
		})
	}
	garbage := encryptionExecution(t, request, nil)
	garbage.ApplyPreparations = []json.RawMessage{json.RawMessage(`{"marker":"other"}`)}
	runner := &fakeRunner{result: lifecycle.RunResult{Outcome: "changed", Evidence: registered("unregistered")}}
	result, err := New(runner).Destroy(context.Background(), garbage)
	if err == nil || result.Outcome != reconciliation.OutcomeFailed || len(runner.requests) != 0 {
		t.Fatalf("a before-state of another marker = %+v (%v) after %d runs", result, err, len(runner.requests))
	}
	observation, err := New(runner).ObserveRemoval(context.Background(), garbage)
	if err == nil || observation.Effect != reconciliation.EffectUnknown || len(runner.requests) != 0 {
		t.Fatalf("a removal observed over a before-state of another marker = %+v (%v) after %d runs", observation, err, len(runner.requests))
	}
}

// A run that fails, an outcome the adapter cannot settle, a missing adapter, an
// unreadable request and a cancelled call each refuse with their own outcome,
// before or instead of any evidence check.
func TestEveryInstallationMutationRefusesWhatItCannotRead(t *testing.T) {
	call, request := execution(t, "digest")
	present := pinnedForms(t, request, false)["present"].evidence
	unreachable := errors.New("unreachable")
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	malformed := call
	malformed.Block.Request = json.RawMessage(`{`)
	unusableOutcome := described(installNoOutcome.Fail("the installation adapter reported no usable outcome", ""))
	settled := lifecycle.RunResult{Outcome: "changed", Evidence: present}
	for name, test := range map[string]struct {
		ctx       context.Context
		runner    *fakeRunner
		missing   bool
		execution lifecycle.Execution
		outcome   reconciliation.Outcome
		refusal   string
		runs      int
		effect    reconciliation.EffectState
	}{
		"runner error":     {context.Background(), &fakeRunner{err: unreachable}, false, call, reconciliation.OutcomeUnknown, described(unreachable), 1, reconciliation.EffectUnknown},
		"unusable outcome": {context.Background(), &fakeRunner{result: lifecycle.RunResult{Outcome: "", Evidence: present}}, false, call, reconciliation.OutcomeUnknown, unusableOutcome, 1, reconciliation.EffectCompleted},
		"other outcome":    {context.Background(), &fakeRunner{result: lifecycle.RunResult{Outcome: "skipped", Evidence: present}}, false, call, reconciliation.OutcomeUnknown, unusableOutcome, 1, reconciliation.EffectCompleted},
		"no adapter":       {context.Background(), nil, true, call, reconciliation.OutcomeFailed, described(installUnconfigured.Fail("the installation adapter is not configured", "")), 0, reconciliation.EffectUnknown},
		"cancelled":        {cancelled, &fakeRunner{result: settled}, false, call, reconciliation.OutcomeFailed, described(context.Canceled), 0, reconciliation.EffectUnknown},
		"malformed":        {context.Background(), &fakeRunner{result: settled}, false, malformed, reconciliation.OutcomeFailed, "", 0, reconciliation.EffectUnknown},
	} {
		t.Run(name, func(t *testing.T) {
			for _, verb := range []string{"apply", "destroy"} {
				var capability Capability
				var runner *fakeRunner
				if test.missing {
					capability = New(nil)
				} else {
					fresh := *test.runner
					runner = &fresh
					capability = New(runner)
				}
				mutate := capability.Apply
				if verb == "destroy" {
					mutate = capability.Destroy
				}
				result, err := mutate(test.ctx, test.execution)
				if err == nil || result.Outcome != test.outcome || result.Evidence != nil || test.refusal != "" && described(err) != test.refusal {
					t.Fatalf("%s = %+v (%s), want %s (%s)", verb, result, described(err), test.outcome, test.refusal)
				}
				if name == "runner error" && !errors.Is(err, unreachable) {
					t.Fatalf("the runner's error was replaced: %v", err)
				}
				if runner != nil && len(runner.requests) != test.runs {
					t.Fatalf("%s ran %d times, want %d", verb, len(runner.requests), test.runs)
				}
			}
			capability := New(nil)
			if !test.missing {
				fresh := *test.runner
				capability = New(&fresh)
			}
			observation, err := capability.Observe(test.ctx, test.execution)
			if (err != nil) != (test.effect == reconciliation.EffectUnknown) || observation.Effect != test.effect {
				t.Fatalf("observe = %+v (%v), want %s", observation, err, test.effect)
			}
		})
	}
}
