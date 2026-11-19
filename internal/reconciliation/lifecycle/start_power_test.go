package lifecycle

import (
	"context"
	"errors"
	"maps"
	"path"
	"reflect"
	"slices"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
)

func (w *testWorkspace) AdmitLifecycle(ctx context.Context, name string, check func(View) error, write func(Transaction) error) error {
	if err := w.ReadLifecycle(ctx, name, check); err != nil {
		return err
	}
	return w.MutateLifecycle(ctx, name, write)
}

func (c *testCapability) StartPowerSecrets(block reconciliation.Block) ([]string, error) {
	return nil, nil
}

func (c *testCapability) StartPower(ctx context.Context, probe Probe) (Quiescence, error) {
	return Quiescence{State: Quiescent}, ctx.Err()
}

type powerMachine struct {
	*testCapability
	readings []Probe
	state    string
}

func (m *powerMachine) StartPower(_ context.Context, probe Probe) (Quiescence, error) {
	m.readings = append(m.readings, probe)
	return Quiescence{State: m.state, Reason: "native state"}, nil
}

func TestFreshMachineAdmissionLeavesNoStateOnRefusalAndRecoveryKeepsItsIdentity(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := newHarness(t)
	bindings := claimResolver{}.Bindings()
	machine := &powerMachine{testCapability: claimingCapability("machine-first-v1", "node-a"), state: Live}
	h.service.capabilities = claimResolver{
		bindings[0]: machine, bindings[1]: claimingCapability("machine-second-v1"), bindings[2]: claimingCapability("os-install-v1"),
	}
	apply := func() (*OperationResult, error) {
		return h.service.Apply(ctx, ApplyRequest{ContextName: "lab", SkipConfirmation: true})
	}
	destroy := func() (*OperationResult, error) {
		return h.service.Destroy(ctx, DestroyRequest{ContextName: "lab", SkipConfirmation: true})
	}
	refused := func(run func() (*OperationResult, error)) {
		t.Helper()
		mutations := h.workspace.mutations
		files := maps.Clone(h.workspace.area.files)
		logs := maps.Clone(h.workspace.logArea.files)
		evidence := slices.Clone(h.workspace.evidence)
		result, err := run()
		if ds := diagnostics.Of(err); result != nil || len(ds) != 1 || ds[0].Code != "lifecycle.start-power" {
			t.Fatalf("result=%+v error=%v", result, err)
		}
		if mutations != h.workspace.mutations || !reflect.DeepEqual(files, h.workspace.area.files) || !reflect.DeepEqual(logs, h.workspace.logArea.files) || !slices.Equal(evidence, h.workspace.evidence) {
			t.Fatal("a refused start changed state")
		}
	}
	refused(apply)
	machine.state = Unproved
	refused(apply)
	if len(machine.applies) != 0 || len(machine.readings) != 2 {
		t.Fatalf("attempts=%v readings=%d", machine.applies, len(machine.readings))
	}
	machine.state = Quiescent
	done, err := apply()
	if err != nil {
		t.Fatal(err)
	}
	machine.state = Live
	refused(destroy)
	last := machine.readings[len(machine.readings)-1]
	if last.ApplyOperation != done.Receipt.Operation {
		t.Fatalf("destroy probed owner %q; want %q", last.ApplyOperation, done.Receipt.Operation)
	}
	if len(machine.destroys) != 0 {
		t.Fatal("destroy ran an inverse before admission")
	}
	if _, err := apply(); err != nil || len(machine.readings) != 4 {
		t.Fatalf("settled apply read power or refused: %v", err)
	}
}

func TestPowerAdmissionGuardsNewRemovalOverPartialApplyAndExemptsRegisteredRecovery(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := newHarness(t)
	bindings := claimResolver{}.Bindings()
	machine := &powerMachine{testCapability: claimingCapability("machine-first-v1", "node-a"), state: Quiescent}
	h.service.capabilities = claimResolver{
		bindings[0]: machine, bindings[1]: claimingCapability("machine-second-v1"), bindings[2]: claimingCapability("os-install-v1"),
	}
	machine.applyErr = errors.New("interrupted")
	_, err := h.service.Apply(ctx, ApplyRequest{SkipConfirmation: true})
	if err == nil {
		t.Fatal("injected apply failure did not stop")
	}
	first := currentOperation(t, h)
	machine.applyErr = nil
	machine.state = Live
	if _, err := h.service.Destroy(ctx, DestroyRequest{SkipConfirmation: true}); err == nil || len(machine.readings) != 2 || currentOperation(t, h) != first {
		t.Fatalf("new destroy over a partial apply: %v", err)
	}
	resumed, err := h.service.Apply(ctx, ApplyRequest{SkipConfirmation: true})
	if err != nil || resumed.Receipt.Operation != first || len(machine.readings) != 2 {
		t.Fatalf("apply did not continue on a running machine: %+v %v", resumed, err)
	}
	rewriteState(t, h, path.Join(first, "operation.json"), string(reconciliation.OperationDone))
	h.workspace.evidence = []byte(`{"operation":"pending","ownership":"retained","version":1}`)
	before := slices.Clone(h.workspace.evidence)
	mutations := h.workspace.mutations
	_, err = h.service.Destroy(ctx, DestroyRequest{SkipConfirmation: true})
	if ds := diagnostics.Of(err); len(ds) != 1 || ds[0].Code != "lifecycle.start-power" || !slices.Equal(before, h.workspace.evidence) || mutations != h.workspace.mutations {
		t.Fatalf("dirty finalization changed before refusal: %v", err)
	}
	machine.state = Quiescent
	prior, err := h.service.Destroy(ctx, DestroyRequest{SkipConfirmation: true})
	if err != nil || prior.Recovered != RecoveredBeforeStart || prior.Receipt.Next != "destroy" || prior.Verb != "apply" || currentOperation(t, h) != first || len(machine.destroys) != 0 {
		t.Fatalf("guarded finalization started the next flow: %+v, %v", prior, err)
	}
	machine.state = Live
	if _, err := h.service.Destroy(ctx, DestroyRequest{SkipConfirmation: true}); err == nil || len(machine.destroys) != 0 {
		t.Fatal("fresh destroy skipped admission after recovery")
	}
}

func TestReplacementDestroyStopsAProvedLiveMachineInsideARegisteredAttempt(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	bindings := claimResolver{}.Bindings()
	m := &powerMachine{testCapability: claimingCapability("machine-first-v1", "node-a"), state: Quiescent}
	h.service.capabilities = claimResolver{bindings[0]: m, bindings[1]: claimingCapability("machine-second-v1"), bindings[2]: claimingCapability("os-install-v1")}
	completeApply(t, h)
	m.outcomes = []Result{{Outcome: reconciliation.OutcomeFailed}}
	if err := destroyWith(h); err == nil {
		t.Fatal("destroy should have been interrupted")
	}
	first, _ := durableOperation(t, h)
	m.state = Live
	stops := 0
	h.service.options.DriveMachine = func(_ context.Context, e Execution, proof BlockEvidence, on bool) error {
		if on || ReadOnlyRun(e) || e.Operation == first.ID || proof.Object != "node-a" || proof.State != reconciliation.BlockDone || len(proof.Evidence) == 0 {
			t.Fatalf("invalid stop: %+v, %+v", e, proof)
		}
		stops++
		m.state = Quiescent
		return nil
	}
	if err := destroyWith(h); err != nil || stops != 1 {
		t.Fatalf("replacement issued %d registered stops: %v", stops, err)
	}
}
