package libvirt

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

type pinnedEvidence struct {
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
	rows      map[string]pinnedEvidence
}

func libvirtArms(t *testing.T) map[string]pinnedArm {
	hosts, _ := HostRequests(labCatalog(), "controller", testContext)
	machines, _ := MachineRequests(labCatalog(), "controller", testContext)
	host, machine := hosts[0], machines[0]
	hostCanonical, _ := host.Canonical()
	machineCanonical, _ := machine.Canonical()
	frozen := func(id string, canonical []byte) lifecycle.Execution {
		return lifecycle.Execution{Block: reconciliation.Block{
			BlockDefinition: reconciliation.BlockDefinition{ID: id, Request: canonical}, RequestDigest: "digest",
		}}
	}
	garbage := json.RawMessage(`{`)
	hostAbsent := encode(t, HostEvidence{CarrierPolicy: CarrierPolicyEvidence{Answered: true}, Absent: true, ConnectionsAnswered: true, FirewallReady: FirewallReadyEvidence{Answered: true}, Directory: observed(false), PoolAnswered: true, Postcondition: true, Request: "digest", URI: true})
	hostForeign := encode(t, HostEvidence{CarrierPolicy: CarrierPolicyEvidence{Answered: true}, Absent: true, ConnectionsAnswered: true, FirewallReady: FirewallReadyEvidence{Answered: true}, Directory: observed(false), PoolAnswered: true, Postcondition: true, Request: "other", URI: true})
	hostPartial := encode(t, HostEvidence{Pool: "active", PoolAnswered: true, PoolOwned: true, Request: "digest", URI: true})
	hostDamaged := func(damage func(*HostEvidence)) json.RawMessage {
		var evidence HostEvidence
		if err := json.Unmarshal(hostEvidence(host, "digest"), &evidence); err != nil {
			t.Fatal(err)
		}
		damage(&evidence)
		evidence.Postcondition = publishedPostcondition(evidence)
		return encode(t, evidence)
	}
	hostUnremoved := hostDamaged(func(e *HostEvidence) { e.Networks[0].Definition = false })
	hostUnfinished := hostDamaged(func(e *HostEvidence) { e.Pool = "inactive" })
	hostPresent := hostEvidence(host, "digest")
	hostRow := func(evidence json.RawMessage, apply, destroy reconciliation.Outcome, observe, removal reconciliation.EffectState) pinnedEvidence {
		row := pinnedEvidence{evidence: evidence, apply: apply, destroy: destroy, observe: observe, removal: removal}
		if apply == reconciliation.OutcomeUnknown {
			row.applyRefusal = ValidateHostPresence(evidence, host, "digest")
		}
		if destroy == reconciliation.OutcomeUnknown {
			row.destroyRefusal = ValidateHostAbsence(evidence, "digest")
		}
		return row
	}
	machineAbsent := encode(t, MachineEvidence{Absent: true, Answered: true, Listener: observed(false), Postcondition: true, Request: "digest"})
	machineForeign := encode(t, MachineEvidence{Absent: true, Answered: true, Listener: observed(false), Postcondition: true, Request: "other"})
	machinePartial := encode(t, MachineEvidence{Domain: machine.Domain, Owned: true, Request: "digest"})
	machineDamaged := func(damage func(*MachineEvidence)) json.RawMessage {
		var evidence MachineEvidence
		if err := json.Unmarshal(machineEvidence(machine, "digest"), &evidence); err != nil {
			t.Fatal(err)
		}
		damage(&evidence)
		evidence.Postcondition = publishedMachinePostcondition(evidence)
		return encode(t, evidence)
	}
	machineUnremoved := machineDamaged(func(e *MachineEvidence) { e.Controller = "docker.io/other@sha256:0" })
	machineUnfinished := machineDamaged(func(e *MachineEvidence) { e.Unit = "inactive" })
	machinePresent := machineEvidence(machine, "digest")
	machineRow := func(evidence json.RawMessage, apply, destroy reconciliation.Outcome, observe, removal reconciliation.EffectState) pinnedEvidence {
		row := pinnedEvidence{evidence: evidence, apply: apply, destroy: destroy, observe: observe, removal: removal}
		if apply == reconciliation.OutcomeUnknown {
			row.applyRefusal = ValidateMachinePresence(evidence, machine, "digest")
		}
		if destroy == reconciliation.OutcomeUnknown {
			row.destroyRefusal = ValidateMachineAbsence(evidence, "digest")
		}
		return row
	}
	changed, unknown := reconciliation.OutcomeChanged, reconciliation.OutcomeUnknown
	return map[string]pinnedArm{
		"host": {
			execution: frozen("substrate-host-lab-libvirt", hostCanonical), subject: "provider host",
			capable: func(runner Runner) lifecycle.Capability { return NewHost(runner) },
			rows: map[string]pinnedEvidence{
				"present":    hostRow(hostPresent, changed, unknown, reconciliation.EffectCompleted, reconciliation.EffectNoEffect),
				"absent":     hostRow(hostAbsent, unknown, changed, reconciliation.EffectNoEffect, reconciliation.EffectCompleted),
				"partial":    hostRow(hostPartial, unknown, unknown, reconciliation.EffectPartial, reconciliation.EffectPartial),
				"unremoved":  hostRow(hostUnremoved, unknown, unknown, reconciliation.EffectPartial, reconciliation.EffectNoEffect),
				"unfinished": hostRow(hostUnfinished, unknown, unknown, reconciliation.EffectPartial, reconciliation.EffectPartial),
				"foreign":    hostRow(hostForeign, unknown, unknown, reconciliation.EffectUnknown, reconciliation.EffectUnknown),
				"garbage":    hostRow(garbage, unknown, unknown, reconciliation.EffectUnknown, reconciliation.EffectUnknown),
			},
		},
		"machine": {
			execution: frozen("machine-rhel-01", machineCanonical), subject: "machine",
			capable: func(runner Runner) lifecycle.Capability { return NewMachine(runner) },
			rows: map[string]pinnedEvidence{
				"present":    machineRow(machinePresent, changed, unknown, reconciliation.EffectCompleted, reconciliation.EffectNoEffect),
				"absent":     machineRow(machineAbsent, unknown, changed, reconciliation.EffectNoEffect, reconciliation.EffectCompleted),
				"partial":    machineRow(machinePartial, unknown, unknown, reconciliation.EffectPartial, reconciliation.EffectPartial),
				"unremoved":  machineRow(machineUnremoved, unknown, unknown, reconciliation.EffectUnknown, reconciliation.EffectNoEffect),
				"unfinished": machineRow(machineUnfinished, unknown, unknown, reconciliation.EffectPartial, reconciliation.EffectPartial),
				"foreign":    machineRow(machineForeign, unknown, unknown, reconciliation.EffectUnknown, reconciliation.EffectUnknown),
				"garbage":    machineRow(garbage, unknown, unknown, reconciliation.EffectUnknown, reconciliation.EffectUnknown),
			},
		},
	}
}

// Every mutation and resolution of both arms is pinned over each observation
// form, so moving them onto the shared lifecycle skeleton changes none.
func TestEveryLibvirtMutationAndResolutionReadsEachObservation(t *testing.T) {
	for arm, pinned := range libvirtArms(t) {
		for name, row := range pinned.rows {
			t.Run(arm+"/"+name, func(t *testing.T) {
				for verb, want := range map[string]struct {
					outcome reconciliation.Outcome
					refusal error
				}{"apply": {row.apply, row.applyRefusal}, "destroy": {row.destroy, row.destroyRefusal}} {
					runner := &fakeRunner{result: lifecycle.RunResult{Outcome: reconciliation.OutcomeChanged, Evidence: row.evidence}}
					capability := pinned.capable(runner)
					mutate := capability.Apply
					if verb == "destroy" {
						mutate = capability.Destroy
					}
					result, err := mutate(context.Background(), pinned.execution)
					wantEvidence := json.RawMessage(nil)
					if want.outcome != reconciliation.OutcomeUnknown {
						wantEvidence = row.evidence
					}
					if result.Outcome != want.outcome || !bytes.Equal(result.Evidence, wantEvidence) || described(err) != described(want.refusal) {
						t.Fatalf("%s = %+v (%s), want %s (%s)", verb, result, described(err), want.outcome, described(want.refusal))
					}
					if (want.outcome == reconciliation.OutcomeUnknown) != (err != nil) {
						t.Fatalf("%s refusal = %v", verb, err)
					}
					if len(runner.requests) != 1 || runner.requests[0].Operation != verb {
						t.Fatalf("%s ran %+v", verb, runner.requests)
					}
				}
				for verb, want := range map[string]reconciliation.EffectState{"apply": row.observe, "destroy": row.removal} {
					runner := &fakeRunner{result: lifecycle.RunResult{Outcome: reconciliation.OutcomeUnchanged, Evidence: row.evidence}}
					capability := pinned.capable(runner)
					observe := capability.Observe
					if verb == "destroy" {
						observe = capability.ObserveRemoval
					}
					observation, err := observe(context.Background(), pinned.execution)
					if err != nil || observation.Effect != want || observation.Outcome != "" || !bytes.Equal(observation.Evidence, row.evidence) {
						t.Fatalf("%s resolution = %+v (%v), want %s", verb, observation, err, want)
					}
					if len(runner.requests) != 1 || runner.requests[0].Operation != "observe" {
						t.Fatalf("%s resolution ran %+v", verb, runner.requests)
					}
				}
			})
		}
	}
}

// A run that fails, an outcome the adapter cannot settle, a missing adapter,
// an unreadable request and a cancelled call each refuse with their own
// outcome, before or instead of any evidence check.
func TestEveryLibvirtMutationRefusesWhatItCannotRead(t *testing.T) {
	unreachable := errors.New("unreachable")
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	for arm, pinned := range libvirtArms(t) {
		present := pinned.rows["present"].evidence
		malformed := pinned.execution
		malformed.Block.Request = json.RawMessage(`{`)
		for name, test := range map[string]struct {
			ctx       context.Context
			runner    Runner
			execution lifecycle.Execution
			outcome   reconciliation.Outcome
			refusal   string
			effect    reconciliation.EffectState
		}{
			"runner error":     {context.Background(), &fakeRunner{err: unreachable}, pinned.execution, reconciliation.OutcomeUnknown, described(unreachable), reconciliation.EffectUnknown},
			"unusable outcome": {context.Background(), &fakeRunner{result: lifecycle.RunResult{Outcome: "", Evidence: present}}, pinned.execution, reconciliation.OutcomeUnknown, described(refusal("lifecycle.state", "the "+pinned.subject+" adapter reported no usable outcome", "")), reconciliation.EffectCompleted},
			"other outcome":    {context.Background(), &fakeRunner{result: lifecycle.RunResult{Outcome: "skipped", Evidence: present}}, pinned.execution, reconciliation.OutcomeUnknown, described(refusal("lifecycle.state", "the "+pinned.subject+" adapter reported no usable outcome", "")), reconciliation.EffectCompleted},
			"no adapter":       {context.Background(), nil, pinned.execution, reconciliation.OutcomeFailed, described(refusal("lifecycle.state", "the "+pinned.subject+" adapter is not configured", "")), reconciliation.EffectUnknown},
			"cancelled":        {cancelled, &fakeRunner{result: lifecycle.RunResult{Outcome: "changed", Evidence: present}}, pinned.execution, reconciliation.OutcomeFailed, described(context.Canceled), reconciliation.EffectUnknown},
			"malformed":        {context.Background(), &fakeRunner{result: lifecycle.RunResult{Outcome: "changed", Evidence: present}}, malformed, reconciliation.OutcomeFailed, "malformed", reconciliation.EffectUnknown},
		} {
			t.Run(arm+"/"+name, func(t *testing.T) {
				capability := pinned.capable(test.runner)
				result, err := capability.Apply(test.ctx, test.execution)
				if err == nil || result.Outcome != test.outcome || result.Evidence != nil || test.refusal != "malformed" && described(err) != test.refusal {
					t.Fatalf("apply = %+v (%s), want %s (%s)", result, described(err), test.outcome, test.refusal)
				}
				if name == "runner error" && !errors.Is(err, unreachable) {
					t.Fatalf("the runner's error was replaced: %v", err)
				}
				observation, err := capability.Observe(test.ctx, test.execution)
				if (err != nil) != (test.effect == reconciliation.EffectUnknown) || observation.Effect != test.effect {
					t.Fatalf("observe = %+v (%v), want %s", observation, err, test.effect)
				}
			})
		}
	}
}
