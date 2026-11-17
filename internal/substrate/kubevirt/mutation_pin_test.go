package kubevirt

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
	evidence                         json.RawMessage
	apply, destroy                   reconciliation.Outcome
	applyRefusal, destroyRefusal     error
	observe, removal                 reconciliation.EffectState
	observedOutcome, removalObserved reconciliation.Outcome
}

func machineExecution(t *testing.T) lifecycle.Execution {
	return lifecycle.Execution{Block: machineBlock(t), ApplyOperation: "op-1", ApplyRegistered: "2026-10-28T10:00:00Z", Context: testContext,
		ApplyProved: []lifecycle.BlockEvidence{provedHostEvidence(t, hostBlock(t), "cluster-1")}}
}

func machineRows(t *testing.T) map[string]pinnedEvidence {
	request, digest := machineRequest(t), strings.Repeat("b", 64)
	absent := func(e *MachineEvidence) {
		e.Absent, e.Owned, e.VirtualMachine, e.Volumes = true, false, "", []VolumeEvidence{{Kind: "DataVolume", Name: request.RootVolume}, {Kind: "DataVolume", Name: request.MediaVolume}}
	}
	foreignPin := refusal("lifecycle.state", "the KubeVirt machine evidence names another cluster than its provider's proof", "")
	row := func(evidence json.RawMessage, apply, destroy reconciliation.Outcome, observe, removal reconciliation.EffectState) pinnedEvidence {
		pinned := pinnedEvidence{evidence: evidence, apply: apply, destroy: destroy, observe: observe, removal: removal}
		if apply == reconciliation.OutcomeUnknown {
			pinned.applyRefusal = ValidateMachinePresence(evidence, request, digest)
		}
		if destroy == reconciliation.OutcomeUnknown {
			pinned.destroyRefusal = ValidateMachineAbsence(evidence, digest)
		}
		if observe == reconciliation.EffectCompleted {
			pinned.observedOutcome = reconciliation.OutcomeUnchanged
		}
		return pinned
	}
	changed, unknown := reconciliation.OutcomeChanged, reconciliation.OutcomeUnknown
	otherCluster := row(machineFixture(t, digest, func(e *MachineEvidence) { e.KubeSystemUID = "cluster-2" }), unknown, unknown, reconciliation.EffectCompleted, reconciliation.EffectNoEffect)
	otherCluster.applyRefusal = foreignPin
	otherAbsent := row(machineFixture(t, digest, func(e *MachineEvidence) { absent(e); e.KubeSystemUID = "cluster-2" }), unknown, unknown, reconciliation.EffectNoEffect, reconciliation.EffectCompleted)
	otherAbsent.destroyRefusal = foreignPin
	return map[string]pinnedEvidence{
		"present":   row(machineFixture(t, digest, nil), changed, unknown, reconciliation.EffectCompleted, reconciliation.EffectNoEffect),
		"absent":    row(machineFixture(t, digest, absent), unknown, changed, reconciliation.EffectNoEffect, reconciliation.EffectCompleted),
		"partial":   row(machineFixture(t, digest, func(e *MachineEvidence) { e.Postcondition, e.Volumes[0].Present = false, false }), unknown, unknown, reconciliation.EffectPartial, reconciliation.EffectPartial),
		"unremoved": row(machineFixture(t, digest, func(e *MachineEvidence) { e.Receipts = nil }), unknown, unknown, reconciliation.EffectUnknown, reconciliation.EffectNoEffect),
		"unfinished": row(machineFixture(t, digest, func(e *MachineEvidence) {
			absent(e)
			e.Absent, e.Postcondition = false, false
			e.Volumes = append(e.Volumes, VolumeEvidence{Kind: "PersistentVolumeClaim", Name: request.RootVolume, Present: true})
		}), unknown, unknown, reconciliation.EffectPartial, reconciliation.EffectPartial),
		"foreign":       row(machineFixture(t, digest, func(e *MachineEvidence) { e.Owned, e.Foreign, e.Postcondition = false, true, false }), unknown, unknown, reconciliation.EffectUnknown, reconciliation.EffectUnknown),
		"another":       row(machineFixture(t, strings.Repeat("c", 64), nil), unknown, unknown, reconciliation.EffectUnknown, reconciliation.EffectUnknown),
		"garbage":       row(json.RawMessage(`{`), unknown, unknown, reconciliation.EffectUnknown, reconciliation.EffectUnknown),
		"other cluster": otherCluster,
		"other absent":  otherAbsent,
	}
}

// Every mutation and resolution of the machine arm is pinned over each
// observation form, so moving it onto the shared lifecycle skeleton changes
// none.
func TestEveryKubeVirtMachineMutationAndResolutionReadsEachObservation(t *testing.T) {
	for name, row := range machineRows(t) {
		t.Run(name, func(t *testing.T) {
			for verb, want := range map[string]struct {
				outcome reconciliation.Outcome
				refusal error
			}{"apply": {row.apply, row.applyRefusal}, "destroy": {row.destroy, row.destroyRefusal}} {
				run := &runner{result: lifecycle.RunResult{Outcome: reconciliation.OutcomeChanged, Evidence: row.evidence}}
				mutate := NewMachine(run).Apply
				if verb == "destroy" {
					mutate = NewMachine(run).Destroy
				}
				result, err := mutate(context.Background(), machineExecution(t))
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
				if len(run.requests) != 1 || run.requests[0].Operation != verb {
					t.Fatalf("%s ran %+v", verb, run.requests)
				}
			}
			for verb, want := range map[string]struct {
				effect  reconciliation.EffectState
				outcome reconciliation.Outcome
			}{"apply": {row.observe, row.observedOutcome}, "destroy": {row.removal, row.removalObserved}} {
				run := &runner{result: lifecycle.RunResult{Outcome: reconciliation.OutcomeChanged, Evidence: row.evidence}}
				observe := NewMachine(run).Observe
				if verb == "destroy" {
					observe = NewMachine(run).ObserveRemoval
				}
				observation, err := observe(context.Background(), machineExecution(t))
				if err != nil || observation.Effect != want.effect || observation.Outcome != want.outcome || !bytes.Equal(observation.Evidence, row.evidence) {
					t.Fatalf("%s resolution = %+v (%v), want %s %q", verb, observation, err, want.effect, want.outcome)
				}
				if len(run.requests) != 1 || run.requests[0].Operation != "observe" {
					t.Fatalf("%s resolution ran %+v", verb, run.requests)
				}
			}
		})
	}
}

// The proof's mutations are pinned over each form of its evidence, and its
// removal never lends the host's access.
func TestEveryKubeVirtHostMutationReadsEachProof(t *testing.T) {
	block, request := hostBlock(t), hostRequest(t)
	held := provedHostEvidence(t, block, "cluster-1").Evidence
	var failing HostEvidence
	if err := json.Unmarshal(held, &failing); err != nil {
		t.Fatal(err)
	}
	failing.Failures = []string{"the storage class guest-block offers no ReadWriteMany claim"}
	released := encoded(t, HostEvidence{Absent: true, Failures: []string{}, Networks: []NetworkEvidence{}, Postcondition: true, Request: block.RequestDigest})
	changed, unknown := reconciliation.OutcomeChanged, reconciliation.OutcomeUnknown
	for name, test := range map[string]struct {
		evidence       json.RawMessage
		apply, destroy reconciliation.Outcome
		observe        reconciliation.EffectState
	}{
		"held":     {held, changed, unknown, reconciliation.EffectCompleted},
		"released": {released, unknown, changed, reconciliation.EffectUnknown},
		"failing":  {encoded(t, failing), unknown, unknown, reconciliation.EffectUnknown},
		"garbage":  {json.RawMessage(`{`), unknown, unknown, reconciliation.EffectUnknown},
	} {
		t.Run(name, func(t *testing.T) {
			for verb, want := range map[string]struct {
				outcome reconciliation.Outcome
				refusal error
			}{
				"apply":   {test.apply, ValidateHostPresence(test.evidence, request, block.RequestDigest)},
				"destroy": {test.destroy, ValidateHostAbsence(test.evidence, block.RequestDigest)},
			} {
				run := &runner{result: lifecycle.RunResult{Outcome: reconciliation.OutcomeChanged, Evidence: test.evidence}}
				mutate := NewHost(run, nil).Apply
				if verb == "destroy" {
					mutate = NewHost(run, nil).Destroy
				}
				result, err := mutate(context.Background(), lifecycle.Execution{Block: block, Context: testContext})
				wantEvidence := json.RawMessage(nil)
				if want.outcome != reconciliation.OutcomeUnknown {
					wantEvidence = test.evidence
				}
				if result.Outcome != want.outcome || !bytes.Equal(result.Evidence, wantEvidence) || described(err) != described(want.refusal) {
					t.Fatalf("%s = %+v (%s), want %s (%s)", verb, result, described(err), want.outcome, described(want.refusal))
				}
				if len(run.requests) != 1 || run.requests[0].Operation != verb || verb == "destroy" && len(run.requests[0].Materials) != 0 {
					t.Fatalf("%s ran %+v", verb, run.requests)
				}
			}
			run := &runner{result: lifecycle.RunResult{Outcome: reconciliation.OutcomeChanged, Evidence: test.evidence}}
			observation, err := NewHost(run, nil).Observe(context.Background(), lifecycle.Execution{Block: block, Context: testContext})
			wantOutcome := reconciliation.Outcome("")
			if test.observe == reconciliation.EffectCompleted {
				wantOutcome = reconciliation.OutcomeUnchanged
			}
			if err != nil || observation.Effect != test.observe || observation.Outcome != wantOutcome || !bytes.Equal(observation.Evidence, test.evidence) {
				t.Fatalf("observation = %+v (%v), want %s", observation, err, test.observe)
			}
		})
	}
}

// A run that fails, an outcome the adapter cannot settle, a missing adapter
// and a cancelled call each refuse with their own outcome before any evidence
// check, in both arms.
func TestEveryKubeVirtMutationRefusesWhatItCannotRead(t *testing.T) {
	unreachable := errors.New("unreachable")
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	held := provedHostEvidence(t, hostBlock(t), "cluster-1").Evidence
	present := machineFixture(t, machineBlock(t).RequestDigest, nil)
	for name, test := range map[string]struct {
		ctx     context.Context
		runner  *runner
		nilRun  bool
		outcome reconciliation.Outcome
		host    string
		machine string
	}{
		"runner error": {context.Background(), &runner{err: unreachable}, false, reconciliation.OutcomeUnknown, described(unreachable), described(unreachable)},
		"unusable outcome": {context.Background(), &runner{result: lifecycle.RunResult{Outcome: "skipped"}}, false, reconciliation.OutcomeUnknown,
			described(refusal("lifecycle.state", "the KubeVirt provider adapter reported no usable outcome", "")),
			described(refusal("lifecycle.state", "the KubeVirt machine adapter reported no usable outcome", ""))},
		"no adapter": {context.Background(), nil, true, reconciliation.OutcomeFailed,
			described(refusal("lifecycle.state", "the KubeVirt provider adapter is not configured", "")),
			described(refusal("lifecycle.state", "the KubeVirt machine adapter is not configured", ""))},
		"cancelled": {cancelled, &runner{}, false, reconciliation.OutcomeFailed, described(context.Canceled), described(context.Canceled)},
	} {
		t.Run(name, func(t *testing.T) {
			for verb := range map[string]bool{"apply": true, "destroy": true} {
				var hostRunner, machineRunner Runner
				if !test.nilRun {
					hostRunner = &runner{result: lifecycle.RunResult{Outcome: test.runner.result.Outcome, Evidence: held}, err: test.runner.err}
					machineRunner = &runner{result: lifecycle.RunResult{Outcome: test.runner.result.Outcome, Evidence: present}, err: test.runner.err}
				}
				host, machine := NewHost(hostRunner, nil), NewMachine(machineRunner)
				hostMutate, machineMutate := host.Apply, machine.Apply
				if verb == "destroy" {
					hostMutate, machineMutate = host.Destroy, machine.Destroy
				}
				result, err := hostMutate(test.ctx, lifecycle.Execution{Block: hostBlock(t), Context: testContext})
				if result.Outcome != test.outcome || result.Evidence != nil || described(err) != test.host {
					t.Fatalf("host %s = %+v (%s), want %s (%s)", verb, result, described(err), test.outcome, test.host)
				}
				result, err = machineMutate(test.ctx, machineExecution(t))
				if result.Outcome != test.outcome || result.Evidence != nil || described(err) != test.machine {
					t.Fatalf("machine %s = %+v (%s), want %s (%s)", verb, result, described(err), test.outcome, test.machine)
				}
			}
		})
	}
}
