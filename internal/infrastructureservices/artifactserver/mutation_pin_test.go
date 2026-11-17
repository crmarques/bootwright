package artifactserver

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

func pinnedForms(t *testing.T, call lifecycle.Execution, fingerprint string) map[string]pinnedForm {
	request, err := DecodeRequest(call.Block.Request)
	if err != nil {
		t.Fatal(err)
	}
	digest := call.Block.RequestDigest
	present := func(change func(*Evidence)) json.RawMessage {
		var evidence Evidence
		if err := json.Unmarshal(presenceEvidence(request, digest, fingerprint), &evidence); err != nil {
			t.Fatal(err)
		}
		change(&evidence)
		data, err := json.Marshal(evidence)
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	changed, unknown, failed := reconciliation.OutcomeChanged, reconciliation.OutcomeUnknown, reconciliation.OutcomeFailed
	row := func(evidence json.RawMessage, apply, destroy reconciliation.Outcome, observe, removal reconciliation.EffectState) pinnedForm {
		form := pinnedForm{evidence: evidence, apply: apply, destroy: destroy, observe: observe, removal: removal}
		if apply != changed {
			form.applyRefusal = ValidatePresence(evidence, request, digest, fingerprint)
		}
		if destroy != changed {
			form.destroyRefusal = ValidateAbsence(evidence, digest)
		}
		return form
	}
	silent := func(e *Evidence) { e.Listeners = []ListenerEvidence{} }
	anotherLeaf := func(e *Evidence) {
		for index := range e.Listeners {
			if e.Listeners[index].Protocol == "https" {
				e.Listeners[index].Fingerprint = testFingerprint
			}
		}
	}
	return map[string]pinnedForm{
		"present":      row(presenceEvidence(request, digest, fingerprint), changed, unknown, reconciliation.EffectCompleted, reconciliation.EffectNoEffect),
		"absent":       row(absenceEvidence(digest), unknown, changed, reconciliation.EffectNoEffect, reconciliation.EffectCompleted),
		"partial":      row(json.RawMessage(`{"absent":false,"container":"","contentRoot":true,"listeners":[],"postcondition":false,"request":"`+digest+`","unit":"active"}`), unknown, unknown, reconciliation.EffectPartial, reconciliation.EffectPartial),
		"unremoved":    row(present(silent), unknown, unknown, reconciliation.EffectPartial, reconciliation.EffectNoEffect),
		"unfinished":   row(present(func(e *Evidence) { e.Container = "registry.example.test/other:1" }), unknown, unknown, reconciliation.EffectPartial, reconciliation.EffectPartial),
		"another leaf": row(present(anotherLeaf), failed, unknown, reconciliation.EffectPartial, reconciliation.EffectNoEffect),
		"foreign":      row(absenceEvidence(testDigest), unknown, unknown, reconciliation.EffectUnknown, reconciliation.EffectUnknown),
		"garbage":      row(json.RawMessage(`{`), unknown, unknown, reconciliation.EffectUnknown, reconciliation.EffectUnknown),
	}
}

// Every mutation and resolution is pinned over each observation form, so
// moving them onto the shared lifecycle skeleton changes none.
func TestEveryArtifactServerMutationAndResolutionReadsEachObservation(t *testing.T) {
	material, fingerprint := issue(t, validOptions())
	call := execution(t, material)
	for name, form := range pinnedForms(t, call, fingerprint) {
		t.Run(name, func(t *testing.T) {
			for verb, want := range map[string]struct {
				outcome reconciliation.Outcome
				refusal error
			}{"apply": {form.apply, form.applyRefusal}, "destroy": {form.destroy, form.destroyRefusal}} {
				runner := &fakeRunner{result: lifecycle.RunResult{Outcome: reconciliation.OutcomeChanged, Evidence: form.evidence}}
				capability := New(runner, fixedClock{})
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
				capability := New(runner, fixedClock{})
				observe := capability.Observe
				if verb == "destroy" {
					observe = capability.ObserveRemoval
				}
				observation, err := observe(context.Background(), call)
				if err != nil || observation.Effect != want || observation.Outcome != "" || !bytes.Equal(observation.Evidence, form.evidence) {
					t.Fatalf("%s resolution = %+v (%v), want %s", verb, observation, err, want)
				}
				if len(runner.requests) != 1 || runner.requests[0].Operation != "observe" || runner.requests[0].Block != call.Block.ID {
					t.Fatalf("%s resolution ran %+v", verb, runner.requests)
				}
			}
		})
	}
}

// A run that fails, an outcome the adapter cannot settle, a missing adapter, an
// unreadable request and a cancelled call each refuse with their own outcome,
// before or instead of any evidence check.
func TestEveryArtifactServerMutationRefusesWhatItCannotRead(t *testing.T) {
	material, fingerprint := issue(t, validOptions())
	call := execution(t, material)
	present := pinnedForms(t, call, fingerprint)["present"].evidence
	unreachable := errors.New("unreachable")
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	malformed := call
	malformed.Block.Request = json.RawMessage(`{`)
	unusableOutcome := described(refusal("lifecycle.state", "the artifact-server adapter reported no usable outcome", ""))
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
		"no adapter":       {context.Background(), nil, true, call, reconciliation.OutcomeFailed, described(refusal("lifecycle.state", "the artifact-server adapter is not configured", "")), 0, reconciliation.EffectUnknown},
		"cancelled":        {cancelled, &fakeRunner{result: settled}, false, call, reconciliation.OutcomeFailed, described(context.Canceled), 0, reconciliation.EffectUnknown},
		"malformed":        {context.Background(), &fakeRunner{result: settled}, false, malformed, reconciliation.OutcomeFailed, "", 0, reconciliation.EffectUnknown},
	} {
		t.Run(name, func(t *testing.T) {
			for _, verb := range []string{"apply", "destroy"} {
				var capability Capability
				var runner *fakeRunner
				if test.missing {
					capability = New(nil, fixedClock{})
				} else {
					fresh := *test.runner
					runner = &fresh
					capability = New(runner, fixedClock{})
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
			observeRunner := &fakeRunner{}
			if test.runner != nil {
				fresh := *test.runner
				observeRunner = &fresh
			}
			capability := New(observeRunner, fixedClock{})
			if test.missing {
				capability = New(nil, fixedClock{})
			}
			observation, err := capability.Observe(test.ctx, test.execution)
			if (err != nil) != (test.effect == reconciliation.EffectUnknown) || observation.Effect != test.effect {
				t.Fatalf("observe = %+v (%v), want %s", observation, err, test.effect)
			}
		})
	}
}

// A certificate the bound material cannot prove refuses an apply before the
// adapter runs, while a removal and an observation, which prove nothing about
// it, still run.
func TestAnUnusableCertificateRefusesOnlyAnApplyBeforeTheAdapter(t *testing.T) {
	material, fingerprint := issue(t, validOptions())
	present := pinnedForms(t, execution(t, material), fingerprint)["present"].evidence
	uncovered, _ := issue(t, certificateOptions{ips: []string{"203.0.113.9"}, notBefore: testMoment.Add(-1), notAfter: testMoment.Add(1)})
	call := execution(t, uncovered)
	runner := &fakeRunner{result: lifecycle.RunResult{Outcome: "changed", Evidence: present}}
	capability := New(runner, fixedClock{})
	result, err := capability.Apply(context.Background(), call)
	if err == nil || result.Outcome != reconciliation.OutcomeFailed || len(runner.requests) != 0 {
		t.Fatalf("apply = %+v (%v) after %d runs", result, err, len(runner.requests))
	}
	result, err = capability.Destroy(context.Background(), call)
	if err == nil || result.Outcome != reconciliation.OutcomeUnknown || len(runner.requests) != 1 || runner.requests[0].Operation != "destroy" {
		t.Fatalf("destroy = %+v (%v) after %+v", result, err, runner.requests)
	}
	observation, err := capability.Observe(context.Background(), call)
	if err != nil || observation.Effect != reconciliation.EffectPartial || len(runner.requests) != 2 {
		t.Fatalf("observe = %+v (%v) after %d runs", observation, err, len(runner.requests))
	}
}
