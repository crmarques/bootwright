package agentinstall

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/secrets/material"
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

type pinnedArm struct {
	execution lifecycle.Execution
	subject   string
	capable   func(Runner) lifecycle.Capability
	forms     map[string]pinnedForm
}

func pinnedArms(t *testing.T) map[string]pinnedArm {
	changed, unknown := reconciliation.OutcomeChanged, reconciliation.OutcomeUnknown
	mediaCall, media := mediaExecution(t, testDigest)
	mediaForm := func(evidence json.RawMessage, apply, destroy reconciliation.Outcome, observe, removal reconciliation.EffectState) pinnedForm {
		form := pinnedForm{evidence: evidence, apply: apply, destroy: destroy, observe: observe, removal: removal}
		if apply != changed {
			form.applyRefusal = ValidateMediaPresence(evidence, media, testDigest)
		}
		if destroy != changed {
			form.destroyRefusal = ValidateMediaAbsence(evidence, testDigest)
		}
		return form
	}
	mediaOf := func(change func(*MediaEvidence)) json.RawMessage { return mediaEvidence(t, testDigest, change) }
	installCall, install := installExecution(t, singleNodeCatalog(), testDigest)
	installForm := func(evidence json.RawMessage, apply, destroy reconciliation.Outcome, observe, removal reconciliation.EffectState) pinnedForm {
		form := pinnedForm{evidence: evidence, apply: apply, destroy: destroy, observe: observe, removal: removal}
		if apply != changed {
			form.applyRefusal = ValidateInstallPresence(evidence, install, testDigest)
		}
		if destroy != changed {
			form.destroyRefusal = ValidateInstallAbsence(evidence, testDigest)
		}
		return form
	}
	installOf := func(change func(*InstallEvidence)) json.RawMessage { return installEvidence(t, testDigest, change) }
	idle := func(e *InstallEvidence) {
		*e = InstallEvidence{Request: testDigest, Media: []string{}, Missing: []string{}, Powered: []string{}}
	}
	return map[string]pinnedArm{
		"media": {
			execution: mediaCall, subject: "boot-media",
			capable: func(runner Runner) lifecycle.Capability { return NewMedia(runner, material.AuthorizedKey) },
			forms: map[string]pinnedForm{
				"present":       mediaForm(mediaOf(nil), changed, unknown, reconciliation.EffectCompleted, reconciliation.EffectNoEffect),
				"absent":        mediaForm(mediaOf(func(e *MediaEvidence) { *e = MediaEvidence{Absent: true, Postcondition: true, Request: testDigest} }), unknown, changed, reconciliation.EffectNoEffect, reconciliation.EffectCompleted),
				"fresh":         mediaForm(mediaOf(func(e *MediaEvidence) { *e = MediaEvidence{Request: testDigest} }), unknown, unknown, reconciliation.EffectNoEffect, reconciliation.EffectCompleted),
				"partial":       mediaForm(mediaOf(func(e *MediaEvidence) { e.Postcondition = false }), unknown, unknown, reconciliation.EffectPartial, reconciliation.EffectPartial),
				"work left":     mediaForm(mediaOf(func(e *MediaEvidence) { e.Postcondition, e.Image = false, false }), unknown, unknown, reconciliation.EffectPartial, reconciliation.EffectPartial),
				"unfinished":    mediaForm(mediaOf(func(e *MediaEvidence) { e.Absent, e.Image = true, false }), unknown, unknown, reconciliation.EffectUnknown, reconciliation.EffectUnknown),
				"another build": mediaForm(mediaOf(func(e *MediaEvidence) { e.Inputs = strings.Repeat("2", 64) }), unknown, unknown, reconciliation.EffectUnknown, reconciliation.EffectUnknown),
				"foreign":       mediaForm(mediaOf(func(e *MediaEvidence) { e.Request = strings.Repeat("3", 64) }), unknown, unknown, reconciliation.EffectUnknown, reconciliation.EffectUnknown),
				"garbage":       mediaForm(json.RawMessage(`{`), unknown, unknown, reconciliation.EffectUnknown, reconciliation.EffectUnknown),
			},
		},
		"install": {
			execution: installCall, subject: "installation",
			capable: func(runner Runner) lifecycle.Capability { return NewInstall(runner) },
			forms: map[string]pinnedForm{
				"present": installForm(installOf(nil), changed, unknown, reconciliation.EffectCompleted, reconciliation.EffectCompleted),
				"absent":  installForm(installOf(func(e *InstallEvidence) { e.Absent, e.Cluster, e.Identity, e.Powered = true, "", "", []string{} }), unknown, changed, reconciliation.EffectNoEffect, reconciliation.EffectCompleted),
				"idle":    installForm(installOf(idle), unknown, unknown, reconciliation.EffectNoEffect, reconciliation.EffectCompleted),
				"partial": installForm(installOf(func(e *InstallEvidence) {
					e.Postcondition, e.Completed, e.Media, e.OwnMedia = false, false, []string{"sno-01"}, []string{"sno-01"}
				}), unknown, unknown, reconciliation.EffectPartial, reconciliation.EffectPartial),
				"not completed":   installForm(installOf(func(e *InstallEvidence) { e.Completed = false }), unknown, unknown, reconciliation.EffectUnknown, reconciliation.EffectCompleted),
				"foreign media":   installForm(installOf(func(e *InstallEvidence) { e.Media = []string{"other-machine"} }), unknown, unknown, reconciliation.EffectUnknown, reconciliation.EffectUnknown),
				"another cluster": installForm(installOf(func(e *InstallEvidence) { e.Cluster = foreignAnswer }), unknown, unknown, reconciliation.EffectUnknown, reconciliation.EffectCompleted),
				"foreign":         installForm(installOf(func(e *InstallEvidence) { e.Request = strings.Repeat("3", 64) }), unknown, unknown, reconciliation.EffectUnknown, reconciliation.EffectUnknown),
				"garbage":         installForm(json.RawMessage(`{`), unknown, unknown, reconciliation.EffectUnknown, reconciliation.EffectUnknown),
			},
		},
	}
}

// Every mutation and resolution of both arms is pinned over each observation
// form, so moving them onto the shared lifecycle helpers changes none.
func TestEveryAgentInstallMutationAndResolutionReadsEachObservation(t *testing.T) {
	for arm, pinned := range pinnedArms(t) {
		for name, form := range pinned.forms {
			t.Run(arm+"/"+name, func(t *testing.T) {
				for verb, want := range map[string]struct {
					outcome reconciliation.Outcome
					refusal error
				}{"apply": {form.apply, form.applyRefusal}, "destroy": {form.destroy, form.destroyRefusal}} {
					runner := &fakeRunner{result: lifecycle.RunResult{Outcome: reconciliation.OutcomeChanged, Evidence: form.evidence}}
					capability := pinned.capable(runner)
					mutate := capability.Apply
					if verb == "destroy" {
						mutate = capability.Destroy
					}
					result, err := mutate(context.Background(), pinned.execution)
					wantEvidence := json.RawMessage(nil)
					if want.outcome == reconciliation.OutcomeChanged {
						wantEvidence = form.evidence
					}
					if result.Outcome != want.outcome || !bytes.Equal(result.Evidence, wantEvidence) || described(err) != described(want.refusal) || len(result.Produced) != 0 {
						t.Fatalf("%s = %+v (%s), want %s (%s)", verb, result, described(err), want.outcome, described(want.refusal))
					}
					if (want.outcome != reconciliation.OutcomeChanged) != (err != nil) {
						t.Fatalf("%s refusal = %v", verb, err)
					}
					if len(runner.requests) != 1 || runner.requests[0].Operation != verb || runner.requests[0].Block != pinned.execution.Block.ID {
						t.Fatalf("%s ran %+v", verb, runner.requests)
					}
				}
				for verb, want := range map[string]reconciliation.EffectState{"apply": form.observe, "destroy": form.removal} {
					runner := &fakeRunner{result: lifecycle.RunResult{Outcome: reconciliation.OutcomeUnchanged, Evidence: form.evidence}}
					capability := pinned.capable(runner)
					observe := capability.Observe
					if verb == "destroy" {
						observe = capability.ObserveRemoval
					}
					observation, err := observe(context.Background(), pinned.execution)
					if err != nil || observation.Effect != want || observation.Outcome != "" || !bytes.Equal(observation.Evidence, form.evidence) || len(observation.Produced) != 0 {
						t.Fatalf("%s resolution = %s (%v), want %s", verb, observation.Effect, err, want)
					}
					if len(runner.requests) != 1 || runner.requests[0].Operation != "observe" || runner.requests[0].Block != pinned.execution.Block.ID {
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
func TestEveryAgentInstallMutationRefusesWhatItCannotRead(t *testing.T) {
	unreachable := errors.New("unreachable")
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	for arm, pinned := range pinnedArms(t) {
		present := pinned.forms["present"].evidence
		malformed := pinned.execution
		malformed.Block.Request = json.RawMessage(`{`)
		unusableOutcome := described(openshiftOutcomeUnusable.Fail("the "+pinned.subject+" adapter reported no usable outcome", ""))
		if arm == "media" {
			unusableOutcome = described(openshiftOutcomeUnusable.Fail("the boot-media adapter reported no usable outcome", ""))
		}
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
			"runner error":     {context.Background(), &fakeRunner{err: unreachable}, false, pinned.execution, reconciliation.OutcomeUnknown, described(unreachable), 1, reconciliation.EffectUnknown},
			"unusable outcome": {context.Background(), &fakeRunner{result: lifecycle.RunResult{Outcome: "", Evidence: present}}, false, pinned.execution, reconciliation.OutcomeUnknown, unusableOutcome, 1, reconciliation.EffectCompleted},
			"other outcome":    {context.Background(), &fakeRunner{result: lifecycle.RunResult{Outcome: "skipped", Evidence: present}}, false, pinned.execution, reconciliation.OutcomeUnknown, unusableOutcome, 1, reconciliation.EffectCompleted},
			"no adapter":       {context.Background(), nil, true, pinned.execution, reconciliation.OutcomeFailed, described(openshiftAdapterUnconfigured.Fail("the "+pinned.subject+" adapter is not configured", "")), 0, reconciliation.EffectUnknown},
			"cancelled":        {cancelled, &fakeRunner{result: settled}, false, pinned.execution, reconciliation.OutcomeFailed, described(context.Canceled), 0, reconciliation.EffectUnknown},
			"malformed":        {context.Background(), &fakeRunner{result: settled}, false, malformed, reconciliation.OutcomeFailed, "", 0, reconciliation.EffectUnknown},
		} {
			t.Run(arm+"/"+name, func(t *testing.T) {
				for _, verb := range []string{"apply", "destroy"} {
					var capability lifecycle.Capability
					var runner *fakeRunner
					if test.missing {
						capability = pinned.capable(nil)
					} else {
						fresh := *test.runner
						runner = &fresh
						capability = pinned.capable(runner)
					}
					mutate := capability.Apply
					if verb == "destroy" {
						mutate = capability.Destroy
					}
					result, err := mutate(test.ctx, test.execution)
					if err == nil || result.Outcome != test.outcome || result.Evidence != nil || len(result.Produced) != 0 || test.refusal != "" && described(err) != test.refusal {
						t.Fatalf("%s = %+v (%s), want %s (%s)", verb, result, described(err), test.outcome, test.refusal)
					}
					if name == "runner error" && !errors.Is(err, unreachable) {
						t.Fatalf("the runner's error was replaced: %v", err)
					}
					if runner != nil && len(runner.requests) != test.runs {
						t.Fatalf("%s ran %d times, want %d", verb, len(runner.requests), test.runs)
					}
				}
				capability := pinned.capable(nil)
				if !test.missing {
					fresh := *test.runner
					capability = pinned.capable(&fresh)
				}
				observation, err := capability.Observe(test.ctx, test.execution)
				if (err != nil) != (test.effect == reconciliation.EffectUnknown) || observation.Effect != test.effect {
					t.Fatalf("observe = %s (%v), want %s", observation.Effect, err, test.effect)
				}
			})
		}
	}
}

// Whatever the installation's run hands back and its outcome does not prove
// is cleared on every exit, never dropped with its bytes intact.
func TestAnInstallationClearsWhatItProducedOnEveryExit(t *testing.T) {
	pinned := pinnedArms(t)["install"]
	present := pinned.forms["present"].evidence
	for name, test := range map[string]struct {
		outcome  reconciliation.Outcome
		evidence json.RawMessage
		err      error
		offers   bool
	}{
		"proved":           {"changed", present, nil, true},
		"runner error":     {"changed", present, errors.New("unreachable"), false},
		"unusable outcome": {"", present, nil, false},
		"unproved":         {"changed", pinned.forms["absent"].evidence, nil, false},
		"garbage evidence": {"changed", json.RawMessage(`{`), nil, false},
	} {
		t.Run(name, func(t *testing.T) {
			handed := []lifecycle.Produced{
				{Name: KubeconfigOutput, Material: secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: []byte("apiVersion: v1\n")})},
				{Name: "password", Material: secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: []byte("kubeadmin")})},
			}
			runner := &fakeRunner{result: lifecycle.RunResult{Outcome: test.outcome, Evidence: test.evidence, Produced: handed}, err: test.err}
			result, _ := NewInstall(runner).Apply(context.Background(), pinned.execution)
			if test.offers != (len(result.Produced) == 1 && result.Produced[0].Name == KubeconfigOutput) || !test.offers && len(result.Produced) != 0 {
				t.Fatalf("offered %v", result.Produced)
			}
			for index, output := range handed {
				if test.offers && index == 0 {
					continue
				}
				if value, _ := output.Material.Part(secrets.ValuePart); strings.Trim(string(value), "\x00") != "" {
					t.Fatalf("the discarded %s was not cleared", output.Name)
				}
			}
		})
	}
}

// An installation whose apply recorded a boot of its nodes reads an idle
// cluster as no effect only when its work area still carries the boot's mark.
func TestAnInstallationObservationReadsARecordedBootAsNoEffectOnlyWhenMarked(t *testing.T) {
	pinned := pinnedArms(t)["install"]
	idle := installEvidence(t, testDigest, func(e *InstallEvidence) {
		*e = InstallEvidence{Request: testDigest, Media: []string{}, Missing: []string{}, Powered: []string{}}
	})
	booted := installEvidence(t, testDigest, func(e *InstallEvidence) {
		*e = InstallEvidence{Booted: true, Request: testDigest, Media: []string{}, Missing: []string{}, Powered: []string{}}
	})
	recorded := pinned.execution
	recorded.ApplyPreparations = []json.RawMessage{json.RawMessage(`{}`)}
	for name, test := range map[string]struct {
		evidence json.RawMessage
		want     reconciliation.EffectState
	}{
		"unmarked": {idle, reconciliation.EffectUnknown},
		"marked":   {booted, reconciliation.EffectNoEffect},
	} {
		t.Run(name, func(t *testing.T) {
			runner := &fakeRunner{result: lifecycle.RunResult{Outcome: "unchanged", Evidence: test.evidence}}
			observation, err := NewInstall(runner).Observe(context.Background(), recorded)
			if err != nil || observation.Effect != test.want {
				t.Fatalf("observation = %s (%v), want %s", observation.Effect, err, test.want)
			}
		})
	}
}
