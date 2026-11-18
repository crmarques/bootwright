package managedservice

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets"
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

func pinnedExecution(t *testing.T, definition Definition, objects ...api.Object) (lifecycle.Execution, Request) {
	catalog := catalogOf(append([]api.Object{controller()}, objects...)...)
	plan, err := NewCapability(definition, nil).Plan(context.Background(), lifecycle.PlanInput{
		Verb: reconciliation.Apply, Context: lifecycle.ContextIdentity{Name: testContext},
		State: compilation.NewState(catalog, catalog, nil), Controller: "controller",
	})
	if err != nil {
		t.Fatal(err)
	}
	frozen, err := reconciliation.NewPlan(reconciliation.Apply, plan.Definitions)
	if err != nil {
		t.Fatal(err)
	}
	call := lifecycle.Execution{Operation: "op-1", Attempt: 1, Block: frozen.Blocks[0], Context: testContext}
	request, err := DecodeRequest(call.Block.Request, definition.Version)
	if err != nil {
		t.Fatal(err)
	}
	return call, request
}

func pinnedForms(t *testing.T, request Request, digest, fingerprint string) map[string]pinnedForm {
	changed, unknown, failed := reconciliation.OutcomeChanged, reconciliation.OutcomeUnknown, reconciliation.OutcomeFailed
	answer := Answer{Address: "192.0.2.1", Answer: "HTTP/1.1 400 Bad Request", Port: request.Port, Fingerprint: fingerprint}
	present := Evidence{
		Answers: []Answer{answer}, Container: request.Image, ContentRoot: true, Postcondition: true, Request: digest, StartedAfterFiles: true, Unit: "active",
	}
	silent := present
	silent.Answers = []Answer{}
	otherImage := present
	otherImage.Container = "docker.io/library/squid:6"
	anotherLeaf := present
	anotherLeaf.Answers = []Answer{{Address: "192.0.2.1", Answer: "HTTP/1.1 400 Bad Request", Port: request.Port, Fingerprint: "other"}}
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
	forms := map[string]pinnedForm{
		"present":    row(encode(t, present), changed, unknown, reconciliation.EffectCompleted, reconciliation.EffectNoEffect),
		"absent":     row(encode(t, Evidence{Absent: true, Answers: []Answer{}, Postcondition: true, Request: digest}), unknown, changed, reconciliation.EffectNoEffect, reconciliation.EffectCompleted),
		"partial":    row(encode(t, Evidence{Answers: []Answer{}, ContentRoot: true, Request: digest, Unit: "inactive"}), unknown, unknown, reconciliation.EffectPartial, reconciliation.EffectPartial),
		"unremoved":  row(encode(t, silent), unknown, unknown, reconciliation.EffectPartial, reconciliation.EffectNoEffect),
		"unfinished": row(encode(t, otherImage), unknown, unknown, reconciliation.EffectPartial, reconciliation.EffectPartial),
		"foreign":    row(encode(t, Evidence{Absent: true, Answers: []Answer{}, Postcondition: true, Request: strings.Repeat("e", 64)}), unknown, unknown, reconciliation.EffectUnknown, reconciliation.EffectUnknown),
		"garbage":    row(json.RawMessage(`{`), unknown, unknown, reconciliation.EffectUnknown, reconciliation.EffectUnknown),
	}
	if fingerprint != "" {
		forms["another leaf"] = row(encode(t, anotherLeaf), failed, unknown, reconciliation.EffectPartial, reconciliation.EffectNoEffect)
	}
	return forms
}

func pinnedTLSDefinition() (Definition, []api.Object) {
	definition := testDefinition()
	definition.Extend = func(_ api.Catalog, _ api.Value, request *Request) error {
		request.TLS = &TLS{MinVersion: "1.2", Secret: "proxy-tls"}
		return nil
	}
	definition.Certificate = func(_ map[string]secrets.Material, _, _ string, _ []string, _ time.Time, _ bool) (string, error) {
		return "bound", nil
	}
	return definition, []api.Object{service(api.Proxy, "lab-proxy")}
}

// Every mutation and resolution is pinned over each observation form, so
// moving them onto the shared lifecycle skeleton changes none.
func TestEveryManagedServiceMutationAndResolutionReadsEachObservation(t *testing.T) {
	plain := testDefinition()
	tls, tlsObjects := pinnedTLSDefinition()
	for flavor, setup := range map[string]struct {
		definition  Definition
		objects     []api.Object
		fingerprint string
	}{
		"plain": {plain, []api.Object{service(api.Proxy, "lab-proxy")}, ""},
		"tls":   {tls, tlsObjects, "bound"},
	} {
		call, request := pinnedExecution(t, setup.definition, setup.objects...)
		for name, form := range pinnedForms(t, request, call.Block.RequestDigest, setup.fingerprint) {
			t.Run(flavor+"/"+name, func(t *testing.T) {
				for verb, want := range map[string]struct {
					outcome reconciliation.Outcome
					refusal error
				}{"apply": {form.apply, form.applyRefusal}, "destroy": {form.destroy, form.destroyRefusal}} {
					runner := &scriptedRunner{result: lifecycle.RunResult{Outcome: reconciliation.OutcomeChanged, Evidence: form.evidence}}
					capability := NewCapability(setup.definition, runner)
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
					runner := &scriptedRunner{result: lifecycle.RunResult{Outcome: reconciliation.OutcomeUnchanged, Evidence: form.evidence}}
					capability := NewCapability(setup.definition, runner)
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
}

// A run that fails, an outcome the adapter cannot settle, a missing adapter, an
// unreadable request and a cancelled call each refuse with their own outcome,
// before or instead of any evidence check.
func TestEveryManagedServiceMutationRefusesWhatItCannotRead(t *testing.T) {
	call, request := pinnedExecution(t, testDefinition(), service(api.Proxy, "lab-proxy"))
	present := pinnedForms(t, request, call.Block.RequestDigest, "")["present"].evidence
	unreachable := errors.New("unreachable")
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	malformed := call
	malformed.Block.Request = json.RawMessage(`{`)
	unusableOutcome := described(serviceOutcomeUnusable.Fail("the managed service adapter reported no usable outcome", ""))
	settled := lifecycle.RunResult{Outcome: "changed", Evidence: present}
	for name, test := range map[string]struct {
		ctx       context.Context
		runner    *scriptedRunner
		missing   bool
		execution lifecycle.Execution
		outcome   reconciliation.Outcome
		refusal   string
		runs      int
		effect    reconciliation.EffectState
	}{
		"runner error":     {context.Background(), &scriptedRunner{err: unreachable}, false, call, reconciliation.OutcomeUnknown, described(unreachable), 1, reconciliation.EffectUnknown},
		"unusable outcome": {context.Background(), &scriptedRunner{result: lifecycle.RunResult{Outcome: "", Evidence: present}}, false, call, reconciliation.OutcomeUnknown, unusableOutcome, 1, reconciliation.EffectCompleted},
		"other outcome":    {context.Background(), &scriptedRunner{result: lifecycle.RunResult{Outcome: "skipped", Evidence: present}}, false, call, reconciliation.OutcomeUnknown, unusableOutcome, 1, reconciliation.EffectCompleted},
		"no adapter":       {context.Background(), nil, true, call, reconciliation.OutcomeFailed, described(serviceUnconfigured.Fail("the managed service adapter is not configured", "")), 0, reconciliation.EffectUnknown},
		"cancelled":        {cancelled, &scriptedRunner{result: settled}, false, call, reconciliation.OutcomeFailed, described(context.Canceled), 0, reconciliation.EffectUnknown},
		"malformed":        {context.Background(), &scriptedRunner{result: settled}, false, malformed, reconciliation.OutcomeFailed, "", 0, reconciliation.EffectUnknown},
	} {
		t.Run(name, func(t *testing.T) {
			for _, verb := range []string{"apply", "destroy"} {
				var capability Capability
				var runner *scriptedRunner
				if test.missing {
					capability = NewCapability(testDefinition(), nil)
				} else {
					fresh := *test.runner
					runner = &fresh
					capability = NewCapability(testDefinition(), runner)
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
			capability := NewCapability(testDefinition(), nil)
			if !test.missing {
				fresh := *test.runner
				capability = NewCapability(testDefinition(), &fresh)
			}
			observation, err := capability.Observe(test.ctx, test.execution)
			if (err != nil) != (test.effect == reconciliation.EffectUnknown) || observation.Effect != test.effect {
				t.Fatalf("observe = %+v (%v), want %s", observation, err, test.effect)
			}
		})
	}
}

// A served certificate the definition cannot prove refuses an apply before
// the adapter runs, while a removal and an observation, which prove nothing
// about it, still run.
func TestAnUnprovedCertificateRefusesOnlyAnApplyBeforeTheAdapter(t *testing.T) {
	definition, objects := pinnedTLSDefinition()
	call, request := pinnedExecution(t, definition, objects...)
	present := pinnedForms(t, request, call.Block.RequestDigest, "bound")["present"].evidence
	definition.Certificate = func(_ map[string]secrets.Material, _, _ string, _ []string, _ time.Time, _ bool) (string, error) {
		return "", Refusal("lifecycle.state", "the certificate does not cover the service", "")
	}
	runner := &scriptedRunner{result: lifecycle.RunResult{Outcome: "changed", Evidence: present}}
	capability := NewCapability(definition, runner)
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
