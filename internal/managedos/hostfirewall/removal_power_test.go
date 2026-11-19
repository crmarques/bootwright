package hostfirewall

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"testing"

	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

func removedEvidence(t *testing.T, block reconciliation.Block, state string) json.RawMessage {
	t.Helper()
	evidence := firewallServiceEvidence(t, block, state)
	evidence.Absent, evidence.Postcondition = true, true
	return mustJSON(t, evidence)
}

func TestAFirewallRemovalCarriesItsPreparationAndNamesItsEffect(t *testing.T) {
	block := firewallBlock(t)
	capability := New(&fakeRunner{})
	if !capability.CarryRemovalPreparation() {
		t.Fatal("the removal receipt is not carried")
	}
	removal, err := capability.Removal(context.Background(), block)
	if err != nil || removal.Description != "remove the host firewall services of "+firewallMachine || !slices.Equal(removal.Impacts, []string{"remove-host-firewall " + firewallMachine}) {
		t.Fatalf("removal = %+v, %v", removal, err)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := capability.Removal(canceled, block); !errors.Is(err, context.Canceled) {
		t.Fatalf("removal under a canceled context = %v", err)
	}
	broken := reconciliation.Block{}
	if _, err := capability.Removal(context.Background(), broken); err == nil {
		t.Fatal("a block without a request described a removal")
	}
	if _, err := capability.SecretReferences(broken); err == nil {
		t.Fatal("a block without a request named secrets")
	}
	if _, err := capability.SecretClasses(broken); err == nil {
		t.Fatal("a block without a request classified secrets")
	}
	if _, err := capability.HostMachines(broken); err == nil {
		t.Fatal("a block without a request named a host")
	}
	references, err := capability.SecretReferences(block)
	if err != nil || len(references) == 0 {
		t.Fatalf("references = %v, %v", references, err)
	}
	if _, err := capability.SecretClasses(block); err != nil {
		t.Fatal(err)
	}
}

func TestAFirewallRemovalNeedsARegisteredPowerTransitionAndStopsOnEachFailure(t *testing.T) {
	block := firewallBlock(t)
	runner := &fakeRunner{result: lifecycle.RunResult{Outcome: "changed", Evidence: removedEvidence(t, block, "absent")}}
	prepared := func(context.Context, json.RawMessage) error { return nil }
	powered := func(context.Context, string, bool) error { return nil }

	bare := firewallExecution(t, block)
	if result, err := New(runner).Destroy(context.Background(), bare); err == nil || result.Outcome != reconciliation.OutcomeFailed {
		t.Fatalf("a removal without power transitions = %+v, %v", result, err)
	}
	readOnly := firewallExecution(t, block)
	readOnly.PowerMachine, readOnly.PrepareRemoval, readOnly.ReadOnly = powered, prepared, true
	if result, err := New(runner).Destroy(context.Background(), readOnly); err == nil || result.Outcome != reconciliation.OutcomeFailed {
		t.Fatalf("a read-only removal = %+v, %v", result, err)
	}
	badReceipt := firewallExecution(t, block)
	badReceipt.PowerMachine, badReceipt.PrepareRemoval = powered, prepared
	badReceipt.RemovalPreparations = []json.RawMessage{removedEvidence(t, block, "written")}
	if result, err := New(runner).Destroy(context.Background(), badReceipt); err == nil || result.Outcome != reconciliation.OutcomeFailed {
		t.Fatalf("a receipt that leaves a service owned = %+v, %v", result, err)
	}
	if len(runner.requests) != 0 {
		t.Fatalf("a refused removal reached the guest: %d runs", len(runner.requests))
	}

	boom := errors.New("power refused")
	failAt := func(call int) lifecycle.Execution {
		execution := firewallExecution(t, block)
		seen := 0
		execution.PowerMachine = func(context.Context, string, bool) error {
			seen++
			if seen == call {
				return boom
			}
			return nil
		}
		execution.PrepareRemoval = prepared
		return execution
	}
	if result, err := New(runner).Destroy(context.Background(), failAt(1)); !errors.Is(err, boom) || result.Outcome != reconciliation.OutcomeUnknown || len(runner.requests) != 0 {
		t.Fatalf("a failed start = %+v, %v, runs %d", result, err, len(runner.requests))
	}
	if result, err := New(runner).Destroy(context.Background(), failAt(2)); !errors.Is(err, boom) || result.Outcome != reconciliation.OutcomeUnknown || len(runner.requests) != 1 {
		t.Fatalf("a failed stop = %+v, %v, runs %d", result, err, len(runner.requests))
	}
	unrecorded := failAt(0)
	unrecorded.PrepareRemoval = func(context.Context, json.RawMessage) error { return boom }
	if result, err := New(runner).Destroy(context.Background(), unrecorded); !errors.Is(err, boom) || result.Outcome != reconciliation.OutcomeUnknown {
		t.Fatalf("a receipt that was not recorded = %+v, %v", result, err)
	}
	notSettled := &fakeRunner{result: lifecycle.RunResult{Outcome: "failed"}}
	if result, err := New(notSettled).Destroy(context.Background(), failAt(0)); err == nil || result.Outcome.Settled() {
		t.Fatalf("an unproved guest removal settled: %+v, %v", result, err)
	}
}

func TestAFirewallRemovalObservationIsPartialUntilTheMachineStopsAgain(t *testing.T) {
	block := firewallBlock(t)
	ctx := context.Background()
	withStopped := func(stopped bool, err error) lifecycle.Execution {
		execution := firewallExecution(t, block)
		execution.MachineStopped = func(_ context.Context, machine string) (bool, error) {
			if machine != firewallMachine {
				t.Fatalf("stop probe targeted %s", machine)
			}
			return stopped, err
		}
		return execution
	}

	runner := &fakeRunner{}
	observation, err := New(runner).ObserveRemoval(ctx, withStopped(true, nil))
	var proof struct {
		Machine         string `json:"machine"`
		Request         string `json:"request"`
		StoppedOrAbsent bool   `json:"stoppedOrAbsent"`
	}
	if err != nil || observation.Effect != reconciliation.EffectPartial || len(runner.requests) != 0 ||
		json.Unmarshal(observation.Evidence, &proof) != nil || proof.Machine != firewallMachine || proof.Request != block.RequestDigest || !proof.StoppedOrAbsent {
		t.Fatalf("a stopped machine = %+v, %v, runs %d", observation, err, len(runner.requests))
	}

	boom := errors.New("probe failed")
	if observation, err := New(runner).ObserveRemoval(ctx, withStopped(false, boom)); !errors.Is(err, boom) || observation.Effect != reconciliation.EffectUnknown {
		t.Fatalf("a failed stop probe = %+v, %v", observation, err)
	}

	runner = &fakeRunner{result: lifecycle.RunResult{Outcome: "unchanged", Evidence: removedEvidence(t, block, "absent")}}
	if observation, err := New(runner).ObserveRemoval(ctx, withStopped(false, nil)); err != nil || observation.Effect != reconciliation.EffectPartial || runner.requests[0].Operation != "observe" {
		t.Fatalf("a running machine whose services are absent = %+v, %v", observation, err)
	}
	runner = &fakeRunner{result: lifecycle.RunResult{Outcome: "unchanged", Evidence: removedEvidence(t, block, "written")}}
	if observation, err := New(runner).ObserveRemoval(ctx, withStopped(false, nil)); err != nil || observation.Effect != reconciliation.EffectPartial {
		t.Fatalf("a running machine that still owns services = %+v, %v", observation, err)
	}
}

func TestAFirewallRemovalObservationReadsItsStoredReceiptAndRefusesCancellation(t *testing.T) {
	block := firewallBlock(t)
	ctx := context.Background()
	runner := &fakeRunner{}
	receipt := removedEvidence(t, block, "absent")

	execution := firewallExecution(t, block)
	execution.RemovalPreparations = []json.RawMessage{receipt}
	observation, err := New(runner).ObserveRemoval(ctx, execution)
	if err != nil || observation.Effect != reconciliation.EffectPartial || string(observation.Evidence) != string(receipt) || len(runner.requests) != 0 {
		t.Fatalf("a stored receipt = %+v, %v, runs %d", observation, err, len(runner.requests))
	}
	execution.RemovalPreparations = []json.RawMessage{removedEvidence(t, block, "written")}
	if observation, err := New(runner).ObserveRemoval(ctx, execution); err == nil || observation.Effect != reconciliation.EffectUnknown {
		t.Fatalf("a receipt that leaves a service owned = %+v, %v", observation, err)
	}

	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if observation, err := New(runner).ObserveRemoval(canceled, execution); !errors.Is(err, context.Canceled) || observation.Effect != reconciliation.EffectUnknown {
		t.Fatalf("an observation under a canceled context = %+v, %v", observation, err)
	}
	if result, err := New(runner).Destroy(canceled, execution); !errors.Is(err, context.Canceled) || result.Outcome != reconciliation.OutcomeCanceled {
		t.Fatalf("a destroy under a canceled context = %+v, %v", result, err)
	}
	if _, err := New(runner).Quiescent(canceled, lifecycle.Probe{Block: block}); !errors.Is(err, context.Canceled) {
		t.Fatalf("quiescence under a canceled context = %v", err)
	}
	if quiescence, err := New(runner).Quiescent(ctx, lifecycle.Probe{Block: block}); err != nil || !quiescence.Settled() {
		t.Fatalf("quiescence = %+v, %v", quiescence, err)
	}
	if len(runner.requests) != 0 {
		t.Fatalf("a stored receipt or a canceled context reached the guest: %d runs", len(runner.requests))
	}
}
